package settings

import (
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/internal/config"
)

// buildAboutTab renders a read-only view of the on-disk config and database
// paths so the user can quickly find where their settings and library live
// without digging through the filesystem.
func buildAboutTab(_ fyne.Window, cfg *config.Config) tab {
	configPath := cfg.Path()
	if configPath == "" {
		configPath = "(not loaded from disk)"
	}
	dbPath := cfg.DatabasePath
	if abs, err := filepath.Abs(dbPath); err == nil {
		dbPath = abs
	}

	configEntry := widget.NewEntry()
	configEntry.SetText(configPath)
	configEntry.Disable()

	dbEntry := widget.NewEntry()
	dbEntry.SetText(dbPath)
	dbEntry.Disable()

	content := container.NewVBox(
		widget.NewLabelWithStyle("File Locations", fyne.TextAlignLeading,
			fyne.TextStyle{Bold: true}),
		widget.NewLabel("Config file:"),
		configEntry,
		widget.NewLabel("Library database:"),
		dbEntry,
		widget.NewLabel("Both paths are read-only. Edit the config file directly or move the database while the app is closed."),
	)

	return tab{
		title:   "About",
		content: content,
		// About tab is read-only; no apply.
	}
}
