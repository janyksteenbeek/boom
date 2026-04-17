package layout

import (
	"image/color"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/internal/event"
	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
)

// playStateIndicator is a tap-to-toggle status light for a deck: a
// colored dot + one-word label (PLAY / STOP). Subscribes to
// TopicEngine / ActionPlayState so the dot reflects the authoritative
// engine state without polling. Tapping publishes ActionPlayPause —
// same contract as the hardware PLAY button.
//
// We use it in mini-mode where the hardware's transport buttons aren't
// on screen — the DJ still needs a glanceable cue/play status, and on
// touch screens it doubles as a big fat play button.
type playStateIndicator struct {
	widget.BaseWidget

	deckID int
	bus    *event.Bus

	mu      sync.Mutex
	playing bool

	dot   *canvas.Circle
	label *canvas.Text
}

var _ fyne.Tappable = (*playStateIndicator)(nil)

func newPlayStateIndicator(deckID int, bus *event.Bus) *playStateIndicator {
	p := &playStateIndicator{deckID: deckID, bus: bus}

	p.dot = canvas.NewCircle(boomtheme.ColorLabelTertiary)
	p.dot.StrokeWidth = 0

	p.label = canvas.NewText("STOP", boomtheme.ColorLabelTertiary)
	p.label.TextSize = 9
	p.label.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}

	bus.Subscribe(event.TopicEngine, func(ev event.Event) error {
		if ev.DeckID != p.deckID {
			return nil
		}
		if ev.Action == event.ActionPlayState {
			p.setPlaying(ev.Value > 0.5)
		}
		return nil
	})

	p.ExtendBaseWidget(p)
	return p
}

func (p *playStateIndicator) setPlaying(playing bool) {
	p.mu.Lock()
	if p.playing == playing {
		p.mu.Unlock()
		return
	}
	p.playing = playing
	p.mu.Unlock()

	fyne.Do(func() {
		if playing {
			c := boomtheme.DeckColor(p.deckID)
			p.dot.FillColor = c
			p.label.Text = "PLAY"
			p.label.Color = c
		} else {
			p.dot.FillColor = boomtheme.ColorLabelTertiary
			p.label.Text = "STOP"
			p.label.Color = boomtheme.ColorLabelTertiary
		}
		p.dot.Refresh()
		p.label.Refresh()
	})
}

// Tapped publishes an ActionPlayPause for this deck. The engine
// toggles, and the dot/label update via the ActionPlayState subscriber.
func (p *playStateIndicator) Tapped(_ *fyne.PointEvent) {
	p.bus.Publish(event.Event{
		Topic:  event.TopicDeck,
		Action: event.ActionPlayPause,
		DeckID: p.deckID,
	})
}

func (p *playStateIndicator) MinSize() fyne.Size {
	// Slightly taller than the dot so the whole row gives a
	// finger-friendly hit target on the Pi touch screen.
	return fyne.NewSize(56, 20)
}

func (p *playStateIndicator) CreateRenderer() fyne.WidgetRenderer {
	return &playStateRenderer{p: p, objs: []fyne.CanvasObject{p.dot, p.label}}
}

type playStateRenderer struct {
	p    *playStateIndicator
	objs []fyne.CanvasObject
}

func (r *playStateRenderer) Layout(size fyne.Size) {
	// Dot sits vertically centered on the left; label to its right
	// with a 4-px gutter.
	dotD := float32(8)
	dotY := (size.Height - dotD) / 2
	r.p.dot.Move(fyne.NewPos(0, dotY))
	r.p.dot.Resize(fyne.NewSize(dotD, dotD))

	labelY := (size.Height - r.p.label.MinSize().Height) / 2
	r.p.label.Move(fyne.NewPos(dotD+4, labelY))
	r.p.label.Resize(fyne.NewSize(size.Width-dotD-4, r.p.label.MinSize().Height))
}

func (r *playStateRenderer) MinSize() fyne.Size              { return r.p.MinSize() }
func (r *playStateRenderer) Refresh()                        { r.Layout(r.p.Size()) }
func (r *playStateRenderer) Objects() []fyne.CanvasObject    { return r.objs }
func (r *playStateRenderer) Destroy()                        {}
func (r *playStateRenderer) BackgroundColor() color.Color    { return color.Transparent }

var _ fyne.Widget = (*playStateIndicator)(nil)
