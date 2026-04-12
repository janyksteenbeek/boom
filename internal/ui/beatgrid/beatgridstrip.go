package beatgrid

import (
	"encoding/json"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/internal/audio"
	"github.com/janyksteenbeek/boom/internal/event"
	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
	"github.com/janyksteenbeek/boom/pkg/model"
)

const (
	defaultZoom = 0.1
	minZoom     = 0.02
	maxZoom     = 1.0
	zoomFactor  = 1.2
)

// BeatGridStrip is a composite widget showing two scrolling waveform strips
// (one per deck) with beat markers, stacked vertically. Both strips share
// a zoom level for visual beat alignment.
type BeatGridStrip struct {
	widget.BaseWidget

	mu       sync.RWMutex
	strip1   *DeckStrip
	strip2   *DeckStrip
	zoom     float64
	trackIDs [2]string
	content  *fyne.Container
}

var _ desktop.Hoverable = (*BeatGridStrip)(nil)
var _ fyne.Tappable = (*BeatGridStrip)(nil)

func NewBeatGridStrip(bus *event.Bus) *BeatGridStrip {
	b := &BeatGridStrip{
		strip1: NewDeckStrip(1),
		strip2: NewDeckStrip(2),
		zoom:   defaultZoom,
	}

	sep := canvas.NewRectangle(boomtheme.ColorSeparator)
	sep.SetMinSize(fyne.NewSize(0, 0.5))

	b.content = container.NewVBox(b.strip1, sep, b.strip2)

	b.ExtendBaseWidget(b)
	return b
}

// UpdatePosition updates the playback position for a deck.
func (b *BeatGridStrip) UpdatePosition(deckID int, pos float64) {
	b.strip(deckID).SetPosition(pos)
}

// SetWaveformData sets the waveform peaks for a deck.
func (b *BeatGridStrip) SetWaveformData(deckID int, data *audio.WaveformData) {
	s := b.strip(deckID)
	s.SetDuration(data.Duration)
	s.SetFrequencyPeaks(data.PeaksLow, data.PeaksMid, data.PeaksHigh)
}

// SetTrack sets track metadata and parses pre-existing beat grid JSON.
func (b *BeatGridStrip) SetTrack(deckID int, track *model.Track) {
	b.mu.Lock()
	if deckID >= 1 && deckID <= 2 {
		b.trackIDs[deckID-1] = track.ID
	}
	b.mu.Unlock()

	s := b.strip(deckID)
	s.SetDuration(track.Duration)

	// Parse beat grid from JSON if available
	if track.BeatGrid != "" {
		var beats []float64
		if json.Unmarshal([]byte(track.BeatGrid), &beats) == nil && len(beats) > 0 {
			s.SetBeatGrid(beats)
		}
	} else {
		s.SetBeatGrid(nil)
	}
}

// SetBeatGrid sets the beat grid directly (from analysis results).
func (b *BeatGridStrip) SetBeatGrid(deckID int, beats []float64) {
	b.strip(deckID).SetBeatGrid(beats)
}

// SetCuePoint forwards cue-point updates to the scrolling strip.
func (b *BeatGridStrip) SetCuePoint(deckID int, pos float64) {
	b.strip(deckID).SetCuePoint(pos)
}

// SetLoopState forwards engine loop-state updates to the scrolling strip
// so the orange region tracks the waveform overview in sync with the deck.
func (b *BeatGridStrip) SetLoopState(deckID int, state *event.LoopState) {
	if state == nil {
		b.strip(deckID).SetLoopState(-1, -1, false)
		return
	}
	b.strip(deckID).SetLoopState(state.Start, state.End, state.Active)
}

// Scrolled handles mouse wheel for zoom control.
func (b *BeatGridStrip) Scrolled(ev *fyne.ScrollEvent) {
	b.mu.Lock()
	if ev.Scrolled.DY > 0 {
		b.zoom /= zoomFactor // zoom in
	} else if ev.Scrolled.DY < 0 {
		b.zoom *= zoomFactor // zoom out
	}
	if b.zoom < minZoom {
		b.zoom = minZoom
	}
	if b.zoom > maxZoom {
		b.zoom = maxZoom
	}
	zoom := b.zoom
	b.mu.Unlock()

	b.strip1.SetZoom(zoom)
	b.strip2.SetZoom(zoom)
}

// Tapped is a no-op but required for scroll events to work.
func (b *BeatGridStrip) Tapped(_ *fyne.PointEvent) {}

// MouseIn, MouseMoved, MouseOut — implement Hoverable so scroll events fire.
func (b *BeatGridStrip) MouseIn(_ *desktop.MouseEvent)    {}
func (b *BeatGridStrip) MouseMoved(_ *desktop.MouseEvent) {}
func (b *BeatGridStrip) MouseOut()                        {}

func (b *BeatGridStrip) strip(deckID int) *DeckStrip {
	if deckID == 2 {
		return b.strip2
	}
	return b.strip1
}

func (b *BeatGridStrip) MinSize() fyne.Size {
	return fyne.NewSize(100, 113)
}

func (b *BeatGridStrip) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(b.content)
}
