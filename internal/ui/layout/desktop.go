package layout

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"

	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
)

// Desktop is the traditional large-window layout: toolbar + beat-grid
// overview at the top, decks with mixer sandwich in the middle, and the
// FX bar + browser at the bottom. Mirrors the pre-refactor composition
// in ui.NewWindow verbatim.
type Desktop struct{}

// NewDesktop returns the desktop layout instance.
func NewDesktop() *Desktop { return &Desktop{} }

// Name returns the layout identifier.
func (Desktop) Name() string { return "desktop" }

// Build assembles the desktop root canvas from d.
func (Desktop) Build(d Deps) fyne.CanvasObject {
	toolbarSep := canvas.NewRectangle(boomtheme.ColorSeparator)
	toolbarSep.SetMinSize(fyne.NewSize(0, 1))

	vSepL := canvas.NewRectangle(boomtheme.ColorSeparator)
	vSepL.SetMinSize(fyne.NewSize(1, 0))
	vSepR := canvas.NewRectangle(boomtheme.ColorSeparator)
	vSepR.SetMinSize(fyne.NewSize(1, 0))
	hSep := canvas.NewRectangle(boomtheme.ColorSeparator)
	hSep.SetMinSize(fyne.NewSize(0, 1))

	mixerCol := container.NewHBox(vSepL, d.Mixer, vSepR)
	rightSide := container.NewBorder(nil, nil, mixerCol, nil, d.Deck2)
	decksRow := container.NewHSplit(d.Deck1, rightSide)
	decksRow.SetOffset(0.5)

	fxBarSep := canvas.NewRectangle(boomtheme.ColorSeparator)
	fxBarSep.SetMinSize(fyne.NewSize(0, 1))

	browserArea := container.NewBorder(
		container.NewVBox(hSep, d.FXBar, fxBarSep),
		nil, nil, nil,
		d.Browser,
	)
	mainContent := container.NewVSplit(decksRow, browserArea)
	mainContent.SetOffset(0.55)

	beatGridSep := canvas.NewRectangle(boomtheme.ColorSeparator)
	beatGridSep.SetMinSize(fyne.NewSize(0, 1))

	return container.NewBorder(
		container.NewVBox(d.Toolbar, toolbarSep, d.BeatGrid, beatGridSep),
		nil, nil, nil,
		mainContent,
	)
}
