package controller

import "sync"

// LayerState tracks the active layer per deck.
type LayerState struct {
	mu     sync.RWMutex
	active map[int]int // deck -> active layer ID
}

// NewLayerState creates a new layer state tracker.
func NewLayerState() *LayerState {
	return &LayerState{
		active: make(map[int]int),
	}
}

// ActiveLayer returns the active layer ID for a deck.
func (s *LayerState) ActiveLayer(deck int) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active[deck]
}

// SetLayer sets the active layer for a deck.
func (s *LayerState) SetLayer(deck int, layerID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active[deck] = layerID
}

// ResetLayer resets a deck back to the base layer (0).
func (s *LayerState) ResetLayer(deck int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active[deck] = 0
}

// ToggleLayer toggles between the base layer and the given layer.
func (s *LayerState) ToggleLayer(deck int, layerID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active[deck] == layerID {
		s.active[deck] = 0
	} else {
		s.active[deck] = layerID
	}
}
