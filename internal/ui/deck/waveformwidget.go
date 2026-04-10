package deck

import (
	"image/color"
	"math"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"

	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
)

// WaveformWidget draws audio waveform peaks with grid, playhead, and deck color.
type WaveformWidget struct {
	widget.BaseWidget

	mu       sync.RWMutex
	peaks    []float64
	position float64
	deckID   int
}

func NewWaveformWidget(deckID int) *WaveformWidget {
	w := &WaveformWidget{deckID: deckID}
	w.ExtendBaseWidget(w)
	return w
}

func (w *WaveformWidget) SetPeaks(peaks []float64) {
	w.mu.Lock()
	w.peaks = peaks
	w.mu.Unlock()
	fyne.Do(func() {
		w.Refresh()
	})
}

func (w *WaveformWidget) SetPosition(pos float64) {
	w.mu.Lock()
	w.position = pos
	w.mu.Unlock()
	fyne.Do(func() {
		w.Refresh()
	})
}

func (w *WaveformWidget) CreateRenderer() fyne.WidgetRenderer {
	r := &waveformRenderer{widget: w}
	r.buildObjects()
	return r
}

func (w *WaveformWidget) MinSize() fyne.Size {
	return fyne.NewSize(100, 130)
}

// --- Renderer ---

type waveformRenderer struct {
	widget  *WaveformWidget
	bg      *canvas.Rectangle
	topLine *canvas.Line
	center  *canvas.Line
	empty   *canvas.Text
	grid    []*canvas.Line
	bars    []*canvas.Line
	head    *canvas.Line
	size    fyne.Size
}

func (r *waveformRenderer) buildObjects() {
	r.bg = canvas.NewRectangle(boomtheme.ColorWaveformBg)
	r.bg.CornerRadius = 8

	deckID := r.widget.deckID
	r.topLine = canvas.NewLine(boomtheme.DeckColor(deckID))
	r.topLine.StrokeWidth = 2

	r.center = canvas.NewLine(boomtheme.ColorWaveformGridMajor)
	r.center.StrokeWidth = 0.5

	r.empty = canvas.NewText("No Track Loaded", boomtheme.ColorLabelTertiary)
	r.empty.TextSize = 11
	r.empty.Alignment = fyne.TextAlignCenter

	// Pre-create grid lines
	r.grid = make([]*canvas.Line, 16)
	for i := range r.grid {
		c := boomtheme.ColorWaveformGrid
		if i%4 == 0 {
			c = boomtheme.ColorWaveformGridMajor
		}
		r.grid[i] = canvas.NewLine(c)
		r.grid[i].StrokeWidth = 0.5
	}

	r.head = canvas.NewLine(boomtheme.ColorPlayhead)
	r.head.StrokeWidth = 1.5
	r.head.Hidden = true
}

func (r *waveformRenderer) Layout(size fyne.Size) {
	if size == r.size {
		return
	}
	r.size = size
	r.layoutObjects(size)
}

func (r *waveformRenderer) layoutObjects(size fyne.Size) {
	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))

	r.topLine.Position1 = fyne.NewPos(0, 1)
	r.topLine.Position2 = fyne.NewPos(size.Width, 1)

	centerY := size.Height / 2
	r.center.Position1 = fyne.NewPos(0, centerY)
	r.center.Position2 = fyne.NewPos(size.Width, centerY)

	r.empty.Move(fyne.NewPos(0, centerY-7))
	r.empty.Resize(fyne.NewSize(size.Width, 14))

	// Position grid lines
	for i, line := range r.grid {
		x := float32(i+1) * size.Width / float32(len(r.grid)+1)
		line.Position1 = fyne.NewPos(x, 4)
		line.Position2 = fyne.NewPos(x, size.Height-2)
	}
}

func (r *waveformRenderer) MinSize() fyne.Size {
	return r.widget.MinSize()
}

func (r *waveformRenderer) Refresh() {
	r.widget.mu.RLock()
	peaks := r.widget.peaks
	position := r.widget.position
	deckID := r.widget.deckID
	r.widget.mu.RUnlock()

	size := r.widget.Size()
	if size.Width <= 0 || size.Height <= 0 {
		return
	}
	r.layoutObjects(size)

	hasPeaks := len(peaks) > 0
	r.empty.Hidden = hasPeaks
	r.head.Hidden = !hasPeaks

	// Rebuild waveform bars
	deckColor := boomtheme.DeckColor(deckID)
	deckDim := boomtheme.DeckColorDim(deckID)
	centerY := size.Height / 2

	if hasPeaks {
		posX := float32(position) * size.Width
		barWidth := size.Width / float32(len(peaks))

		step := 1
		if barWidth < 1.5 {
			step = int(math.Ceil(float64(1.5 / barWidth)))
		}

		needed := len(peaks) / step
		// Grow bars slice if needed
		for len(r.bars) < needed {
			l := canvas.NewLine(deckColor)
			r.bars = append(r.bars, l)
		}

		idx := 0
		for i := 0; i < len(peaks); i += step {
			peak := peaks[i]
			for j := 1; j < step && i+j < len(peaks); j++ {
				if peaks[i+j] > peak {
					peak = peaks[i+j]
				}
			}

			x := float32(i) * (size.Width / float32(len(peaks)))
			h := float32(peak) * (centerY - 4)
			if h < 0.5 {
				h = 0.5
			}

			var c color.Color
			if x < posX {
				c = deckDim
			} else {
				c = deckColor
			}

			if idx < len(r.bars) {
				bar := r.bars[idx]
				bw := barWidth * float32(step) * 0.85
				if bw < 1 {
					bw = 1
				}
				bar.StrokeColor = c
				bar.StrokeWidth = bw
				bar.Position1 = fyne.NewPos(x+bw/2, centerY-h)
				bar.Position2 = fyne.NewPos(x+bw/2, centerY+h)
				bar.Hidden = false
				bar.Refresh()
				idx++
			}
		}
		// Hide unused bars
		for ; idx < len(r.bars); idx++ {
			r.bars[idx].Hidden = true
		}

		// Playhead
		r.head.Position1 = fyne.NewPos(posX, 0)
		r.head.Position2 = fyne.NewPos(posX, size.Height)
		r.head.Refresh()
	} else {
		for _, b := range r.bars {
			b.Hidden = true
		}
	}

	r.bg.Refresh()
	r.topLine.Refresh()
	r.center.Refresh()
	r.empty.Refresh()
	for _, g := range r.grid {
		g.Refresh()
	}
}

func (r *waveformRenderer) Objects() []fyne.CanvasObject {
	objs := []fyne.CanvasObject{r.bg, r.topLine}
	for _, g := range r.grid {
		objs = append(objs, g)
	}
	objs = append(objs, r.center, r.empty)
	for _, b := range r.bars {
		objs = append(objs, b)
	}
	objs = append(objs, r.head)
	return objs
}

func (r *waveformRenderer) Destroy() {}
