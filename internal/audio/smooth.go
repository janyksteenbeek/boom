package audio

import (
	"math"
	"sync/atomic"
)

// SmoothParam is a lock-free, allocation-free parameter smoother for the audio thread.
// Set() can be called from any goroutine. Tick() must only be called from the audio thread.
type SmoothParam struct {
	target atomic.Uint32 // float32 bits, set from any goroutine
	current float32      // only accessed from audio thread
	coeff   float32      // smoothing coefficient (0.0–1.0)
}

// NewSmoothParam creates a parameter smoother with the given initial value.
// smoothTimeSec is the approximate time to reach the target (~5ms recommended for audio).
func NewSmoothParam(initial float32, sampleRate int, smoothTimeSec float64) SmoothParam {
	sp := SmoothParam{
		current: initial,
		coeff:   float32(1.0 - math.Exp(-1.0/(smoothTimeSec*float64(sampleRate)))),
	}
	sp.target.Store(math.Float32bits(initial))
	return sp
}

// Set atomically stores a new target value. Safe to call from any goroutine.
func (s *SmoothParam) Set(v float32) {
	s.target.Store(math.Float32bits(v))
}

// Get returns the current target value. Safe to call from any goroutine.
func (s *SmoothParam) Get() float32 {
	return math.Float32frombits(s.target.Load())
}

// Tick advances one sample toward the target using a one-pole filter.
// Only call from the audio thread.
func (s *SmoothParam) Tick() float32 {
	t := math.Float32frombits(s.target.Load())
	s.current += s.coeff * (t - s.current)
	return s.current
}

// Snap instantly sets current to the target value, skipping smoothing.
// Only call from the audio thread.
func (s *SmoothParam) Snap() {
	s.current = math.Float32frombits(s.target.Load())
}

// Current returns the current smoothed value without advancing.
// Only call from the audio thread.
func (s *SmoothParam) Current() float32 {
	return s.current
}

// FadeState represents the current state of a fade envelope.
type FadeState int32

const (
	FadeSilent   FadeState = iota // Output is zero
	FadingIn                      // Ramping up
	FadeActive                    // Full volume, no processing needed
	FadingOut                     // Ramping down
)

// FadeEnvelope applies a linear amplitude ramp for click-free play/stop transitions.
// Only use from the audio thread.
type FadeEnvelope struct {
	state   FadeState
	pos     int // current position within fade
	fadeLen int // fade length in samples
}

// NewFadeEnvelope creates a fade envelope.
// fadeTimeSec is the duration of fade-in/fade-out (~0.01 = 10ms recommended).
func NewFadeEnvelope(sampleRate int, fadeTimeSec float64) FadeEnvelope {
	fadeLen := int(fadeTimeSec * float64(sampleRate))
	if fadeLen < 1 {
		fadeLen = 1
	}
	return FadeEnvelope{
		state:   FadeSilent,
		fadeLen: fadeLen,
	}
}

// TriggerFadeIn starts a fade-in from silent or interrupts a fade-out.
func (f *FadeEnvelope) TriggerFadeIn() {
	switch f.state {
	case FadeSilent:
		f.pos = 0
		f.state = FadingIn
	case FadingOut:
		// Reverse: calculate equivalent fade-in position from current gain
		gain := 1.0 - float64(f.pos)/float64(f.fadeLen)
		f.pos = int(gain * float64(f.fadeLen))
		f.state = FadingIn
	case FadingIn, FadeActive:
		// Already fading in or active, do nothing
	}
}

// TriggerFadeOut starts a fade-out from active or interrupts a fade-in.
func (f *FadeEnvelope) TriggerFadeOut() {
	switch f.state {
	case FadeActive:
		f.pos = 0
		f.state = FadingOut
	case FadingIn:
		// Reverse: calculate equivalent fade-out position from current gain
		gain := float64(f.pos) / float64(f.fadeLen)
		f.pos = int((1.0 - gain) * float64(f.fadeLen))
		f.state = FadingOut
	case FadingOut, FadeSilent:
		// Already fading out or silent, do nothing
	}
}

// State returns the current fade state.
func (f *FadeEnvelope) State() FadeState {
	return f.state
}

// Process applies the fade envelope to a buffer of stereo samples in-place.
// Returns the state after processing.
func (f *FadeEnvelope) Process(samples [][2]float32) FadeState {
	switch f.state {
	case FadeSilent:
		for i := range samples {
			samples[i] = [2]float32{}
		}

	case FadeActive:
		// No processing needed — samples pass through at full volume
		return FadeActive

	case FadingIn:
		for i := range samples {
			if f.pos >= f.fadeLen {
				f.state = FadeActive
				return f.state
			}
			gain := float32(f.pos) / float32(f.fadeLen)
			samples[i][0] *= gain
			samples[i][1] *= gain
			f.pos++
		}

	case FadingOut:
		for i := range samples {
			if f.pos >= f.fadeLen {
				f.state = FadeSilent
				// Zero-fill the rest
				for j := i; j < len(samples); j++ {
					samples[j] = [2]float32{}
				}
				return f.state
			}
			gain := 1.0 - float32(f.pos)/float32(f.fadeLen)
			samples[i][0] *= gain
			samples[i][1] *= gain
			f.pos++
		}
	}
	return f.state
}
