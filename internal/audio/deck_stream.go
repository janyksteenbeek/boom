package audio

import (
	"log"
	"math"

	"github.com/janyksteenbeek/boom/internal/event"
)

// Stream fills the output buffer with audio samples.
// Called ONLY from the malgo audio callback thread.
// Zero allocations, zero I/O, zero locks — just array reads and math.
// All mutable state (pos, fpos, fade, EQ delay) is only modified here.
func (d *Deck) Stream(samples [][2]float32) {
	p := d.pcm.Load()
	if p == nil {
		for i := range samples {
			samples[i] = [2]float32{}
		}
		return
	}

	d.applyPendingCommands()

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

	loopActive, loopStartSamples, loopEndSamples := d.snapshotLoopBounds(p, scratching)

	eotReached := false
	if tempo == 1.0 {
		eotReached = d.fillUnitTempo(samples, p, loopActive, loopStartSamples, loopEndSamples)
	} else {
		d.fillTempoInterpolated(samples, p, tempo, loopActive, loopStartSamples, loopEndSamples)
	}

	if d.fpos < 0 {
		d.pos = 0
	} else {
		d.pos = int(d.fpos)
	}
	// Only flip playing→false on EoT when not scratching, so scratching
	// off the end of the track doesn't kill the deck.
	if !scratching && !loopActive && (eotReached || d.pos >= p.len) {
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

	// Update position snapshot for UI (atomic, read by Position())
	if p.len > 0 {
		d.storeFloat(&d.posSnapshot, float64(d.pos)/float64(p.len))
	}

	d.decayJogVelocities(scratching, scratchVel, pitchOff, n)
}

// applyPendingCommands handles reset/fade/seek commands posted from non-audio
// threads. Must run before any audio processing so subsequent steps see the
// new state.
func (d *Deck) applyPendingCommands() {
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
	p := d.pcm.Load()
	if p == nil {
		return
	}
	seekBits := d.pendingSeek.Load()
	seekVal := math.Float64frombits(seekBits)
	if !math.IsNaN(seekVal) {
		if d.pendingSeek.CompareAndSwap(seekBits, math.Float64bits(math.NaN())) {
			newPos := int(seekVal * float64(p.len))
			if newPos < 0 {
				newPos = 0
			}
			if newPos >= p.len {
				newPos = p.len - 1
			}
			d.pos = newPos
			d.fpos = float64(newPos)
		}
	}
}

// snapshotLoopBounds reads the loop atomics once per Stream() call and turns
// the normalized 0..1 boundaries into absolute sample indices. Loops are
// suppressed while scratching: the user is manually positioning the platter.
func (d *Deck) snapshotLoopBounds(p *pcmBuffer, scratching bool) (active bool, startSamples, endSamples float64) {
	active = d.loopActive.Load() && !scratching
	if !active {
		return false, 0, 0
	}
	ls := d.loadFloat(&d.loopStart)
	le := d.loadFloat(&d.loopEnd)
	if math.IsNaN(ls) || math.IsNaN(le) || le <= ls {
		return false, 0, 0
	}
	startSamples = ls * float64(p.len)
	endSamples = le * float64(p.len)
	if endSamples > float64(p.len) {
		endSamples = float64(p.len)
	}
	if endSamples <= startSamples {
		return false, 0, 0
	}
	return true, startSamples, endSamples
}

// fillUnitTempo is the fast path: tempo == 1.0, direct buffer copy. When a
// loop is active the copy is split at the loop end so wrapping is sample-
// accurate even if the loop is shorter than one block.
func (d *Deck) fillUnitTempo(samples [][2]float32, p *pcmBuffer, loopActive bool, loopStartSamples, loopEndSamples float64) (eot bool) {
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
		if startIdx >= p.len {
			for j := i; j < n; j++ {
				samples[j] = [2]float32{}
			}
			return true
		}
		capIdx := p.len
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
func (d *Deck) fillTempoInterpolated(samples [][2]float32, p *pcmBuffer, tempo float64, loopActive bool, loopStartSamples, loopEndSamples float64) {
	n := len(samples)
	for i := 0; i < n; i++ {
		if loopActive && d.fpos >= loopEndSamples {
			d.fpos = loopStartSamples + (d.fpos - loopEndSamples)
			if d.fpos < loopStartSamples {
				d.fpos = loopStartSamples
			}
		}
		idx := int(math.Floor(d.fpos))
		if idx < 0 || idx >= p.len-1 {
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

func (d *Deck) generateWaveform() {
	p := d.pcm.Load()
	if p == nil {
		return
	}
	track := d.track.Load()

	// Try the on-disk cache first — regenerating the FFT-per-chunk peaks
	// for a full track is seconds of wall time, but reading the blob back
	// is microseconds.
	if d.wfCache != nil && track != nil && track.ID != "" {
		if cached, ok := d.wfCache.GetWaveform(track.ID, d.sampleRate, track.FileMtime); ok && cached != nil {
			d.waveform.Store(cached)
			d.bus.PublishAsync(event.Event{
				Topic: event.TopicEngine, Action: event.ActionWaveformReady,
				DeckID: d.id, Payload: cached,
			})
			return
		}
	}

	data := GenerateWaveformFromPCM(p.samples, d.sampleRate)
	d.waveform.Store(data)
	d.bus.PublishAsync(event.Event{
		Topic: event.TopicEngine, Action: event.ActionWaveformReady,
		DeckID: d.id, Payload: data,
	})

	// Write back to the cache so the next load of this track is instant.
	if d.wfCache != nil && track != nil && track.ID != "" {
		if err := d.wfCache.PutWaveform(track.ID, data, track.FileMtime); err != nil {
			log.Printf("deck %d: cache waveform: %v", d.id, err)
		}
	}
}
