package settings

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/internal/config"
)

var beatLoopOptions = []string{"1/4", "1/2", "1", "2", "4", "8", "16", "32"}
var beatLoopValues = map[string]float64{
	"1/4": 0.25, "1/2": 0.5, "1": 1, "2": 2, "4": 4, "8": 8, "16": 16, "32": 32,
}

func buildLoopsTab(_ fyne.Window, cfg *config.Config) tab {
	loopQuantizeCheck := widget.NewCheck("Quantize loop in/out to beat grid", nil)
	loopQuantizeCheck.SetChecked(cfg.Loop.Quantize)

	loopSmartCheck := widget.NewCheck("Smart loop (clamp near track end instead of skipping)", nil)
	loopSmartCheck.SetChecked(cfg.Loop.SmartLoop)

	defaultBeatLoopSelect := widget.NewSelect(beatLoopOptions, nil)
	defaultBeatLoopSelect.SetSelected(formatBeatLoopLabel(cfg.Loop.DefaultBeatLoop))

	content := container.NewVBox(
		widget.NewLabel("Loop Behavior"),
		loopQuantizeCheck,
		loopSmartCheck,
		widget.NewSeparator(),
		widget.NewLabel("Default Auto Beat Loop (beats)"),
		defaultBeatLoopSelect,
	)

	return tab{
		title:   "Loops",
		content: content,
		apply: func(c *config.Config) {
			c.Loop.Quantize = loopQuantizeCheck.Checked
			c.Loop.SmartLoop = loopSmartCheck.Checked
			if v, ok := beatLoopValues[defaultBeatLoopSelect.Selected]; ok {
				c.Loop.DefaultBeatLoop = v
			}
		},
	}
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
