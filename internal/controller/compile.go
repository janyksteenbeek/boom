package controller

import (
	"fmt"
	"log"
	"strings"
)

// Compile builds a CompiledMapping from a ControllerConfig.
func Compile(cfg ControllerConfig) (*CompiledMapping, error) {
	cm := &CompiledMapping{
		Config:     cfg,
		InputMap:   make(map[MIDIKey]*ResolvedMapping),
		HighResMap: make(map[MIDIKey]*HighResState),
		LSBToMSB:   make(map[MIDIKey]MIDIKey),
	}

	if err := compileDecks(cm, cfg); err != nil {
		return nil, err
	}
	compileGlobals(cm, cfg)
	compileFX(cm, cfg)

	log.Printf("compiled %d input mappings, %d highres, %d LED bindings",
		len(cm.InputMap), len(cm.HighResMap), len(cm.LEDBindings))

	return cm, nil
}

// compileDecks walks each deck channel mapping in the config and registers
// all controls (with LED bindings and 14-bit pairs) into the compiled output.
func compileDecks(cm *CompiledMapping, cfg ControllerConfig) error {
	for deckName, ch := range cfg.DeckCh {
		deckNum := 0
		if _, err := fmt.Sscanf(deckName, "deck%d", &deckNum); err != nil {
			continue
		}

		allControls := collectDeckControls(cfg.Decks)
		for _, ctrl := range allControls {
			expanded := expandControl(ctrl)
			for _, c := range expanded {
				registerControl(cm, c, ch, deckNum, true)
			}
		}
	}
	return nil
}

// compileGlobals registers controls that are not bound to a specific deck.
func compileGlobals(cm *CompiledMapping, cfg ControllerConfig) {
	for _, ctrl := range cfg.Global.Controls {
		expanded := expandControl(ctrl)
		for _, c := range expanded {
			registerControl(cm, c, cfg.Global.Channel, 0, false)
		}
	}
}

// compileFX registers the FX section's controls. They live on a separate MIDI
// channel and DeckID 0 since the active FX target is decided by the UI.
func compileFX(cm *CompiledMapping, cfg ControllerConfig) {
	for _, ctrl := range cfg.FX.Controls {
		expanded := expandControl(ctrl)
		for _, c := range expanded {
			registerControl(cm, c, cfg.FX.Channel, 0, false)
		}
	}
}

// registerControl writes a single control's input map entries (and optional
// LED binding) into the compiled mapping. wantLED is false for global/FX
// controls, which never carry LED feedback.
func registerControl(cm *CompiledMapping, c ControlMapping, ch ChannelPair, deckNum int, wantLED bool) {
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

	if c.HighRes != nil && c.HighRes.Enabled {
		msbKey := MIDIKey{Channel: statusByte & 0x0F, Status: 0xB0, Number: c.HighRes.MSB}
		lsbKey := MIDIKey{Channel: statusByte & 0x0F, Status: 0xB0, Number: c.HighRes.LSB}
		cm.HighResMap[msbKey] = NewHighResState()
		cm.LSBToMSB[lsbKey] = msbKey
		cm.InputMap[msbKey] = rm
	}

	if wantLED && c.LED != nil {
		cm.LEDBindings = append(cm.LEDBindings, LEDBinding{
			Deck:       deckNum,
			Action:     c.Action,
			Channel:    statusByte & 0x0F,
			StatusByte: ledStatusByte(c.LED.Status),
			Number:     c.LED.Number,
			OnValue:    c.LED.OnValue,
			OffValue:   c.LED.OffValue,
		})
	}
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

// expandControl turns a templated control with Count > 0 into Count concrete
// controls, substituting `{{n}}` in names/actions and stepping MIDI numbers.
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

// buildActions returns the layerID → action map for one control. Layer 0 is
// always the base action; non-base layers are assigned ascending IDs in
// iteration order.
func buildActions(c ControlMapping) map[int]string {
	actions := map[int]string{0: c.Action}
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
