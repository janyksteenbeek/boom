package audio

// Schroeder/Freeverb-style reverb: 4 parallel comb filters → 2 series allpass filters.
// All buffers pre-allocated. Zero allocations at runtime.
// Only call ProcessBuffer from the audio thread.

// Comb filter delay lengths (in samples at 48kHz, prime-ish to reduce metallic resonance).
var reverbCombDelays = [4]int{1557, 1617, 1491, 1422}

// Allpass filter delay lengths.
var reverbAllpassDelays = [2]int{556, 441}

const (
	reverbDamping     = 0.4
	reverbAllpassGain = 0.5
)

type combFilter struct {
	buf      []float32
	bufSize  int
	pos      int
	feedback float32
	damp     float32
	dampPrev float32 // low-pass state in feedback path
}

func newCombFilter(size int, feedback float32) combFilter {
	return combFilter{
		buf:      make([]float32, size),
		bufSize:  size,
		feedback: feedback,
		damp:     reverbDamping,
	}
}

func (c *combFilter) process(input float32) float32 {
	output := c.buf[c.pos]

	// One-pole low-pass in feedback path (damping)
	c.dampPrev = output*(1-c.damp) + c.dampPrev*c.damp

	c.buf[c.pos] = input + c.dampPrev*c.feedback
	c.pos++
	if c.pos >= c.bufSize {
		c.pos = 0
	}
	return output
}

func (c *combFilter) reset() {
	for i := range c.buf {
		c.buf[i] = 0
	}
	c.pos = 0
	c.dampPrev = 0
}

type allpassFilter struct {
	buf     []float32
	bufSize int
	pos     int
}

func newAllpassFilter(size int) allpassFilter {
	return allpassFilter{
		buf:     make([]float32, size),
		bufSize: size,
	}
}

func (a *allpassFilter) process(input float32) float32 {
	bufOut := a.buf[a.pos]
	output := -input + bufOut
	a.buf[a.pos] = input + bufOut*reverbAllpassGain
	a.pos++
	if a.pos >= a.bufSize {
		a.pos = 0
	}
	return output
}

func (a *allpassFilter) reset() {
	for i := range a.buf {
		a.buf[i] = 0
	}
	a.pos = 0
}

// ReverbFX implements a Schroeder reverb.
type ReverbFX struct {
	sampleRate int
	combs      [4]combFilter
	allpasses  [2]allpassFilter
}

func NewReverbFX(sampleRate int) *ReverbFX {
	r := &ReverbFX{sampleRate: sampleRate}

	// Scale delay lengths from 48kHz reference to actual sample rate
	scale := float64(sampleRate) / 48000.0
	defaultFeedback := float32(0.84) // moderate decay

	for i := 0; i < 4; i++ {
		size := int(float64(reverbCombDelays[i]) * scale)
		if size < 1 {
			size = 1
		}
		r.combs[i] = newCombFilter(size, defaultFeedback)
	}
	for i := 0; i < 2; i++ {
		size := int(float64(reverbAllpassDelays[i]) * scale)
		if size < 1 {
			size = 1
		}
		r.allpasses[i] = newAllpassFilter(size)
	}
	return r
}

func (r *ReverbFX) SetTime(ms float32) {
	// Map ms (100–5000) to comb feedback (0.7–0.95)
	if ms < 100 {
		ms = 100
	}
	if ms > 5000 {
		ms = 5000
	}
	feedback := float32(0.7 + (float64(ms)/5000.0)*0.25)
	if feedback > 0.95 {
		feedback = 0.95
	}
	for i := range r.combs {
		r.combs[i].feedback = feedback
	}
}

func (r *ReverbFX) ProcessBuffer(samples [][2]float32, n int) {
	for i := 0; i < n; i++ {
		// Mono-sum input
		mono := (samples[i][0] + samples[i][1]) * 0.5

		// Parallel comb filters
		var combSum float32
		for c := range r.combs {
			combSum += r.combs[c].process(mono)
		}
		combSum *= 0.25 // normalize by number of combs

		// Series allpass filters
		out := combSum
		for a := range r.allpasses {
			out = r.allpasses[a].process(out)
		}

		// Output to both channels (wet signal)
		samples[i][0] = out
		samples[i][1] = out
	}
}

func (r *ReverbFX) Reset() {
	for i := range r.combs {
		r.combs[i].reset()
	}
	for i := range r.allpasses {
		r.allpasses[i].reset()
	}
}
