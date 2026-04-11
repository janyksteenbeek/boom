package audio

import "math"

const (
	echoMaxDelayMs  = 2000  // Maximum delay time in milliseconds
	echoFeedback    = 0.5   // Fixed feedback coefficient
	echoMaxFeedback = 0.9   // Absolute max to prevent runaway
)

// EchoFX is a stereo delay effect with feedback.
// Uses a pre-allocated circular buffer per channel. Zero allocations at runtime.
// Only call ProcessBuffer from the audio thread.
type EchoFX struct {
	sampleRate int
	delayBuf   [2][]float32 // circular buffers, one per channel
	bufLen     int
	writePos   int
	delaySamp  int // current delay in samples
}

func NewEchoFX(sampleRate int) *EchoFX {
	bufLen := sampleRate * echoMaxDelayMs / 1000
	return &EchoFX{
		sampleRate: sampleRate,
		delayBuf:   [2][]float32{make([]float32, bufLen), make([]float32, bufLen)},
		bufLen:     bufLen,
		delaySamp:  sampleRate / 4, // default 250ms
	}
}

func (e *EchoFX) SetTime(ms float32) {
	samp := int(float64(ms) * float64(e.sampleRate) / 1000.0)
	if samp < 1 {
		samp = 1
	}
	if samp >= e.bufLen {
		samp = e.bufLen - 1
	}
	e.delaySamp = samp
}

func (e *EchoFX) ProcessBuffer(samples [][2]float32, n int) {
	bl := e.bufLen
	ds := e.delaySamp
	wp := e.writePos

	for i := 0; i < n; i++ {
		readPos := (wp - ds + bl) % bl

		for ch := 0; ch < 2; ch++ {
			delayed := e.delayBuf[ch][readPos]

			// Write input + feedback into buffer, soft-clip feedback to prevent runaway
			fb := delayed * echoFeedback
			fb = float32(math.Tanh(float64(fb))) // soft-clip
			e.delayBuf[ch][wp] = samples[i][ch] + fb

			// Output is the delayed signal (wet only — BeatFX handles dry/wet blend)
			samples[i][ch] = delayed
		}

		wp = (wp + 1) % bl
	}
	e.writePos = wp
}

func (e *EchoFX) Reset() {
	for ch := 0; ch < 2; ch++ {
		for i := range e.delayBuf[ch] {
			e.delayBuf[ch][i] = 0
		}
	}
	e.writePos = 0
}
