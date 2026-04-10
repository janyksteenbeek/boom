package audio

import "sync/atomic"

const (
	eqLowFreq  = 320.0
	eqMidFreq  = 1000.0
	eqHighFreq = 3200.0
	eqQ        = 0.707
	minGain    = 0.01
)

// ThreeBandEQ is a DJ-style 3-band equalizer.
// Processes sample buffers in-place. Lock-free, allocation-free.
type ThreeBandEQ struct {
	sampleRate float64
	lowActive, midActive, highActive atomic.Int32
	lowFilter, midFilter, highFilter *BiquadFilter
}

func NewThreeBandEQ(sampleRate int) *ThreeBandEQ {
	sr := float64(sampleRate)
	return &ThreeBandEQ{
		sampleRate: sr,
		lowFilter:  NewBiquadFilter(BiquadLowShelf, sr, eqLowFreq, 1.0, eqQ),
		midFilter:  NewBiquadFilter(BiquadPeaking, sr, eqMidFreq, 1.0, eqQ),
		highFilter: NewBiquadFilter(BiquadHighShelf, sr, eqHighFreq, 1.0, eqQ),
	}
}

func eqGain(v float64) float64 {
	gain := v * 2.0
	if gain < minGain {
		gain = minGain
	}
	return gain
}

func (eq *ThreeBandEQ) SetLow(v float64) {
	gain := eqGain(v)
	eq.lowFilter.Update(BiquadLowShelf, eq.sampleRate, eqLowFreq, gain, eqQ)
	if gain == 1.0 {
		eq.lowActive.Store(0)
	} else {
		eq.lowActive.Store(1)
	}
}

func (eq *ThreeBandEQ) SetMid(v float64) {
	gain := eqGain(v)
	eq.midFilter.Update(BiquadPeaking, eq.sampleRate, eqMidFreq, gain, eqQ)
	if gain == 1.0 {
		eq.midActive.Store(0)
	} else {
		eq.midActive.Store(1)
	}
}

func (eq *ThreeBandEQ) SetHigh(v float64) {
	gain := eqGain(v)
	eq.highFilter.Update(BiquadHighShelf, eq.sampleRate, eqHighFreq, gain, eqQ)
	if gain == 1.0 {
		eq.highActive.Store(0)
	} else {
		eq.highActive.Store(1)
	}
}

// ProcessBuffer applies EQ to samples in-place. Called from audio thread only.
func (eq *ThreeBandEQ) ProcessBuffer(samples [][2]float32, n int) {
	if eq.lowActive.Load() != 0 {
		eq.lowFilter.ProcessBuffer(samples, n)
	}
	if eq.midActive.Load() != 0 {
		eq.midFilter.ProcessBuffer(samples, n)
	}
	if eq.highActive.Load() != 0 {
		eq.highFilter.ProcessBuffer(samples, n)
	}
}

// Reset clears filter state (call when loading new track).
func (eq *ThreeBandEQ) Reset() {
	eq.lowFilter.Reset()
	eq.midFilter.Reset()
	eq.highFilter.Reset()
}
