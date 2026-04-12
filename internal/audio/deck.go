package audio

import (
	"log"
	"math"
	"sync/atomic"

	"github.com/gopxl/beep/v2"

	"github.com/janyksteenbeek/boom/internal/event"
	"github.com/janyksteenbeek/boom/pkg/model"
)

// pcmBuffer holds decoded audio samples at the target sample rate.
type pcmBuffer struct {
	samples [][2]float32
	len     int
}

// Deck represents one playback channel.
// Audio is decoded fully upfront into a PCM buffer.
// The audio callback only reads from this buffer — zero allocations, zero I/O.
type Deck struct {
	id         int
	sampleRate int
	bus        *event.Bus

	// PCM buffer — set on LoadTrack, read from audio thread
	pcm atomic.Pointer[pcmBuffer]

	// Playback position — ONLY modified by audio thread in Stream()
	pos  int
	fpos float64 // fractional position accumulator for tempo (drift-free)

	// EQ — lock-free (uses atomic coefficient swap internally)
	eq *ThreeBandEQ

	// Beat FX — lock-free effect processor (echo/flanger/reverb)
	beatFX *BeatFX

	// Smoothed volume & gain — Set() from any goroutine, Tick() from audio thread
	volume SmoothParam
	gain   SmoothParam // Trim/gain knob: 0.0-2.0 multiplier (1.0 = unity)

	// Fade envelope for click-free play/stop — ONLY accessed from audio thread
	fade FadeEnvelope

	// Atomic control values
	playing   atomic.Bool
	tempoBits atomic.Uint64 // float64 as bits, ratio

	// Cue point state — written by UI/MIDI threads via the engine
	cuePoint    atomic.Uint64 // float64 bits, normalized 0..1; negative = unset
	cueHeld     atomic.Bool   // CUE button currently held down
	cuePreview  atomic.Bool   // playing because of CUE-hold preview (release → snap back)

	// Pending commands from non-audio threads (written by UI/MIDI, consumed by audio thread)
	pendingSeek  atomic.Uint64 // float64 bits; NaN = no pending seek
	pendingFade  atomic.Int32  // 0=none, 1=fade-in, 2=fade-out
	pendingReset atomic.Bool   // set by LoadTrack, audio thread resets all state

	// Position snapshot for UI (written by audio thread, read by UI/position loop)
	posSnapshot atomic.Uint64 // float64 bits, fractional position 0.0-1.0

	track    atomic.Pointer[model.Track]
	waveform atomic.Pointer[WaveformData]
}

func NewDeck(id int, sampleRate int, bus *event.Bus) *Deck {
	d := &Deck{
		id:         id,
		sampleRate: sampleRate,
		bus:        bus,
		eq:         NewThreeBandEQ(sampleRate),
		beatFX:     NewBeatFX(sampleRate),
		fade:       NewFadeEnvelope(sampleRate, 0.01),
	}
	d.volume.Init(0.8, sampleRate, 0.005)
	d.gain.Init(1.0, sampleRate, 0.005)
	d.storeFloat(&d.tempoBits, 1.0)
	d.storeFloat(&d.pendingSeek, math.NaN()) // sentinel: no pending seek
	d.storeFloat(&d.cuePoint, -1)            // sentinel: no cue set
	return d
}

func (d *Deck) ID() int { return d.id }

// LoadTrack decodes the entire file to a PCM buffer at the target sample rate.
// This runs on the calling goroutine (NOT the audio thread). May take a moment.
func (d *Deck) LoadTrack(track *model.Track) error {
	log.Printf("deck %d: decoding '%s'...", d.id, track.Title)

	src, format, err := Decode(track.Path)
	if err != nil {
		return err
	}

	// Build offline decode chain: source → resample (if needed)
	var streamer beep.Streamer = src
	needsResample := format.SampleRate != beep.SampleRate(d.sampleRate)
	if needsResample {
		log.Printf("deck %d: resampling %d → %d Hz", d.id, format.SampleRate, d.sampleRate)
		streamer = beep.Resample(4, format.SampleRate, beep.SampleRate(d.sampleRate), src)
	}

	// Pre-allocate PCM buffer based on known track length to avoid append/realloc
	estimatedSamples := src.Len()
	if needsResample {
		estimatedSamples = int(float64(estimatedSamples) * float64(d.sampleRate) / float64(format.SampleRate))
	}
	pcm := make([][2]float32, 0, estimatedSamples+8192) // +margin

	// Decode in chunks — beep returns float64, we convert to float32
	buf := make([][2]float64, 8192)
	for {
		n, ok := streamer.Stream(buf)
		if n > 0 {
			for i := 0; i < n; i++ {
				pcm = append(pcm, [2]float32{float32(buf[i][0]), float32(buf[i][1])})
			}
		}
		if !ok {
			break
		}
	}
	src.Close()

	log.Printf("deck %d: decoded %d samples (%.1f sec)", d.id, len(pcm), float64(len(pcm))/float64(d.sampleRate))

	// Stop playback BEFORE swapping the PCM buffer so the callback never
	// reads the new buffer at the old position (race window elimination).
	d.playing.Store(false)
	d.pendingFade.Store(0)                                // clear any pending fade
	d.storeFloat(&d.pendingSeek, math.NaN())              // clear any pending seek
	d.cueHeld.Store(false)
	d.cuePreview.Store(false)
	// cuePoint is set by the engine after LoadTrack returns (from track.CuePoint).
	d.pcm.Store(&pcmBuffer{samples: pcm, len: len(pcm)}) // swap buffer
	d.track.Store(track)
	d.pendingReset.Store(true)                             // signal audio thread to reset pos/fade/EQ

	go d.generateWaveform()

	d.bus.PublishAsync(event.Event{
		Topic: event.TopicEngine, Action: event.ActionTrackLoaded,
		DeckID: d.id, Payload: track,
	})
	return nil
}

func (d *Deck) Play() {
	if d.pcm.Load() == nil {
		return
	}
	log.Printf("deck %d: Play()", d.id)
	d.playing.Store(true)
	d.pendingFade.Store(1) // audio thread will trigger fade-in
}

func (d *Deck) Pause() {
	d.pendingFade.Store(2) // audio thread will trigger fade-out
	// playing will be set to false when fade-out completes in Stream()
}

func (d *Deck) TogglePlay() {
	if d.IsPlaying() {
		d.Pause()
	} else {
		d.Play()
	}
}

func (d *Deck) IsPlaying() bool { return d.playing.Load() }

func (d *Deck) SetVolume(v float64) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	d.volume.Set(float32(v))
}

// SetGain sets the trim/gain multiplier (0.0 = mute, 1.0 = unity, 2.0 = +6dB).
func (d *Deck) SetGain(v float32) {
	if v < 0 {
		v = 0
	}
	if v > 2 {
		v = 2
	}
	d.gain.Set(v)
}

func (d *Deck) SetEQHigh(v float64) { d.eq.SetHigh(v) }
func (d *Deck) SetEQMid(v float64)  { d.eq.SetMid(v) }
func (d *Deck) SetEQLow(v float64)  { d.eq.SetLow(v) }

func (d *Deck) SetBeatFXType(t FXType)    { d.beatFX.SetFXType(t) }
func (d *Deck) SetBeatFXActive(on bool)   { d.beatFX.SetActive(on) }
func (d *Deck) SetBeatFXWetDry(v float32) { d.beatFX.SetWetDry(v) }
func (d *Deck) SetBeatFXTime(ms float32)  { d.beatFX.SetTime(ms) }

func (d *Deck) SetTempo(ratio float64) {
	if ratio <= 0 {
		ratio = 0.01
	}
	if ratio > 4 {
		ratio = 4
	}
	d.storeFloat(&d.tempoBits, ratio)
}

func (d *Deck) Seek(pos float64) {
	if d.pcm.Load() == nil {
		return
	}
	if pos < 0 {
		pos = 0
	}
	if pos > 1 {
		pos = 1
	}
	d.storeFloat(&d.pendingSeek, pos) // audio thread will apply
}

// CuePoint returns the current cue point as a normalized 0..1 fraction,
// or a negative value if no cue is set.
func (d *Deck) CuePoint() float64 { return d.loadFloat(&d.cuePoint) }

// SetCuePoint stores a new cue point. Pass a negative value (or call ClearCue)
// to mark the cue as unset.
func (d *Deck) SetCuePoint(p float64) {
	if p > 1 {
		p = 1
	}
	d.storeFloat(&d.cuePoint, p)
}

// HasCue reports whether a cue point is currently set.
func (d *Deck) HasCue() bool { return d.CuePoint() >= 0 }

// ClearCue removes the cue point.
func (d *Deck) ClearCue() { d.storeFloat(&d.cuePoint, -1) }

// CueHeld reports whether the CUE button is currently held down.
func (d *Deck) CueHeld() bool { return d.cueHeld.Load() }

// SetCueHeld marks the CUE button as held / released.
func (d *Deck) SetCueHeld(v bool) { d.cueHeld.Store(v) }

// CuePreview reports whether playback is currently a cue-hold preview
// (will snap back to cue on release unless latched by PLAY).
func (d *Deck) CuePreview() bool { return d.cuePreview.Load() }

// SetCuePreview marks the current playback as a cue-hold preview.
func (d *Deck) SetCuePreview(v bool) { d.cuePreview.Store(v) }

// AtCue reports whether the playhead is at the cue point within a
// ~50 ms tolerance. Returns false when no cue is set or no track is loaded.
func (d *Deck) AtCue() bool {
	if !d.HasCue() {
		return false
	}
	p := d.pcm.Load()
	if p == nil || p.len == 0 {
		return false
	}
	eps := float64(d.sampleRate) * 0.05 / float64(p.len)
	return math.Abs(d.Position()-d.CuePoint()) <= eps
}

func (d *Deck) Position() float64 {
	return d.loadFloat(&d.posSnapshot)
}

func (d *Deck) Track() *model.Track     { return d.track.Load() }
func (d *Deck) Waveform() *WaveformData { return d.waveform.Load() }
func (d *Deck) SampleRate() int          { return d.sampleRate }
func (d *Deck) Tempo() float64           { return d.loadFloat(&d.tempoBits) }

// PCMSamples returns a read-only reference to the decoded PCM buffer.
// The returned slice must not be modified. Returns nil if no track is loaded.
func (d *Deck) PCMSamples() [][2]float32 {
	p := d.pcm.Load()
	if p == nil {
		return nil
	}
	return p.samples[:p.len]
}

// EffectiveBPM returns the track's BPM adjusted by the current tempo ratio.
// Returns 0 if no track is loaded or the track has no BPM metadata.
func (d *Deck) EffectiveBPM() float64 {
	t := d.track.Load()
	if t == nil || t.BPM <= 0 {
		return 0
	}
	return t.BPM * d.Tempo()
}

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

	// ── Process pending commands from non-audio threads ──

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

	// ── Audio processing ──

	// If not playing and fade is silent, zero-fill
	if !d.playing.Load() && d.fade.State() == FadeSilent {
		for i := range samples {
			samples[i] = [2]float32{}
		}
		return
	}

	tempo := d.loadFloat(&d.tempoBits)
	n := len(samples)

	if tempo == 1.0 {
		// Fast path: no tempo change, direct copy (fpos stays source of truth)
		startIdx := int(d.fpos)
		remaining := p.len - startIdx
		if remaining <= 0 {
			for i := range samples {
				samples[i] = [2]float32{}
			}
			d.playing.Store(false)
			d.fade = NewFadeEnvelope(d.sampleRate, 0.01)
			return
		}
		count := n
		if count > remaining {
			count = remaining
		}
		copy(samples[:count], p.samples[startIdx:startIdx+count])
		d.fpos += float64(count)
		for i := count; i < n; i++ {
			samples[i] = [2]float32{}
		}
	} else {
		// Tempo: linear interpolation with float64 accumulator (fpos is persistent)
		for i := 0; i < n; i++ {
			idx := int(d.fpos)
			if idx >= p.len-1 {
				samples[i] = [2]float32{}
				continue
			}
			frac := float32(d.fpos - float64(idx))
			samples[i][0] = p.samples[idx][0]*(1-frac) + p.samples[idx+1][0]*frac
			samples[i][1] = p.samples[idx][1]*(1-frac) + p.samples[idx+1][1]*frac
			d.fpos += tempo
		}
	}
	d.pos = int(d.fpos)
	if d.pos >= p.len {
		d.playing.Store(false)
		d.fade = NewFadeEnvelope(d.sampleRate, 0.01)
	}

	// Apply EQ in-place (lock-free biquad filters)
	d.eq.ProcessBuffer(samples, n)

	// Apply Beat FX in-place (lock-free, zero-alloc)
	d.beatFX.ProcessBuffer(samples, n)

	// Apply gain and volume in a single pass (biquad filters are linear, so
	// gain*EQ(x) == EQ(gain*x) — safe to combine post-EQ)
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
}

func (d *Deck) generateWaveform() {
	p := d.pcm.Load()
	if p == nil {
		return
	}
	data := GenerateWaveformFromPCM(p.samples, d.sampleRate)
	d.waveform.Store(data)
	d.bus.PublishAsync(event.Event{
		Topic: event.TopicEngine, Action: event.ActionWaveformReady,
		DeckID: d.id, Payload: data,
	})
}

func (d *Deck) storeFloat(a *atomic.Uint64, v float64) {
	a.Store(math.Float64bits(v))
}

func (d *Deck) loadFloat(a *atomic.Uint64) float64 {
	return math.Float64frombits(a.Load())
}
