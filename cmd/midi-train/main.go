package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"gitlab.com/gomidi/midi/v2"
	_ "gitlab.com/gomidi/midi/v2/drivers/rtmididrv"
)

func main() {
	reader := bufio.NewReader(os.Stdin)

	clearScreen()
	printHeader()

	stopFns, ok := openInputs()
	if !ok {
		os.Exit(1)
	}
	defer func() {
		for _, stop := range stopFns {
			stop()
		}
		midi.CloseDriver()
	}()

	fmt.Println("Klaar! Alle apparaten luisteren.")
	printSeparator()

	controls := runTrainingLoop(reader)

	if len(controls) == 0 {
		fmt.Println()
		fmt.Println("Geen controls opgeslagen. Tot ziens!")
		return
	}

	if err := saveResult(controls); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

// openInputs lists the available MIDI input ports, opens every one it can,
// and returns the close callbacks. Reports false when no port could be opened.
func openInputs() ([]func(), bool) {
	ins := midi.GetInPorts()
	if len(ins) == 0 {
		fmt.Println("Geen MIDI-apparaten gevonden. Sluit een controller aan en probeer opnieuw.")
		return nil, false
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

	if len(stopFns) == 0 {
		fmt.Println("Kon geen enkel MIDI-apparaat openen.")
		return nil, false
	}
	return stopFns, true
}

// runTrainingLoop drives the interactive trainer: ask for a control name,
// record the min/max passes, show the result, and either save, retry, or
// exit. Returns the full list of accepted controls.
func runTrainingLoop(reader *bufio.Reader) []LearnedControl {
	var controls []LearnedControl
	for {
		fmt.Print("Wat is dit voor control? (bv. \"Volume fader deck 1\"): ")
		name, _ := reader.ReadString('\n')
		name = strings.TrimSpace(name)
		if name == "" {
			fmt.Println("Naam mag niet leeg zijn. Probeer opnieuw.")
			continue
		}

		learned, action := trainOneControl(reader, name)
		if learned != nil && (action == actionSave || action == actionExit) {
			controls = append(controls, *learned)
			fmt.Printf("Opgeslagen! (%d controls totaal)\n", len(controls))
			printSeparator()
		}
		if action == actionExit {
			return controls
		}
	}
}

// userAction is the outcome of the post-record menu.
type userAction int

const (
	actionSkip userAction = iota
	actionSave
	actionExit
)

// trainOneControl runs the record/show/menu cycle for a single control. The
// caller decides what to do with the returned learned control based on the
// action: save it, skip it, or exit the trainer.
func trainOneControl(reader *bufio.Reader, name string) (*LearnedControl, userAction) {
	for {
		minMsgs, maxMsgs := captureMinMaxPass(reader)
		allMsgs := append(minMsgs, maxMsgs...)
		if len(allMsgs) == 0 {
			fmt.Println()
			fmt.Println("Geen MIDI-berichten ontvangen! Beweeg de control terwijl je traint.")
			fmt.Println("Opnieuw proberen...")
			continue
		}

		dom := dominantMessage(allMsgs)
		if dom == nil {
			fmt.Println("Kon geen dominant MIDI-signaal bepalen. Opnieuw proberen...")
			continue
		}

		minVal, maxVal, sample := extractRange(dom, minMsgs, maxMsgs)
		learned := &LearnedControl{
			Name:     name,
			Type:     detectType(minMsgs, maxMsgs),
			Status:   dom.Status,
			Channel:  dom.Channel,
			Number:   dom.Number,
			MinValue: minVal,
			MaxValue: maxVal,
			Sample:   sample,
		}
		printLearnedBox(learned, len(allMsgs))

		switch promptUserAction(reader) {
		case "s":
			return learned, actionSave
		case "r":
			fmt.Println("Opnieuw opnemen...")
			continue
		case "d":
			fmt.Println("Overgeslagen.")
			printSeparator()
			return nil, actionSkip
		case "x":
			return learned, actionExit
		}
	}
}

// captureMinMaxPass walks the user through the two-pass min/max recording.
func captureMinMaxPass(reader *bufio.Reader) (minMsgs, maxMsgs []MIDIMessage) {
	fmt.Println()
	fmt.Println("Zet de control naar zijn MINIMUM positie.")
	fmt.Print("Druk op [Enter] als het op minimum staat...")
	startCapture()
	waitEnter(reader)
	minMsgs = stopCapture()

	// Brief pause to flush any lingering messages
	time.Sleep(50 * time.Millisecond)

	fmt.Println()
	fmt.Println("Draai/schuif nu LANGZAAM naar de MAXIMUM positie.")
	fmt.Print("Druk op [Enter] als het op maximum staat...")
	startCapture()
	waitEnter(reader)
	maxMsgs = stopCapture()
	return minMsgs, maxMsgs
}

// promptUserAction shows the post-record menu and returns the user's choice
// (one of "s", "r", "d", "x"). Loops on invalid input.
func promptUserAction(reader *bufio.Reader) string {
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
		case "s", "r", "d", "x":
			return choice
		default:
			fmt.Println("Ongeldige keuze. Gebruik s, r, d, of x.")
		}
	}
}

// saveResult writes the trained controls to a timestamped JSON file and also
// prints the JSON to stdout for easy copy/paste.
func saveResult(controls []LearnedControl) error {
	result := TrainResult{
		Timestamp: time.Now().Format(time.RFC3339),
		Controls:  controls,
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("JSON fout: %w", err)
	}

	filename := fmt.Sprintf("midi-training-%s.json", time.Now().Format("2006-01-02-150405"))
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("kon bestand niet schrijven: %w", err)
	}

	printSavedFooter(len(controls), filename)

	fmt.Println("JSON output:")
	fmt.Println(string(data))
	return nil
}
