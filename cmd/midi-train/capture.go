package main

import (
	"sync"

	"gitlab.com/gomidi/midi/v2"
)

// MIDIMessage represents a captured MIDI message.
type MIDIMessage struct {
	Channel uint8  `json:"channel"`
	Status  string `json:"status"` // "note" or "cc"
	Number  uint8  `json:"number"`
	Value   uint8  `json:"value"`
}

// LearnedControl represents a fully trained MIDI control.
type LearnedControl struct {
	Name     string        `json:"name"`
	Type     string        `json:"type"` // "button", "fader", "knob", "encoder"
	Status   string        `json:"status"`
	Channel  uint8         `json:"channel"`
	Number   uint8         `json:"number"`
	MinValue uint8         `json:"min_value"`
	MaxValue uint8         `json:"max_value"`
	Sample   []MIDIMessage `json:"sample"`
}

// TrainResult is the final JSON output.
type TrainResult struct {
	Timestamp string           `json:"timestamp"`
	Controls  []LearnedControl `json:"controls"`
}

var (
	mu        sync.Mutex
	messages  []MIDIMessage
	capturing bool
)

func pushMessage(msg MIDIMessage) {
	mu.Lock()
	defer mu.Unlock()
	if capturing {
		messages = append(messages, msg)
	}
}

func startCapture() {
	mu.Lock()
	defer mu.Unlock()
	messages = nil
	capturing = true
}

func stopCapture() []MIDIMessage {
	mu.Lock()
	defer mu.Unlock()
	capturing = false
	result := make([]MIDIMessage, len(messages))
	copy(result, messages)
	return result
}

// handleMIDI is the gomidi callback for incoming MIDI on any open input port.
// Note On and Control Change messages are stored in the capture buffer when
// capturing is active; everything else is dropped.
func handleMIDI(msg midi.Message, _ int32) {
	var channel, data1, data2 uint8
	switch {
	case msg.GetNoteOn(&channel, &data1, &data2):
		pushMessage(MIDIMessage{Channel: channel, Status: "note", Number: data1, Value: data2})
	case msg.GetControlChange(&channel, &data1, &data2):
		pushMessage(MIDIMessage{Channel: channel, Status: "cc", Number: data1, Value: data2})
	}
}
