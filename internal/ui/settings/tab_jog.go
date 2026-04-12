package settings

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/internal/config"
)

func buildJogTab(_ fyne.Window, cfg *config.Config) tab {
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

	content := container.NewVBox(
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

	return tab{
		title:   "Jog",
		content: content,
		apply: func(c *config.Config) {
			c.Jog.VinylMode = vinylModeCheck.Checked
			c.Jog.ScratchSensitivity = scratchSlider.Value
			c.Jog.PitchSensitivity = pitchSlider.Value
		},
	}
}
