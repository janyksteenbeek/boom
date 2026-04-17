package layout

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"

	"github.com/janyksteenbeek/boom/internal/event"
	"github.com/janyksteenbeek/boom/internal/ui/components"
)

// newDeckVU builds a thin vertical peak meter for a single deck and
// wires it to TopicEngine / ActionVULevel. Returns a sized container
// so callers can drop it into a row without juggling widget sizes.
// Width is tight so it can sit alongside the waveform without eating
// horizontal real estate.
func newDeckVU(deckID int, bus *event.Bus) fyne.CanvasObject {
	meter := components.NewPeakMeter()

	bus.Subscribe(event.TopicEngine, func(ev event.Event) error {
		if ev.Action != event.ActionVULevel || ev.DeckID != deckID {
			return nil
		}
		meter.SetLevel(ev.Value)
		return nil
	})

	return container.New(&fixedWidth{w: 8}, meter)
}

// fixedWidth sizes its only child to a fixed pixel width with the
// container's assigned height. Used so the peak meter stays a thin
// column inside the deck card.
type fixedWidth struct {
	w float32
}

func (f *fixedWidth) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Resize(fyne.NewSize(f.w, size.Height))
		o.Move(fyne.NewPos(0, 0))
	}
}

func (f *fixedWidth) MinSize(objs []fyne.CanvasObject) fyne.Size {
	h := float32(0)
	for _, o := range objs {
		m := o.MinSize()
		if m.Height > h {
			h = m.Height
		}
	}
	return fyne.NewSize(f.w, h)
}
