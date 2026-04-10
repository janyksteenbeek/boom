package components

import (
	"image/color"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"

	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
)

// Fader is a custom-drawn vertical or horizontal slider.
type Fader struct {
	widget.BaseWidget

	mu        sync.RWMutex
	value     float64
	vertical  bool
	color     color.Color
	OnChanged func(float64)
}

var _ fyne.Draggable = (*Fader)(nil)
var _ fyne.Tappable = (*Fader)(nil)

func NewFader(vertical bool, value float64, c color.Color, onChange func(float64)) *Fader {
	f := &Fader{
		vertical:  vertical,
		value:     value,
		color:     c,
		OnChanged: onChange,
	}
	f.ExtendBaseWidget(f)
	return f
}

func (f *Fader) SetValue(v float64) {
	f.mu.Lock()
	f.value = clamp(v)
	f.mu.Unlock()
	fyne.Do(func() {
		f.Refresh()
	})
}

func (f *Fader) Dragged(ev *fyne.DragEvent) {
	f.mu.Lock()
	size := f.Size()
	var newVal float64
	if f.vertical {
		newVal = 1.0 - float64(ev.Position.Y)/float64(size.Height)
	} else {
		newVal = float64(ev.Position.X) / float64(size.Width)
	}
	f.value = clamp(newVal)
	v := f.value
	f.mu.Unlock()
	f.Refresh()
	if f.OnChanged != nil {
		f.OnChanged(v)
	}
}

func (f *Fader) DragEnd() {}

func (f *Fader) Tapped(ev *fyne.PointEvent) {
	f.mu.Lock()
	size := f.Size()
	var newVal float64
	if f.vertical {
		newVal = 1.0 - float64(ev.Position.Y)/float64(size.Height)
	} else {
		newVal = float64(ev.Position.X) / float64(size.Width)
	}
	f.value = clamp(newVal)
	v := f.value
	f.mu.Unlock()
	f.Refresh()
	if f.OnChanged != nil {
		f.OnChanged(v)
	}
}

func (f *Fader) MinSize() fyne.Size {
	if f.vertical {
		return fyne.NewSize(24, 80)
	}
	return fyne.NewSize(80, 20)
}

func (f *Fader) CreateRenderer() fyne.WidgetRenderer {
	r := &faderRenderer{fader: f}
	r.build()
	return r
}

type faderRenderer struct {
	fader   *Fader
	track   *canvas.Rectangle
	fill    *canvas.Rectangle
	thumb   *canvas.Rectangle
	allObjs []fyne.CanvasObject
}

func (r *faderRenderer) build() {
	r.track = canvas.NewRectangle(boomtheme.ColorBackgroundTertiary)
	r.track.CornerRadius = 2
	r.fill = canvas.NewRectangle(boomtheme.ColorBlue)
	r.fill.CornerRadius = 2
	r.thumb = canvas.NewRectangle(boomtheme.ColorLabel)
	r.thumb.CornerRadius = 3

	r.allObjs = []fyne.CanvasObject{r.track, r.fill, r.thumb}
	r.Refresh()
}

func (r *faderRenderer) Layout(size fyne.Size) { r.layout(size) }
func (r *faderRenderer) MinSize() fyne.Size    { return r.fader.MinSize() }
func (r *faderRenderer) Destroy()              {}
func (r *faderRenderer) Objects() []fyne.CanvasObject { return r.allObjs }

func (r *faderRenderer) Refresh() {
	r.layout(r.fader.Size())
}

func (r *faderRenderer) layout(size fyne.Size) {
	r.fader.mu.RLock()
	val := r.fader.value
	vertical := r.fader.vertical
	c := r.fader.color
	r.fader.mu.RUnlock()

	if size.Width <= 0 || size.Height <= 0 {
		return
	}

	r.fill.FillColor = c

	if vertical {
		trackW := float32(4)
		trackX := (size.Width - trackW) / 2
		r.track.Resize(fyne.NewSize(trackW, size.Height))
		r.track.Move(fyne.NewPos(trackX, 0))

		fillH := float32(val) * size.Height
		r.fill.Resize(fyne.NewSize(trackW, fillH))
		r.fill.Move(fyne.NewPos(trackX, size.Height-fillH))

		thumbH := float32(6)
		thumbW := size.Width * 0.8
		thumbY := size.Height - float32(val)*size.Height - thumbH/2
		r.thumb.Resize(fyne.NewSize(thumbW, thumbH))
		r.thumb.Move(fyne.NewPos((size.Width-thumbW)/2, thumbY))
	} else {
		trackH := float32(4)
		trackY := (size.Height - trackH) / 2
		r.track.Resize(fyne.NewSize(size.Width, trackH))
		r.track.Move(fyne.NewPos(0, trackY))

		fillW := float32(val) * size.Width
		r.fill.Resize(fyne.NewSize(fillW, trackH))
		r.fill.Move(fyne.NewPos(0, trackY))

		thumbW := float32(6)
		thumbH := size.Height * 0.7
		thumbX := float32(val)*size.Width - thumbW/2
		r.thumb.Resize(fyne.NewSize(thumbW, thumbH))
		r.thumb.Move(fyne.NewPos(thumbX, (size.Height-thumbH)/2))
	}

	r.track.Refresh()
	r.fill.Refresh()
	r.thumb.Refresh()
}
