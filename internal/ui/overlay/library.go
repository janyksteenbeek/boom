// Package overlay holds modal/popup UI overlays that layer on top of the
// main layout. The library overlay is triggered by the MIDI browse
// encoder in mini-mode and renders a compact track-picker centered on the
// screen — CDJ-style. The main layout is otherwise untouched.
package overlay

import (
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/internal/event"
	"github.com/janyksteenbeek/boom/internal/ui/browser"
	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
	"github.com/janyksteenbeek/boom/pkg/model"
)

// Library wraps a compact BrowserView in a modal popup. Opening, scroll,
// focus-toggle, and closing are driven entirely by bus events so the
// behavior matches what a CDJ user expects:
//
//   - browse encoder push      → open / cycle focus (sidebar ↔ track list)
//   - browse encoder rotate    → scroll the focused pane
//   - LOAD A / LOAD B button   → load selected track, close overlay
//   - ESC key                  → close overlay
type Library struct {
	win     fyne.Window
	browser *browser.BrowserView

	// mu guards popup and visible against concurrent Show/Hide calls.
	// Bus callbacks fire on the bus goroutine; UI callbacks (Close
	// button, ESC) fire on the Fyne thread; both can race.
	mu      sync.Mutex
	popup   *widget.PopUp
	visible bool
}

// NewLibrary constructs the overlay and subscribes to the bus. The
// returned Library does nothing visible until ActionBrowseSelect fires
// for the first time.
func NewLibrary(win fyne.Window, b *browser.BrowserView, bus *event.Bus) *Library {
	l := &Library{win: win, browser: b}

	bus.Subscribe(event.TopicLibrary, func(ev event.Event) error {
		if ev.Action == event.ActionBrowseSelect {
			l.handleBrowseSelect()
		}
		return nil
	})

	// A load_track event with a filled payload means the browser resolved
	// a selection and published it to the engine — that's our cue to
	// close. Events with nil payload are the MIDI-side intents and don't
	// mean a track actually loaded.
	bus.Subscribe(event.TopicDeck, func(ev event.Event) error {
		if ev.Action == event.ActionLoadTrack && ev.Payload != nil {
			if _, ok := ev.Payload.(*model.Track); ok {
				l.Hide()
			}
		}
		return nil
	})

	return l
}

// handleBrowseSelect toggles the overlay when hidden; when visible, the
// BrowserView itself cycles focus between sidebar and track list (its
// own subscriber on the same event). We only drive visibility here.
func (l *Library) handleBrowseSelect() {
	l.mu.Lock()
	shouldShow := !l.visible
	l.mu.Unlock()
	if shouldShow {
		fyne.Do(func() { l.Show() })
	}
}

// Show renders the overlay at 90 % of the current window size, centered.
// Must run on the Fyne thread (callers use fyne.Do).
func (l *Library) Show() {
	l.mu.Lock()
	if l.visible {
		l.mu.Unlock()
		return
	}
	l.mu.Unlock()

	winSize := l.win.Canvas().Size()
	w := winSize.Width * 0.95
	h := winSize.Height * 0.9
	if w < 320 {
		w = 320
	}
	if h < 200 {
		h = 200
	}

	// Popup content is the compact browser wrapped with a thin header
	// carrying a title and a close affordance so touch users have an
	// obvious exit path. The header uses canvas.Text to avoid the
	// built-in button's padding.
	title := canvas.NewText("LIBRARY", boomtheme.ColorLabel)
	title.TextSize = 12
	title.TextStyle = fyne.TextStyle{Bold: true}

	closeBtn := widget.NewButton("Close", func() { l.Hide() })
	header := container.NewBorder(nil, nil, title, closeBtn)

	sep := canvas.NewRectangle(boomtheme.ColorSeparator)
	sep.SetMinSize(fyne.NewSize(0, 1))

	body := container.NewBorder(container.NewVBox(header, sep), nil, nil, nil, l.browser)

	bg := canvas.NewRectangle(boomtheme.ColorBackground)
	sized := container.New(&fixedSizeLayout{w: w, h: h}, container.NewStack(bg, body))

	p := widget.NewModalPopUp(sized, l.win.Canvas())

	l.mu.Lock()
	l.popup = p
	l.visible = true
	l.mu.Unlock()

	p.Show()
}

// Hide dismisses the overlay if it's currently shown. Safe to call from
// any goroutine — the actual Hide() runs on the Fyne thread via fyne.Do,
// and we capture the popup reference locally so the follow-up nil
// assignment can't race the queued call.
func (l *Library) Hide() {
	l.mu.Lock()
	if !l.visible {
		l.mu.Unlock()
		return
	}
	p := l.popup
	l.popup = nil
	l.visible = false
	l.mu.Unlock()

	if p == nil {
		return
	}
	fyne.Do(func() { p.Hide() })
}

// Visible reports whether the overlay is currently displayed.
func (l *Library) Visible() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.visible
}

// fixedSizeLayout sizes its single child to a fixed (w, h). Used so the
// popup contents get the pre-calculated 95 % × 90 % box instead of
// collapsing to MinSize inside widget.ModalPopUp.
type fixedSizeLayout struct {
	w, h float32
}

func (f *fixedSizeLayout) Layout(objs []fyne.CanvasObject, _ fyne.Size) {
	for _, o := range objs {
		o.Resize(fyne.NewSize(f.w, f.h))
		o.Move(fyne.NewPos(0, 0))
	}
}

func (f *fixedSizeLayout) MinSize(_ []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(f.w, f.h)
}
