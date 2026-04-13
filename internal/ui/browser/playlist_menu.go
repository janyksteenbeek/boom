package browser

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/pkg/model"
)

// onSidebarNodeAction is the sidebar's right-click hook. action is "menu"
// for an item and "menu_root" for a section-header "+"; the popup is shown
// at the supplied absolute canvas position so it appears next to the click
// rather than at a guessed location.
func (b *BrowserView) onSidebarNodeAction(nodeID, action string, at fyne.Position) {
	win := fyne.CurrentApp().Driver().AllWindows()
	if len(win) == 0 {
		return
	}
	w := win[0]
	switch action {
	case "menu_root":
		b.showRootCreateMenu(w, at)
	case "menu":
		b.showNodeMenu(w, nodeID, at)
	}
}

// showRootCreateMenu opens the "+" affordance on the PLAYLISTS section —
// lets the user create a playlist, folder, or auto playlist at the root.
func (b *BrowserView) showRootCreateMenu(w fyne.Window, at fyne.Position) {
	menu := fyne.NewMenu("",
		fyne.NewMenuItem("New Playlist…", func() { b.showPlaylistCreateDialog(w, "") }),
		fyne.NewMenuItem("New Folder…", func() { b.showFolderCreateDialog(w, "") }),
		fyne.NewMenuItem("New Auto Playlist…", func() {
			showAutoPlaylistEditor(w, "", func(parentID, name string, rules model.SmartRules) {
				if _, err := b.playlists.CreateSmart(parentID, name, rules); err != nil {
					dialog.ShowError(err, w)
				}
			})
		}),
	)
	popup := widget.NewPopUpMenu(menu, w.Canvas())
	popup.ShowAtPosition(at)
}

func (b *BrowserView) showNodeMenu(w fyne.Window, nodeID string, at fyne.Position) {
	if b.playlists == nil {
		return
	}
	node, err := b.playlists.Node(nodeID)
	if err != nil || node == nil {
		return
	}
	items := []*fyne.MenuItem{}
	if node.Kind == model.KindFolder {
		items = append(items,
			fyne.NewMenuItem("New Folder…", func() { b.showFolderCreateDialog(w, node.ID) }),
			fyne.NewMenuItem("New Playlist…", func() { b.showPlaylistCreateDialog(w, node.ID) }),
		)
	}
	items = append(items,
		fyne.NewMenuItem("Rename…", func() { b.showRenameDialog(w, node) }),
		fyne.NewMenuItem("Delete", func() { b.showDeleteConfirm(w, node) }),
	)
	menu := fyne.NewMenu("", items...)
	popup := widget.NewPopUpMenu(menu, w.Canvas())
	popup.ShowAtPosition(at)
}

func (b *BrowserView) showPlaylistCreateDialog(w fyne.Window, parentID string) {
	b.showCreateDialog(w, "New Playlist", "Playlist name", func(name string) {
		if _, err := b.playlists.CreatePlaylist(parentID, name); err != nil {
			dialog.ShowError(err, w)
		}
	})
}

func (b *BrowserView) showFolderCreateDialog(w fyne.Window, parentID string) {
	b.showCreateDialog(w, "New Folder", "Folder name", func(name string) {
		if _, err := b.playlists.CreateFolder(parentID, name); err != nil {
			dialog.ShowError(err, w)
		}
	})
}

func (b *BrowserView) showCreateDialog(w fyne.Window, title, placeholder string, onConfirm func(name string)) {
	entry := widget.NewEntry()
	entry.SetPlaceHolder(placeholder)
	d := dialog.NewForm(title, "Create", "Cancel", []*widget.FormItem{
		{Text: "Name", Widget: entry},
	}, func(ok bool) {
		if !ok || strings.TrimSpace(entry.Text) == "" {
			return
		}
		onConfirm(entry.Text)
	}, w)
	d.Show()
}

func (b *BrowserView) showRenameDialog(w fyne.Window, node *model.PlaylistNode) {
	entry := widget.NewEntry()
	entry.SetText(node.Name)
	d := dialog.NewForm("Rename", "Save", "Cancel", []*widget.FormItem{
		{Text: "Name", Widget: entry},
	}, func(ok bool) {
		if !ok || strings.TrimSpace(entry.Text) == "" {
			return
		}
		if err := b.playlists.Rename(node.ID, entry.Text); err != nil {
			dialog.ShowError(err, w)
		}
	}, w)
	d.Show()
}

func (b *BrowserView) showDeleteConfirm(w fyne.Window, node *model.PlaylistNode) {
	msg := "Delete playlist \"" + node.Name + "\"? Tracks stay in the library."
	if node.Kind == model.KindFolder {
		msg = "Delete folder \"" + node.Name + "\" and all playlists inside it?"
	}
	dialog.ShowConfirm("Delete", msg, func(ok bool) {
		if !ok {
			return
		}
		if err := b.playlists.Delete(node.ID); err != nil {
			dialog.ShowError(err, w)
		}
	}, w)
}
