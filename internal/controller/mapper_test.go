package controller

import (
	"testing"
)

func TestCompileAndLookup(t *testing.T) {
	cfg := ControllerConfig{
		Controller: ControllerInfo{
			Name:      "Test Controller",
			DeckCount: 2,
		},
		DeckCh: map[string]ChannelPair{
			"deck1": {Note: 0x90, CC: 0xB0},
			"deck2": {Note: 0x91, CC: 0xB1},
		},
		Decks: DeckMappings{
			Transport: []ControlMapping{
				{
					Name:   "play",
					Type:   "button",
					MIDI:   MIDIAddress{Status: "note", Number: 0x0B},
					Action: "play_pause",
				},
			},
			Faders: []ControlMapping{
				{
					Name:   "volume",
					Type:   "fader",
					MIDI:   MIDIAddress{Status: "cc", Number: 0x13},
					Action: "volume",
				},
			},
		},
		Global: GlobalMappings{
			Channel: ChannelPair{Note: 0x96, CC: 0xB6},
			Controls: []ControlMapping{
				{
					Name:   "crossfader",
					Type:   "fader",
					MIDI:   MIDIAddress{Status: "cc", Number: 0x1F},
					Action: "crossfader",
				},
			},
		},
	}

	compiled, err := Compile(cfg)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	// Deck 1 play button
	key := MIDIKey{Channel: 0x00, Status: 0x90, Number: 0x0B}
	rm, ok := compiled.InputMap[key]
	if !ok {
		t.Fatal("deck 1 play button not found in input map")
	}
	if rm.Deck != 1 {
		t.Errorf("expected deck 1, got %d", rm.Deck)
	}
	if rm.Actions[0] != "play_pause" {
		t.Errorf("expected action play_pause, got %s", rm.Actions[0])
	}

	// Deck 2 play button
	key2 := MIDIKey{Channel: 0x01, Status: 0x90, Number: 0x0B}
	rm2, ok := compiled.InputMap[key2]
	if !ok {
		t.Fatal("deck 2 play button not found in input map")
	}
	if rm2.Deck != 2 {
		t.Errorf("expected deck 2, got %d", rm2.Deck)
	}

	// Global crossfader
	keyGlobal := MIDIKey{Channel: 0x06, Status: 0xB0, Number: 0x1F}
	rmGlobal, ok := compiled.InputMap[keyGlobal]
	if !ok {
		t.Fatal("global crossfader not found in input map")
	}
	if rmGlobal.Deck != 0 {
		t.Errorf("expected deck 0 (global), got %d", rmGlobal.Deck)
	}
}

func TestExpandControl(t *testing.T) {
	ctrl := ControlMapping{
		Name:  "pad_{{n}}",
		Type:  "button",
		Count: 4,
		MIDI: MIDIAddress{
			Status:      "note",
			NumberStart: 0x00,
			NumberStep:  1,
		},
		Action: "hotcue_{{n}}",
	}

	expanded := expandControl(ctrl)
	if len(expanded) != 4 {
		t.Fatalf("expected 4 expanded controls, got %d", len(expanded))
	}

	for i, c := range expanded {
		expectedName := "pad_" + string(rune('0'+i+1))
		if c.MIDI.Number != uint8(i) {
			t.Errorf("pad %d: expected MIDI number %d, got %d", i+1, i, c.MIDI.Number)
		}
		_ = expectedName
	}

	if expanded[0].Action != "hotcue_1" {
		t.Errorf("expected action hotcue_1, got %s", expanded[0].Action)
	}
	if expanded[3].Action != "hotcue_4" {
		t.Errorf("expected action hotcue_4, got %s", expanded[3].Action)
	}
}

func TestHighResState(t *testing.T) {
	hrs := NewHighResState()

	// Set MSB first
	hrs.SetMSB(0x40) // 64

	// Combine with LSB
	combined, valid := hrs.CombineWithLSB(0x20) // 32
	if !valid {
		t.Fatal("expected valid combination")
	}

	expected := uint16(0x40<<7 | 0x20) // 64*128 + 32 = 8224
	if combined != expected {
		t.Errorf("expected %d, got %d", expected, combined)
	}

	// Second combination without MSB should be invalid
	_, valid = hrs.CombineWithLSB(0x10)
	if valid {
		t.Error("expected invalid combination without MSB")
	}
}

func TestLayerState(t *testing.T) {
	ls := NewLayerState()

	if ls.ActiveLayer(1) != 0 {
		t.Error("default layer should be 0")
	}

	ls.SetLayer(1, 1)
	if ls.ActiveLayer(1) != 1 {
		t.Error("layer should be 1")
	}

	// Deck 2 should be independent
	if ls.ActiveLayer(2) != 0 {
		t.Error("deck 2 should still be on layer 0")
	}

	ls.ResetLayer(1)
	if ls.ActiveLayer(1) != 0 {
		t.Error("layer should be reset to 0")
	}

	ls.ToggleLayer(1, 1)
	if ls.ActiveLayer(1) != 1 {
		t.Error("toggle should set layer to 1")
	}
	ls.ToggleLayer(1, 1)
	if ls.ActiveLayer(1) != 0 {
		t.Error("toggle should set layer back to 0")
	}
}

func TestSoftTakeoverFilter(t *testing.T) {
	so := NewSoftTakeoverState(0.05)
	so.SetSoftwareValue(0.5)

	// Value far from software value should be rejected
	_, ok := so.Filter(0.0)
	if ok {
		t.Error("expected value to be rejected (too far)")
	}

	// Value close to software value should engage
	val, ok := so.Filter(0.48)
	if !ok {
		t.Error("expected value to be accepted (close enough)")
	}
	if val != 0.48 {
		t.Errorf("expected 0.48, got %f", val)
	}

	// Subsequent values should pass through
	val, ok = so.Filter(0.3)
	if !ok {
		t.Error("expected engaged value to pass through")
	}
	if val != 0.3 {
		t.Errorf("expected 0.3, got %f", val)
	}
}
