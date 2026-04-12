package settings

import (
	"fmt"
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"

	"github.com/janyksteenbeek/boom/internal/config"
)

// tab is the contract each settings tab file implements: build the view
// once with the current config, and return an apply callback that writes
// the user's edits back into *config.Config when Save is clicked.
type tab struct {
	title   string
	content fyne.CanvasObject
	apply   func(*config.Config)
}

// tabBuilder is the factory signature every tab_*.go file exports so
// dialog.go can assemble them in one place without caring about internals.
type tabBuilder func(window fyne.Window, cfg *config.Config) tab

// ShowSettingsDialog opens the settings dialog. Each tab is built by its
// own file (tab_library.go, tab_audio.go, ...); this function only wires
// them into a tab bar and drives the Save flow.
func ShowSettingsDialog(window fyne.Window, cfg *config.Config, onSave func(*config.Config)) {
	builders := []tabBuilder{
		buildLibraryTab,
		buildAudioTab,
		buildAnalysisTab,
		buildLoopsTab,
		buildJogTab,
		buildPerformanceTab,
		buildAboutTab,
	}

	tabs := make([]tab, 0, len(builders))
	items := make([]*container.TabItem, 0, len(builders))
	for _, b := range builders {
		t := b(window, cfg)
		tabs = append(tabs, t)
		items = append(items, container.NewTabItem(t.title,
			container.NewVBox(t.content, layout.NewSpacer())))
	}

	appTabs := container.NewAppTabs(items...)
	appTabs.SetTabLocation(container.TabLocationTop)

	content := container.NewVBox(appTabs)
	content.Resize(fyne.NewSize(500, 400))

	d := dialog.NewCustomConfirm("Settings", "Save", "Cancel", content, func(save bool) {
		if !save {
			return
		}

		for _, t := range tabs {
			if t.apply != nil {
				t.apply(cfg)
			}
		}

		if err := cfg.Save(); err != nil {
			log.Printf("failed to save config: %v", err)
			dialog.ShowError(fmt.Errorf("Failed to save settings: %w", err), window)
			return
		}

		log.Printf("settings saved")

		if onSave != nil {
			onSave(cfg)
		}

		dialog.ShowInformation("Settings Saved",
			"Music library will be rescanned.\nAudio device changes require an app restart.",
			window)
	}, window)

	d.Resize(fyne.NewSize(520, 450))
	d.Show()
}
