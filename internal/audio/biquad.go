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

// biquadCoeffs holds the immutable filter coefficients in float64
// for full numerical precision during processing.
type biquadCoeffs struct {
	b0, b1, b2 float64
	a1, a2     float64
}

// BiquadFilter is a 2nd-order IIR biquad filter for audio DSP.
// Coefficient updates are lock-free via atomic pointer swap.
// Processing uses float64 internally to prevent quantization-induced instability.
// Process is NOT thread-safe with itself — only call from one goroutine (the audio callback).
type BiquadFilter struct {
	coeffs unsafe.Pointer // *biquadCoeffs, swapped atomically

	// Per-channel delay state in float64 (only accessed from audio thread)
	x1, x2 [2]float64
	y1, y2 [2]float64
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
// Uses float64 internally for full numerical precision.
// Only call from the audio callback goroutine.
func (f *BiquadFilter) ProcessBuffer(samples [][2]float32, n int) {
	c := (*biquadCoeffs)(atomic.LoadPointer(&f.coeffs))
	if c == nil {
		return
	}

	b0, b1, b2 := c.b0, c.b1, c.b2
	a1, a2 := c.a1, c.a2

	// Single interleaved pass over both channels — keeps delay registers
	// in local vars and halves the memory passes over samples[] compared
	// to processing each channel in its own loop.
	xL1, xL2 := f.x1[0], f.x2[0]
	yL1, yL2 := f.y1[0], f.y2[0]
	xR1, xR2 := f.x1[1], f.x2[1]
	yR1, yR2 := f.y1[1], f.y2[1]

	for i := 0; i < n; i++ {
		xL0 := float64(samples[i][0])
		yL0 := b0*xL0 + b1*xL1 + b2*xL2 - a1*yL1 - a2*yL2
		if yL0 != yL0 {
			xL1, xL2, yL1, yL2 = 0, 0, 0, 0
			yL0 = 0
		}
		xL2 = xL1
		xL1 = xL0
		yL2 = yL1
		yL1 = yL0
		samples[i][0] = float32(yL0)

		xR0 := float64(samples[i][1])
		yR0 := b0*xR0 + b1*xR1 + b2*xR2 - a1*yR1 - a2*yR2
		if yR0 != yR0 {
			xR1, xR2, yR1, yR2 = 0, 0, 0, 0
			yR0 = 0
		}
		xR2 = xR1
		xR1 = xR0
		yR2 = yR1
		yR1 = yR0
		samples[i][1] = float32(yR0)
	}

	f.x1[0], f.x2[0] = xL1, xL2
	f.y1[0], f.y2[0] = yL1, yL2
	f.x1[1], f.x2[1] = xR1, xR2
	f.y1[1], f.y2[1] = yR1, yR2
}

// Reset clears the filter delay state.
func (f *BiquadFilter) Reset() {
	f.x1 = [2]float64{}
	f.x2 = [2]float64{}
	f.y1 = [2]float64{}
	f.y2 = [2]float64{}
}

// calcCoeffs computes biquad coefficients in float64 for full numerical precision.
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
		b0: b0 / a0, b1: b1 / a0, b2: b2 / a0,
		a1: a1 / a0, a2: a2 / a0,
	}
}
