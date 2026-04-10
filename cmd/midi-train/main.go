package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"gitlab.com/gomidi/midi/v2"
	_ "gitlab.com/gomidi/midi/v2/drivers/rtmididrv"
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
	mu          sync.Mutex
	messages    []MIDIMessage
	capturing   bool
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

func handleMIDI(msg midi.Message, _ int32) {
	var channel, data1, data2 uint8
	switch {
	case msg.GetNoteOn(&channel, &data1, &data2):
		pushMessage(MIDIMessage{Channel: channel, Status: "note", Number: data1, Value: data2})
	case msg.GetControlChange(&channel, &data1, &data2):
		pushMessage(MIDIMessage{Channel: channel, Status: "cc", Number: data1, Value: data2})
	}
}

func waitEnter(reader *bufio.Reader) {
	reader.ReadString('\n')
}

func clearScreen() {
	fmt.Print("\033[2J\033[H")
}

func printHeader() {
	fmt.Println("╔══════════════════════════════════════════╗")
	fmt.Println("║        🎛️  BOOM MIDI TRAINER  🎛️         ║")
	fmt.Println("╚══════════════════════════════════════════╝")
	fmt.Println()
}

func detectType(minMsgs, maxMsgs []MIDIMessage) string {
	if len(minMsgs) == 0 && len(maxMsgs) == 0 {
		return "button"
	}

	// Check if the primary control uses notes → button
	var primary MIDIMessage
	if len(maxMsgs) > 0 {
		primary = maxMsgs[0]
	} else if len(minMsgs) > 0 {
		primary = minMsgs[0]
	}

	if primary.Status == "note" {
		return "button"
	}

	// CC-based: determine by how many distinct values we saw
	valueSet := make(map[uint8]bool)
	for _, m := range maxMsgs {
		if m.Status == primary.Status && m.Number == primary.Number && m.Channel == primary.Channel {
			valueSet[m.Value] = true
		}
	}

	if len(valueSet) <= 3 {
		return "button" // Toggle-style CC
	}

	return "fader" // Could be fader or knob, user named it
}

func dominantMessage(msgs []MIDIMessage) *MIDIMessage {
	if len(msgs) == 0 {
		return nil
	}

	// Count occurrences of each (channel, status, number) combo
	type key struct {
		ch     uint8
		status string
		num    uint8
	}
	counts := make(map[key]int)
	for _, m := range msgs {
		k := key{m.Channel, m.Status, m.Number}
		counts[k]++
	}

	// Find the most frequent
	var bestKey key
	bestCount := 0
	for k, c := range counts {
		if c > bestCount {
			bestKey = k
			bestCount = c
		}
	}

	// Get min and max value for this key
	var minVal uint8 = 127
	var maxVal uint8 = 0
	for _, m := range msgs {
		if m.Channel == bestKey.ch && m.Status == bestKey.status && m.Number == bestKey.num {
			if m.Value < minVal {
				minVal = m.Value
			}
			if m.Value > maxVal {
				maxVal = m.Value
			}
		}
	}

	return &MIDIMessage{
		Channel: bestKey.ch,
		Status:  bestKey.status,
		Number:  bestKey.num,
		Value:   maxVal,
	}
}

func main() {
	reader := bufio.NewReader(os.Stdin)

	clearScreen()
	printHeader()

	// Open MIDI inputs
	ins := midi.GetInPorts()
	if len(ins) == 0 {
		fmt.Println("Geen MIDI-apparaten gevonden. Sluit een controller aan en probeer opnieuw.")
		os.Exit(1)
	}

	fmt.Println("Gevonden MIDI-apparaten:")
	for i, in := range ins {
		fmt.Printf("  [%d] %s\n", i+1, in.String())
	}
	fmt.Println()

	var stopFns []func()
	for _, in := range ins {
		stop, err := midi.ListenTo(in, handleMIDI)
		if err != nil {
			fmt.Printf("  Waarschuwing: kon %s niet openen: %v\n", in.String(), err)
			continue
		}
		stopFns = append(stopFns, stop)
	}
	defer func() {
		for _, stop := range stopFns {
			stop()
		}
		midi.CloseDriver()
	}()

	if len(stopFns) == 0 {
		fmt.Println("Kon geen enkel MIDI-apparaat openen.")
		os.Exit(1)
	}

	fmt.Println("Klaar! Alle apparaten luisteren.")
	fmt.Println()
	fmt.Println("────────────────────────────────────────────")
	fmt.Println()

	var controls []LearnedControl

	for {
		// Step 1: Ask what the control is
		fmt.Print("Wat is dit voor control? (bv. \"Volume fader deck 1\"): ")
		name, _ := reader.ReadString('\n')
		name = strings.TrimSpace(name)
		if name == "" {
			fmt.Println("Naam mag niet leeg zijn. Probeer opnieuw.")
			continue
		}

	recordLoop:
		for {
			// Step 2: Set to minimum
			fmt.Println()
			fmt.Println("Zet de control naar zijn MINIMUM positie.")
			fmt.Print("Druk op [Enter] als het op minimum staat...")
			startCapture()
			waitEnter(reader)
			minMsgs := stopCapture()

			// Brief pause to flush any lingering messages
			time.Sleep(50 * time.Millisecond)

			// Step 3: Slowly move to maximum
			fmt.Println()
			fmt.Println("Draai/schuif nu LANGZAAM naar de MAXIMUM positie.")
			fmt.Print("Druk op [Enter] als het op maximum staat...")
			startCapture()
			waitEnter(reader)
			maxMsgs := stopCapture()

			// Combine all messages for analysis
			allMsgs := append(minMsgs, maxMsgs...)
			if len(allMsgs) == 0 {
				fmt.Println()
				fmt.Println("Geen MIDI-berichten ontvangen! Beweeg de control terwijl je traint.")
				fmt.Println("Opnieuw proberen...")
				continue
			}

			// Determine the dominant control
			dom := dominantMessage(allMsgs)
			if dom == nil {
				fmt.Println("Kon geen dominant MIDI-signaal bepalen. Opnieuw proberen...")
				continue
			}

			// Get min value from minMsgs for this control
			var minVal uint8 = dom.Value
			for _, m := range minMsgs {
				if m.Channel == dom.Channel && m.Status == dom.Status && m.Number == dom.Number {
					minVal = m.Value
				}
			}

			// Get max value from maxMsgs for this control
			var maxVal uint8 = 0
			for _, m := range maxMsgs {
				if m.Channel == dom.Channel && m.Status == dom.Status && m.Number == dom.Number {
					if m.Value > maxVal {
						maxVal = m.Value
					}
				}
			}
			// Fallback if maxMsgs had nothing for this control
			if maxVal == 0 {
				maxVal = dom.Value
			}

			controlType := detectType(minMsgs, maxMsgs)

			// Collect the full sample filtered to the dominant control
			var sample []MIDIMessage
			for _, m := range allMsgs {
				if m.Channel == dom.Channel && m.Status == dom.Status && m.Number == dom.Number {
					sample = append(sample, m)
				}
			}

			learned := &LearnedControl{
				Name:     name,
				Type:     controlType,
				Status:   dom.Status,
				Channel:  dom.Channel,
				Number:   dom.Number,
				MinValue: minVal,
				MaxValue: maxVal,
				Sample:   sample,
			}

			// Show result
			fmt.Println()
			fmt.Println("┌──────────────────────────────────────────┐")
			fmt.Printf("│ Naam:      %-29s │\n", learned.Name)
			fmt.Printf("│ Type:      %-29s │\n", learned.Type)
			fmt.Printf("│ Status:    %-29s │\n", learned.Status)
			fmt.Printf("│ Channel:   %-29d │\n", learned.Channel)
			fmt.Printf("│ Number:    %-29d │\n", learned.Number)
			fmt.Printf("│ Min:       %-29d │\n", learned.MinValue)
			fmt.Printf("│ Max:       %-29d │\n", learned.MaxValue)
			fmt.Printf("│ Berichten: %-29d │\n", len(allMsgs))
			fmt.Println("└──────────────────────────────────────────┘")

			// Step 4: Options
			for {
				fmt.Println()
				fmt.Println("Wat wil je doen?")
				fmt.Println("  [s] Opslaan & volgende control")
				fmt.Println("  [r] Opnieuw opnemen")
				fmt.Println("  [d] Verwijderen (overslaan)")
				fmt.Println("  [x] Opslaan & afsluiten")
				fmt.Print("> ")
				choice, _ := reader.ReadString('\n')
				choice = strings.TrimSpace(strings.ToLower(choice))

				switch choice {
				case "s":
					controls = append(controls, *learned)
					fmt.Printf("Opgeslagen! (%d controls totaal)\n", len(controls))
					fmt.Println()
					fmt.Println("────────────────────────────────────────────")
					fmt.Println()
					break recordLoop

				case "r":
					fmt.Println("Opnieuw opnemen...")
					continue recordLoop

				case "d":
					fmt.Println("Overgeslagen.")
					fmt.Println()
					fmt.Println("────────────────────────────────────────────")
					fmt.Println()
					break recordLoop

				case "x":
					controls = append(controls, *learned)
					goto done

				default:
					fmt.Println("Ongeldige keuze. Gebruik s, r, d, of x.")
				}
			}
		}
	}

done:
	if len(controls) == 0 {
		fmt.Println()
		fmt.Println("Geen controls opgeslagen. Tot ziens!")
		return
	}

	result := TrainResult{
		Timestamp: time.Now().Format(time.RFC3339),
		Controls:  controls,
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "JSON fout: %v\n", err)
		os.Exit(1)
	}

	// Write to file
	filename := fmt.Sprintf("midi-training-%s.json", time.Now().Format("2006-01-02-150405"))
	if err := os.WriteFile(filename, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Kon bestand niet schrijven: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("════════════════════════════════════════════")
	fmt.Printf("  %d controls opgeslagen naar:\n", len(controls))
	fmt.Printf("  %s\n", filename)
	fmt.Println("════════════════════════════════════════════")
	fmt.Println()
	fmt.Println("Geef dit bestand aan Claude om een controller-mapping te genereren.")
	fmt.Println()

	// Also print to stdout for convenience
	fmt.Println("JSON output:")
	fmt.Println(string(data))
}
