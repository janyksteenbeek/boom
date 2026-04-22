package layout

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"

	"github.com/janyksteenbeek/boom/internal/ui/overlay"
	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
)

// Mini is the 800x480 controller-screen layout for Raspberry Pi + 5"
// touch. A fixed-height scrolling beat-grid band sits on top; two deck
// cards fill the remainder side by side. Each card stacks an accent
// banner, a tappable play/stop indicator, title/artist + key + BPM, a
// time row, a phrase counter, and the full-track overview with a
// compact VU meter. Transport / EQ / FX controls are deliberately
// absent — the hardware controller owns those.
type Mini struct{}

// NewMini returns the mini layout instance.
func NewMini() *Mini { return &Mini{} }

// Name returns the layout identifier.
func (Mini) Name() string { return "mini" }

// Build assembles the mini root canvas. The browser widget is not laid
// out inline; the library-overlay package lifts it into a modal popup
// when the user presses the browse encoder.
func (Mini) Build(d Deps) fyne.CanvasObject {
	// Shrink the pre-allocated waveform bar count. Each bar is 3
	// canvas.Line objects (one per frequency band) and Fyne's
	// compositor iterates every visible line per frame — at 128 bars
	// we pay 768 lines across both decks vs 2400 at the desktop
	// default. The detail loss is invisible on an 800-px-wide card.
	d.Deck1.WaveformWidget().SetMaxBars(128)
	d.Deck2.WaveformWidget().SetMaxBars(128)

	// Beat-grid band: each scrolling strip gets its own deck-colored
	// accent strip on the left so the two decks are clearly
	// separable even when one hasn't loaded a track yet (otherwise
	// the empty strip's near-black bg blends into the window and
	// looks like the deck is missing).
	gridRow := func(deckID int) fyne.CanvasObject {
		accent := canvas.NewRectangle(boomtheme.DeckColor(deckID))
		accent.SetMinSize(fyne.NewSize(4, 0))
		return container.NewBorder(nil, nil, accent, nil, d.BeatGrid.Strip(deckID))
	}
	topBand := container.New(&fixedHeight{h: 140},
		container.NewGridWithRows(2, gridRow(1), gridRow(2)),
	)

	hSep := canvas.NewRectangle(boomtheme.ColorSeparator)
	hSep.SetMinSize(fyne.NewSize(0, 1))

	// Deck cards take the rest of the screen.
	cards := container.NewGridWithColumns(2, deckCard(d, 1), deckCard(d, 2))

	// Wire the fullscreen library-overlay. The BrowserView is not
	// rendered inline in mini mode — the overlay hands it to a modal
	// popup when the user presses the browse encoder.
	overlay.NewLibrary(d.Window, d.Browser, d.Bus)

	return container.NewBorder(
		container.NewVBox(topBand, hSep),
		nil, nil, nil,
		cards,
	)
}

// deckCard composes the per-deck card for mini-mode.
//
// Vertical stack, top → bottom:
//
//	 4-px accent strip (deck color)
//	 status row:     ● PLAY/STOP (tap to toggle) | loop-badge
//	 header:         title/artist + key + BPM    (from DeckView)
//	 time row:       elapsed / remaining         (tappable to swap)
//	 phrase counter: 16 blocks tracking beat position in a 4-bar phrase
//	 waveform + VU:  full-track overview alongside a thin peak meter
//
// All sub-widgets own their own bus subscriptions; this function only
// arranges them and supplies the per-deck context.
func deckCard(d Deps, deckID int) fyne.CanvasObject {
	accent := boomtheme.DeckColor(deckID)

	// Thin colored accent strip at the very top — deck identity at a
	// glance, distinguishing decks from across the room.
	banner := canvas.NewRectangle(tintedAccent(accent))
	banner.SetMinSize(fyne.NewSize(0, 4))

	deckView := d.Deck1
	if deckID == 2 {
		deckView = d.Deck2
	}

	playState := newPlayStateIndicator(deckID, d.Bus)
	loopBadge := newLoopBadge(deckID, d.Bus)
	phraseRow := newPhraseCounter(deckID, d.Bus)
	vu := newDeckVU(deckID, d.Bus)

	// Status row: tap-to-play/stop on the left, loop badge on the
	// right. Border keeps both pinned so long titles in the header
	// below can't push them around.
	statusRow := container.NewBorder(nil, nil, playState, loopBadge)

	textRows := container.NewVBox(
		banner,
		statusRow,
		deckView.Header(),
		deckView.TimeRow(),
		phraseRow,
	)

	// Waveform + VU meter share the remaining vertical space. The VU
	// pinned right is only 8 px wide, so the waveform still dominates.
	waveRow := container.NewBorder(nil, nil, nil, vu, deckView.WaveformWidget())

	return container.NewBorder(textRows, nil, nil, nil, waveRow)
}

// tintedAccent returns a slightly muted version of the deck color so
// the 4-px banner reads as an accent instead of a harsh solid bar.
func tintedAccent(c color.Color) color.Color {
	r, g, b, _ := c.RGBA()
	return color.NRGBA{
		R: uint8(r >> 8),
		G: uint8(g >> 8),
		B: uint8(b >> 8),
		A: 220,
	}
}

// fixedHeight forces its (single) child to a fixed pixel height while
// letting it stretch horizontally. Used for the beat-grid band so its
// two children get predictable vertical slices instead of collapsing
// to their MinSize stack.
type fixedHeight struct {
	h float32
}

func (f *fixedHeight) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Resize(fyne.NewSize(size.Width, f.h))
		o.Move(fyne.NewPos(0, 0))
	}
}

func (f *fixedHeight) MinSize(objs []fyne.CanvasObject) fyne.Size {
	w := float32(0)
	for _, o := range objs {
		m := o.MinSize()
		if m.Width > w {
			w = m.Width
		}
	}
	return fyne.NewSize(w, f.h)
}
