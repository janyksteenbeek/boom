package model

import "time"

// DeckState represents the observable state of a deck for UI rendering.
type DeckState struct {
	ID            int
	Track         *Track
	Playing       bool
	Position      float64 // 0.0 to 1.0
	PositionTime  time.Duration
	Volume        float64 // 0.0 to 1.0
	Tempo         float64 // Relative: -1.0 to +1.0
	EQHigh        float64
	EQMid         float64
	EQLow         float64
	WaveformReady bool
}
