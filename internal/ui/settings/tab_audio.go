package settings

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/internal/audio"
	"github.com/janyksteenbeek/boom/internal/config"
)

func buildAudioTab(_ fyne.Window, cfg *config.Config) tab {
	// We display device names in the dropdown but persist the opaque
	// device ID — names can collide and aren't stable across reboots.
	devices := audio.ListOutputDevices()

	masterLabels := make([]string, len(devices))
	idByLabel := make(map[string]string, len(devices))
	labelByID := make(map[string]string, len(devices))
	for i, d := range devices {
		label := d.Name
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
	} else if len(masterLabels) > 0 {
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

	content := container.NewVBox(
		widget.NewLabel("Master Output:"),
		outputSelect,
		widget.NewLabel("Cue/Headphone Output:"),
		cueSelect,
	)

	return tab{
		title:   "Audio",
		content: content,
		apply: func(c *config.Config) {
			c.AudioOutputDevice = idByLabel[outputSelect.Selected]
			if cueSelect.Selected == "Disabled" {
				c.CueOutputDevice = ""
			} else {
				c.CueOutputDevice = idByLabel[cueSelect.Selected]
			}
		},
	}
}
