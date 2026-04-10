package audio

import (
	"math"
	"sync/atomic"
	"unsafe"
)

// BiquadType defines the type of biquad filter.
type BiquadType int

const (
	BiquadLowShelf  BiquadType = iota
	BiquadHighShelf
	BiquadPeaking
)

// biquadCoeffs holds the immutable filter coefficients.
// Stored as float32 for cache-friendly audio processing.
type biquadCoeffs struct {
	b0, b1, b2 float32
	a1, a2     float32
}

// BiquadFilter is a 2nd-order IIR biquad filter for audio DSP.
// Coefficient updates are lock-free via atomic pointer swap.
// Process is NOT thread-safe with itself — only call from one goroutine (the audio callback).
type BiquadFilter struct {
	coeffs unsafe.Pointer // *biquadCoeffs, swapped atomically

	// Per-channel delay state (only accessed from audio thread)
	x1, x2 [2]float32
	y1, y2 [2]float32
}

// NewBiquadFilter creates a biquad filter.
func NewBiquadFilter(filterType BiquadType, sampleRate, freq, gain, q float64) *BiquadFilter {
	f := &BiquadFilter{}
	c := calcCoeffs(filterType, sampleRate, freq, gain, q)
	atomic.StorePointer(&f.coeffs, unsafe.Pointer(c))
	return f
}

// Update recalculates coefficients. Safe to call from any goroutine.
func (f *BiquadFilter) Update(filterType BiquadType, sampleRate, freq, gain, q float64) {
	c := calcCoeffs(filterType, sampleRate, freq, gain, q)
	atomic.StorePointer(&f.coeffs, unsafe.Pointer(c))
}

// ProcessBuffer applies the filter to a buffer of stereo samples in-place.
// Only call from the audio callback goroutine.
func (f *BiquadFilter) ProcessBuffer(samples [][2]float32, n int) {
	c := (*biquadCoeffs)(atomic.LoadPointer(&f.coeffs))
	if c == nil {
		return
	}

	b0, b1, b2 := c.b0, c.b1, c.b2
	a1, a2 := c.a1, c.a2

	for ch := 0; ch < 2; ch++ {
		x1, x2 := f.x1[ch], f.x2[ch]
		y1, y2 := f.y1[ch], f.y2[ch]

		for i := 0; i < n; i++ {
			x0 := samples[i][ch]
			y0 := b0*x0 + b1*x1 + b2*x2 - a1*y1 - a2*y2
			x2 = x1
			x1 = x0
			y2 = y1
			y1 = y0
			samples[i][ch] = y0
		}

		f.x1[ch], f.x2[ch] = x1, x2
		f.y1[ch], f.y2[ch] = y1, y2
	}
}

// Reset clears the filter delay state.
func (f *BiquadFilter) Reset() {
	f.x1 = [2]float32{}
	f.x2 = [2]float32{}
	f.y1 = [2]float32{}
	f.y2 = [2]float32{}
}

// calcCoeffs computes biquad coefficients using float64 for numerical stability,
// then stores the result as float32 for cache-friendly processing.
func calcCoeffs(filterType BiquadType, sampleRate, freq, gain, q float64) *biquadCoeffs {
	w0 := 2.0 * math.Pi * freq / sampleRate
	cosW0 := math.Cos(w0)
	sinW0 := math.Sin(w0)
	alpha := sinW0 / (2.0 * q)
	A := math.Sqrt(gain)

	var b0, b1, b2, a0, a1, a2 float64

	switch filterType {
	case BiquadLowShelf:
		sa := 2.0 * math.Sqrt(A) * alpha
		b0 = A * ((A + 1) - (A-1)*cosW0 + sa)
		b1 = 2 * A * ((A - 1) - (A+1)*cosW0)
		b2 = A * ((A + 1) - (A-1)*cosW0 - sa)
		a0 = (A + 1) + (A-1)*cosW0 + sa
		a1 = -2 * ((A - 1) + (A+1)*cosW0)
		a2 = (A + 1) + (A-1)*cosW0 - sa

	case BiquadHighShelf:
		sa := 2.0 * math.Sqrt(A) * alpha
		b0 = A * ((A + 1) + (A-1)*cosW0 + sa)
		b1 = -2 * A * ((A - 1) + (A+1)*cosW0)
		b2 = A * ((A + 1) + (A-1)*cosW0 - sa)
		a0 = (A + 1) - (A-1)*cosW0 + sa
		a1 = 2 * ((A - 1) - (A+1)*cosW0)
		a2 = (A + 1) - (A-1)*cosW0 - sa

	case BiquadPeaking:
		b0 = 1 + alpha*A
		b1 = -2 * cosW0
		b2 = 1 - alpha*A
		a0 = 1 + alpha/A
		a1 = -2 * cosW0
		a2 = 1 - alpha/A
	}

	return &biquadCoeffs{
		b0: float32(b0 / a0), b1: float32(b1 / a0), b2: float32(b2 / a0),
		a1: float32(a1 / a0), a2: float32(a2 / a0),
	}
}
