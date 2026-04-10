package controller

import (
	"sync"
	"time"
)

const highResTimeout = 10 * time.Millisecond

// HighResState tracks 14-bit MSB/LSB pairs for high-resolution controls.
type HighResState struct {
	mu       sync.Mutex
	msbValue uint8
	msbTime  time.Time
	pending  bool
}

// NewHighResState creates a new high-resolution state tracker.
func NewHighResState() *HighResState {
	return &HighResState{}
}

// SetMSB stores the MSB value and marks it as pending.
func (h *HighResState) SetMSB(value uint8) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.msbValue = value
	h.msbTime = time.Now()
	h.pending = true
}

// CombineWithLSB combines the pending MSB with the given LSB to produce
// a 14-bit value (0-16383). Returns the combined value and true if
// a valid MSB was pending. If no MSB is pending, returns (lsb, false).
func (h *HighResState) CombineWithLSB(lsb uint8) (uint16, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.pending {
		return uint16(lsb), false
	}

	// Check timeout
	if time.Since(h.msbTime) > highResTimeout {
		h.pending = false
		return uint16(lsb), false
	}

	combined := (uint16(h.msbValue) << 7) | uint16(lsb)
	h.pending = false
	return combined, true
}

// FlushMSB returns the MSB-only value if one is pending and has timed out.
// Returns (value, true) if there was a timed-out MSB, (0, false) otherwise.
func (h *HighResState) FlushMSB() (uint16, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.pending {
		return 0, false
	}

	if time.Since(h.msbTime) > highResTimeout {
		value := uint16(h.msbValue) << 7
		h.pending = false
		return value, true
	}

	return 0, false
}
