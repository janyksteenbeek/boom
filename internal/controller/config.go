package controller

// ControllerConfig is the top-level structure parsed from a YAML mapping file.
type ControllerConfig struct {
	Controller ControllerInfo            `yaml:"controller"`
	Defaults   Defaults                  `yaml:"defaults"`
	Layers     []LayerDef                `yaml:"layers"`
	DeckCh     map[string]ChannelPair    `yaml:"deck_channels"`
	Decks      DeckMappings              `yaml:"decks"`
	Global     GlobalMappings            `yaml:"global"`
	LEDOutput  []LEDOutputDef            `yaml:"led_output"`
}

// ControllerInfo identifies the hardware.
type ControllerInfo struct {
	Name      string `yaml:"name"`
	Vendor    string `yaml:"vendor"`
	ProductID string `yaml:"product_id"`
	USB       USBIDs `yaml:"usb"`
	DeckCount int    `yaml:"deck_count"`
}

// USBIDs holds USB vendor/product identifiers for auto-detection.
type USBIDs struct {
	VendorID  uint16 `yaml:"vendor_id"`
	ProductID uint16 `yaml:"product_id"`
}

// Defaults contains global fallback values.
type Defaults struct {
	ButtonDebounceMs      int     `yaml:"button_debounce_ms"`
	SoftTakeoverThreshold float64 `yaml:"soft_takeover_threshold"`
}

// ChannelPair holds the note and CC channels for a deck.
type ChannelPair struct {
	Note uint8 `yaml:"note"`
	CC   uint8 `yaml:"cc"`
}

// LayerDef defines a modifier layer (base, shift, etc.).
type LayerDef struct {
	Name      string          `yaml:"name"`
	ID        int             `yaml:"id"`
	Activator *LayerActivator `yaml:"activator,omitempty"`
	PerDeck   bool            `yaml:"per_deck"`
	PerDeckCh map[string]uint8 `yaml:"per_deck_channels,omitempty"`
}

// LayerActivator defines how a layer is activated.
type LayerActivator struct {
	Type    string `yaml:"type"`    // "note" or "cc"
	Channel uint8  `yaml:"channel"`
	Number  uint8  `yaml:"number"`
	Mode    string `yaml:"mode"`    // "momentary" or "toggle"
}

// DeckMappings groups all per-deck control sections.
type DeckMappings struct {
	Transport []ControlMapping `yaml:"transport"`
	Jog       []ControlMapping `yaml:"jog"`
	Loops     []ControlMapping `yaml:"loops"`
	Pads      []ControlMapping `yaml:"pads"`
	Faders    []ControlMapping `yaml:"faders"`
	EQ        []ControlMapping `yaml:"eq"`
	Headphone []ControlMapping `yaml:"headphone"`
}

// GlobalMappings holds controls not tied to a specific deck.
type GlobalMappings struct {
	Channel  ChannelPair      `yaml:"channel"`
	Controls []ControlMapping `yaml:"controls"`
}

// ControlMapping represents a single physical control on the device.
type ControlMapping struct {
	Name         string                   `yaml:"name"`
	Type         string                   `yaml:"type"` // "button", "fader", "knob", "encoder"
	Count        int                      `yaml:"count,omitempty"`
	MIDI         MIDIAddress              `yaml:"midi"`
	HighRes      *HighResConfig           `yaml:"highres,omitempty"`
	Action       string                   `yaml:"action"`
	Encoder      *EncoderConfig           `yaml:"encoder,omitempty"`
	Range        *RangeConfig             `yaml:"range,omitempty"`
	SoftTakeover bool                     `yaml:"soft_takeover,omitempty"`
	LED          *LEDConfig               `yaml:"led,omitempty"`
	Layers       map[string]LayerOverride `yaml:"layers,omitempty"`
}

// MIDIAddress identifies the MIDI message for input.
type MIDIAddress struct {
	Status      string `yaml:"status"` // "note" or "cc"
	Number      uint8  `yaml:"number"`
	NumberStart uint8  `yaml:"number_start,omitempty"`
	NumberStep  uint8  `yaml:"number_step,omitempty"`
}

// HighResConfig defines 14-bit MSB/LSB pairing.
type HighResConfig struct {
	Enabled bool  `yaml:"enabled"`
	MSB     uint8 `yaml:"msb"`
	LSB     uint8 `yaml:"lsb"`
}

// EncoderConfig defines relative encoder behavior.
type EncoderConfig struct {
	Mode        string  `yaml:"mode"` // "relative_twos_complement", "relative_offset"
	Sensitivity float64 `yaml:"sensitivity"`
}

// RangeConfig defines the value range for continuous controls.
type RangeConfig struct {
	Min          int    `yaml:"min"`
	Max          int    `yaml:"max"`
	Inverted     bool   `yaml:"inverted,omitempty"`
	Center       *int   `yaml:"center,omitempty"`
	CenterAction string `yaml:"center_action,omitempty"`
}

// LayerOverride replaces the action when a layer is active.
type LayerOverride struct {
	Action string     `yaml:"action"`
	LED    *LEDConfig `yaml:"led,omitempty"`
}

// LEDConfig defines output MIDI for a control's LED.
type LEDConfig struct {
	Status      string `yaml:"status"` // "note" or "cc"
	Number      uint8  `yaml:"number"`
	OnValue     uint8  `yaml:"on_value"`
	OffValue    uint8  `yaml:"off_value"`
	NumberStart uint8  `yaml:"number_start,omitempty"`
	NumberStep  uint8  `yaml:"number_step,omitempty"`
}

// LEDOutputDef defines standalone output-only LED mappings.
type LEDOutputDef struct {
	Name    string      `yaml:"name"`
	PerDeck bool        `yaml:"per_deck"`
	MIDI    MIDIAddress `yaml:"midi"`
	Source  string      `yaml:"source"`
	Mode    string      `yaml:"mode"` // "boolean", "range"
	Range   *RangeConfig `yaml:"range,omitempty"`
}
