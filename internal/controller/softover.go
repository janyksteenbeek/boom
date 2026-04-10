package controller

import (
	"math"
	"sync"
	"time"
)

const softTakeoverDisengageTimeout = time.Second

// SoftTakeoverState tracks per-control takeover status to prevent
// parameter jumps when a physical fader doesn't match the software value.
type SoftTakeoverState struct {
	mu          sync.Mutex
	engaged     bool
	softwareVal float64 // Current software value (0.0-1.0)
	threshold   float64
	lastInput   time.Time
}

// NewSoftTakeoverState creates a soft takeover tracker with the given threshold.
func NewSoftTakeoverState(threshold float64) *SoftTakeoverState {
	if threshold <= 0 {
		threshold = 0.05
	}
	return &SoftTakeoverState{
		threshold: threshold,
	}
}

// SetSoftwareValue updates the current software value (called when the
// parameter changes externally, e.g., via sync or loading a new track).
func (s *SoftTakeoverState) SetSoftwareValue(v float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.softwareVal = v
	s.engaged = false // Disengage when software value changes externally
}

// Filter processes a hardware input value and returns the value to apply
// and whether it should be applied.
func (s *SoftTakeoverState) Filter(inputVal float64) (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	// Reset if no input for a while
	if !s.lastInput.IsZero() && now.Sub(s.lastInput) > softTakeoverDisengageTimeout && !s.engaged {
		s.lastInput = now
		// Fresh start — check if close enough
		if math.Abs(inputVal-s.softwareVal) < s.threshold {
			s.engaged = true
			s.softwareVal = inputVal
			return inputVal, true
		}
		return 0, false
	}

	s.lastInput = now

	if s.engaged {
		s.softwareVal = inputVal
		return inputVal, true
	}

	// Not engaged: check if the fader has "caught up"
	if math.Abs(inputVal-s.softwareVal) < s.threshold {
		s.engaged = true
		s.softwareVal = inputVal
		return inputVal, true
	}

	return 0, false
}
