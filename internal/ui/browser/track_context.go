package browser

import (
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/internal/event"
	"github.com/janyksteenbeek/boom/pkg/model"
)

// showTrackContextMenu opens the right-click menu for the current selection
// in the track list at the given absolute canvas position. Menu actions
// (load to deck, add to playlist, analyze) operate on SelectedTracks() so
// they transparently handle multi-selection.
func (b *BrowserView) showTrackContextMenu(at fyne.Position) {
	sel := b.trackList.SelectedTracks()
	if len(sel) == 0 {
		return
	}
	win := fyne.CurrentApp().Driver().AllWindows()
	if len(win) == 0 {
		return
	}
	w := win[0]

	items := []*fyne.MenuItem{
		fyne.NewMenuItem("Load to Deck 1", func() {
			track := sel[0]
			b.bus.Publish(event.Event{Topic: event.TopicDeck, Action: event.ActionLoadTrack, DeckID: 1, Payload: &track})
		}),
		fyne.NewMenuItem("Load to Deck 2", func() {
			track := sel[0]
			b.bus.Publish(event.Event{Topic: event.TopicDeck, Action: event.ActionLoadTrack, DeckID: 2, Payload: &track})
		}),
		fyne.NewMenuItemSeparator(),
		{Label: "Add to Playlist", ChildMenu: b.buildAddToPlaylistMenu(sel)},
	}
	if b.currentPlaylistID != "" && b.trackList.PlaylistID() == b.currentPlaylistID {
		items = append(items, fyne.NewMenuItem("Remove from Playlist", func() {
			ids := make([]string, len(sel))
			for i, t := range sel {
				ids[i] = t.ID
			}
			_ = b.playlists.RemoveTracks(b.currentPlaylistID, ids)
		}))
	}
	items = append(items, fyne.NewMenuItem("Analyze", func() {
		b.bus.PublishAsync(event.Event{Topic: event.TopicAnalysis, Action: event.ActionAnalyzeRequest, Payload: sel})
	}))

	menu := fyne.NewMenu("", items...)
	popup := widget.NewPopUpMenu(menu, w.Canvas())
	popup.ShowAtPosition(at)
}

// buildAddToPlaylistMenu recursively builds a submenu mirroring the playlist
// tree. Folders become nested submenus; manual playlists are leaves that
// append the current selection when picked. Auto playlists are omitted —
// they can't accept manual track inserts.
func (b *BrowserView) buildAddToPlaylistMenu(sel []model.Track) *fyne.Menu {
	if b.playlists == nil {
		return fyne.NewMenu("")
	}
	tree, err := b.playlists.Tree()
	if err != nil || len(tree) == 0 {
		return fyne.NewMenu("", fyne.NewMenuItem("(no playlists)", nil))
	}
	return fyne.NewMenu("", b.buildAddItems(tree, "", sel)...)
}

func (b *BrowserView) buildAddItems(tree []*model.PlaylistNode, parentID string, sel []model.Track) []*fyne.MenuItem {
	var items []*fyne.MenuItem
	for _, n := range tree {
		if n.ParentID != parentID {
			continue
		}
		switch n.Kind {
		case model.KindFolder:
			children := b.buildAddItems(tree, n.ID, sel)
			if len(children) == 0 {
				continue
			}
			items = append(items, &fyne.MenuItem{Label: n.Name, ChildMenu: fyne.NewMenu("", children...)})
		case model.KindManual:
			nodeID := n.ID
			items = append(items, fyne.NewMenuItem(n.Name, func() {
				ids := make([]string, len(sel))
				for i, t := range sel {
					ids[i] = t.ID
				}
				if err := b.playlists.AddTracks(nodeID, ids); err != nil {
					log.Printf("add tracks: %v", err)
				}
			}))
		}
	}
	return items
}
