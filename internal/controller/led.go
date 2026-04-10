package controller

// LEDBinding represents a connection between an action state and a MIDI output.
type LEDBinding struct {
	Deck      int
	Action    string
	Channel   uint8
	StatusByte uint8 // 0x90 for Note, 0xB0 for CC
	Number    uint8
	OnValue   uint8
	OffValue  uint8
}

// LEDManager sends LED state updates to MIDI output.
type LEDManager struct {
	bindings []LEDBinding
	sendFn   func(status, data1, data2 uint8)
}

// NewLEDManager creates a new LED manager.
func NewLEDManager(sendFn func(status, data1, data2 uint8)) *LEDManager {
	return &LEDManager{
		sendFn: sendFn,
	}
}

// AddBinding adds a LED binding.
func (m *LEDManager) AddBinding(b LEDBinding) {
	m.bindings = append(m.bindings, b)
}

// Update sends LED state for a given action.
func (m *LEDManager) Update(action string, deck int, on bool) {
	if m.sendFn == nil {
		return
	}
	for _, b := range m.bindings {
		if b.Action == action && b.Deck == deck {
			val := b.OffValue
			if on {
				val = b.OnValue
			}
			m.sendFn(b.StatusByte|b.Channel, b.Number, val)
		}
	}
}

// ClearAll turns off all LEDs.
func (m *LEDManager) ClearAll() {
	if m.sendFn == nil {
		return
	}
	for _, b := range m.bindings {
		m.sendFn(b.StatusByte|b.Channel, b.Number, b.OffValue)
	}
}
