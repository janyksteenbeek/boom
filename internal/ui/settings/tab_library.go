package settings

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/internal/config"
)

func buildLibraryTab(window fyne.Window, cfg *config.Config) tab {
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

	content := container.NewVBox(
		widget.NewLabel("Music Library Paths (one per line):"),
		musicDirsEntry,
		addFolderBtn,
	)

	return tab{
		title:   "Library",
		content: content,
		apply: func(c *config.Config) {
			dirs := strings.Split(musicDirsEntry.Text, "\n")
			var cleanDirs []string
			for _, d := range dirs {
				d = strings.TrimSpace(d)
				if d != "" {
					cleanDirs = append(cleanDirs, d)
				}
			}
			c.MusicDirs = cleanDirs
		},
	}
}
