package settings

import (
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/internal/config"
)

func buildPerformanceTab(_ fyne.Window, cfg *config.Config) tab {
	sampleRates := []string{"44100", "48000", "96000"}
	sampleRateSelect := widget.NewSelect(sampleRates, nil)
	sampleRateSelect.SetSelected(strconv.Itoa(cfg.SampleRate))

	bufferSizes := []string{"128", "256", "512", "1024", "2048"}
	bufferSizeSelect := widget.NewSelect(bufferSizes, nil)
	bufferSizeSelect.SetSelected(strconv.Itoa(cfg.BufferSize))

	content := container.NewVBox(
		container.NewGridWithColumns(2,
			widget.NewLabel("Sample Rate:"),
			sampleRateSelect,
			widget.NewLabel("Buffer Size:"),
			bufferSizeSelect,
		),
	)

	return tab{
		title:   "Performance",
		content: content,
		apply: func(c *config.Config) {
			if sr, err := strconv.Atoi(sampleRateSelect.Selected); err == nil {
				c.SampleRate = sr
			}
			if bs, err := strconv.Atoi(bufferSizeSelect.Selected); err == nil {
				c.BufferSize = bs
			}
		},
	}
}
