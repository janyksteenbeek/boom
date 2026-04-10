package components

import (
	"image/color"
	"math"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"

	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
)

// Knob is a modern arc-based rotary control.
type Knob struct {
	widget.BaseWidget

	mu        sync.RWMutex
	value     float64
	label     string
	accentClr color.Color
	OnChanged func(float64)
}

var _ fyne.Draggable = (*Knob)(nil)
var _ fyne.Tappable = (*Knob)(nil)

func NewKnob(label string, value float64, c color.Color, onChange func(float64)) *Knob {
	k := &Knob{
		label:     label,
		value:     value,
		accentClr: c,
		OnChanged: onChange,
	}
	k.ExtendBaseWidget(k)
	return k
}

func (k *Knob) SetValue(v float64) {
	k.mu.Lock()
	k.value = clamp(v)
	k.mu.Unlock()
	fyne.Do(func() {
		k.Refresh()
	})
}

func (k *Knob) Tapped(_ *fyne.PointEvent) {}

func (k *Knob) Dragged(ev *fyne.DragEvent) {
	k.mu.Lock()
	delta := float64(-ev.Dragged.DY) / 120.0
	k.value = clamp(k.value + delta)
	v := k.value
	k.mu.Unlock()
	k.Refresh()
	if k.OnChanged != nil {
		k.OnChanged(v)
	}
}

func (k *Knob) DragEnd() {}

func (k *Knob) Scrolled(ev *fyne.ScrollEvent) {
	k.mu.Lock()
	delta := float64(ev.Scrolled.DY) / 600.0
	k.value = clamp(k.value + delta)
	v := k.value
	k.mu.Unlock()
	k.Refresh()
	if k.OnChanged != nil {
		k.OnChanged(v)
	}
}

func (k *Knob) MinSize() fyne.Size {
	return fyne.NewSize(50, 68)
}

func (k *Knob) CreateRenderer() fyne.WidgetRenderer {
	r := &knobRenderer{knob: k}
	r.build()
	return r
}

const arcSegs = 36

type knobRenderer struct {
	knob *Knob

	// Fixed objects — created once, updated in-place
	bg       *canvas.Circle
	tracks   [arcSegs]*canvas.Line
	arcs     [arcSegs]*canvas.Line
	dot      *canvas.Circle
	label    *canvas.Text
	allObjs  []fyne.CanvasObject
}

func (r *knobRenderer) build() {
	r.bg = canvas.NewCircle(boomtheme.ColorBackgroundTertiary)
	r.bg.StrokeColor = color.RGBA{R: 60, G: 60, B: 65, A: 255}
	r.bg.StrokeWidth = 1

	for i := range r.tracks {
		r.tracks[i] = canvas.NewLine(color.RGBA{R: 50, G: 50, B: 55, A: 255})
		r.tracks[i].StrokeWidth = 3
	}
	for i := range r.arcs {
		r.arcs[i] = canvas.NewLine(boomtheme.ColorBlue)
		r.arcs[i].StrokeWidth = 3
		r.arcs[i].Hidden = true
	}

	r.dot = canvas.NewCircle(boomtheme.ColorBlue)
	r.label = canvas.NewText("", boomtheme.ColorLabelSecondary)
	r.label.TextSize = 10
	r.label.TextStyle = fyne.TextStyle{Bold: true}
	r.label.Alignment = fyne.TextAlignCenter

	// Build fixed object list — never changes after creation
	r.allObjs = make([]fyne.CanvasObject, 0, 2+arcSegs*2+2)
	r.allObjs = append(r.allObjs, r.bg)
	for i := range r.tracks {
		r.allObjs = append(r.allObjs, r.tracks[i])
	}
	for i := range r.arcs {
		r.allObjs = append(r.allObjs, r.arcs[i])
	}
	r.allObjs = append(r.allObjs, r.dot, r.label)

	// Initial layout
	r.Refresh()
}

func (r *knobRenderer) Layout(size fyne.Size) {
	r.layout(size)
}

func (r *knobRenderer) MinSize() fyne.Size { return r.knob.MinSize() }
func (r *knobRenderer) Destroy()           {}

func (r *knobRenderer) Objects() []fyne.CanvasObject {
	return r.allObjs
}

func (r *knobRenderer) Refresh() {
	r.layout(r.knob.Size())
}

func (r *knobRenderer) layout(size fyne.Size) {
	r.knob.mu.RLock()
	val := r.knob.value
	lbl := r.knob.label
	accent := r.knob.accentClr
	r.knob.mu.RUnlock()

	if size.Width <= 0 || size.Height <= 0 {
		return
	}

	knobD := float32(math.Min(float64(size.Width), float64(size.Height-18)))
	if knobD < 24 {
		knobD = 24
	}
	rad := knobD / 2
	cx := size.Width / 2
	cy := rad + 1

	// Background circle
	r.bg.Resize(fyne.NewSize(knobD, knobD))
	r.bg.Move(fyne.NewPos(cx-rad, cy-rad))

	startDeg := 135.0
	sweepDeg := 270.0
	arcR := rad * 0.82

	valueSeg := int(math.Round(val * float64(arcSegs)))

	for i := 0; i < arcSegs; i++ {
		frac := float64(i) / float64(arcSegs)
		fracN := float64(i+1) / float64(arcSegs)
		a1 := (startDeg + frac*sweepDeg) * math.Pi / 180
		a2 := (startDeg + fracN*sweepDeg) * math.Pi / 180

		x1 := cx + float32(math.Cos(a1))*arcR
		y1 := cy + float32(math.Sin(a1))*arcR
		x2 := cx + float32(math.Cos(a2))*arcR
		y2 := cy + float32(math.Sin(a2))*arcR

		// Background track
		r.tracks[i].Position1 = fyne.NewPos(x1, y1)
		r.tracks[i].Position2 = fyne.NewPos(x2, y2)

		// Value arc
		if i < valueSeg {
			r.arcs[i].Position1 = fyne.NewPos(x1, y1)
			r.arcs[i].Position2 = fyne.NewPos(x2, y2)
			r.arcs[i].StrokeColor = accent
			r.arcs[i].Hidden = false
		} else {
			r.arcs[i].Hidden = true
		}
	}

	// Position dot
	angle := (startDeg + val*sweepDeg) * math.Pi / 180
	dotR := float32(3)
	dotX := cx + float32(math.Cos(angle))*arcR
	dotY := cy + float32(math.Sin(angle))*arcR
	r.dot.FillColor = accent
	r.dot.Resize(fyne.NewSize(dotR*2, dotR*2))
	r.dot.Move(fyne.NewPos(dotX-dotR, dotY-dotR))

	// Label
	r.label.Text = lbl
	r.label.Move(fyne.NewPos(0, cy+rad+3))
	r.label.Resize(fyne.NewSize(size.Width, 14))

	// Refresh all persistent objects
	r.bg.Refresh()
	for i := range r.tracks {
		r.tracks[i].Refresh()
	}
	for i := range r.arcs {
		r.arcs[i].Refresh()
	}
	r.dot.Refresh()
	r.label.Refresh()
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
