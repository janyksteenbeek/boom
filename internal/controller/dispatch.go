package controller

import (
	"fmt"
	"log"
)

// resolveAction picks the action name for the deck's currently active layer,
// falling back to the base layer if the active layer doesn't override it.
func (m *Mapper) resolveAction(rm *ResolvedMapping) string {
	activeLayer := m.layers.ActiveLayer(rm.Deck)
	if action, ok := rm.Actions[activeLayer]; ok {
		return action
	}
	return rm.Actions[0]
}

// dispatch sends a resolved action through the registry. Used for trigger and
// relative-encoder controls.
func (m *Mapper) dispatch(action string, deck int, pressed bool, delta, value float64) {
	_, handler, ok := m.registry.Lookup(action)
	if !ok {
		log.Printf("unknown action: %s", action)
		return
	}
	handler(ActionContext{
		Action:  action,
		Deck:    deck,
		Pressed: pressed,
		Delta:   delta,
		Value:   value,
		Layer:   m.layerName(deck),
	})
}

// dispatchContinuous sends a normalized 0..1 value through the registry. If
// the control has soft-takeover enabled, the per-deck/per-action state filters
// jumps until the physical control crosses the engine value.
func (m *Mapper) dispatchContinuous(action string, deck int, normalized float64, ctrl *ControlMapping) {
	if ctrl.SoftTakeover {
		soKey := fmt.Sprintf("%d:%s", deck, action)
		so := m.softover[soKey]
		if so == nil {
			so = NewSoftTakeoverState(0.05)
			m.softover[soKey] = so
		}
		filtered, ok := so.Filter(normalized)
		if !ok {
			return
		}
		normalized = filtered
	}

	m.dispatch(action, deck, true, 0, normalized)
}

// decodeEncoder turns a raw 7-bit CC value from a relative encoder into a
// signed delta, applying the encoder's sensitivity multiplier.
func (m *Mapper) decodeEncoder(value uint8, cfg *EncoderConfig) float64 {
	if cfg == nil {
		return 0
	}

	var delta float64
	switch cfg.Mode {
	case "relative_twos_complement":
		if value < 64 {
			delta = float64(value)
		} else {
			delta = float64(value) - 128
		}
	case "relative_offset":
		delta = float64(value) - 64
	default:
		delta = float64(value) - 64
	}

	return delta * cfg.Sensitivity
}

func (m *Mapper) layerName(deck int) string {
	activeID := m.layers.ActiveLayer(deck)
	for _, l := range m.compiled.Config.Layers {
		if l.ID == activeID {
			return l.Name
		}
	}
	return "base"
}
