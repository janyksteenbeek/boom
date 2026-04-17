// Package layout defines pluggable window layouts for the Boom UI. The
// desktop layout is the traditional workstation composition; mini is a
// compact CDJ-like composition for a Raspberry Pi + 5" touch screen.
//
// Layouts receive a Deps bundle containing pre-constructed, event-wired
// widgets and return the root canvas object that Window.SetContent accepts.
// Event subscriptions stay on the Window so layouts don't have to re-wire
// the same events per variant.
package layout

import (
	"fyne.io/fyne/v2"

	"github.com/janyksteenbeek/boom/internal/event"
	"github.com/janyksteenbeek/boom/internal/ui/beatgrid"
	"github.com/janyksteenbeek/boom/internal/ui/browser"
	"github.com/janyksteenbeek/boom/internal/ui/deck"
	"github.com/janyksteenbeek/boom/internal/ui/fxbar"
	"github.com/janyksteenbeek/boom/internal/ui/mixer"
)

// Deps is the set of already-constructed widgets and references a layout
// needs to compose a root canvas object. The window assembles this bundle
// once and hands it to the selected Layout's Build method.
type Deps struct {
	Deck1       *deck.DeckView
	Deck2       *deck.DeckView
	Mixer       *mixer.MixerView
	FXBar       *fxbar.FXBarView
	Browser     *browser.BrowserView
	BeatGrid    *beatgrid.BeatGridStrip
	Toolbar fyne.CanvasObject
	Window  fyne.Window
	Bus     *event.Bus
}

// Layout produces the root content canvas for a window variant.
type Layout interface {
	Name() string
	Build(d Deps) fyne.CanvasObject
}
