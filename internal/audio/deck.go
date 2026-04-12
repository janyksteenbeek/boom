package audio

import (
	"encoding/json"
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

// Jog wheel decay tunables. Halflives are kept as constants — they affect
// the *feel* of platter inertia and pitch bend rebound and rarely need user
// tuning. The per-tick gains are exposed as per-deck atomics so the engine
// can apply user settings.
const (
	jogScratchHalflifeMs = 100.0 // platter inertia decay
	jogPitchHalflifeMs   = 220.0 // pitch bend rubber-band

	defaultJogScratchSensitivity = 0.4  // dimensionless gain (matches tempo units)
	defaultJogPitchSensitivity   = 0.04
)

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

	// Cue point state — written by UI/MIDI threads via the engine.
	// cuePoint holds the manual cue (persisted to DB); negative = unset.
	// fallbackCue is the auto-cue / track-start position computed on load
	// and is always a valid value (0..1).
	cuePoint    atomic.Uint64 // float64 bits, normalized 0..1; negative = unset
	fallbackCue atomic.Uint64 // float64 bits, 0..1
	cueHeld     atomic.Bool   // CUE button currently held down
	cuePreview  atomic.Bool   // playing because of CUE-hold preview (release → snap back)

	// Loop state. Positions are normalized 0..1 stored as float64 bits.
	// A NaN in loopStart/loopEnd means "unset". loopActive controls whether
	// Stream() wraps playback between the points. loopBeats is the beat
	// length used for halve/double math (0 = manual loop).
	loopStart   atomic.Uint64
	loopEnd     atomic.Uint64
	loopBeats   atomic.Uint64
	loopActive  atomic.Bool

	// Jog wheel state — written by engine, consumed by Stream().
	// vinylMode true = top touch enables scratch; false = all rotation is pitch bend.
	// jogScratchVel is samples-per-output-sample (signed, decays each block).
	// jogPitchOffset is an additive tempo delta (signed, decays each block).
	// prevPlayingJog snapshots playing state on touch-down so release can restore it.
	vinylMode          atomic.Bool
	jogTouched         atomic.Bool
	jogScratchVel      atomic.Uint64
	jogPitchOffset     atomic.Uint64
	jogScratchGainBits atomic.Uint64 // float64 bits, samples-per-sample per tick
	jogPitchGainBits   atomic.Uint64 // float64 bits, tempo delta per tick
	prevPlayingJog     atomic.Bool

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
	d.storeFloat(&d.loopStart, math.NaN())
	d.storeFloat(&d.loopEnd, math.NaN())
	d.storeFloat(&d.loopBeats, 0)
	d.storeFloat(&d.jogScratchVel, 0)
	d.storeFloat(&d.jogPitchOffset, 0)
	d.storeFloat(&d.jogScratchGainBits, defaultJogScratchSensitivity)
	d.storeFloat(&d.jogPitchGainBits, defaultJogPitchSensitivity)
	d.vinylMode.Store(true) // default: classic CDJ scratch behavior
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
	// Discard any loop from the previous track — loop points are per-session
	// and anchored to absolute sample indices of a specific PCM buffer.
	d.loopActive.Store(false)
	d.storeFloat(&d.loopStart, math.NaN())
	d.storeFloat(&d.loopEnd, math.NaN())
	d.storeFloat(&d.loopBeats, 0)
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

// CuePoint returns the manual cue point as a normalized 0..1 fraction,
// or a negative value if no manual cue is set.
func (d *Deck) CuePoint() float64 { return d.loadFloat(&d.cuePoint) }

// SetCuePoint stores a new manual cue point. Pass a negative value (or call
// ClearCue) to mark the manual cue as unset.
func (d *Deck) SetCuePoint(p float64) {
	if p > 1 {
		p = 1
	}
	d.storeFloat(&d.cuePoint, p)
}

// HasCue reports whether a manual cue point is currently set.
func (d *Deck) HasCue() bool { return d.CuePoint() >= 0 }

// ClearCue removes the manual cue point. The fallback cue remains available
// via EffectiveCue().
func (d *Deck) ClearCue() { d.storeFloat(&d.cuePoint, -1) }

// FallbackCue returns the auto-cue / track-start position computed on load.
// Always a valid 0..1 value (defaults to 0 when not explicitly set).
func (d *Deck) FallbackCue() float64 { return d.loadFloat(&d.fallbackCue) }

// SetFallbackCue stores the auto-cue / track-start position.
func (d *Deck) SetFallbackCue(p float64) {
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	d.storeFloat(&d.fallbackCue, p)
}

// EffectiveCue returns the manual cue if one is set, otherwise the fallback
// (auto-cue or track start).
func (d *Deck) EffectiveCue() float64 {
	if d.HasCue() {
		return d.CuePoint()
	}
	return d.FallbackCue()
}

// CueHeld reports whether the CUE button is currently held down.
func (d *Deck) CueHeld() bool { return d.cueHeld.Load() }

// SetCueHeld marks the CUE button as held / released.
func (d *Deck) SetCueHeld(v bool) { d.cueHeld.Store(v) }

// CuePreview reports whether playback is currently a cue-hold preview
// (will snap back to cue on release unless latched by PLAY).
func (d *Deck) CuePreview() bool { return d.cuePreview.Load() }

// SetCuePreview marks the current playback as a cue-hold preview.
func (d *Deck) SetCuePreview(v bool) { d.cuePreview.Store(v) }

// FirstAudioFrame scans the PCM buffer for the first sample whose absolute
// amplitude exceeds a silence threshold (~-60 dBFS) and returns the position
// as a normalized 0..1 fraction. Returns 0 if the buffer is empty or the track
// never rises above silence. Safe to call after LoadTrack completes.
func (d *Deck) FirstAudioFrame() float64 {
	p := d.pcm.Load()
	if p == nil || p.len == 0 {
		return 0
	}
	const threshold float32 = 0.001 // ~-60 dBFS
	samples := p.samples[:p.len]
	for i := 0; i < p.len; i++ {
		l := samples[i][0]
		r := samples[i][1]
		if l < 0 {
			l = -l
		}
		if r < 0 {
			r = -r
		}
		if l > threshold || r > threshold {
			return float64(i) / float64(p.len)
		}
	}
	return 0
}

// AtEffectiveCue reports whether the playhead is at the effective cue point
// (manual cue if set, else fallback) within a ~50 ms tolerance. Returns false
// when no track is loaded.
func (d *Deck) AtEffectiveCue() bool {
	p := d.pcm.Load()
	if p == nil || p.len == 0 {
		return false
	}
	eps := float64(d.sampleRate) * 0.05 / float64(p.len)
	return math.Abs(d.Position()-d.EffectiveCue()) <= eps
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

// ── Loop state ───────────────────────────────────────────────────────────────

// LoopStart returns the normalized loop-start position, or NaN if unset.
func (d *Deck) LoopStart() float64 { return d.loadFloat(&d.loopStart) }

// LoopEnd returns the normalized loop-end position, or NaN if unset.
func (d *Deck) LoopEnd() float64 { return d.loadFloat(&d.loopEnd) }

// LoopBeats returns the beat-length of the current loop (0 = manual/unknown).
func (d *Deck) LoopBeats() float64 { return d.loadFloat(&d.loopBeats) }

// IsLoopActive reports whether the audio thread is currently wrapping
// playback inside the loop boundaries.
func (d *Deck) IsLoopActive() bool { return d.loopActive.Load() }

// HasLoop reports whether both loop boundaries are set to a valid range.
func (d *Deck) HasLoop() bool {
	s := d.LoopStart()
	e := d.LoopEnd()
	return !math.IsNaN(s) && !math.IsNaN(e) && e > s && s >= 0
}

// ClearLoop removes any configured loop and deactivates wrapping.
func (d *Deck) ClearLoop() {
	d.loopActive.Store(false)
	d.storeFloat(&d.loopStart, math.NaN())
	d.storeFloat(&d.loopEnd, math.NaN())
	d.storeFloat(&d.loopBeats, 0)
}

// SetLoopStart stores the loop-start position and deactivates any active
// loop — the out-point must be set before the loop wraps.
func (d *Deck) SetLoopStart(pos float64) {
	if pos < 0 {
		pos = 0
	}
	if pos > 1 {
		pos = 1
	}
	d.loopActive.Store(false)
	d.storeFloat(&d.loopStart, pos)
	d.storeFloat(&d.loopEnd, math.NaN())
	d.storeFloat(&d.loopBeats, 0)
}

// SetManualLoop stores an in/out pair and activates the loop. beats is the
// measured length (or 0 if unknown).
func (d *Deck) SetManualLoop(start, end, beats float64) {
	if end <= start {
		return
	}
	if start < 0 {
		start = 0
	}
	if end > 1 {
		end = 1
	}
	d.storeFloat(&d.loopStart, start)
	d.storeFloat(&d.loopEnd, end)
	d.storeFloat(&d.loopBeats, beats)
	d.loopActive.Store(true)
}

// SetLoopActive toggles the wrap flag without touching the boundaries.
func (d *Deck) SetLoopActive(v bool) { d.loopActive.Store(v) }

// ── Jog wheel state ──────────────────────────────────────────────────────────

// VinylMode reports whether the deck is in vinyl mode (top touch enables
// scratching). When false the deck is in CDJ "jog mode" — all rotation is
// pitch bend.
func (d *Deck) VinylMode() bool { return d.vinylMode.Load() }

// SetVinylMode toggles vinyl mode. Switching to jog mode while the platter
// is touched also clears the captured play state and any pending scratch
// velocity so the deck snaps back to normal playback cleanly.
func (d *Deck) SetVinylMode(on bool) {
	d.vinylMode.Store(on)
	if !on {
		d.storeFloat(&d.jogScratchVel, 0)
	}
}

// ToggleVinylMode flips vinyl mode and returns the new value.
func (d *Deck) ToggleVinylMode() bool {
	d.SetVinylMode(!d.VinylMode())
	return d.VinylMode()
}

// JogTouched reports whether the top sensor of the platter is currently held.
func (d *Deck) JogTouched() bool { return d.jogTouched.Load() }

// SetJogTouch is called from the engine on platter top-touch press/release.
// In vinyl mode the press captures the current play state and forces playing
// = true so Stream() runs while the user scratches; the release restores
// the captured state. In jog mode it's just a flag bookkeeping operation.
func (d *Deck) SetJogTouch(on bool) {
	if on {
		if !d.jogTouched.Load() && d.vinylMode.Load() {
			d.prevPlayingJog.Store(d.playing.Load())
			d.playing.Store(true)
		}
		d.jogTouched.Store(true)
		return
	}
	wasTouched := d.jogTouched.Swap(false)
	if !wasTouched || !d.vinylMode.Load() {
		return
	}
	d.storeFloat(&d.jogScratchVel, 0)
	if !d.prevPlayingJog.Load() {
		d.Pause()
	}
}

// SetJogScratchSensitivity adjusts the per-tick scratch gain (samples-per-
// output-sample contribution per encoder tick). Default 0.4. Higher = more
// audio movement per platter increment.
func (d *Deck) SetJogScratchSensitivity(g float64) {
	if g <= 0 {
		g = defaultJogScratchSensitivity
	}
	d.storeFloat(&d.jogScratchGainBits, g)
}

// SetJogPitchSensitivity adjusts the per-tick pitch bend gain. Default 0.04.
func (d *Deck) SetJogPitchSensitivity(g float64) {
	if g <= 0 {
		g = defaultJogPitchSensitivity
	}
	d.storeFloat(&d.jogPitchGainBits, g)
}

// AddJogScratchDelta accumulates a scratch velocity contribution from one
// MIDI encoder tick. Uses a CAS loop so concurrent ticks (e.g. fast platter
// movement firing several events between Stream() calls) compose additively
// without locking.
func (d *Deck) AddJogScratchDelta(delta float64) {
	contrib := delta * d.loadFloat(&d.jogScratchGainBits)
	for {
		old := d.jogScratchVel.Load()
		newV := math.Float64frombits(old) + contrib
		if d.jogScratchVel.CompareAndSwap(old, math.Float64bits(newV)) {
			return
		}
	}
}

// AddJogPitchDelta accumulates a pitch-bend tempo offset from one MIDI
// encoder tick. Same CAS pattern as AddJogScratchDelta.
func (d *Deck) AddJogPitchDelta(delta float64) {
	contrib := delta * d.loadFloat(&d.jogPitchGainBits)
	for {
		old := d.jogPitchOffset.Load()
		newV := math.Float64frombits(old) + contrib
		if d.jogPitchOffset.CompareAndSwap(old, math.Float64bits(newV)) {
			return
		}
	}
}

// SamplesPerBeat returns the number of samples in one beat at the track's
// native (non-tempo-adjusted) BPM. Using the native BPM keeps halve/double
// musically meaningful when the user later changes tempo.
func (d *Deck) SamplesPerBeat() float64 {
	t := d.track.Load()
	if t == nil || t.BPM <= 0 {
		return 0
	}
	return float64(d.sampleRate) * 60.0 / t.BPM
}

// NearestBeatBefore snaps a normalized position to the nearest beat at or
// before it, using Track.BeatGrid (JSON []float64 seconds) when available,
// falling back to a computed grid from BPM, and finally returning pos
// unchanged if neither is available.
func (d *Deck) NearestBeatBefore(pos float64) float64 {
	p := d.pcm.Load()
	if p == nil || p.len == 0 {
		return pos
	}
	t := d.track.Load()
	if t == nil {
		return pos
	}
	totalSeconds := float64(p.len) / float64(d.sampleRate)
	if totalSeconds <= 0 {
		return pos
	}
	posSeconds := pos * totalSeconds

	if t.BeatGrid != "" {
		var beats []float64
		if err := json.Unmarshal([]byte(t.BeatGrid), &beats); err == nil && len(beats) > 0 {
			best := beats[0]
			for _, b := range beats {
				if b <= posSeconds {
					best = b
				} else {
					break
				}
			}
			return best / totalSeconds
		}
	}
	if t.BPM > 0 {
		beatPeriod := 60.0 / t.BPM
		n := math.Floor(posSeconds / beatPeriod)
		return (n * beatPeriod) / totalSeconds
	}
	return pos
}

// UpdateTrackAnalysis refreshes the deck's cached track metadata after an
// analysis pass completes. Without this the deck keeps the BPM=0 / empty
// beatgrid snapshot captured at LoadTrack time, and beat-based features
// (loops, sync) silently no-op on newly analyzed tracks.
func (d *Deck) UpdateTrackAnalysis(trackID string, bpm float64, key string, beatGridJSON string) bool {
	t := d.track.Load()
	if t == nil || t.ID != trackID {
		return false
	}
	updated := *t
	updated.BPM = bpm
	updated.Key = key
	updated.BeatGrid = beatGridJSON
	d.track.Store(&updated)
	return true
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

	// Snapshot loop bounds once per call — cheap atomic loads, used from the
	// hot inner loops below without re-checking every sample. Loops are
	// suppressed while scratching: the user is manually positioning the platter.
	loopActive := d.loopActive.Load() && !scratching
	var loopStartSamples, loopEndSamples float64
	if loopActive {
		ls := d.loadFloat(&d.loopStart)
		le := d.loadFloat(&d.loopEnd)
		if math.IsNaN(ls) || math.IsNaN(le) || le <= ls {
			loopActive = false
		} else {
			loopStartSamples = ls * float64(p.len)
			loopEndSamples = le * float64(p.len)
			if loopEndSamples > float64(p.len) {
				loopEndSamples = float64(p.len)
			}
			if loopEndSamples <= loopStartSamples {
				loopActive = false
			}
		}
	}

	eotReached := false

	if tempo == 1.0 {
		// Fast path: direct buffer copy. When a loop is active we split the
		// copy at the loop end so wrapping is sample-accurate even if the
		// loop is shorter than one block.
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
				eotReached = true
				break
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
				eotReached = true
				break
			}
			count := n - i
			if count > remaining {
				count = remaining
			}
			copy(samples[i:i+count], p.samples[startIdx:startIdx+count])
			d.fpos += float64(count)
			i += count
		}
	} else {
		// Tempo path: linear interpolation with float64 accumulator. Handles
		// both forward (tempo > 0, including pitch bend) and reverse (tempo
		// < 0, only reachable while scratching). The loop wrap is checked
		// once per output sample, which is trivial compared to the
		// interpolation itself.
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

	// Decay jog velocities. Halflife → per-block factor. CAS so concurrent
	// AddJog* calls between snapshot and store don't get clobbered.
	blockSecsMs := 1000.0 * float64(n) / float64(d.sampleRate)
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
