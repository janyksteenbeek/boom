package controller

import (
	"fmt"
	"log"
)

// MIDIKey uniquely identifies a MIDI message for O(1) lookup.
type MIDIKey struct {
	Channel uint8
	Status  uint8 // 0x90=NoteOn, 0xB0=CC
	Number  uint8
}

// ResolvedMapping is a fully resolved, per-deck mapping ready for runtime dispatch.
type ResolvedMapping struct {
	Deck    int
	Control ControlMapping
	Actions map[int]string // layerID -> action name
}

// CompiledMapping is the runtime-optimized representation.
type CompiledMapping struct {
	Config      ControllerConfig
	InputMap    map[MIDIKey]*ResolvedMapping
	HighResMap  map[MIDIKey]*HighResState // MSB CC -> state
	LSBToMSB    map[MIDIKey]MIDIKey       // LSB CC -> MSB CC key
	LEDBindings []LEDBinding
}

// Mapper dispatches incoming MIDI messages to action handlers.
type Mapper struct {
	compiled *CompiledMapping
	registry *ActionRegistry
	layers   *LayerState
	softover map[string]*SoftTakeoverState // key: "deck:action"
}

// NewMapper creates a new MIDI mapper from a compiled mapping.
func NewMapper(compiled *CompiledMapping, registry *ActionRegistry) *Mapper {
	return &Mapper{
		compiled: compiled,
		registry: registry,
		layers:   NewLayerState(),
		softover: make(map[string]*SoftTakeoverState),
	}
}

// HandleNoteOn processes a MIDI Note On message.
func (m *Mapper) HandleNoteOn(channel, note, velocity uint8) {
	// NoteOn with velocity 0 == NoteOff in the MIDI spec. Many controllers
	// only ever send these instead of true NoteOff messages, so forward them
	// to the release path.
	if velocity == 0 {
		m.HandleNoteOff(channel, note)
		return
	}

	key := MIDIKey{Channel: channel, Status: 0x90, Number: note}
	rm := m.compiled.InputMap[key]
	if rm == nil {
		log.Printf("MIDI: unmapped note ch=%d n=%d v=%d", channel, note, velocity)
		return
	}
	log.Printf("MIDI: note ch=%d n=%d v=%d → action=%s deck=%d", channel, note, velocity, rm.Actions[0], rm.Deck)

	if m.handleLayerActivator(channel, note, rm.Deck, true) {
		return
	}

	action := m.resolveAction(rm)
	m.dispatch(action, rm.Deck, true, 0, float64(velocity)/127.0)
}

// HandleNoteOff processes a MIDI Note Off message.
func (m *Mapper) HandleNoteOff(channel, note uint8) {
	key := MIDIKey{Channel: channel, Status: 0x90, Number: note}
	rm := m.compiled.InputMap[key]
	if rm == nil {
		return
	}

	if m.handleLayerActivator(channel, note, rm.Deck, false) {
		return
	}

	action := m.resolveAction(rm)
	m.dispatch(action, rm.Deck, false, 0, 0)
}

// HandleCC processes a MIDI Control Change message.
func (m *Mapper) HandleCC(channel, cc, value uint8) {
	key := MIDIKey{Channel: channel, Status: 0xB0, Number: cc}

	// 14-bit pair: LSB → combine with stored MSB and dispatch the high-res value.
	if msbKey, ok := m.compiled.LSBToMSB[key]; ok {
		m.handleHighResLSB(msbKey, value)
		return
	}

	// 14-bit pair: MSB → store and wait for the matching LSB.
	if hrState := m.compiled.HighResMap[key]; hrState != nil {
		hrState.SetMSB(value)
		return
	}

	rm := m.compiled.InputMap[key]
	if rm == nil {
		return
	}

	action := m.resolveAction(rm)

	switch rm.Control.Type {
	case "encoder":
		delta := m.decodeEncoder(value, rm.Control.Encoder)
		m.dispatch(action, rm.Deck, true, delta, 0)

	case "fader", "knob":
		normalized := normalizedCC(value, rm.Control.Range)
		m.dispatchContinuous(action, rm.Deck, normalized, &rm.Control)

	case "button":
		m.dispatch(action, rm.Deck, value > 0, 0, float64(value)/127.0)
	}
}

// handleLayerActivator checks whether the incoming note (press/release) is a
// layer activator for `deck`, and if so flips the layer state. Returns true
// when the message was consumed and the caller should not dispatch an action.
func (m *Mapper) handleLayerActivator(channel, note uint8, deck int, pressed bool) bool {
	for _, layer := range m.compiled.Config.Layers {
		if layer.Activator == nil || layer.Activator.Type != "note" {
			continue
		}
		// Releases only matter for momentary activators — toggle layers stay
		// latched until the next press.
		if !pressed && layer.Activator.Mode != "momentary" {
			continue
		}
		activatorCh := layer.Activator.Channel
		if layer.PerDeck {
			if ch, ok := layer.PerDeckCh[fmt.Sprintf("deck%d", deck)]; ok {
				activatorCh = ch
			}
		}
		if channel != activatorCh || note != layer.Activator.Number {
			continue
		}
		if pressed {
			if layer.Activator.Mode == "momentary" {
				m.layers.SetLayer(deck, layer.ID)
			} else {
				m.layers.ToggleLayer(deck, layer.ID)
			}
		} else {
			m.layers.ResetLayer(deck)
		}
		return true
	}
	return false
}

// handleHighResLSB combines the incoming LSB with the stored MSB and, if the
// pair forms a valid 14-bit value, dispatches it as a continuous control.
func (m *Mapper) handleHighResLSB(msbKey MIDIKey, lsbValue uint8) {
	hrState := m.compiled.HighResMap[msbKey]
	if hrState == nil {
		return
	}
	combined, valid := hrState.CombineWithLSB(lsbValue)
	if !valid {
		return
	}
	rm := m.compiled.InputMap[msbKey]
	if rm == nil {
		return
	}
	normalized := float64(combined) / 16383.0
	if rm.Control.Range != nil && rm.Control.Range.Inverted {
		normalized = 1.0 - normalized
	}
	action := m.resolveAction(rm)
	m.dispatchContinuous(action, rm.Deck, normalized, &rm.Control)
}

// normalizedCC converts a 7-bit CC value into a 0..1 fraction, honoring an
// optional inverted range.
func normalizedCC(value uint8, r *RangeConfig) float64 {
	n := float64(value) / 127.0
	if r != nil && r.Inverted {
		n = 1.0 - n
	}
	return n
}
