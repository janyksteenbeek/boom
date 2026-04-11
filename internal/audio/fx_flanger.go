package audio

import "math"

const (
	flangerMaxDelaySamples = 1024      // ~21ms at 48kHz
	flangerBaseDelay       = 48.0      // ~1ms base delay at 48kHz (in samples)
	flangerDepth           = 240.0     // ~5ms sweep depth at 48kHz (in samples)
	flangerFeedback        = 0.4       // feedback coefficient
)

// FlangerFX is a stereo comb filter with a sine LFO-modulated delay.
// Uses linear interpolation for fractional delay. Zero allocations at runtime.
// Only call ProcessBuffer from the audio thread.
type FlangerFX struct {
	sampleRate float64
	delayBuf   [2][]float32 // short circular buffers
	bufLen     int
	writePos   int
	lfoPhase   float64 // 0.0–1.0
	lfoRate    float64 // Hz
	baseDelay  float64 // in samples
	depth      float64 // modulation depth in samples
}

func NewFlangerFX(sampleRate int) *FlangerFX {
	sr := float64(sampleRate)
	return &FlangerFX{
		sampleRate: sr,
		delayBuf:   [2][]float32{make([]float32, flangerMaxDelaySamples), make([]float32, flangerMaxDelaySamples)},
		bufLen:     flangerMaxDelaySamples,
		lfoRate:    2.0,                                 // default 2Hz (500ms period)
		baseDelay:  flangerBaseDelay * sr / 48000.0,     // scale to actual sample rate
		depth:      flangerDepth * sr / 48000.0,         // scale to actual sample rate
	}
}

func (f *FlangerFX) SetTime(ms float32) {
	// ms = LFO period → rate = 1000/ms Hz
	if ms < 50 {
		ms = 50
	}
	if ms > 5000 {
		ms = 5000
	}
	f.lfoRate = 1000.0 / float64(ms)
}

func (f *FlangerFX) ProcessBuffer(samples [][2]float32, n int) {
	bl := f.bufLen
	wp := f.writePos

	for i := 0; i < n; i++ {
		// LFO: sine wave 0.0–1.0
		lfoVal := (math.Sin(2.0*math.Pi*f.lfoPhase) + 1.0) * 0.5

		// Modulated delay in samples (fractional)
		delay := f.baseDelay + f.depth*lfoVal
		if delay < 1 {
			delay = 1
		}
		if delay >= float64(bl-1) {
			delay = float64(bl - 2)
		}

		// Integer and fractional parts for interpolation
		delayInt := int(delay)
		delayFrac := float32(delay - float64(delayInt))

		for ch := 0; ch < 2; ch++ {
			// Read with linear interpolation
			readPos0 := (wp - delayInt + bl) % bl
			readPos1 := (readPos0 - 1 + bl) % bl
			delayed := f.delayBuf[ch][readPos0]*(1-delayFrac) + f.delayBuf[ch][readPos1]*delayFrac

			// Write input + feedback
			f.delayBuf[ch][wp] = samples[i][ch] + delayed*flangerFeedback

			// Output wet signal
			samples[i][ch] = delayed
		}

		wp = (wp + 1) % bl
		f.lfoPhase += f.lfoRate / f.sampleRate
		if f.lfoPhase >= 1.0 {
			f.lfoPhase -= 1.0
		}
	}
	f.writePos = wp
}

func (f *FlangerFX) Reset() {
	for ch := 0; ch < 2; ch++ {
		for i := range f.delayBuf[ch] {
			f.delayBuf[ch][i] = 0
		}
	}
	f.writePos = 0
	f.lfoPhase = 0
}
