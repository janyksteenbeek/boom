package audio

import "math"

// Stream fills the output buffer with audio samples.
// Called ONLY from the malgo audio callback thread.
// Zero allocations, zero I/O, zero locks — just array reads and math.
// All mutable state (pos, fpos, fade, EQ delay) is only modified here.
//
// During a streaming decode the audio thread may see pLen grow over
// successive calls; we always snapshot Len() once per Stream() call so the
// bounds are stable for the duration of this block.
func (d *Deck) Stream(samples [][2]float32) {
	p := d.pcm.Load()
	if p == nil {
		for i := range samples {
			samples[i] = [2]float32{}
		}
		return
	}
	pLen := p.Len()
	pTotal := p.Total()

	d.applyPendingCommands(p, pTotal)

	// Snapshot jog state. While scratching (vinyl mode + top touch), the
	// platter velocity replaces the tempo and the deck must run even if
	// !playing — SetJogTouch() forces playing=true on touch-down for this.
	vinylMode := d.vinylMode.Load()
	touched := d.jogTouched.Load()
	scratching := vinylMode && touched
	scratchVel := d.loadFloat(&d.jogScratchVel)
	pitchOff := d.loadFloat(&d.jogPitchOffset)

	// Early-return when not playing AND not scratching. Skipping the early
	// return while scratching keeps audio flowing during a scratch from a
	// paused deck.
	if !scratching && !d.playing.Load() && d.fade.State() == FadeSilent {
		for i := range samples {
			samples[i] = [2]float32{}
		}
		return
	}

	var tempo float64
	if scratching {
		tempo = scratchVel
	} else {
		tempo = d.loadFloat(&d.tempoBits) + pitchOff
	}
	n := len(samples)

	loopActive, loopStartSamples, loopEndSamples := d.snapshotLoopBounds(pTotal, scratching)

	eotReached := false
	if tempo == 1.0 {
		eotReached = d.fillUnitTempo(samples, p, pLen, loopActive, loopStartSamples, loopEndSamples)
	} else {
		d.fillTempoInterpolated(samples, p, pLen, tempo, loopActive, loopStartSamples, loopEndSamples)
	}

	if d.fpos < 0 {
		d.pos = 0
	} else {
		d.pos = int(d.fpos)
	}
	// Only flip playing→false on EoT when not scratching, so scratching
	// off the end of the track doesn't kill the deck. Also require that
	// the decoded length has reached the expected total — otherwise we'd
	// stop at every temporary underrun during streaming decode.
	fullyDecoded := pLen >= pTotal
	if !scratching && !loopActive && fullyDecoded && (eotReached || d.pos >= pLen) {
		d.playing.Store(false)
		d.fade = NewFadeEnvelope(d.sampleRate, 0.01)
	}

	// Apply EQ in-place (lock-free biquad filters)
	d.eq.ProcessBuffer(samples, n)

	// Apply Beat FX in-place (lock-free, zero-alloc)
	d.beatFX.ProcessBuffer(samples, n)

	// Apply gain and volume in a single pass (biquad filters are linear, so
	// gain*EQ(x) == EQ(gain*x) — safe to combine post-EQ).
	for i := 0; i < n; i++ {
		gv := d.gain.Tick() * d.volume.Tick()
		samples[i][0] *= gv
		samples[i][1] *= gv
	}

	// Apply fade envelope for click-free start/stop
	state := d.fade.Process(samples)
	if state == FadeSilent && !d.playing.Load() {
		// Fade-out complete — already paused
	} else if state == FadeSilent {
		d.playing.Store(false)
	}

	d.updatePeakLevel(samples)

	// Update position snapshot for UI (atomic, read by Position()). Use
	// the expected total so the playhead doesn't jump while more samples
	// stream in.
	if pTotal > 0 {
		d.storeFloat(&d.posSnapshot, float64(d.pos)/float64(pTotal))
	}

	d.decayJogVelocities(scratching, scratchVel, pitchOff, n)
}

// applyPendingCommands handles reset/fade/seek commands posted from non-audio
// threads. Must run before any audio processing so subsequent steps see the
// new state.
func (d *Deck) applyPendingCommands(p *pcmBuffer, pTotal int) {
	// 1. Reset (LoadTrack completed) — must be first so fade/seek apply to clean state
	if d.pendingReset.CompareAndSwap(true, false) {
		d.pos = 0
		d.fpos = 0
		d.fade = NewFadeEnvelope(d.sampleRate, 0.01)
		d.eq.Reset()
		d.beatFX.Reset()
	}

	// 2. Fade command (Play/Pause)
	if cmd := d.pendingFade.Swap(0); cmd != 0 {
		switch cmd {
		case 1:
			d.fade.TriggerFadeIn()
		case 2:
			d.fade.TriggerFadeOut()
		}
	}

	// 3. Seek (CAS to avoid clearing a newer seek that arrived between load and swap)
	if p == nil || pTotal == 0 {
		return
	}
	seekBits := d.pendingSeek.Load()
	seekVal := math.Float64frombits(seekBits)
	if math.IsNaN(seekVal) {
		return
	}
	if !d.pendingSeek.CompareAndSwap(seekBits, math.Float64bits(math.NaN())) {
		return
	}
	// Seek targets are normalized against the expected total — that's what
	// the UI and cue-point storage use. Clamp to what's actually decoded so
	// the audio thread never reads past the current buffer tail.
	newPos := int(seekVal * float64(pTotal))
	if newPos < 0 {
		newPos = 0
	}
	pLen := p.Len()
	if pLen > 0 && newPos >= pLen {
		newPos = pLen - 1
	}
	d.pos = newPos
	d.fpos = float64(newPos)
}

// snapshotLoopBounds reads the loop atomics once per Stream() call and turns
// the normalized 0..1 boundaries into absolute sample indices. Loops are
// suppressed while scratching: the user is manually positioning the platter.
// Loop positions are normalized against the expected total track length so
// they stay anchored to the same audio position as decoding progresses.
func (d *Deck) snapshotLoopBounds(pTotal int, scratching bool) (active bool, startSamples, endSamples float64) {
	active = d.loopActive.Load() && !scratching
	if !active {
		return false, 0, 0
	}
	ls := d.loadFloat(&d.loopStart)
	le := d.loadFloat(&d.loopEnd)
	if math.IsNaN(ls) || math.IsNaN(le) || le <= ls {
		return false, 0, 0
	}
	startSamples = ls * float64(pTotal)
	endSamples = le * float64(pTotal)
	if endSamples > float64(pTotal) {
		endSamples = float64(pTotal)
	}
	if endSamples <= startSamples {
		return false, 0, 0
	}
	return true, startSamples, endSamples
}

// fillUnitTempo is the fast path: tempo == 1.0, direct buffer copy. When a
// loop is active the copy is split at the loop end so wrapping is sample-
// accurate even if the loop is shorter than one block. pLen is the count
// of samples actually decoded — reading beyond it is not safe.
func (d *Deck) fillUnitTempo(samples [][2]float32, p *pcmBuffer, pLen int, loopActive bool, loopStartSamples, loopEndSamples float64) (eot bool) {
	n := len(samples)
	i := 0
	for i < n {
		if loopActive && d.fpos >= loopEndSamples {
			d.fpos = loopStartSamples + (d.fpos - loopEndSamples)
			if d.fpos < loopStartSamples {
				d.fpos = loopStartSamples
			}
		}
		startIdx := int(d.fpos)
		if startIdx >= pLen {
			for j := i; j < n; j++ {
				samples[j] = [2]float32{}
			}
			return true
		}
		capIdx := pLen
		if loopActive {
			if le := int(loopEndSamples); le < capIdx {
				capIdx = le
			}
		}
		remaining := capIdx - startIdx
		if remaining <= 0 {
			if loopActive {
				d.fpos = loopStartSamples
				continue
			}
			for j := i; j < n; j++ {
				samples[j] = [2]float32{}
			}
			return true
		}
		count := n - i
		if count > remaining {
			count = remaining
		}
		copy(samples[i:i+count], p.samples[startIdx:startIdx+count])
		d.fpos += float64(count)
		i += count
	}
	return false
}

// fillTempoInterpolated is the variable-tempo path: linear interpolation with
// a float64 accumulator. Handles both forward (tempo > 0, including pitch
// bend) and reverse (tempo < 0, only reachable while scratching). The loop
// wrap is checked once per output sample, which is trivial compared to the
// interpolation itself.
func (d *Deck) fillTempoInterpolated(samples [][2]float32, p *pcmBuffer, pLen int, tempo float64, loopActive bool, loopStartSamples, loopEndSamples float64) {
	n := len(samples)
	for i := 0; i < n; i++ {
		if loopActive && d.fpos >= loopEndSamples {
			d.fpos = loopStartSamples + (d.fpos - loopEndSamples)
			if d.fpos < loopStartSamples {
				d.fpos = loopStartSamples
			}
		}
		idx := int(math.Floor(d.fpos))
		if idx < 0 || idx >= pLen-1 {
			samples[i] = [2]float32{}
		} else {
			frac := float32(d.fpos - float64(idx))
			samples[i][0] = p.samples[idx][0]*(1-frac) + p.samples[idx+1][0]*frac
			samples[i][1] = p.samples[idx][1]*(1-frac) + p.samples[idx+1][1]*frac
		}
		d.fpos += tempo
		if d.fpos < 0 {
			d.fpos = 0
		}
	}
}

// peakDecayPerBlock controls how fast the deck's peak meter falls between
// audio blocks once the input quiets. ~0.95 gives a ~80 ms half-life at
// 44.1 kHz / 256-sample blocks, which feels responsive without strobing.
const peakDecayPerBlock = 0.95

// updatePeakLevel scans the post-gain output buffer for the largest
// absolute sample and folds it into the smoothed peakBits atomic the UI
// and LED loops poll. Cheap (one pass, no allocs) and safe to leave on the
// audio thread.
func (d *Deck) updatePeakLevel(samples [][2]float32) {
	var blockPeak float32
	for i := range samples {
		l := samples[i][0]
		if l < 0 {
			l = -l
		}
		r := samples[i][1]
		if r < 0 {
			r = -r
		}
		if l > blockPeak {
			blockPeak = l
		}
		if r > blockPeak {
			blockPeak = r
		}
	}
	prev := math.Float64frombits(d.peakBits.Load())
	next := float64(blockPeak)
	if decayed := prev * peakDecayPerBlock; decayed > next {
		next = decayed
	}
	if next > 1 {
		next = 1
	}
	d.peakBits.Store(math.Float64bits(next))
}

// decayJogVelocities applies the per-block exponential decay to scratch
// velocity and pitch offset. CAS so concurrent AddJog* calls between snapshot
// and store don't get clobbered.
func (d *Deck) decayJogVelocities(scratching bool, scratchVel, pitchOff float64, blockFrames int) {
	blockSecsMs := 1000.0 * float64(blockFrames) / float64(d.sampleRate)
	if scratching && scratchVel != 0 {
		factor := math.Pow(0.5, blockSecsMs/jogScratchHalflifeMs)
		for {
			oldBits := d.jogScratchVel.Load()
			newV := math.Float64frombits(oldBits) * factor
			if d.jogScratchVel.CompareAndSwap(oldBits, math.Float64bits(newV)) {
				break
			}
		}
	}
	if pitchOff != 0 {
		factor := math.Pow(0.5, blockSecsMs/jogPitchHalflifeMs)
		for {
			oldBits := d.jogPitchOffset.Load()
			newV := math.Float64frombits(oldBits) * factor
			if d.jogPitchOffset.CompareAndSwap(oldBits, math.Float64bits(newV)) {
				break
			}
		}
	}
}

