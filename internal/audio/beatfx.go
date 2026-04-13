package audio

import "sync/atomic"

// FXType identifies the active Beat FX effect.
type FXType int32

const (
	FXNone    FXType = 0
	FXEcho    FXType = 1
	FXFlanger FXType = 2
	FXReverb  FXType = 3
)

// BeatFXProcessor is the interface each effect implements.
type BeatFXProcessor interface {
	ProcessBuffer(samples [][2]float32, n int)
	SetTime(ms float32)
	Reset()
}

// BeatFX routes audio to the currently selected effect with wet/dry blending.
// Parameter updates (SetFXType, SetActive, SetWetDry, SetTime) are lock-free
// and safe to call from any goroutine. ProcessBuffer must only be called from
// the audio thread.
type BeatFX struct {
	active  atomic.Int32 // 0=bypass, 1=active
	fxType  atomic.Int32 // FXType as int32
	lastType int32       // audio-thread-local: detect type changes

	wetDry  SmoothParam // 0.0 (dry) – 1.0 (wet)
	timePar SmoothParam // time in ms (smoothed for click-free changes)

	echo    *EchoFX
	flanger *FlangerFX
	reverb  *ReverbFX

	// Scratch buffer for dry signal copy — pre-allocated, never reallocated.
	dry [maxBufSize][2]float32
}

// NewBeatFX creates a Beat FX processor with all effects pre-allocated.
func NewBeatFX(sampleRate int) *BeatFX {
	b := &BeatFX{
		echo:    NewEchoFX(sampleRate),
		flanger: NewFlangerFX(sampleRate),
		reverb:  NewReverbFX(sampleRate),
	}
	b.wetDry.Init(0.5, sampleRate, 0.010)  // 10ms smoothing
	b.timePar.Init(250, sampleRate, 0.010)  // default 250ms
	return b
}

// SetFXType selects the active effect. Safe from any goroutine.
func (b *BeatFX) SetFXType(t FXType) {
	b.fxType.Store(int32(t))
}

// SetActive enables/disables the effect. Safe from any goroutine.
func (b *BeatFX) SetActive(on bool) {
	if on {
		b.active.Store(1)
	} else {
		b.active.Store(0)
	}
}

// SetWetDry sets the wet/dry mix (0.0–1.0). Safe from any goroutine.
func (b *BeatFX) SetWetDry(v float32) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	b.wetDry.Set(v)
}

// SetTime sets the effect time parameter in ms. Safe from any goroutine.
func (b *BeatFX) SetTime(ms float32) {
	if ms < 1 {
		ms = 1
	}
	b.timePar.Set(ms)
}

// ProcessBuffer applies the active effect with wet/dry blending in-place.
// Audio thread only. Zero allocations.
func (b *BeatFX) ProcessBuffer(samples [][2]float32, n int) {
	if b.active.Load() == 0 {
		return
	}

	ft := b.fxType.Load()
	if ft == int32(FXNone) {
		return
	}

	// Detect effect type change → reset new effect to clear stale buffer data
	if ft != b.lastType {
		b.resetEffect(FXType(ft))
		b.lastType = ft
	}

	// Resolve the active processor
	var proc BeatFXProcessor
	switch FXType(ft) {
	case FXEcho:
		proc = b.echo
	case FXFlanger:
		proc = b.flanger
	case FXReverb:
		proc = b.reverb
	default:
		return
	}

	// Advance the time smoother by the whole block in one step and hand
	// the end-of-block value to the effect. Time changes are slow enough
	// that a single per-buffer update is inaudible.
	timeMs := b.timePar.Advance(n)
	proc.SetTime(timeMs)

	// Copy dry signal
	copy(b.dry[:n], samples[:n])

	// Process in-place (outputs wet signal)
	proc.ProcessBuffer(samples, n)

	// Blend: out = dry*(1-wet) + wet_signal*wet — linearly interpolated
	// across the block instead of per-sample atomic-loading the smoother.
	wetStart, wetStep := b.wetDry.PrepareBlock(n)
	for i := 0; i < n; i++ {
		wet := wetStart + wetStep*float32(i)
		dry := 1.0 - wet
		samples[i][0] = b.dry[i][0]*dry + samples[i][0]*wet
		samples[i][1] = b.dry[i][1]*dry + samples[i][1]*wet
	}
}

// Reset clears state on all effects. Audio thread only.
func (b *BeatFX) Reset() {
	b.echo.Reset()
	b.flanger.Reset()
	b.reverb.Reset()
	b.lastType = int32(FXNone)
}

func (b *BeatFX) resetEffect(t FXType) {
	switch t {
	case FXEcho:
		b.echo.Reset()
	case FXFlanger:
		b.flanger.Reset()
	case FXReverb:
		b.reverb.Reset()
	}
}
