package settings

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/internal/config"
)

func buildAnalysisTab(_ fyne.Window, cfg *config.Config) tab {
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

	content := container.NewVBox(
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

	return tab{
		title:   "Analysis",
		content: content,
		apply: func(c *config.Config) {
			c.AutoAnalyzeOnDeckLoad = autoAnalyzeOnLoad.Checked
			c.AutoAnalyzeOnImport = autoAnalyzeOnImport.Checked
			c.BPMRange = bpmRangeSelect.Selected
			c.AutoCue = autoCueCheck.Checked
		},
	}
}
