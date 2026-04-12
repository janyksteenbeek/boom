package beatgrid

import (
	"image"
	"image/color"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"

	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
)

// DeckStrip renders a single deck's scrolling waveform with beat markers.
// The view is centered on the current playback position and shows a
// configurable fraction of the track (zoom level).
// Uses canvas.Raster for efficient pixel-level rendering instead of
// thousands of individual canvas objects.
type DeckStrip struct {
	widget.BaseWidget

	mu        sync.RWMutex
	deckID    int
	peaksLow  []float64
	peaksMid  []float64
	peaksHigh []float64
	beatGrid  []float64     // beat positions in seconds
	duration  time.Duration // track duration
	position  float64       // 0.0-1.0 normalized
	zoom      float64       // visible fraction of track (0.02-1.0)

	// Loop overlay — normalized 0..1; <0 = unset.
	loopStart  float64
	loopEnd    float64
	loopActive bool

	// Cue marker — normalized 0..1; <0 = unset.
	cuePoint float64
}

func NewDeckStrip(deckID int) *DeckStrip {
	d := &DeckStrip{
		deckID:    deckID,
		zoom:      0.1,
		loopStart: -1,
		loopEnd:   -1,
		cuePoint:  -1,
	}
	d.ExtendBaseWidget(d)
	return d
}

// SetCuePoint updates the cue marker position. Pass a negative value to hide.
func (d *DeckStrip) SetCuePoint(pos float64) {
	d.mu.Lock()
	if d.cuePoint == pos {
		d.mu.Unlock()
		return
	}
	d.cuePoint = pos
	d.mu.Unlock()
	fyne.Do(func() { d.Refresh() })
}

// SetLoopState updates the scrolling loop overlay. Pass start<0 to hide.
func (d *DeckStrip) SetLoopState(start, end float64, active bool) {
	d.mu.Lock()
	if d.loopStart == start && d.loopEnd == end && d.loopActive == active {
		d.mu.Unlock()
		return
	}
	d.loopStart = start
	d.loopEnd = end
	d.loopActive = active
	d.mu.Unlock()
	fyne.Do(func() { d.Refresh() })
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
	widget  *DeckStrip
	bg      *canvas.Rectangle
	topLine *canvas.Line
	empty   *canvas.Text
	raster  *canvas.Raster
	img     *image.RGBA
	size    fyne.Size
}

func (r *deckStripRenderer) buildObjects() {
	deckID := r.widget.deckID

	r.bg = canvas.NewRectangle(boomtheme.ColorWaveformBg)

	r.topLine = canvas.NewLine(boomtheme.DeckColor(deckID))
	r.topLine.StrokeWidth = 1.5

	r.empty = canvas.NewText("No Track Loaded", boomtheme.ColorLabelTertiary)
	r.empty.TextSize = 10
	r.empty.Alignment = fyne.TextAlignCenter

	r.raster = canvas.NewRaster(r.draw)
	r.raster.ScaleMode = canvas.ImageScalePixels
	r.raster.Hidden = true
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
	size := r.widget.Size()
	if size.Width <= 0 || size.Height <= 0 {
		return
	}

	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))

	r.topLine.Position1 = fyne.NewPos(0, 0.5)
	r.topLine.Position2 = fyne.NewPos(size.Width, 0.5)

	r.widget.mu.RLock()
	hasPeaks := len(r.widget.peaksLow) > 0
	r.widget.mu.RUnlock()

	r.empty.Hidden = hasPeaks
	r.raster.Hidden = !hasPeaks

	if hasPeaks {
		r.raster.Resize(size)
		r.raster.Move(fyne.NewPos(0, 0))
		r.raster.Refresh()
	} else {
		centerY := size.Height / 2
		r.empty.Move(fyne.NewPos(0, centerY-6))
		r.empty.Resize(fyne.NewSize(size.Width, 12))
	}

	r.bg.Refresh()
	r.topLine.Refresh()
	r.empty.Refresh()
}

// draw is called by Fyne's render thread to produce the raster image.
// It reads widget state under RLock and draws everything into a single
// image buffer: waveform, beat markers, and playhead.
func (r *deckStripRenderer) draw(w, h int) image.Image {
	if w <= 0 || h <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}

	// Snapshot widget state
	r.widget.mu.RLock()
	peaksLow := r.widget.peaksLow
	peaksMid := r.widget.peaksMid
	peaksHigh := r.widget.peaksHigh
	beatGrid := r.widget.beatGrid
	duration := r.widget.duration
	position := r.widget.position
	zoom := r.widget.zoom
	loopStart := r.widget.loopStart
	loopEnd := r.widget.loopEnd
	loopActive := r.widget.loopActive
	cuePoint := r.widget.cuePoint
	r.widget.mu.RUnlock()

	// Resize image buffer if needed
	if r.img == nil || r.img.Bounds().Dx() != w || r.img.Bounds().Dy() != h {
		r.img = image.NewRGBA(image.Rect(0, 0, w, h))
	}

	// Clear with background color (fill first row, then copy)
	bg := boomtheme.ColorWaveformBg
	pix := r.img.Pix
	stride := r.img.Stride
	for i := 0; i < stride; i += 4 {
		pix[i] = bg.R
		pix[i+1] = bg.G
		pix[i+2] = bg.B
		pix[i+3] = 255
	}
	for y := 1; y < h; y++ {
		copy(pix[y*stride:(y+1)*stride], pix[:stride])
	}

	peakCount := len(peaksLow)
	if peakCount == 0 || len(peaksMid) != peakCount || len(peaksHigh) != peakCount {
		return r.img
	}

	// Compute visible window
	if zoom <= 0 {
		zoom = 0.1
	}
	viewStart := position - zoom/2
	viewEnd := position + zoom/2

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

	centerY := h / 2
	maxH := float64(centerY - 2)
	headX := int(((position - viewStart) / viewRange) * float64(w))

	// Draw waveform — one pixel column at a time
	for x := 0; x < w; x++ {
		trackPos := viewStart + float64(x)/float64(w)*viewRange
		peakIdxF := trackPos * float64(peakCount)

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

		hLow := int(pLow * 0.50 * maxH)
		hMid := int(pMid * 0.30 * maxH)
		hHigh := int(pHigh * 0.20 * maxH)
		totalH := hLow + hMid + hHigh
		lowMidH := hLow + hMid

		beforePlayhead := x < headX

		// Draw stacked layers (outside-in: high → mid → low)
		if totalH > 0 {
			var cHigh, cMid, cLow color.RGBA
			if beforePlayhead {
				cHigh = boomtheme.ColorWaveformHighDim
				cMid = boomtheme.ColorWaveformMidDim
				cLow = boomtheme.ColorWaveformLowDim
			} else {
				cHigh = boomtheme.ColorWaveformHigh
				cMid = boomtheme.ColorWaveformMid
				cLow = boomtheme.ColorWaveformLow
			}

			// High (outermost)
			r.vLine(x, centerY-totalH, centerY+totalH, cHigh)
			// Mid (middle)
			if lowMidH > 0 {
				r.vLine(x, centerY-lowMidH, centerY+lowMidH, cMid)
			}
			// Low (innermost, bass core)
			if hLow > 0 {
				r.vLine(x, centerY-hLow, centerY+hLow, cLow)
			}
		}
	}

	// Draw loop overlay under the beat markers so they stay legible.
	if loopStart >= 0 && loopEnd > loopStart {
		xStartF := (loopStart - viewStart) / viewRange * float64(w)
		xEndF := (loopEnd - viewStart) / viewRange * float64(w)
		xStart := int(xStartF)
		xEnd := int(xEndF)
		if xEnd > w {
			xEnd = w
		}
		if xStart < 0 {
			xStart = 0
		}
		fill := boomtheme.ColorLoopFill
		if !loopActive {
			fill.A = 30
		}
		// Fill region with alpha-blended orange.
		for x := xStart; x < xEnd; x++ {
			r.vLineAlpha(x, 2, h-3, fill)
		}
		// Bright boundary lines at in/out.
		marker := boomtheme.ColorLoopMarker
		if xStartF >= 0 && xStartF < float64(w) {
			bx := int(xStartF)
			r.vLineAlpha(bx, 0, h-1, marker)
			r.vLineAlpha(bx+1, 0, h-1, marker)
		}
		if xEndF >= 0 && xEndF < float64(w) {
			bx := int(xEndF)
			r.vLineAlpha(bx, 0, h-1, marker)
			r.vLineAlpha(bx-1, 0, h-1, marker)
		}
	}

	// Draw beat markers on top of waveform
	durSec := duration.Seconds()
	if durSec > 0 && len(beatGrid) > 0 {
		for i, beatTime := range beatGrid {
			beatNorm := beatTime / durSec
			xFrac := (beatNorm - viewStart) / viewRange
			if xFrac < -0.01 || xFrac > 1.01 {
				continue
			}
			bx := int(xFrac * float64(w))

			var c color.RGBA
			if i%4 == 0 {
				c = boomtheme.ColorBeatLineStrong
			} else {
				c = boomtheme.ColorBeatLine
			}
			r.vLineAlpha(bx, 2, h-3, c)
		}
	}

	// Draw cue marker — solid orange vertical line within the visible window.
	if cuePoint >= 0 {
		cueXF := (cuePoint - viewStart) / viewRange * float64(w)
		if cueXF >= 0 && cueXF < float64(w) {
			cx := int(cueXF)
			cueColor := boomtheme.ColorCueActive
			r.vLineAlpha(cx, 0, h-1, cueColor)
			r.vLineAlpha(cx+1, 0, h-1, cueColor)
		}
	}

	// Draw playhead (2px wide for visibility)
	playheadColor := boomtheme.ColorPlayhead
	r.vLineAlpha(headX-1, 0, h-1, playheadColor)
	r.vLineAlpha(headX, 0, h-1, playheadColor)

	return r.img
}

// vLine draws a solid vertical line into the image buffer.
func (r *deckStripRenderer) vLine(x, y0, y1 int, c color.RGBA) {
	img := r.img
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	if x < 0 || x >= w {
		return
	}
	if y0 < 0 {
		y0 = 0
	}
	if y1 >= h {
		y1 = h - 1
	}
	pix := img.Pix
	stride := img.Stride
	if c.A == 255 {
		for y := y0; y <= y1; y++ {
			off := y*stride + x*4
			pix[off] = c.R
			pix[off+1] = c.G
			pix[off+2] = c.B
			pix[off+3] = 255
		}
	} else {
		a := uint32(c.A)
		ia := 255 - a
		for y := y0; y <= y1; y++ {
			off := y*stride + x*4
			pix[off] = uint8((uint32(pix[off])*ia + uint32(c.R)*a) / 255)
			pix[off+1] = uint8((uint32(pix[off+1])*ia + uint32(c.G)*a) / 255)
			pix[off+2] = uint8((uint32(pix[off+2])*ia + uint32(c.B)*a) / 255)
			pix[off+3] = 255
		}
	}
}

// vLineAlpha draws a vertical line with alpha blending onto existing pixels.
func (r *deckStripRenderer) vLineAlpha(x, y0, y1 int, c color.RGBA) {
	img := r.img
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	if x < 0 || x >= w {
		return
	}
	if y0 < 0 {
		y0 = 0
	}
	if y1 >= h {
		y1 = h - 1
	}
	a := uint32(c.A)
	ia := 255 - a
	pix := img.Pix
	stride := img.Stride
	for y := y0; y <= y1; y++ {
		off := y*stride + x*4
		pix[off] = uint8((uint32(pix[off])*ia + uint32(c.R)*a) / 255)
		pix[off+1] = uint8((uint32(pix[off+1])*ia + uint32(c.G)*a) / 255)
		pix[off+2] = uint8((uint32(pix[off+2])*ia + uint32(c.B)*a) / 255)
		pix[off+3] = 255
	}
}

func (r *deckStripRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.topLine, r.empty, r.raster}
}

func (r *deckStripRenderer) Destroy() {}
