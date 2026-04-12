package settings

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/internal/audio"
	"github.com/janyksteenbeek/boom/internal/config"
)

// ShowSettingsDialog opens the settings dialog.
func ShowSettingsDialog(window fyne.Window, cfg *config.Config, onSave func(*config.Config)) {
	// --- Music Library ---
	musicDirsEntry := widget.NewMultiLineEntry()
	musicDirsEntry.SetText(strings.Join(cfg.MusicDirs, "\n"))
	musicDirsEntry.SetPlaceHolder("/path/to/music\n/another/path")
	musicDirsEntry.SetMinRowsVisible(3)

	addFolderBtn := widget.NewButton("Add Folder...", func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil || uri == nil {
				return
			}
			current := musicDirsEntry.Text
			if current != "" {
				current += "\n"
			}
			musicDirsEntry.SetText(current + uri.Path())
		}, window)
	})

	musicSection := container.NewVBox(
		widget.NewLabel("Music Library Paths (one per line):"),
		musicDirsEntry,
		addFolderBtn,
	)

	// --- Audio Output ---
	// We display device names in the dropdown but persist the opaque
	// device ID — names can collide and aren't stable across reboots.
	devices := audio.ListOutputDevices()

	masterLabels := make([]string, len(devices))
	idByLabel := make(map[string]string, len(devices))
	labelByID := make(map[string]string, len(devices))
	for i, d := range devices {
		label := d.Name
		// Disambiguate duplicate names by appending a short ID prefix.
		if _, exists := idByLabel[label]; exists {
			suffix := d.ID
			if len(suffix) > 6 {
				suffix = suffix[:6]
			}
			label = fmt.Sprintf("%s (%s)", d.Name, suffix)
		}
		masterLabels[i] = label
		idByLabel[label] = d.ID
		if _, ok := labelByID[d.ID]; !ok {
			labelByID[d.ID] = label
		}
	}

	outputSelect := widget.NewSelect(masterLabels, nil)
	if label, ok := labelByID[cfg.AudioOutputDevice]; ok {
		outputSelect.SetSelected(label)
	} else {
		outputSelect.SetSelected(masterLabels[0])
	}

	cueLabels := append([]string{"Disabled"}, masterLabels...)
	cueSelect := widget.NewSelect(cueLabels, nil)
	if cfg.CueOutputDevice == "" {
		cueSelect.SetSelected("Disabled")
	} else if label, ok := labelByID[cfg.CueOutputDevice]; ok {
		cueSelect.SetSelected(label)
	} else {
		cueSelect.SetSelected("Disabled")
	}

	audioSection := container.NewVBox(
		widget.NewLabel("Master Output:"),
		outputSelect,
		widget.NewLabel("Cue/Headphone Output:"),
		cueSelect,
	)

	// --- Performance ---
	sampleRates := []string{"44100", "48000", "96000"}
	sampleRateSelect := widget.NewSelect(sampleRates, nil)
	sampleRateSelect.SetSelected(strconv.Itoa(cfg.SampleRate))

	bufferSizes := []string{"128", "256", "512", "1024", "2048"}
	bufferSizeSelect := widget.NewSelect(bufferSizes, nil)
	bufferSizeSelect.SetSelected(strconv.Itoa(cfg.BufferSize))

	perfSection := container.NewVBox(
		container.NewGridWithColumns(2,
			widget.NewLabel("Sample Rate:"),
			sampleRateSelect,
			widget.NewLabel("Buffer Size:"),
			bufferSizeSelect,
		),
	)

	// --- Analysis ---
	autoAnalyzeOnLoad := widget.NewCheck("Analyze tracks when loaded to a deck", nil)
	autoAnalyzeOnLoad.SetChecked(cfg.AutoAnalyzeOnDeckLoad)

	autoAnalyzeOnImport := widget.NewCheck("Analyze tracks on library import", nil)
	autoAnalyzeOnImport.SetChecked(cfg.AutoAnalyzeOnImport)

	bpmRangeSelect := widget.NewSelect(config.BPMRangeLabels(), nil)
	if cfg.BPMRange == "" {
		bpmRangeSelect.SetSelected(config.BPMRangePresets[0].Label)
	} else {
		bpmRangeSelect.SetSelected(cfg.BPMRange)
	}

	autoCueCheck := widget.NewCheck("Auto Cue: seek to first audio on load", nil)
	autoCueCheck.SetChecked(cfg.AutoCue)

	// --- Loops ---
	loopQuantizeCheck := widget.NewCheck("Quantize loop in/out to beat grid", nil)
	loopQuantizeCheck.SetChecked(cfg.Loop.Quantize)

	loopSmartCheck := widget.NewCheck("Smart loop (clamp near track end instead of skipping)", nil)
	loopSmartCheck.SetChecked(cfg.Loop.SmartLoop)

	beatLoopOptions := []string{"1/4", "1/2", "1", "2", "4", "8", "16", "32"}
	beatLoopValues := map[string]float64{
		"1/4": 0.25, "1/2": 0.5, "1": 1, "2": 2, "4": 4, "8": 8, "16": 16, "32": 32,
	}
	defaultBeatLoopSelect := widget.NewSelect(beatLoopOptions, nil)
	defaultBeatLoopSelect.SetSelected(formatBeatLoopLabel(cfg.Loop.DefaultBeatLoop))

	loopSection := container.NewVBox(
		widget.NewLabel("Loop Behavior"),
		loopQuantizeCheck,
		loopSmartCheck,
		widget.NewSeparator(),
		widget.NewLabel("Default Auto Beat Loop (beats)"),
		defaultBeatLoopSelect,
	)

	// --- Jog Wheel ---
	vinylModeCheck := widget.NewCheck("Vinyl Mode (top touch enables scratching)", nil)
	vinylModeCheck.SetChecked(cfg.Jog.VinylMode)

	scratchSlider := widget.NewSlider(0.05, 1.5)
	scratchSlider.Step = 0.05
	scratchSlider.SetValue(cfg.Jog.ScratchSensitivity)
	scratchValueLabel := widget.NewLabel(fmt.Sprintf("%.2f", cfg.Jog.ScratchSensitivity))
	scratchSlider.OnChanged = func(v float64) {
		scratchValueLabel.SetText(fmt.Sprintf("%.2f", v))
	}

	pitchSlider := widget.NewSlider(0.005, 0.2)
	pitchSlider.Step = 0.005
	pitchSlider.SetValue(cfg.Jog.PitchSensitivity)
	pitchValueLabel := widget.NewLabel(fmt.Sprintf("%.3f", cfg.Jog.PitchSensitivity))
	pitchSlider.OnChanged = func(v float64) {
		pitchValueLabel.SetText(fmt.Sprintf("%.3f", v))
	}

	jogSection := container.NewVBox(
		vinylModeCheck,
		widget.NewLabel("When off, top-touch + rotate behaves as pitch bend (no scratching)."),
		widget.NewSeparator(),
		widget.NewLabel("Vinyl Scratch Sensitivity"),
		container.NewBorder(nil, nil, nil, scratchValueLabel, scratchSlider),
		widget.NewLabel("Higher = audio moves more per platter increment."),
		widget.NewSeparator(),
		widget.NewLabel("Pitch Bend Sensitivity"),
		container.NewBorder(nil, nil, nil, pitchValueLabel, pitchSlider),
		widget.NewLabel("Side-touch nudge depth on the jog wheel."),
	)

	analysisSection := container.NewVBox(
		widget.NewLabel("Auto-analyze"),
		autoAnalyzeOnLoad,
		autoAnalyzeOnImport,
		widget.NewSeparator(),
		widget.NewLabel("BPM Range"),
		bpmRangeSelect,
		widget.NewSeparator(),
		widget.NewLabel("Cue"),
		autoCueCheck,
	)

	// --- Tabs ---
	tabs := container.NewAppTabs(
		container.NewTabItem("Library", container.NewVBox(musicSection, layout.NewSpacer())),
		container.NewTabItem("Audio", container.NewVBox(audioSection, layout.NewSpacer())),
		container.NewTabItem("Analysis", container.NewVBox(analysisSection, layout.NewSpacer())),
		container.NewTabItem("Loops", container.NewVBox(loopSection, layout.NewSpacer())),
		container.NewTabItem("Jog", container.NewVBox(jogSection, layout.NewSpacer())),
		container.NewTabItem("Performance", container.NewVBox(perfSection, layout.NewSpacer())),
	)
	tabs.SetTabLocation(container.TabLocationTop)

	content := container.NewVBox(tabs)
	content.Resize(fyne.NewSize(500, 400))

	d := dialog.NewCustomConfirm("Settings", "Save", "Cancel", content, func(save bool) {
		if !save {
			return
		}

		// Parse music dirs
		dirs := strings.Split(musicDirsEntry.Text, "\n")
		var cleanDirs []string
		for _, d := range dirs {
			d = strings.TrimSpace(d)
			if d != "" {
				cleanDirs = append(cleanDirs, d)
			}
		}
		cfg.MusicDirs = cleanDirs

		// Audio output — persist the opaque Device.ID, not the label.
		cfg.AudioOutputDevice = idByLabel[outputSelect.Selected]

		if cueSelect.Selected == "Disabled" {
			cfg.CueOutputDevice = ""
		} else {
			cfg.CueOutputDevice = idByLabel[cueSelect.Selected]
		}

		// Performance
		if sr, err := strconv.Atoi(sampleRateSelect.Selected); err == nil {
			cfg.SampleRate = sr
		}
		if bs, err := strconv.Atoi(bufferSizeSelect.Selected); err == nil {
			cfg.BufferSize = bs
		}

		// Analysis
		cfg.AutoAnalyzeOnDeckLoad = autoAnalyzeOnLoad.Checked
		cfg.AutoAnalyzeOnImport = autoAnalyzeOnImport.Checked
		cfg.BPMRange = bpmRangeSelect.Selected
		cfg.AutoCue = autoCueCheck.Checked

		// Loops
		cfg.Loop.Quantize = loopQuantizeCheck.Checked
		cfg.Loop.SmartLoop = loopSmartCheck.Checked
		if v, ok := beatLoopValues[defaultBeatLoopSelect.Selected]; ok {
			cfg.Loop.DefaultBeatLoop = v
		}

		// Jog
		cfg.Jog.VinylMode = vinylModeCheck.Checked
		cfg.Jog.ScratchSensitivity = scratchSlider.Value
		cfg.Jog.PitchSensitivity = pitchSlider.Value

		// Save to disk
		if err := cfg.Save(); err != nil {
			log.Printf("failed to save config: %v", err)
			dialog.ShowError(fmt.Errorf("Failed to save settings: %w", err), window)
			return
		}

		log.Printf("settings saved")

		// Notify caller
		if onSave != nil {
			onSave(cfg)
		}

		// Show restart notice for audio changes
		dialog.ShowInformation("Settings Saved",
			"Music library will be rescanned.\nAudio device changes require an app restart.",
			window)
	}, window)

	d.Resize(fyne.NewSize(520, 450))
	d.Show()
}

// formatBeatLoopLabel renders a beat count as the user-facing label used in
// the settings dropdown ("1/4", "1", "4", etc.).
func formatBeatLoopLabel(beats float64) string {
	switch {
	case beats <= 0.25:
		return "1/4"
	case beats <= 0.5:
		return "1/2"
	case beats <= 1:
		return "1"
	case beats <= 2:
		return "2"
	case beats <= 4:
		return "4"
	case beats <= 8:
		return "8"
	case beats <= 16:
		return "16"
	default:
		return "32"
	}
}
