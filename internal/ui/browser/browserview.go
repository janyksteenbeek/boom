package browser

import (
	"image/color"
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/internal/event"
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

type BrowserView struct {
	widget.BaseWidget

	bus               *event.Bus
	sidebar           *Sidebar
	toolbar           *BrowserToolbar
	columnHeader      *ColumnHeader
	trackList         *TrackList
	content           *fyne.Container
	targetDeck        int
	focus             browseFocus
	sidebarFocusBar   *canvas.Rectangle
	trackListFocusBar *canvas.Rectangle
}

func NewBrowserView(bus *event.Bus) *BrowserView {
	b := &BrowserView{bus: bus, targetDeck: 1}

	// Sidebar
	b.sidebar = NewSidebar(func(categoryID string) {
		bus.PublishAsync(event.Event{
			Topic: event.TopicLibrary, Action: event.ActionFilterCategory, Payload: categoryID,
		})
	})

	// Track list
	b.trackList = NewTrackList(func(track model.Track) {
		bus.Publish(event.Event{
			Topic: event.TopicDeck, Action: event.ActionLoadTrack, DeckID: b.targetDeck, Payload: &track,
		})
	})

	// Column header with sort
	b.columnHeader = NewColumnHeader(defaultColumns, func(colID string, ascending bool) {
		b.trackList.Sort(colID, ascending)
		b.columnHeader.SetSort(colID, ascending)
	})

	// Toolbar
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

	// Right panel: toolbar + column header + track list, with focus bar on left
	rightInner := container.NewBorder(
		container.NewVBox(b.toolbar, b.columnHeader),
		nil, nil, nil,
		b.trackList,
	)
	rightPanel := container.NewBorder(nil, nil, b.trackListFocusBar, nil, rightInner)

	// Main layout: focus bar | sidebar | separator | right panel
	// Border layout uses sidebar's MinSize (180px) for the left width
	sidebarWrap := container.NewBorder(nil, nil, b.sidebarFocusBar, sidebarSep, b.sidebar)
	b.content = container.NewBorder(
		nil, nil,
		sidebarWrap,
		nil,
		rightPanel,
	)

	b.applyFocus()
	b.subscribeEvents()
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

	// MIDI browse wheel: rotate moves selection one item, click toggles focus
	// between the sidebar (directory tree) and the track list.
	b.bus.Subscribe(event.TopicLibrary, func(ev event.Event) error {
		switch ev.Action {
		case event.ActionBrowseScroll:
			// Hardware sends positive for CCW on the Pioneer library rotary;
			// invert so CW advances to the next item.
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

func (b *BrowserView) SetTracks(tracks []model.Track) {
	b.trackList.SetTracks(tracks)
	b.toolbar.UpdateTrackCount(len(tracks))
}

// SetGenres updates the sidebar with available genres.
func (b *BrowserView) SetGenres(genres []string) {
	b.sidebar.SetGenres(genres)
}

func (b *BrowserView) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(b.content)
}
