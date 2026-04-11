package controller

import (
	"fmt"
	"log"
	"strings"
)

// MIDIKey uniquely identifies a MIDI message for O(1) lookup.
type MIDIKey struct {
	Channel uint8
	Status  uint8 // 0x90=NoteOn, 0xB0=CC
	Number  uint8
}

// ResolvedMapping is a fully resolved, per-deck mapping ready for runtime dispatch.
type ResolvedMapping struct {
	Deck     int
	Control  ControlMapping
	Actions  map[int]string // layerID -> action name
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
	compiled  *CompiledMapping
	registry  *ActionRegistry
	layers    *LayerState
	softover  map[string]*SoftTakeoverState // key: "deck:action"
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
	// NoteOn with velocity 0 = NoteOff in MIDI spec. Ignore for buttons.
	if velocity == 0 {
		return
	}

	key := MIDIKey{Channel: channel, Status: 0x90, Number: note}
	rm := m.compiled.InputMap[key]
	if rm == nil {
		return
	}

	// Check if this is a layer activator
	for _, layer := range m.compiled.Config.Layers {
		if layer.Activator != nil && layer.Activator.Type == "note" {
			activatorCh := layer.Activator.Channel
			if layer.PerDeck {
				if ch, ok := layer.PerDeckCh[fmt.Sprintf("deck%d", rm.Deck)]; ok {
					activatorCh = ch
				}
			}
			if channel == activatorCh && note == layer.Activator.Number {
				if layer.Activator.Mode == "momentary" {
					m.layers.SetLayer(rm.Deck, layer.ID)
				} else {
					m.layers.ToggleLayer(rm.Deck, layer.ID)
				}
				return
			}
		}
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

	// Check layer deactivation (momentary)
	for _, layer := range m.compiled.Config.Layers {
		if layer.Activator != nil && layer.Activator.Type == "note" && layer.Activator.Mode == "momentary" {
			activatorCh := layer.Activator.Channel
			if layer.PerDeck {
				if ch, ok := layer.PerDeckCh[fmt.Sprintf("deck%d", rm.Deck)]; ok {
					activatorCh = ch
				}
			}
			if channel == activatorCh && note == layer.Activator.Number {
				m.layers.ResetLayer(rm.Deck)
				return
			}
		}
	}

	action := m.resolveAction(rm)
	m.dispatch(action, rm.Deck, false, 0, 0)
}

// HandleCC processes a MIDI Control Change message.
func (m *Mapper) HandleCC(channel, cc, value uint8) {
	key := MIDIKey{Channel: channel, Status: 0xB0, Number: cc}

	// Check if this is the LSB of a 14-bit pair
	if msbKey, ok := m.compiled.LSBToMSB[key]; ok {
		if hrState := m.compiled.HighResMap[msbKey]; hrState != nil {
			combined, valid := hrState.CombineWithLSB(value)
			if valid {
				if rm := m.compiled.InputMap[msbKey]; rm != nil {
					normalized := float64(combined) / 16383.0
					if rm.Control.Range != nil && rm.Control.Range.Inverted {
						normalized = 1.0 - normalized
					}
					action := m.resolveAction(rm)
					m.dispatchContinuous(action, rm.Deck, normalized, &rm.Control)
				}
			}
		}
		return
	}

	// Check if this is the MSB of a 14-bit pair
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
		normalized := float64(value) / 127.0
		if rm.Control.Range != nil && rm.Control.Range.Inverted {
			normalized = 1.0 - normalized
		}
		m.dispatchContinuous(action, rm.Deck, normalized, &rm.Control)

	case "button":
		m.dispatch(action, rm.Deck, value > 0, 0, float64(value)/127.0)
	}
}

func (m *Mapper) resolveAction(rm *ResolvedMapping) string {
	activeLayer := m.layers.ActiveLayer(rm.Deck)
	if action, ok := rm.Actions[activeLayer]; ok {
		return action
	}
	return rm.Actions[0] // Fall back to base layer
}

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

func (m *Mapper) dispatchContinuous(action string, deck int, normalized float64, ctrl *ControlMapping) {
	// Apply soft takeover if enabled
	if ctrl.SoftTakeover {
		soKey := fmt.Sprintf("%d:%s", deck, action)
		so := m.softover[soKey]
		if so == nil {
			threshold := 0.05
			so = NewSoftTakeoverState(threshold)
			m.softover[soKey] = so
		}
		filtered, ok := so.Filter(normalized)
		if !ok {
			return // Value rejected by soft takeover
		}
		normalized = filtered
	}

	m.dispatch(action, deck, true, 0, normalized)
}

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

// Compile builds a CompiledMapping from a ControllerConfig.
func Compile(cfg ControllerConfig) (*CompiledMapping, error) {
	cm := &CompiledMapping{
		Config:     cfg,
		InputMap:   make(map[MIDIKey]*ResolvedMapping),
		HighResMap: make(map[MIDIKey]*HighResState),
		LSBToMSB:   make(map[MIDIKey]MIDIKey),
	}

	// Process deck mappings for each deck
	for deckName, ch := range cfg.DeckCh {
		deckNum := 0
		if _, err := fmt.Sscanf(deckName, "deck%d", &deckNum); err != nil {
			continue
		}

		allControls := collectDeckControls(cfg.Decks)
		for _, ctrl := range allControls {
			expanded := expandControl(ctrl)
			for _, c := range expanded {
				statusByte := statusForType(c.MIDI.Status, ch)
				key := MIDIKey{
					Channel: statusByte & 0x0F,
					Status:  statusByte & 0xF0,
					Number:  c.MIDI.Number,
				}

				rm := &ResolvedMapping{
					Deck:    deckNum,
					Control: c,
					Actions: buildActions(c),
				}
				cm.InputMap[key] = rm

				// Register 14-bit high-res
				if c.HighRes != nil && c.HighRes.Enabled {
					msbKey := MIDIKey{Channel: statusByte & 0x0F, Status: 0xB0, Number: c.HighRes.MSB}
					lsbKey := MIDIKey{Channel: statusByte & 0x0F, Status: 0xB0, Number: c.HighRes.LSB}
					cm.HighResMap[msbKey] = NewHighResState()
					cm.LSBToMSB[lsbKey] = msbKey
					cm.InputMap[msbKey] = rm
				}

				// Register LED bindings
				if c.LED != nil {
					binding := LEDBinding{
						Deck:       deckNum,
						Action:     c.Action,
						Channel:    statusByte & 0x0F,
						StatusByte: ledStatusByte(c.LED.Status),
						Number:     c.LED.Number,
						OnValue:    c.LED.OnValue,
						OffValue:   c.LED.OffValue,
					}
					cm.LEDBindings = append(cm.LEDBindings, binding)
				}
			}
		}
	}

	// Process global controls
	for _, ctrl := range cfg.Global.Controls {
		expanded := expandControl(ctrl)
		for _, c := range expanded {
			statusByte := statusForType(c.MIDI.Status, cfg.Global.Channel)
			key := MIDIKey{
				Channel: statusByte & 0x0F,
				Status:  statusByte & 0xF0,
				Number:  c.MIDI.Number,
			}

			rm := &ResolvedMapping{
				Deck:    0, // Global
				Control: c,
				Actions: buildActions(c),
			}
			cm.InputMap[key] = rm

			if c.HighRes != nil && c.HighRes.Enabled {
				msbKey := MIDIKey{Channel: statusByte & 0x0F, Status: 0xB0, Number: c.HighRes.MSB}
				lsbKey := MIDIKey{Channel: statusByte & 0x0F, Status: 0xB0, Number: c.HighRes.LSB}
				cm.HighResMap[msbKey] = NewHighResState()
				cm.LSBToMSB[lsbKey] = msbKey
				cm.InputMap[msbKey] = rm
			}
		}
	}

	// Process FX controls (separate MIDI channel, DeckID=0 for global FX)
	for _, ctrl := range cfg.FX.Controls {
		expanded := expandControl(ctrl)
		for _, c := range expanded {
			statusByte := statusForType(c.MIDI.Status, cfg.FX.Channel)
			key := MIDIKey{
				Channel: statusByte & 0x0F,
				Status:  statusByte & 0xF0,
				Number:  c.MIDI.Number,
			}

			rm := &ResolvedMapping{
				Deck:    0, // FX are global, routed by UI target
				Control: c,
				Actions: buildActions(c),
			}
			cm.InputMap[key] = rm

			if c.HighRes != nil && c.HighRes.Enabled {
				msbKey := MIDIKey{Channel: statusByte & 0x0F, Status: 0xB0, Number: c.HighRes.MSB}
				lsbKey := MIDIKey{Channel: statusByte & 0x0F, Status: 0xB0, Number: c.HighRes.LSB}
				cm.HighResMap[msbKey] = NewHighResState()
				cm.LSBToMSB[lsbKey] = msbKey
				cm.InputMap[msbKey] = rm
			}
		}
	}

	log.Printf("compiled %d input mappings, %d highres, %d LED bindings",
		len(cm.InputMap), len(cm.HighResMap), len(cm.LEDBindings))

	return cm, nil
}

func collectDeckControls(d DeckMappings) []ControlMapping {
	var all []ControlMapping
	all = append(all, d.Transport...)
	all = append(all, d.Jog...)
	all = append(all, d.Loops...)
	all = append(all, d.Pads...)
	all = append(all, d.Faders...)
	all = append(all, d.EQ...)
	all = append(all, d.Headphone...)
	return all
}

func expandControl(c ControlMapping) []ControlMapping {
	if c.Count <= 0 {
		return []ControlMapping{c}
	}

	var expanded []ControlMapping
	for i := 1; i <= c.Count; i++ {
		ec := c
		ec.Count = 0
		ec.Name = strings.ReplaceAll(c.Name, "{{n}}", fmt.Sprintf("%d", i))
		ec.Action = strings.ReplaceAll(c.Action, "{{n}}", fmt.Sprintf("%d", i))
		ec.MIDI.Number = c.MIDI.NumberStart + uint8(i-1)*c.MIDI.NumberStep

		if ec.LED != nil {
			led := *ec.LED
			if led.NumberStart > 0 || led.NumberStep > 0 {
				led.Number = led.NumberStart + uint8(i-1)*led.NumberStep
			}
			ec.LED = &led
		}

		// Expand layer actions
		if ec.Layers != nil {
			newLayers := make(map[string]LayerOverride)
			for k, v := range ec.Layers {
				v.Action = strings.ReplaceAll(v.Action, "{{n}}", fmt.Sprintf("%d", i))
				newLayers[k] = v
			}
			ec.Layers = newLayers
		}

		expanded = append(expanded, ec)
	}
	return expanded
}

func buildActions(c ControlMapping) map[int]string {
	actions := map[int]string{0: c.Action} // Layer 0 (base)
	if c.Layers != nil {
		layerID := 1
		for _, override := range c.Layers {
			actions[layerID] = override.Action
			layerID++
		}
	}
	return actions
}

func statusForType(statusType string, ch ChannelPair) uint8 {
	switch statusType {
	case "note":
		return ch.Note
	case "cc":
		return ch.CC
	default:
		return ch.CC
	}
}

func ledStatusByte(statusType string) uint8 {
	switch statusType {
	case "note":
		return 0x90
	case "cc":
		return 0xB0
	default:
		return 0x90
	}
}
