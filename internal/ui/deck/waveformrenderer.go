package deck

import (
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"

	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
)

type waveformRenderer struct {
	widget     *WaveformWidget
	bg         *canvas.Rectangle
	topLine    *canvas.Line
	empty      *canvas.Text
	grid       []*canvas.Line
	barsLow    []*canvas.Line
	barsMid    []*canvas.Line
	barsHigh   []*canvas.Line
	head       *canvas.Line
	cueMark    *canvas.Line
	loopRegion *canvas.Rectangle
	loopInMark *canvas.Line
	loopOutMrk *canvas.Line
	loopLabel  *canvas.Text
	size       fyne.Size

	// Cache of the widget state that was last applied to canvas objects.
	// If none of these change between Refresh() calls we can skip the
	// expensive bar/grid/loop relayout and just move the playhead.
	lastLayoutVersion uint64
	lastSize          fyne.Size
	lastHadPeaks      bool
	layoutReady       bool
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

	// Pre-allocate bars for all 3 frequency layers. The count is read
	// from the widget so mini-mode can halve it to reduce Pi GPU work.
	maxBars := r.widget.MaxBars()
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

	r.loopRegion = canvas.NewRectangle(boomtheme.ColorLoopFill)
	r.loopRegion.CornerRadius = 2
	r.loopRegion.Hidden = true

	r.loopInMark = canvas.NewLine(boomtheme.ColorLoopMarker)
	r.loopInMark.StrokeWidth = 2
	r.loopInMark.Hidden = true

	r.loopOutMrk = canvas.NewLine(boomtheme.ColorLoopMarker)
	r.loopOutMrk.StrokeWidth = 2
	r.loopOutMrk.Hidden = true

	r.loopLabel = canvas.NewText("", boomtheme.ColorLoopLabel)
	r.loopLabel.TextSize = 9
	r.loopLabel.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
	r.loopLabel.Alignment = fyne.TextAlignCenter
	r.loopLabel.Hidden = true
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

	for i, line := range r.grid {
		x := float32(i+1) * size.Width / float32(len(r.grid)+1)
		line.Position1 = fyne.NewPos(x, 2)
		line.Position2 = fyne.NewPos(x, size.Height-2)
	}
}

func (r *waveformRenderer) MinSize() fyne.Size {
	return r.widget.MinSize()
}

// waveformSnapshot captures the widget state Refresh() needs under a single
// read-lock so the renderer doesn't block UI updates while it's drawing.
type waveformSnapshot struct {
	peaksLow, peaksMid, peaksHigh []float64
	position                      float64
	cuePoint                      float64
	loopStart, loopEnd, loopBeats float64
	loopActive                    bool
	layoutVersion                 uint64
}

func (r *waveformRenderer) snapshot() waveformSnapshot {
	r.widget.mu.RLock()
	defer r.widget.mu.RUnlock()
	return waveformSnapshot{
		peaksLow:      r.widget.peaksLow,
		peaksMid:      r.widget.peaksMid,
		peaksHigh:     r.widget.peaksHigh,
		position:      r.widget.position,
		cuePoint:      r.widget.cuePoint,
		loopStart:     r.widget.loopStart,
		loopEnd:       r.widget.loopEnd,
		loopBeats:     r.widget.loopBeats,
		loopActive:    r.widget.loopActive,
		layoutVersion: r.widget.layoutVersion,
	}
}

func (r *waveformRenderer) Refresh() {
	snap := r.snapshot()

	size := r.widget.Size()
	if size.Width <= 0 || size.Height <= 0 {
		return
	}

	peakCount := len(snap.peaksLow)
	hasPeaks := peakCount > 0 && len(snap.peaksMid) == peakCount && len(snap.peaksHigh) == peakCount

	// Fast path: only the playhead moved. Skip bar layout, grid refresh,
	// background refresh and the per-object refresh storm — just reposition
	// r.head and refresh that one line. This is the 30 Hz hot path driven
	// by ActionPositionUpdate while a track plays.
	layoutDirty := !r.layoutReady ||
		snap.layoutVersion != r.lastLayoutVersion ||
		size != r.lastSize ||
		hasPeaks != r.lastHadPeaks

	if !layoutDirty {
		if hasPeaks {
			r.drawPlayhead(snap.position, size)
		}
		return
	}

	// Slow path: peaks, loop, cue, or size changed — full redraw.
	r.layoutObjects(size)

	r.empty.Hidden = hasPeaks
	r.head.Hidden = !hasPeaks

	if hasPeaks {
		r.drawBars(snap, size)
		r.drawPlayhead(snap.position, size)
		r.drawCue(snap.cuePoint, size)
		r.drawLoop(snap.loopStart, snap.loopEnd, snap.loopBeats, snap.loopActive, size)
	} else {
		r.hideAllBars()
		r.cueMark.Hidden = true
		r.loopRegion.Hidden = true
		r.loopInMark.Hidden = true
		r.loopOutMrk.Hidden = true
		r.loopLabel.Hidden = true
	}

	r.bg.Refresh()
	r.topLine.Refresh()
	r.empty.Refresh()
	for _, g := range r.grid {
		g.Refresh()
	}

	r.lastLayoutVersion = snap.layoutVersion
	r.lastSize = size
	r.lastHadPeaks = hasPeaks
	r.layoutReady = true
}

// drawBars renders the three stacked frequency layers.
func (r *waveformRenderer) drawBars(snap waveformSnapshot, size fyne.Size) {
	peakCount := len(snap.peaksLow)
	posX := float32(snap.position) * size.Width
	barWidth := size.Width / float32(peakCount)
	centerY := size.Height / 2

	step := 1
	if barWidth < 1.5 {
		step = int(math.Ceil(float64(1.5 / barWidth)))
	}
	// Also respect the renderer's pre-allocated bar budget. Without
	// this, a peakCount larger than maxBars would silently truncate
	// the right side of the waveform (we'd iterate past the end of
	// r.barsLow/Mid/High and the nil-index guards below would drop
	// the bars). Forcing step up keeps the waveform spanning the
	// full width at a coarser granularity.
	if n := len(r.barsLow); n > 0 {
		minStep := (peakCount + n - 1) / n
		if minStep > step {
			step = minStep
		}
	}

	// Leave padding at top/bottom for breathing room
	maxH := centerY - 8

	idx := 0
	for i := 0; i < peakCount; i += step {
		pLow := snap.peaksLow[i]
		pMid := snap.peaksMid[i]
		pHigh := snap.peaksHigh[i]
		for j := 1; j < step && i+j < peakCount; j++ {
			if snap.peaksLow[i+j] > pLow {
				pLow = snap.peaksLow[i+j]
			}
			if snap.peaksMid[i+j] > pMid {
				pMid = snap.peaksMid[i+j]
			}
			if snap.peaksHigh[i+j] > pHigh {
				pHigh = snap.peaksHigh[i+j]
			}
		}

		x := float32(i) * (size.Width / float32(peakCount))
		bw := barWidth * float32(step) * 0.75
		if bw < 1 {
			bw = 1
		}

		// Stacked bar heights (weighted proportions)
		// Bass gets most visual space (DJ focus), highs get the edges.
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

	for i := idx; i < len(r.barsLow); i++ {
		r.barsLow[i].Hidden = true
	}
	for i := idx; i < len(r.barsMid); i++ {
		r.barsMid[i].Hidden = true
	}
	for i := idx; i < len(r.barsHigh); i++ {
		r.barsHigh[i].Hidden = true
	}
}

func (r *waveformRenderer) drawPlayhead(position float64, size fyne.Size) {
	posX := float32(position) * size.Width
	r.head.Position1 = fyne.NewPos(posX, 0)
	r.head.Position2 = fyne.NewPos(posX, size.Height)
	r.head.Refresh()
}

func (r *waveformRenderer) drawCue(cuePoint float64, size fyne.Size) {
	if cuePoint < 0 {
		r.cueMark.Hidden = true
		return
	}
	cueX := float32(cuePoint) * size.Width
	r.cueMark.Position1 = fyne.NewPos(cueX, 0)
	r.cueMark.Position2 = fyne.NewPos(cueX, size.Height)
	r.cueMark.Hidden = false
	r.cueMark.Refresh()
}

func (r *waveformRenderer) drawLoop(loopStart, loopEnd, loopBeats float64, loopActive bool, size fyne.Size) {
	if loopStart < 0 || loopEnd <= loopStart {
		r.loopRegion.Hidden = true
		r.loopInMark.Hidden = true
		r.loopOutMrk.Hidden = true
		r.loopLabel.Hidden = true
		return
	}

	startX := float32(loopStart) * size.Width
	endX := float32(loopEnd) * size.Width
	width := endX - startX
	if width < 1 {
		width = 1
	}

	fill := boomtheme.ColorLoopFill
	if !loopActive {
		// Dimmer fill when loop is stored but not wrapping.
		fill.A = 30
	}
	r.loopRegion.FillColor = fill
	r.loopRegion.Move(fyne.NewPos(startX, 2))
	r.loopRegion.Resize(fyne.NewSize(width, size.Height-4))
	r.loopRegion.Hidden = false
	r.loopRegion.Refresh()

	r.loopInMark.Position1 = fyne.NewPos(startX, 0)
	r.loopInMark.Position2 = fyne.NewPos(startX, size.Height)
	r.loopInMark.Hidden = false
	r.loopInMark.Refresh()

	r.loopOutMrk.Position1 = fyne.NewPos(endX, 0)
	r.loopOutMrk.Position2 = fyne.NewPos(endX, size.Height)
	r.loopOutMrk.Hidden = false
	r.loopOutMrk.Refresh()

	r.loopLabel.Text = labelForBeats(loopBeats)
	r.loopLabel.Move(fyne.NewPos(startX, 2))
	r.loopLabel.Resize(fyne.NewSize(width, 12))
	r.loopLabel.Hidden = r.loopLabel.Text == "" || width < 24
	r.loopLabel.Refresh()
}

func (r *waveformRenderer) hideAllBars() {
	for _, b := range r.barsLow {
		b.Hidden = true
	}
	for _, b := range r.barsMid {
		b.Hidden = true
	}
	for _, b := range r.barsHigh {
		b.Hidden = true
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
	// Loop region drawn under the playhead so the head line stays on top.
	objs = append(objs, r.loopRegion)
	objs = append(objs, r.loopInMark, r.loopOutMrk)
	objs = append(objs, r.head)
	objs = append(objs, r.cueMark)
	objs = append(objs, r.loopLabel)
	return objs
}

func (r *waveformRenderer) Destroy() {}
