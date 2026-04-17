package browser

import (
	"image/color"
	"log"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/internal/event"
	"github.com/janyksteenbeek/boom/internal/library"
	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
	"github.com/janyksteenbeek/boom/pkg/model"
)

// browseFocus identifies which pane of the browser currently receives MIDI
// wheel input. New levels (e.g. nested sidebar sections, playlists, crates)
// can be added here without touching call sites.
type browseFocus int

const (
	focusTrackList browseFocus = iota
	focusSidebar
)

func (f browseFocus) String() string {
	switch f {
	case focusSidebar:
		return "sidebar"
	case focusTrackList:
		return "track list"
	default:
		return "unknown"
	}
}

// nextFocus returns the focus that should be active after a browse_select
// press. Kept as a method so the cycle order can grow (e.g. track list →
// sidebar → nested sidebar → back) without rewriting the event handler.
func (b *BrowserView) nextFocus() browseFocus {
	switch b.focus {
	case focusTrackList:
		return focusSidebar
	case focusSidebar:
		return focusTrackList
	default:
		return focusTrackList
	}
}

// SidebarMode selects how the left-hand navigation renders.
type SidebarMode int

const (
	// SidebarModeClassic is the desktop source-list sidebar (default).
	SidebarModeClassic SidebarMode = iota
	// SidebarModeFolder swaps the sidebar for a breadcrumb + single
	// folder listing that navigates one level at a time. Used by mini-mode
	// where a fixed 180-px sidebar eats too much horizontal space.
	SidebarModeFolder
)

// BrowserOpts tunes which BrowserView sub-panels are rendered. The zero
// value matches the pre-refactor desktop behavior, so existing call sites
// keep working by passing a BrowserOpts{}.
type BrowserOpts struct {
	HideToolbar      bool
	HideColumnHeader bool
	SidebarMode      SidebarMode
	// SidebarWidth, when > 0, overrides the default sidebar width. Ignored
	// in SidebarModeFolder (folder mode draws full-width anyway).
	SidebarWidth float32
}

type BrowserView struct {
	widget.BaseWidget

	bus               *event.Bus
	opts              BrowserOpts
	playlists         *library.PlaylistService
	sidebar           *Sidebar
	toolbar           *BrowserToolbar
	columnHeader      *ColumnHeader
	trackList         *TrackList
	content           *fyne.Container
	targetDeck        int
	focus             browseFocus
	sidebarFocusBar   *canvas.Rectangle
	trackListFocusBar *canvas.Rectangle

	// currentPlaylistID tracks the currently opened manual playlist (if any),
	// used to scope reorder / delete / DnD operations and to decide whether a
	// TopicPlaylist tracks-changed event should trigger a refetch.
	currentPlaylistID string
}

// NewBrowserView creates the default desktop browser view. Equivalent to
// NewBrowserViewWithOpts(bus, playlists, BrowserOpts{}).
func NewBrowserView(bus *event.Bus, playlists *library.PlaylistService) *BrowserView {
	return NewBrowserViewWithOpts(bus, playlists, BrowserOpts{})
}

// NewBrowserViewWithOpts builds a BrowserView with caller-controlled
// composition. Mini-mode uses this with HideToolbar/HideColumnHeader true
// and SidebarModeFolder so the overlay stays compact on a 5" screen.
func NewBrowserViewWithOpts(bus *event.Bus, playlists *library.PlaylistService, opts BrowserOpts) *BrowserView {
	b := &BrowserView{bus: bus, opts: opts, playlists: playlists, targetDeck: 1}

	b.sidebar = NewSidebar(func(categoryID string) {
		b.onCategorySelected(categoryID)
	})
	b.sidebar.SetOnNodeAction(b.onSidebarNodeAction)

	b.trackList = NewTrackList(func(track model.Track) {
		bus.Publish(event.Event{
			Topic: event.TopicDeck, Action: event.ActionLoadTrack, DeckID: b.targetDeck, Payload: &track,
		})
	})
	b.trackList.SetOnContext(func(_ int, at fyne.Position) { b.showTrackContextMenu(at) })
	b.trackList.SetOnReorder(func(ids []string, newIndex int) {
		if b.currentPlaylistID == "" || b.playlists == nil {
			return
		}
		if err := b.playlists.ReorderMany(b.currentPlaylistID, ids, newIndex); err != nil {
			log.Printf("reorder: %v", err)
		}
	})

	b.columnHeader = NewColumnHeader(defaultColumns, func(colID string, ascending bool) {
		b.trackList.Sort(colID, ascending)
		b.columnHeader.SetSort(colID, ascending)
	})

	b.toolbar = NewBrowserToolbar(bus, func(deck int) {
		b.targetDeck = deck
	}, func() []model.Track {
		return b.trackList.UnanalyzedTracks()
	})

	// Vertical separator between sidebar and content
	sidebarSep := canvas.NewRectangle(boomtheme.ColorSeparator)
	sidebarSep.SetMinSize(fyne.NewSize(0.5, 0))

	// Thin accent strips that light up when the MIDI wheel targets that pane.
	b.sidebarFocusBar = canvas.NewRectangle(color.Transparent)
	b.sidebarFocusBar.SetMinSize(fyne.NewSize(2, 0))
	b.trackListFocusBar = canvas.NewRectangle(color.Transparent)
	b.trackListFocusBar.SetMinSize(fyne.NewSize(2, 0))

	// Top bar inside the right pane: toolbar + column header, each optionally
	// hidden via BrowserOpts so mini-mode's overlay can strip them.
	var top fyne.CanvasObject
	switch {
	case b.opts.HideToolbar && b.opts.HideColumnHeader:
		top = nil
	case b.opts.HideToolbar:
		top = b.columnHeader
	case b.opts.HideColumnHeader:
		top = b.toolbar
	default:
		top = container.NewVBox(b.toolbar, b.columnHeader)
	}
	rightInner := container.NewBorder(top, nil, nil, nil, b.trackList)
	rightPanel := container.NewBorder(nil, nil, b.trackListFocusBar, nil, rightInner)

	sidebarWrap := container.NewBorder(nil, nil, b.sidebarFocusBar, sidebarSep, b.sidebar)

	switch b.opts.SidebarMode {
	case SidebarModeFolder:
		// Folder-mode: sidebar stays on the left but in a narrow compact
		// column (120 px vs the classic 180 px) so the track list gets
		// more breathing room on an 800-px-wide overlay. Scroll + focus
		// paths are unchanged; the sidebar still acts as the top-level
		// folder index (Playlists / Genres / BPM / etc.).
		compactWrap := container.New(&fixedWidthLayout{w: 120}, sidebarWrap)
		b.content = container.NewBorder(nil, nil, compactWrap, nil, rightPanel)
	default:
		b.content = container.NewBorder(
			nil, nil,
			sidebarWrap,
			nil,
			rightPanel,
		)
	}

	b.applyFocus()
	b.subscribeEvents()
	b.refreshPlaylistTree()
	b.ExtendBaseWidget(b)
	return b
}

// applyFocus updates the focus accent strips to match the current focus state.
func (b *BrowserView) applyFocus() {
	b.sidebarFocusBar.FillColor = color.Transparent
	b.trackListFocusBar.FillColor = color.Transparent
	switch b.focus {
	case focusSidebar:
		b.sidebarFocusBar.FillColor = boomtheme.ColorBlue
	case focusTrackList:
		b.trackListFocusBar.FillColor = boomtheme.ColorBlue
	}
	b.sidebarFocusBar.Refresh()
	b.trackListFocusBar.Refresh()
}

func (b *BrowserView) subscribeEvents() {
	// Analysis completion: update BPM/Key in track list live
	b.bus.Subscribe(event.TopicAnalysis, func(ev event.Event) error {
		if ev.Action == event.ActionAnalyzeComplete {
			result, ok := ev.Payload.(*event.AnalysisResult)
			if ok {
				b.trackList.UpdateTrackAnalysis(result.TrackID, result.BPM, result.Key)
			}
		}
		return nil
	})

	// Playlist mutations: refresh sidebar tree and, if the currently open
	// playlist changed, refetch its tracks.
	b.bus.Subscribe(event.TopicPlaylist, func(ev event.Event) error {
		switch ev.Action {
		case event.ActionPlaylistTreeChanged:
			b.refreshPlaylistTree()
		case event.ActionPlaylistTracksChanged, event.ActionPlaylistInvalidated:
			id, _ := ev.Payload.(string)
			if id != "" && id == b.currentPlaylistID {
				b.openPlaylist(id)
			}
		}
		return nil
	})

	// MIDI browse wheel: rotate moves selection one item, click toggles focus
	// between the sidebar (directory tree) and the track list.
	b.bus.Subscribe(event.TopicLibrary, func(ev event.Event) error {
		switch ev.Action {
		case event.ActionBrowseScroll:
			// Library rotary sends positive for CCW; invert so CW advances
			// to the next item.
			delta := -int(ev.Value)
			if delta == 0 {
				return nil
			}
			switch b.focus {
			case focusSidebar:
				b.sidebar.ScrollBy(delta)
			case focusTrackList:
				b.trackList.ScrollBy(delta)
			}
		case event.ActionBrowseSelect:
			b.focus = b.nextFocus()
			b.applyFocus()
			log.Printf("browser: browse_select → focus %s", b.focus)
		}
		return nil
	})

	// MIDI load track buttons: intercept load_track events without payload,
	// fill in the currently selected track and re-publish
	b.bus.Subscribe(event.TopicDeck, func(ev event.Event) error {
		if ev.Action == event.ActionLoadTrack && ev.Payload == nil && ev.DeckID > 0 {
			track := b.trackList.SelectedTrack()
			if track != nil {
				log.Printf("browser: load_track deck %d → loading '%s'", ev.DeckID, track.Title)
				b.bus.PublishAsync(event.Event{
					Topic:   event.TopicDeck,
					Action:  event.ActionLoadTrack,
					DeckID:  ev.DeckID,
					Payload: track,
				})
			}
		}
		return nil
	})
}

// TrackList exposes the inner track list so the window can wire
// browser-scoped keyboard shortcuts against it.
func (b *BrowserView) TrackList() *TrackList { return b.trackList }

func (b *BrowserView) SetTracks(tracks []model.Track) {
	b.trackList.SetTracks(tracks)
	b.toolbar.UpdateTrackCount(len(tracks))
}

// SetGenres updates the sidebar with available genres.
func (b *BrowserView) SetGenres(genres []string) {
	b.sidebar.SetGenres(genres)
}

// onCategorySelected routes sidebar selection changes. Playlist categories
// ("playlist:<id>") are handled locally via the PlaylistService so the
// trackList gets the manual order as stored; everything else goes through
// the existing library bus filter.
func (b *BrowserView) onCategorySelected(categoryID string) {
	if strings.HasPrefix(categoryID, "playlist:") {
		id := strings.TrimPrefix(categoryID, "playlist:")
		b.openPlaylist(id)
		return
	}
	b.currentPlaylistID = ""
	b.trackList.SetReorderable(false)
	b.bus.PublishAsync(event.Event{
		Topic: event.TopicLibrary, Action: event.ActionFilterCategory, Payload: categoryID,
	})
}

func (b *BrowserView) openPlaylist(id string) {
	if b.playlists == nil {
		return
	}
	node, err := b.playlists.Node(id)
	if err != nil || node == nil {
		return
	}
	tracks, err := b.playlists.Tracks(id)
	if err != nil {
		log.Printf("playlist tracks: %v", err)
		return
	}
	b.currentPlaylistID = id
	b.trackList.SetReorderable(node.Kind == model.KindManual)
	b.trackList.SetPlaylistID(id)
	b.SetTracks(tracks)
}

// refreshPlaylistTree fetches the full tree from the service and pushes it
// into the sidebar. Safe to call on any goroutine.
func (b *BrowserView) refreshPlaylistTree() {
	if b.playlists == nil {
		return
	}
	tree, err := b.playlists.Tree()
	if err != nil {
		log.Printf("playlist tree: %v", err)
		return
	}
	b.sidebar.SetPlaylistTree(tree)
}

func (b *BrowserView) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(b.content)
}
