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

// SegmentedControl is an Apple HIG-style two-segment capsule control.
type SegmentedControl struct {
	widget.BaseWidget

	mu         sync.RWMutex
	options    []string
	colors     []color.Color
	selected   int
	hoverIndex int
	OnChanged  func(index int)
}

var _ desktop.Hoverable = (*SegmentedControl)(nil)
var _ fyne.Tappable = (*SegmentedControl)(nil)

func NewSegmentedControl(options []string, colors []color.Color, onChanged func(int)) *SegmentedControl {
	s := &SegmentedControl{
		options:    options,
		colors:     colors,
		selected:   0,
		hoverIndex: -1,
		OnChanged:  onChanged,
	}
	s.ExtendBaseWidget(s)
	return s
}

func (s *SegmentedControl) SetSelected(index int) {
	s.mu.Lock()
	s.selected = index
	s.mu.Unlock()
	fyne.Do(func() { s.Refresh() })
}

func (s *SegmentedControl) Tapped(ev *fyne.PointEvent) {
	s.mu.Lock()
	half := s.Size().Width / 2
	var idx int
	if ev.Position.X > half {
		idx = 1
	}
	s.selected = idx
	s.mu.Unlock()
	s.Refresh()
	if s.OnChanged != nil {
		s.OnChanged(idx)
	}
}

func (s *SegmentedControl) MouseIn(ev *desktop.MouseEvent) {
	s.mu.Lock()
	half := s.Size().Width / 2
	if ev.Position.X > half {
		s.hoverIndex = 1
	} else {
		s.hoverIndex = 0
	}
	s.mu.Unlock()
	s.Refresh()
}

func (s *SegmentedControl) MouseMoved(ev *desktop.MouseEvent) {
	s.mu.Lock()
	half := s.Size().Width / 2
	newIdx := 0
	if ev.Position.X > half {
		newIdx = 1
	}
	changed := s.hoverIndex != newIdx
	s.hoverIndex = newIdx
	s.mu.Unlock()
	if changed {
		s.Refresh()
	}
}

func (s *SegmentedControl) MouseOut() {
	s.mu.Lock()
	s.hoverIndex = -1
	s.mu.Unlock()
	s.Refresh()
}

func (s *SegmentedControl) MinSize() fyne.Size {
	return fyne.NewSize(140, 28)
}

func (s *SegmentedControl) CreateRenderer() fyne.WidgetRenderer {
	r := &segRenderer{seg: s}
	r.build()
	return r
}

type segRenderer struct {
	seg    *SegmentedControl
	bg     *canvas.Rectangle
	seg0Bg *canvas.Rectangle
	seg1Bg *canvas.Rectangle
	label0 *canvas.Text
	label1 *canvas.Text
	divider *canvas.Rectangle
	allObjs []fyne.CanvasObject
}

func (r *segRenderer) build() {
	r.bg = canvas.NewRectangle(boomtheme.ColorBackgroundTertiary)
	r.bg.CornerRadius = 8
	r.bg.StrokeColor = boomtheme.ColorSeparator
	r.bg.StrokeWidth = 0.5

	r.seg0Bg = canvas.NewRectangle(color.Transparent)
	r.seg0Bg.CornerRadius = 7

	r.seg1Bg = canvas.NewRectangle(color.Transparent)
	r.seg1Bg.CornerRadius = 7

	r.divider = canvas.NewRectangle(boomtheme.ColorSeparator)

	r.label0 = canvas.NewText("", boomtheme.ColorLabelSecondary)
	r.label0.TextSize = 11
	r.label0.TextStyle = fyne.TextStyle{Bold: true}
	r.label0.Alignment = fyne.TextAlignCenter

	r.label1 = canvas.NewText("", boomtheme.ColorLabelSecondary)
	r.label1.TextSize = 11
	r.label1.TextStyle = fyne.TextStyle{Bold: true}
	r.label1.Alignment = fyne.TextAlignCenter

	r.allObjs = []fyne.CanvasObject{r.bg, r.seg0Bg, r.seg1Bg, r.divider, r.label0, r.label1}
	r.Refresh()
}

func (r *segRenderer) Layout(size fyne.Size) { r.layout(size) }
func (r *segRenderer) MinSize() fyne.Size    { return r.seg.MinSize() }
func (r *segRenderer) Destroy()              {}
func (r *segRenderer) Objects() []fyne.CanvasObject { return r.allObjs }

func (r *segRenderer) Refresh() {
	r.layout(r.seg.Size())
}

func (r *segRenderer) layout(size fyne.Size) {
	r.seg.mu.RLock()
	selected := r.seg.selected
	hoverIdx := r.seg.hoverIndex
	options := r.seg.options
	colors := r.seg.colors
	r.seg.mu.RUnlock()

	if size.Width <= 0 || size.Height <= 0 {
		return
	}

	half := size.Width / 2
	inset := float32(2)

	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))

	// Segment backgrounds
	segSize := fyne.NewSize(half-inset*2, size.Height-inset*2)

	r.seg0Bg.Resize(segSize)
	r.seg0Bg.Move(fyne.NewPos(inset, inset))

	r.seg1Bg.Resize(segSize)
	r.seg1Bg.Move(fyne.NewPos(half+inset, inset))

	// Divider between segments
	r.divider.Resize(fyne.NewSize(0.5, size.Height-8))
	r.divider.Move(fyne.NewPos(half, 4))

	// Selection and hover colors — white/neutral, no colored highlights
	var seg0Color, seg1Color color.Color = color.Transparent, color.Transparent

	if selected == 0 {
		seg0Color = color.RGBA{R: 255, G: 255, B: 255, A: 40}
		r.divider.Hidden = true
	} else {
		seg1Color = color.RGBA{R: 255, G: 255, B: 255, A: 40}
		r.divider.Hidden = true
	}

	if hoverIdx == 0 && selected != 0 {
		seg0Color = boomtheme.ColorSidebarItemHover
	}
	if hoverIdx == 1 && selected != 1 {
		seg1Color = boomtheme.ColorSidebarItemHover
	}
	_ = colors

	r.seg0Bg.FillColor = seg0Color
	r.seg1Bg.FillColor = seg1Color

	// Labels
	if len(options) > 0 {
		r.label0.Text = options[0]
	}
	if len(options) > 1 {
		r.label1.Text = options[1]
	}

	if selected == 0 {
		r.label0.Color = boomtheme.ColorLabel
	} else {
		r.label0.Color = boomtheme.ColorLabelSecondary
	}
	if selected == 1 {
		r.label1.Color = boomtheme.ColorLabel
	} else {
		r.label1.Color = boomtheme.ColorLabelSecondary
	}

	labelY := (size.Height - 13) / 2
	r.label0.Move(fyne.NewPos(0, labelY))
	r.label0.Resize(fyne.NewSize(half, 13))
	r.label1.Move(fyne.NewPos(half, labelY))
	r.label1.Resize(fyne.NewSize(half, 13))

	r.bg.Refresh()
	r.seg0Bg.Refresh()
	r.seg1Bg.Refresh()
	r.divider.Refresh()
	r.label0.Refresh()
	r.label1.Refresh()
}

func withAlpha(c color.Color, a uint8) color.Color {
	r, g, b, _ := c.RGBA()
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: a}
}
