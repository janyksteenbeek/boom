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
	devices := audio.ListAudioDevices()

	outputSelect := widget.NewSelect(devices, nil)
	if cfg.AudioOutputDevice == "" {
		outputSelect.SetSelected("System Default")
	} else {
		outputSelect.SetSelected(cfg.AudioOutputDevice)
	}

	cueSelect := widget.NewSelect(append([]string{"Disabled"}, devices...), nil)
	if cfg.CueOutputDevice == "" {
		cueSelect.SetSelected("Disabled")
	} else {
		cueSelect.SetSelected(cfg.CueOutputDevice)
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

		// Audio output
		if outputSelect.Selected == "System Default" {
			cfg.AudioOutputDevice = ""
		} else {
			cfg.AudioOutputDevice = outputSelect.Selected
		}

		if cueSelect.Selected == "Disabled" {
			cfg.CueOutputDevice = ""
		} else {
			cfg.CueOutputDevice = cueSelect.Selected
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
