package deck

import (
	"math"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"

	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
)

// WaveformWidget draws a frequency-colored waveform with 3 stacked layers:
// low (blue), mid (orange), high (white).
type WaveformWidget struct {
	widget.BaseWidget

	mu        sync.RWMutex
	peaksLow  []float64
	peaksMid  []float64
	peaksHigh []float64
	position  float64
	cuePoint  float64 // -1 = unset
	deckID    int
}

func NewWaveformWidget(deckID int) *WaveformWidget {
	w := &WaveformWidget{deckID: deckID, cuePoint: -1}
	w.ExtendBaseWidget(w)
	return w
}

// SetCuePoint updates the cue marker position. Pass a negative value to hide it.
func (w *WaveformWidget) SetCuePoint(p float64) {
	w.mu.Lock()
	if w.cuePoint == p {
		w.mu.Unlock()
		return
	}
	w.cuePoint = p
	w.mu.Unlock()
	fyne.Do(func() {
		w.Refresh()
	})
}

func (w *WaveformWidget) SetFrequencyPeaks(low, mid, high []float64) {
	w.mu.Lock()
	w.peaksLow = low
	w.peaksMid = mid
	w.peaksHigh = high
	w.mu.Unlock()
	fyne.Do(func() {
		w.Refresh()
	})
}

func (w *WaveformWidget) SetPosition(pos float64) {
	w.mu.Lock()
	if math.Abs(w.position-pos) < 0.001 {
		w.mu.Unlock()
		return
	}
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
	widget   *WaveformWidget
	bg       *canvas.Rectangle
	topLine  *canvas.Line
	empty    *canvas.Text
	grid     []*canvas.Line
	barsLow  []*canvas.Line
	barsMid  []*canvas.Line
	barsHigh []*canvas.Line
	head     *canvas.Line
	cueMark  *canvas.Line
	size     fyne.Size
}

func (r *waveformRenderer) buildObjects() {
	r.bg = canvas.NewRectangle(boomtheme.ColorWaveformBg)
	r.bg.CornerRadius = 6

	deckID := r.widget.deckID
	r.topLine = canvas.NewLine(boomtheme.DeckColor(deckID))
	r.topLine.StrokeWidth = 1.5

	r.empty = canvas.NewText("No Track Loaded", boomtheme.ColorLabelTertiary)
	r.empty.TextSize = 11
	r.empty.Alignment = fyne.TextAlignCenter

	// Subtle grid lines
	r.grid = make([]*canvas.Line, 16)
	for i := range r.grid {
		c := boomtheme.ColorWaveformGrid
		if i%4 == 0 {
			c = boomtheme.ColorWaveformGridMajor
		}
		r.grid[i] = canvas.NewLine(c)
		r.grid[i].StrokeWidth = 0.5
	}

	// Pre-allocate bars for all 3 frequency layers
	const maxBars = 400
	r.barsLow = make([]*canvas.Line, maxBars)
	r.barsMid = make([]*canvas.Line, maxBars)
	r.barsHigh = make([]*canvas.Line, maxBars)
	for i := 0; i < maxBars; i++ {
		r.barsLow[i] = canvas.NewLine(boomtheme.ColorWaveformLow)
		r.barsLow[i].Hidden = true
		r.barsMid[i] = canvas.NewLine(boomtheme.ColorWaveformMid)
		r.barsMid[i].Hidden = true
		r.barsHigh[i] = canvas.NewLine(boomtheme.ColorWaveformHigh)
		r.barsHigh[i].Hidden = true
	}

	r.head = canvas.NewLine(boomtheme.ColorPlayhead)
	r.head.StrokeWidth = 1.5
	r.head.Hidden = true

	r.cueMark = canvas.NewLine(boomtheme.ColorCueActive)
	r.cueMark.StrokeWidth = 2
	r.cueMark.Hidden = true
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

	r.topLine.Position1 = fyne.NewPos(0, 0.5)
	r.topLine.Position2 = fyne.NewPos(size.Width, 0.5)

	centerY := size.Height / 2
	r.empty.Move(fyne.NewPos(0, centerY-7))
	r.empty.Resize(fyne.NewSize(size.Width, 14))

	// Position grid lines
	for i, line := range r.grid {
		x := float32(i+1) * size.Width / float32(len(r.grid)+1)
		line.Position1 = fyne.NewPos(x, 2)
		line.Position2 = fyne.NewPos(x, size.Height-2)
	}
}

func (r *waveformRenderer) MinSize() fyne.Size {
	return r.widget.MinSize()
}

func (r *waveformRenderer) Refresh() {
	r.widget.mu.RLock()
	peaksLow := r.widget.peaksLow
	peaksMid := r.widget.peaksMid
	peaksHigh := r.widget.peaksHigh
	position := r.widget.position
	cuePoint := r.widget.cuePoint
	r.widget.mu.RUnlock()

	size := r.widget.Size()
	if size.Width <= 0 || size.Height <= 0 {
		return
	}
	r.layoutObjects(size)

	peakCount := len(peaksLow)
	hasPeaks := peakCount > 0 && len(peaksMid) == peakCount && len(peaksHigh) == peakCount
	r.empty.Hidden = hasPeaks
	r.head.Hidden = !hasPeaks

	centerY := size.Height / 2

	if hasPeaks {
		posX := float32(position) * size.Width
		barWidth := size.Width / float32(peakCount)

		step := 1
		if barWidth < 1.5 {
			step = int(math.Ceil(float64(1.5 / barWidth)))
		}

		needed := peakCount / step
		if needed > len(r.barsLow) {
			needed = len(r.barsLow)
		}

		// Leave padding at top/bottom for breathing room
		maxH := centerY - 8

		idx := 0
		for i := 0; i < peakCount; i += step {
			pLow := peaksLow[i]
			pMid := peaksMid[i]
			pHigh := peaksHigh[i]
			for j := 1; j < step && i+j < peakCount; j++ {
				if peaksLow[i+j] > pLow {
					pLow = peaksLow[i+j]
				}
				if peaksMid[i+j] > pMid {
					pMid = peaksMid[i+j]
				}
				if peaksHigh[i+j] > pHigh {
					pHigh = peaksHigh[i+j]
				}
			}

			x := float32(i) * (size.Width / float32(peakCount))
			bw := barWidth * float32(step) * 0.75
			if bw < 1 {
				bw = 1
			}

			// Stacked bar heights (weighted proportions)
			// Bass gets most visual space (DJ focus), highs get the edges
			const wLow, wMid, wHigh float32 = 0.50, 0.30, 0.20
			hLow := float32(pLow) * wLow * maxH
			hMid := float32(pMid) * wMid * maxH
			hHigh := float32(pHigh) * wHigh * maxH
			totalH := hLow + hMid + hHigh
			lowMidH := hLow + hMid

			beforePlayhead := x < posX
			cx := x + bw/2

			// High (outermost, drawn behind): fills full stacked height
			if idx < len(r.barsHigh) {
				bar := r.barsHigh[idx]
				bar.StrokeWidth = bw
				bar.Position1 = fyne.NewPos(cx, centerY-totalH)
				bar.Position2 = fyne.NewPos(cx, centerY+totalH)
				if beforePlayhead {
					bar.StrokeColor = boomtheme.ColorWaveformHighDim
				} else {
					bar.StrokeColor = boomtheme.ColorWaveformHigh
				}
				bar.Hidden = totalH < 0.3
				bar.Refresh()
			}

			// Mid (middle layer): covers low+mid area
			if idx < len(r.barsMid) {
				bar := r.barsMid[idx]
				bar.StrokeWidth = bw
				bar.Position1 = fyne.NewPos(cx, centerY-lowMidH)
				bar.Position2 = fyne.NewPos(cx, centerY+lowMidH)
				if beforePlayhead {
					bar.StrokeColor = boomtheme.ColorWaveformMidDim
				} else {
					bar.StrokeColor = boomtheme.ColorWaveformMid
				}
				bar.Hidden = lowMidH < 0.3
				bar.Refresh()
			}

			// Low (innermost, drawn on top): bass core
			if idx < len(r.barsLow) {
				bar := r.barsLow[idx]
				bar.StrokeWidth = bw
				bar.Position1 = fyne.NewPos(cx, centerY-hLow)
				bar.Position2 = fyne.NewPos(cx, centerY+hLow)
				if beforePlayhead {
					bar.StrokeColor = boomtheme.ColorWaveformLowDim
				} else {
					bar.StrokeColor = boomtheme.ColorWaveformLow
				}
				bar.Hidden = hLow < 0.3
				bar.Refresh()
			}

			idx++
		}

		// Hide unused bars
		for i := idx; i < len(r.barsLow); i++ {
			r.barsLow[i].Hidden = true
		}
		for i := idx; i < len(r.barsMid); i++ {
			r.barsMid[i].Hidden = true
		}
		for i := idx; i < len(r.barsHigh); i++ {
			r.barsHigh[i].Hidden = true
		}

		// Playhead
		r.head.Position1 = fyne.NewPos(posX, 0)
		r.head.Position2 = fyne.NewPos(posX, size.Height)
		r.head.Refresh()

		// Cue marker — vertical line at the saved cue point
		if cuePoint >= 0 {
			cueX := float32(cuePoint) * size.Width
			r.cueMark.Position1 = fyne.NewPos(cueX, 0)
			r.cueMark.Position2 = fyne.NewPos(cueX, size.Height)
			r.cueMark.Hidden = false
			r.cueMark.Refresh()
		} else {
			r.cueMark.Hidden = true
		}
	} else {
		for _, b := range r.barsLow {
			b.Hidden = true
		}
		for _, b := range r.barsMid {
			b.Hidden = true
		}
		for _, b := range r.barsHigh {
			b.Hidden = true
		}
		r.cueMark.Hidden = true
	}

	r.bg.Refresh()
	r.topLine.Refresh()
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
	objs = append(objs, r.empty)
	// Stacked layer order: high (back/outermost) → mid → low (front/innermost)
	for _, b := range r.barsHigh {
		objs = append(objs, b)
	}
	for _, b := range r.barsMid {
		objs = append(objs, b)
	}
	for _, b := range r.barsLow {
		objs = append(objs, b)
	}
	objs = append(objs, r.head)
	objs = append(objs, r.cueMark)
	return objs
}

func (r *waveformRenderer) Destroy() {}
