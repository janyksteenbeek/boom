package browser

import (
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/internal/event"
	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
	"github.com/janyksteenbeek/boom/pkg/model"
)

type BrowserView struct {
	widget.BaseWidget

	bus          *event.Bus
	sidebar      *Sidebar
	toolbar      *BrowserToolbar
	columnHeader *ColumnHeader
	trackList    *TrackList
	content      *fyne.Container
	targetDeck   int
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

	// Right panel: toolbar + column header + track list
	rightPanel := container.NewBorder(
		container.NewVBox(b.toolbar, b.columnHeader),
		nil, nil, nil,
		b.trackList,
	)

	// Main layout: sidebar | separator | right panel
	// Border layout uses sidebar's MinSize (180px) for the left width
	b.content = container.NewBorder(
		nil, nil,
		container.NewBorder(nil, nil, nil, sidebarSep, b.sidebar),
		nil,
		rightPanel,
	)

	b.subscribeEvents()
	b.ExtendBaseWidget(b)
	return b
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

	// MIDI browse scroll: move selection up/down in the track list
	b.bus.Subscribe(event.TopicLibrary, func(ev event.Event) error {
		switch ev.Action {
		case event.ActionBrowseScroll:
			delta := int(ev.Value)
			if delta == 0 {
				return nil
			}
			b.trackList.ScrollBy(delta)
		case event.ActionBrowseSelect:
			// Load the currently highlighted track on the target deck
			track := b.trackList.SelectedTrack()
			if track != nil {
				log.Printf("browser: browse_select → loading '%s' on deck %d", track.Title, b.targetDeck)
				b.bus.PublishAsync(event.Event{
					Topic:   event.TopicDeck,
					Action:  event.ActionLoadTrack,
					DeckID:  b.targetDeck,
					Payload: track,
				})
			}
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
