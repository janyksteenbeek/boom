package components

import (
	"image/color"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
)

// DJButton is a custom-drawn button with an active color indicator.
type DJButton struct {
	widget.BaseWidget

	mu       sync.RWMutex
	text     string
	active   bool
	color    color.Color
	hovered  bool
	OnTapped func()
}

func NewDJButton(text string, activeColor color.Color, onTapped func()) *DJButton {
	b := &DJButton{
		text:     text,
		color:    activeColor,
		OnTapped: onTapped,
	}
	b.ExtendBaseWidget(b)
	return b
}

func (b *DJButton) SetActive(active bool) {
	b.mu.Lock()
	b.active = active
	b.mu.Unlock()
	fyne.Do(func() { b.Refresh() })
}

func (b *DJButton) SetText(text string) {
	b.mu.Lock()
	b.text = text
	b.mu.Unlock()
	fyne.Do(func() { b.Refresh() })
}

func (b *DJButton) Tapped(_ *fyne.PointEvent) {
	// Intentionally empty — MouseDown handles the action for instant response
}

// MouseDown fires immediately on press — more reliable than Tapped for DJ use.
func (b *DJButton) MouseDown(ev *desktop.MouseEvent) {
	if ev.Button == desktop.MouseButtonPrimary && b.OnTapped != nil {
		b.OnTapped()
	}
}

func (b *DJButton) MouseUp(_ *desktop.MouseEvent) {}

func (b *DJButton) MouseIn(_ *desktop.MouseEvent) {
	b.mu.Lock()
	b.hovered = true
	b.mu.Unlock()
	b.Refresh()
}

func (b *DJButton) MouseMoved(_ *desktop.MouseEvent) {}

func (b *DJButton) MouseOut() {
	b.mu.Lock()
	b.hovered = false
	b.mu.Unlock()
	b.Refresh()
}

func (b *DJButton) MinSize() fyne.Size {
	return fyne.NewSize(60, 28)
}

func (b *DJButton) CreateRenderer() fyne.WidgetRenderer {
	r := &djBtnRenderer{btn: b}
	r.build()
	return r
}

type djBtnRenderer struct {
	btn     *DJButton
	bg      *canvas.Rectangle
	accent  *canvas.Rectangle
	label   *canvas.Text
	allObjs []fyne.CanvasObject
}

func (r *djBtnRenderer) build() {
	r.bg = canvas.NewRectangle(boomtheme.ColorBackgroundTertiary)
	r.bg.CornerRadius = 6
	r.bg.StrokeColor = boomtheme.ColorSeparator
	r.bg.StrokeWidth = 1

	r.accent = canvas.NewRectangle(color.Transparent)
	r.accent.CornerRadius = 1

	r.label = canvas.NewText("", boomtheme.ColorLabelSecondary)
	r.label.TextSize = 10
	r.label.TextStyle = fyne.TextStyle{Bold: true}
	r.label.Alignment = fyne.TextAlignCenter

	r.allObjs = []fyne.CanvasObject{r.bg, r.accent, r.label}
	r.Refresh()
}

func (r *djBtnRenderer) Layout(size fyne.Size) { r.layout(size) }
func (r *djBtnRenderer) MinSize() fyne.Size    { return r.btn.MinSize() }
func (r *djBtnRenderer) Destroy()              {}
func (r *djBtnRenderer) Objects() []fyne.CanvasObject { return r.allObjs }

func (r *djBtnRenderer) Refresh() {
	r.layout(r.btn.Size())
}

func (r *djBtnRenderer) layout(size fyne.Size) {
	r.btn.mu.RLock()
	text := r.btn.text
	active := r.btn.active
	activeColor := r.btn.color
	hovered := r.btn.hovered
	r.btn.mu.RUnlock()

	if size.Width <= 0 || size.Height <= 0 {
		return
	}

	// Background
	var bgColor color.Color = boomtheme.ColorBackgroundTertiary
	if hovered {
		bgColor = boomtheme.ColorFill
	}
	if active {
		bgColor = blendColor(boomtheme.ColorBackgroundTertiary, activeColor, 0.2)
	}
	r.bg.FillColor = bgColor
	if active {
		r.bg.StrokeColor = activeColor
	} else {
		r.bg.StrokeColor = boomtheme.ColorSeparator
	}
	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))

	// Top accent
	if active {
		r.accent.FillColor = activeColor
		r.accent.Resize(fyne.NewSize(size.Width-8, 2))
		r.accent.Move(fyne.NewPos(4, 2))
	} else {
		r.accent.FillColor = color.Transparent
	}

	// Label
	if active {
		r.label.Color = boomtheme.ColorLabel
	} else {
		r.label.Color = boomtheme.ColorLabelSecondary
	}
	r.label.Text = text
	r.label.Move(fyne.NewPos(0, (size.Height-12)/2))
	r.label.Resize(fyne.NewSize(size.Width, 12))

	r.bg.Refresh()
	r.accent.Refresh()
	r.label.Refresh()
}

func blendColor(base, overlay color.Color, alpha float64) color.Color {
	br, bg, bb, _ := base.RGBA()
	or, og, ob, _ := overlay.RGBA()
	return color.RGBA{
		R: uint8((float64(br>>8)*(1-alpha) + float64(or>>8)*alpha)),
		G: uint8((float64(bg>>8)*(1-alpha) + float64(og>>8)*alpha)),
		B: uint8((float64(bb>>8)*(1-alpha) + float64(ob>>8)*alpha)),
		A: 255,
	}
}
