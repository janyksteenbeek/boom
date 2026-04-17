package layout

import (
	"fmt"
	"image/color"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/internal/event"
	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
)

// loopBadge is a small pill that appears above each deck card when a
// loop is set — "LOOP 4", "LOOP 1/2", etc. Hidden when no loop is
// active/stored. Mirrors the RELOOP button state on desktop so the
// hardware controller's loop knob has a visible screen confirmation.
type loopBadge struct {
	widget.BaseWidget

	deckID int

	mu sync.Mutex

	label *canvas.Text
	bg    *canvas.Rectangle
	stack *fyne.Container
}

func newLoopBadge(deckID int, bus *event.Bus) *loopBadge {
	b := &loopBadge{deckID: deckID}

	b.label = canvas.NewText("", boomtheme.ColorLabel)
	b.label.TextSize = 9
	b.label.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
	b.label.Alignment = fyne.TextAlignCenter

	b.bg = canvas.NewRectangle(loopIdleBg())
	b.bg.CornerRadius = 3

	b.stack = container.NewStack(b.bg, container.NewPadded(b.label))
	b.stack.Hide()

	bus.Subscribe(event.TopicEngine, func(ev event.Event) error {
		if ev.DeckID != b.deckID {
			return nil
		}
		if ev.Action != event.ActionLoopStateUpdate {
			return nil
		}
		state, _ := ev.Payload.(*event.LoopState)
		b.setState(state)
		return nil
	})

	b.ExtendBaseWidget(b)
	return b
}

func (b *loopBadge) setState(state *event.LoopState) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if state == nil || state.Start < 0 || state.End <= state.Start {
		fyne.Do(func() { b.stack.Hide() })
		return
	}

	text := "LOOP"
	if state.Beats > 0 {
		text = "LOOP " + compactBeatStr(state.Beats)
	}
	active := state.Active

	b.label.Text = text
	if active {
		b.label.Color = boomtheme.ColorBackground
		b.bg.FillColor = boomtheme.DeckColor(b.deckID)
	} else {
		b.label.Color = boomtheme.ColorLabel
		b.bg.FillColor = loopIdleBg()
	}

	fyne.Do(func() {
		b.label.Refresh()
		b.bg.Refresh()
		b.stack.Show()
	})
}

func (b *loopBadge) MinSize() fyne.Size {
	return b.stack.MinSize()
}

func (b *loopBadge) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(b.stack)
}

// loopIdleBg is the dim background used when the loop is set but not
// currently wrapping playback. Semi-transparent to stay subtle.
func loopIdleBg() color.Color {
	return color.NRGBA{R: 60, G: 60, B: 68, A: 255}
}

// compactBeatStr formats a beat count into a short "4" / "1/2" style
// string. Mirrors the formatter that lives inside deckview.go; kept
// local so the mini package stays decoupled from deckview internals.
func compactBeatStr(beats float64) string {
	switch {
	case beats >= 0.999:
		if beats == float64(int(beats)) {
			return fmt.Sprintf("%d", int(beats))
		}
		return fmt.Sprintf("%.1f", beats)
	case beats >= 0.49 && beats <= 0.51:
		return "1/2"
	case beats >= 0.24 && beats <= 0.26:
		return "1/4"
	case beats >= 0.124 && beats <= 0.126:
		return "1/8"
	case beats >= 0.062 && beats <= 0.063:
		return "1/16"
	case beats >= 0.031 && beats <= 0.032:
		return "1/32"
	default:
		return fmt.Sprintf("%.2f", beats)
	}
}

var _ fyne.Widget = (*loopBadge)(nil)
