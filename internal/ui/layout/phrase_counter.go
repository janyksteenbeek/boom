package layout

import (
	"image/color"
	"sort"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/internal/audio"
	"github.com/janyksteenbeek/boom/internal/event"
	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
	"github.com/janyksteenbeek/boom/pkg/model"
)

// phraseBeats is the canonical phrase length in 4/4. 16 beats = 4 bars,
// the typical DJ "phrase" building block. Keep as a constant — changing
// it to 32 would double the pixel count per block and no longer fit
// inside a mini-card comfortably.
const phraseBeats = 16

// phraseCounter renders a row of 16 blocks that light up as playback
// advances through a 4-bar phrase. The currently-playing beat pulses
// brightest; blocks already played stay lit (deck color); blocks not
// yet reached are dim. Resets every 16 beats so the DJ can anticipate
// phrase boundaries at a glance.
//
// Fed by three bus topics:
//   - TopicEngine / ActionTrackLoaded     → capture duration, reset
//   - TopicEngine / ActionWaveformReady   → update duration
//   - TopicAnalysis / ActionAnalyzeComplete → capture beat grid
//   - TopicEngine / ActionPositionUpdate  → drive the animation
type phraseCounter struct {
	widget.BaseWidget

	deckID int

	mu       sync.Mutex
	beats    []float64     // beat times, seconds from track start
	duration time.Duration // full track duration; 0 if unknown
	cur      int           // current beat index; -1 before play

	blocks []*canvas.Rectangle
	dim    color.Color
	accent color.NRGBA
}

func newPhraseCounter(deckID int, bus *event.Bus) *phraseCounter {
	p := &phraseCounter{
		deckID: deckID,
		cur:    -1,
		dim:    color.NRGBA{R: 40, G: 40, B: 48, A: 255},
	}
	accent := boomtheme.DeckColor(deckID)
	if c, ok := accent.(color.RGBA); ok {
		p.accent = color.NRGBA{R: c.R, G: c.G, B: c.B, A: 255}
	} else if c, ok := accent.(color.NRGBA); ok {
		p.accent = c
	} else {
		r, g, b, _ := accent.RGBA()
		p.accent = color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 255}
	}

	p.blocks = make([]*canvas.Rectangle, phraseBeats)
	for i := range p.blocks {
		p.blocks[i] = canvas.NewRectangle(p.dim)
		p.blocks[i].CornerRadius = 1
	}

	bus.Subscribe(event.TopicEngine, func(ev event.Event) error {
		if ev.DeckID != p.deckID {
			return nil
		}
		switch ev.Action {
		case event.ActionTrackLoaded:
			track, _ := ev.Payload.(*model.Track)
			p.resetTrack(track)
		case event.ActionWaveformReady:
			data, _ := ev.Payload.(*audio.WaveformData)
			if data != nil && data.Duration > 0 {
				p.mu.Lock()
				p.duration = data.Duration
				p.mu.Unlock()
			}
		case event.ActionPositionUpdate:
			p.onPosition(ev.Value)
		}
		return nil
	})

	bus.Subscribe(event.TopicAnalysis, func(ev event.Event) error {
		if ev.Action != event.ActionAnalyzeComplete {
			return nil
		}
		res, _ := ev.Payload.(*event.AnalysisResult)
		if res == nil || res.DeckID != p.deckID {
			return nil
		}
		if len(res.BeatGrid) > 0 {
			p.mu.Lock()
			p.beats = res.BeatGrid
			p.mu.Unlock()
		}
		return nil
	})

	p.ExtendBaseWidget(p)
	return p
}

func (p *phraseCounter) resetTrack(track *model.Track) {
	p.mu.Lock()
	if track != nil {
		p.beats = track.BeatGrid
		p.duration = track.Duration
	} else {
		p.beats = nil
		p.duration = 0
	}
	p.cur = -1
	p.mu.Unlock()
	p.repaint(-1)
}

// onPosition is called at the engine's position-update rate (~30 Hz).
// It binary-searches the beat grid to find the current beat index, then
// only repaints when the index crosses a boundary — canvas.Rectangle
// fills are cheap to change but the Refresh call goes through Fyne's
// driver thread, so skipping unchanged frames matters on Pi.
func (p *phraseCounter) onPosition(pos float64) {
	p.mu.Lock()
	beats := p.beats
	dur := p.duration
	p.mu.Unlock()

	if len(beats) == 0 || dur <= 0 {
		return
	}
	if pos < 0 {
		pos = 0
	}
	if pos > 1 {
		pos = 1
	}
	tSec := pos * dur.Seconds()

	// sort.Search: smallest i where beats[i] > tSec, so current beat
	// is i-1. Beats are sorted ascending by construction.
	idx := sort.Search(len(beats), func(i int) bool { return beats[i] > tSec }) - 1
	if idx < 0 {
		idx = -1
	}

	p.mu.Lock()
	if p.cur == idx {
		p.mu.Unlock()
		return
	}
	p.cur = idx
	p.mu.Unlock()

	p.repaint(idx)
}

func (p *phraseCounter) repaint(beatIdx int) {
	inPhrase := -1
	if beatIdx >= 0 {
		inPhrase = beatIdx % phraseBeats
	}

	// Build the color set off-thread, then apply on the Fyne thread
	// in a single pass so we don't churn the driver with 16 separate
	// Refresh calls scheduled from arbitrary goroutines.
	colors := make([]color.Color, phraseBeats)
	for i := range colors {
		switch {
		case inPhrase < 0:
			colors[i] = p.dim
		case i == inPhrase:
			// Current beat — full accent.
			colors[i] = p.accent
		case i < inPhrase:
			// Already played — deck color muted 60 %.
			c := p.accent
			c.A = 160
			colors[i] = c
		default:
			colors[i] = p.dim
		}
	}
	fyne.Do(func() {
		for i, c := range colors {
			p.blocks[i].FillColor = c
			p.blocks[i].Refresh()
		}
	})
}

func (p *phraseCounter) MinSize() fyne.Size {
	return fyne.NewSize(phraseBeats*6+float32(phraseBeats-1), 10)
}

func (p *phraseCounter) CreateRenderer() fyne.WidgetRenderer {
	objs := make([]fyne.CanvasObject, len(p.blocks))
	for i, b := range p.blocks {
		objs[i] = b
	}
	return &phraseCounterRenderer{p: p, objs: objs}
}

type phraseCounterRenderer struct {
	p    *phraseCounter
	objs []fyne.CanvasObject
}

func (r *phraseCounterRenderer) Layout(size fyne.Size) {
	// Fixed-gap grid of 16 blocks. Each block gets an equal share of
	// the width minus inter-block gaps.
	gap := float32(2)
	total := size.Width
	blockW := (total - gap*float32(phraseBeats-1)) / float32(phraseBeats)
	if blockW < 2 {
		blockW = 2
	}
	h := size.Height
	for i, b := range r.p.blocks {
		b.Move(fyne.NewPos(float32(i)*(blockW+gap), 0))
		b.Resize(fyne.NewSize(blockW, h))
	}
}

func (r *phraseCounterRenderer) MinSize() fyne.Size             { return r.p.MinSize() }
func (r *phraseCounterRenderer) Refresh()                       { r.Layout(r.p.Size()) }
func (r *phraseCounterRenderer) Objects() []fyne.CanvasObject   { return r.objs }
func (r *phraseCounterRenderer) Destroy()                       {}
func (r *phraseCounterRenderer) BackgroundColor() color.Color   { return color.Transparent }

var _ fyne.Widget = (*phraseCounter)(nil)
