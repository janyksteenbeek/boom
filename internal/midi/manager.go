package midi

import (
	"fmt"
	"log"
	"sync"

	"gitlab.com/gomidi/midi/v2"
	"gitlab.com/gomidi/midi/v2/drivers"
	_ "gitlab.com/gomidi/midi/v2/drivers/rtmididrv"

	"github.com/janyksteenbeek/boom/internal/controller"
	"github.com/janyksteenbeek/boom/internal/event"
)

// Manager handles MIDI device discovery, lifecycle, and message routing.
type Manager struct {
	bus      *event.Bus
	mapper   *controller.Mapper
	ledMgr   *controller.LEDManager
	inPorts  []drivers.In
	outPorts []drivers.Out
	stopFns  []func()
	mu       sync.Mutex
}

// NewManager creates a new MIDI manager.
func NewManager(bus *event.Bus) *Manager {
	return &Manager{
		bus: bus,
	}
}

// SetMapper sets the controller mapper for incoming MIDI messages.
func (m *Manager) SetMapper(mapper *controller.Mapper) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mapper = mapper
}

// SetLEDManager sets the LED manager for outgoing MIDI messages.
func (m *Manager) SetLEDManager(ledMgr *controller.LEDManager) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ledMgr = ledMgr
}

// ListInputs returns available MIDI input port names.
func (m *Manager) ListInputs() []string {
	ins := midi.GetInPorts()
	names := make([]string, len(ins))
	for i, in := range ins {
		names[i] = in.String()
	}
	return names
}

// ListOutputs returns available MIDI output port names.
func (m *Manager) ListOutputs() []string {
	outs := midi.GetOutPorts()
	names := make([]string, len(outs))
	for i, out := range outs {
		names[i] = out.String()
	}
	return names
}

// Start opens all available MIDI inputs and begins listening.
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ins := midi.GetInPorts()
	if len(ins) == 0 {
		log.Println("no MIDI input ports found")
		return nil
	}

	for _, in := range ins {
		log.Printf("opening MIDI input: %s", in.String())
		stop, err := midi.ListenTo(in, m.handleMessage)
		if err != nil {
			log.Printf("failed to open MIDI input %s: %v", in.String(), err)
			continue
		}
		m.inPorts = append(m.inPorts, in)
		m.stopFns = append(m.stopFns, stop)
	}

	// Open output ports
	outs := midi.GetOutPorts()
	for _, out := range outs {
		log.Printf("found MIDI output: %s", out.String())
		m.outPorts = append(m.outPorts, out)
	}

	return nil
}

// Stop closes all MIDI connections.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, stop := range m.stopFns {
		stop()
	}
	m.stopFns = nil
	m.inPorts = nil
	m.outPorts = nil

	midi.CloseDriver()
}

// SendMIDI sends a raw MIDI message to the first output port.
func (m *Manager) SendMIDI(status, data1, data2 uint8) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.outPorts) == 0 {
		return fmt.Errorf("no MIDI output ports available")
	}

	out := m.outPorts[0]
	sender, err := midi.SendTo(out)
	if err != nil {
		return fmt.Errorf("send to %s: %w", out.String(), err)
	}

	// Construct the appropriate MIDI message based on status byte type
	var msg midi.Message
	msgType := status & 0xF0
	channel := status & 0x0F

	switch msgType {
	case 0x90: // Note On
		msg = midi.NoteOn(channel, data1, data2)
	case 0x80: // Note Off
		msg = midi.NoteOff(channel, data1)
	case 0xB0: // Control Change
		msg = midi.ControlChange(channel, data1, data2)
	default:
		msg = midi.Message([]byte{status, data1, data2})
	}

	return sender(msg)
}

func (m *Manager) handleMessage(msg midi.Message, timestampms int32) {
	m.mu.Lock()
	mapper := m.mapper
	m.mu.Unlock()

	if mapper == nil {
		return
	}

	var channel, data1, data2 uint8

	switch {
	case msg.GetNoteOn(&channel, &data1, &data2):
		log.Printf("MIDI: NoteOn ch=%d note=%d vel=%d", channel, data1, data2)
		mapper.HandleNoteOn(channel, data1, data2)
	case msg.GetNoteOff(&channel, &data1, &data2):
		mapper.HandleNoteOff(channel, data1)
	case msg.GetControlChange(&channel, &data1, &data2):
		// Only log non-jog CC (jog wheels spam too much)
		if data1 != 33 && data1 != 34 && data1 != 41 {
			log.Printf("MIDI: CC ch=%d cc=%d val=%d", channel, data1, data2)
		}
		mapper.HandleCC(channel, data1, data2)
	}
}
