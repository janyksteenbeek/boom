package beatgrid

import (
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"

	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
)

const (
	maxBars      = 2000
	maxBeatLines = 200
)

// DeckStrip renders a single deck's scrolling waveform with beat markers.
// The view is centered on the current playback position and shows a
// configurable fraction of the track (zoom level).
type DeckStrip struct {
	widget.BaseWidget

	mu       sync.RWMutex
	deckID   int
	peaksLow []float64
	peaksMid []float64
	peaksHigh []float64
	beatGrid []float64     // beat positions in seconds
	duration time.Duration // track duration
	position float64       // 0.0-1.0 normalized
	zoom     float64       // visible fraction of track (0.02-1.0)
}

func NewDeckStrip(deckID int) *DeckStrip {
	d := &DeckStrip{
		deckID: deckID,
		zoom:   0.1,
	}
	d.ExtendBaseWidget(d)
	return d
}

func (d *DeckStrip) SetFrequencyPeaks(low, mid, high []float64) {
	d.mu.Lock()
	d.peaksLow = low
	d.peaksMid = mid
	d.peaksHigh = high
	d.mu.Unlock()
	fyne.Do(func() { d.Refresh() })
}

func (d *DeckStrip) SetPosition(pos float64) {
	d.mu.Lock()
	d.position = pos
	d.mu.Unlock()
	fyne.Do(func() { d.Refresh() })
}

func (d *DeckStrip) SetZoom(zoom float64) {
	d.mu.Lock()
	d.zoom = zoom
	d.mu.Unlock()
	fyne.Do(func() { d.Refresh() })
}

func (d *DeckStrip) SetBeatGrid(beats []float64) {
	d.mu.Lock()
	d.beatGrid = beats
	d.mu.Unlock()
	fyne.Do(func() { d.Refresh() })
}

func (d *DeckStrip) SetDuration(dur time.Duration) {
	d.mu.Lock()
	d.duration = dur
	d.mu.Unlock()
}

func (d *DeckStrip) MinSize() fyne.Size {
	return fyne.NewSize(100, 56)
}

func (d *DeckStrip) CreateRenderer() fyne.WidgetRenderer {
	r := &deckStripRenderer{widget: d}
	r.buildObjects()
	return r
}

// --- Renderer ---

type deckStripRenderer struct {
	widget    *DeckStrip
	bg        *canvas.Rectangle
	topLine   *canvas.Line
	empty     *canvas.Text
	barsLow   []*canvas.Line
	barsMid   []*canvas.Line
	barsHigh  []*canvas.Line
	beatLines []*canvas.Line
	head      *canvas.Line
	size      fyne.Size
}

func (r *deckStripRenderer) buildObjects() {
	deckID := r.widget.deckID

	r.bg = canvas.NewRectangle(boomtheme.ColorWaveformBg)

	r.topLine = canvas.NewLine(boomtheme.DeckColor(deckID))
	r.topLine.StrokeWidth = 1.5

	r.empty = canvas.NewText("No Track Loaded", boomtheme.ColorLabelTertiary)
	r.empty.TextSize = 10
	r.empty.Alignment = fyne.TextAlignCenter

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

	r.beatLines = make([]*canvas.Line, maxBeatLines)
	for i := 0; i < maxBeatLines; i++ {
		r.beatLines[i] = canvas.NewLine(boomtheme.ColorBeatLine)
		r.beatLines[i].StrokeWidth = 0.5
		r.beatLines[i].Hidden = true
	}

	r.head = canvas.NewLine(boomtheme.ColorPlayhead)
	r.head.StrokeWidth = 1.5
	r.head.Hidden = true
}

func (r *deckStripRenderer) Layout(size fyne.Size) {
	if size == r.size {
		return
	}
	r.size = size
}

func (r *deckStripRenderer) MinSize() fyne.Size {
	return r.widget.MinSize()
}

func (r *deckStripRenderer) Refresh() {
	r.widget.mu.RLock()
	peaksLow := r.widget.peaksLow
	peaksMid := r.widget.peaksMid
	peaksHigh := r.widget.peaksHigh
	beatGrid := r.widget.beatGrid
	duration := r.widget.duration
	position := r.widget.position
	zoom := r.widget.zoom
	r.widget.mu.RUnlock()

	size := r.widget.Size()
	if size.Width <= 0 || size.Height <= 0 {
		return
	}

	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))

	r.topLine.Position1 = fyne.NewPos(0, 0.5)
	r.topLine.Position2 = fyne.NewPos(size.Width, 0.5)

	peakCount := len(peaksLow)
	hasPeaks := peakCount > 0 && len(peaksMid) == peakCount && len(peaksHigh) == peakCount
	r.empty.Hidden = hasPeaks
	r.head.Hidden = !hasPeaks

	centerY := size.Height / 2

	if !hasPeaks {
		r.empty.Move(fyne.NewPos(0, centerY-6))
		r.empty.Resize(fyne.NewSize(size.Width, 12))
		r.hideAllBars()
		r.hideAllBeats()
		r.refreshAll()
		return
	}

	// Compute visible window
	if zoom <= 0 {
		zoom = 0.1
	}
	viewStart := position - zoom/2
	viewEnd := position + zoom/2

	// Clamp to track boundaries
	if viewStart < 0 {
		viewEnd -= viewStart
		viewStart = 0
		if viewEnd > 1.0 {
			viewEnd = 1.0
		}
	}
	if viewEnd > 1.0 {
		viewStart -= (viewEnd - 1.0)
		viewEnd = 1.0
		if viewStart < 0 {
			viewStart = 0
		}
	}

	viewRange := viewEnd - viewStart
	if viewRange <= 0 {
		viewRange = 1.0
	}

	// Render waveform bars using pixel-stepping with linear interpolation.
	// Each bar is ~1.5px wide, and we interpolate between the 400 peak data
	// points to fill the full widget width smoothly at any zoom level.
	maxH := centerY - 2 // padding
	idx := 0

	// Playhead x position
	headX := float32((position - viewStart) / viewRange) * size.Width

	// Step through pixels at ~1.5px intervals
	barW := float32(1.5)
	numBars := int(size.Width / barW)
	if numBars > maxBars {
		numBars = maxBars
	}

	for b := 0; b < numBars; b++ {
		x := float32(b) * barW
		cx := x + barW/2

		// Map pixel position to track position, then to peak index (float)
		trackPos := viewStart + float64(x/size.Width)*viewRange
		peakIdxF := trackPos * float64(peakCount)

		// Linear interpolation between adjacent peaks
		idx0 := int(peakIdxF)
		if idx0 < 0 {
			idx0 = 0
		}
		idx1 := idx0 + 1
		if idx1 >= peakCount {
			idx1 = peakCount - 1
			idx0 = idx1
		}
		frac := peakIdxF - float64(idx0)
		if frac < 0 {
			frac = 0
		}

		pLow := peaksLow[idx0]*(1-frac) + peaksLow[idx1]*frac
		pMid := peaksMid[idx0]*(1-frac) + peaksMid[idx1]*frac
		pHigh := peaksHigh[idx0]*(1-frac) + peaksHigh[idx1]*frac

		// Stacked bar heights (weighted proportions)
		const wLow, wMid, wHigh float32 = 0.50, 0.30, 0.20
		hLow := float32(pLow) * wLow * maxH
		hMid := float32(pMid) * wMid * maxH
		hHigh := float32(pHigh) * wHigh * maxH
		totalH := hLow + hMid + hHigh
		lowMidH := hLow + hMid

		beforePlayhead := cx < headX

		// High (outermost, drawn behind): fills full stacked height
		bar := r.barsHigh[b]
		bar.StrokeWidth = barW
		bar.Position1 = fyne.NewPos(cx, centerY-totalH)
		bar.Position2 = fyne.NewPos(cx, centerY+totalH)
		if beforePlayhead {
			bar.StrokeColor = boomtheme.ColorWaveformHighDim
		} else {
			bar.StrokeColor = boomtheme.ColorWaveformHigh
		}
		bar.Hidden = totalH < 0.3
		bar.Refresh()

		// Mid (middle layer): covers low+mid area
		bar = r.barsMid[b]
		bar.StrokeWidth = barW
		bar.Position1 = fyne.NewPos(cx, centerY-lowMidH)
		bar.Position2 = fyne.NewPos(cx, centerY+lowMidH)
		if beforePlayhead {
			bar.StrokeColor = boomtheme.ColorWaveformMidDim
		} else {
			bar.StrokeColor = boomtheme.ColorWaveformMid
		}
		bar.Hidden = lowMidH < 0.3
		bar.Refresh()

		// Low (innermost, drawn on top): bass core
		bar = r.barsLow[b]
		bar.StrokeWidth = barW
		bar.Position1 = fyne.NewPos(cx, centerY-hLow)
		bar.Position2 = fyne.NewPos(cx, centerY+hLow)
		if beforePlayhead {
			bar.StrokeColor = boomtheme.ColorWaveformLowDim
		} else {
			bar.StrokeColor = boomtheme.ColorWaveformLow
		}
		bar.Hidden = hLow < 0.3
		bar.Refresh()

		idx = b + 1
	}

	// Hide unused bars
	for i := idx; i < maxBars; i++ {
		r.barsLow[i].Hidden = true
		r.barsMid[i].Hidden = true
		r.barsHigh[i].Hidden = true
	}

	// Render beat markers
	beatIdx := 0
	durSec := duration.Seconds()
	if durSec > 0 && len(beatGrid) > 0 {
		for i, beatTime := range beatGrid {
			if beatIdx >= maxBeatLines {
				break
			}
			beatNorm := beatTime / durSec
			xFrac := (beatNorm - viewStart) / viewRange
			if xFrac < -0.01 || xFrac > 1.01 {
				continue
			}
			bx := float32(xFrac) * size.Width

			line := r.beatLines[beatIdx]
			line.Position1 = fyne.NewPos(bx, 2)
			line.Position2 = fyne.NewPos(bx, size.Height-2)
			// Every 4th beat (downbeat) is stronger
			if i%4 == 0 {
				line.StrokeColor = boomtheme.ColorBeatLineStrong
				line.StrokeWidth = 1.0
			} else {
				line.StrokeColor = boomtheme.ColorBeatLine
				line.StrokeWidth = 0.5
			}
			line.Hidden = false
			line.Refresh()
			beatIdx++
		}
	}
	for i := beatIdx; i < maxBeatLines; i++ {
		r.beatLines[i].Hidden = true
	}

	// Playhead — fixed at current position within view
	r.head.Position1 = fyne.NewPos(headX, 0)
	r.head.Position2 = fyne.NewPos(headX, size.Height)
	r.head.Refresh()

	r.bg.Refresh()
	r.topLine.Refresh()
	r.empty.Refresh()
}

func (r *deckStripRenderer) hideAllBars() {
	for i := 0; i < maxBars; i++ {
		r.barsLow[i].Hidden = true
		r.barsMid[i].Hidden = true
		r.barsHigh[i].Hidden = true
	}
}

func (r *deckStripRenderer) hideAllBeats() {
	for i := 0; i < maxBeatLines; i++ {
		r.beatLines[i].Hidden = true
	}
}

func (r *deckStripRenderer) refreshAll() {
	r.bg.Refresh()
	r.topLine.Refresh()
	r.empty.Refresh()
	r.head.Refresh()
}

func (r *deckStripRenderer) Objects() []fyne.CanvasObject {
	objs := []fyne.CanvasObject{r.bg, r.topLine, r.empty}
	// Stacked layers: high (back/outermost) → mid → low (front/innermost)
	for _, b := range r.barsHigh {
		objs = append(objs, b)
	}
	for _, b := range r.barsMid {
		objs = append(objs, b)
	}
	for _, b := range r.barsLow {
		objs = append(objs, b)
	}
	// Beat lines ON TOP of waveform for visibility
	for _, b := range r.beatLines {
		objs = append(objs, b)
	}
	objs = append(objs, r.head)
	return objs
}

func (r *deckStripRenderer) Destroy() {}
