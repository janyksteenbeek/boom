package beatgrid

import (
	"image"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"

	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
)

type deckStripRenderer struct {
	widget  *DeckStrip
	bg      *canvas.Rectangle
	topLine *canvas.Line
	empty   *canvas.Text
	raster  *canvas.Raster
	img     *image.RGBA
	size    fyne.Size

	// Caches used to gate redundant raster redraws: if neither the layout
	// (peaks/loop/cue/beats/zoom) nor the pixel position has changed since
	// the last draw, skip the raster.Refresh() — which in turn avoids the
	// alpha-blended draw() call on Fyne's render thread.
	lastLayoutVersion uint64
	lastDrawnPosition float64
	lastSize          fyne.Size
	lastHadPeaks      bool
	layoutReady       bool
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

	r.widget.mu.RLock()
	layoutVersion := r.widget.layoutVersion
	position := r.widget.position
	zoom := r.widget.zoom
	hasPeaks := len(r.widget.peaksLow) > 0
	r.widget.mu.RUnlock()

	layoutChanged := !r.layoutReady ||
		layoutVersion != r.lastLayoutVersion ||
		size != r.lastSize ||
		hasPeaks != r.lastHadPeaks

	// Pixel-delta gate: the view window is `zoom` wide in normalized
	// track units. One pixel of strip width corresponds to zoom/width
	// normalized units, so a sub-pixel position delta can't possibly
	// change the rendered image and we can skip the redraw entirely.
	positionChanged := false
	if hasPeaks && zoom > 0 && size.Width > 0 {
		delta := position - r.lastDrawnPosition
		if delta < 0 {
			delta = -delta
		}
		positionChanged = delta*float64(size.Width)/zoom >= 1
	}

	if !layoutChanged && !positionChanged {
		return
	}

	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))
	r.topLine.Position1 = fyne.NewPos(0, 0.5)
	r.topLine.Position2 = fyne.NewPos(size.Width, 0.5)

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

	// Bg/topLine/empty only need to be re-marked on true layout changes
	// (size or content changed). Skipping these on pixel-only scrolls
	// avoids a driver round-trip per element per frame.
	if layoutChanged {
		r.bg.Refresh()
		r.topLine.Refresh()
		r.empty.Refresh()
	}

	r.lastLayoutVersion = layoutVersion
	r.lastSize = size
	r.lastHadPeaks = hasPeaks
	r.lastDrawnPosition = position
	r.layoutReady = true
}

func (r *deckStripRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.topLine, r.empty, r.raster}
}

func (r *deckStripRenderer) Destroy() {}

// stripSnapshot captures the widget state needed for one frame.
type stripSnapshot struct {
	peaksLow, peaksMid, peaksHigh []float64
	beatGrid                      []float64
	duration                      time.Duration
	position                      float64
	zoom                          float64
	loopStart, loopEnd            float64
	loopActive                    bool
	cuePoint                      float64
}

func (r *deckStripRenderer) snapshot() stripSnapshot {
	r.widget.mu.RLock()
	defer r.widget.mu.RUnlock()
	return stripSnapshot{
		peaksLow:   r.widget.peaksLow,
		peaksMid:   r.widget.peaksMid,
		peaksHigh:  r.widget.peaksHigh,
		beatGrid:   r.widget.beatGrid,
		duration:   r.widget.duration,
		position:   r.widget.position,
		zoom:       r.widget.zoom,
		loopStart:  r.widget.loopStart,
		loopEnd:    r.widget.loopEnd,
		loopActive: r.widget.loopActive,
		cuePoint:   r.widget.cuePoint,
	}
}

// computeView returns the visible (start, end) window centered on the
// current playhead, clamped to [0,1] without losing zoom width.
func computeView(position, zoom float64) (start, end float64) {
	if zoom <= 0 {
		zoom = 0.1
	}
	start = position - zoom/2
	end = position + zoom/2
	if start < 0 {
		end -= start
		start = 0
		if end > 1.0 {
			end = 1.0
		}
	}
	if end > 1.0 {
		start -= (end - 1.0)
		end = 1.0
		if start < 0 {
			start = 0
		}
	}
	return start, end
}

// draw is called by Fyne's render thread to produce the raster image.
// It snapshots the widget under RLock, draws the waveform, beats, loop and
// playhead into a single buffer, and returns it.
func (r *deckStripRenderer) draw(w, h int) image.Image {
	if w <= 0 || h <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}

	snap := r.snapshot()

	if r.img == nil || r.img.Bounds().Dx() != w || r.img.Bounds().Dy() != h {
		r.img = image.NewRGBA(image.Rect(0, 0, w, h))
	}
	r.fillBackground()

	peakCount := len(snap.peaksLow)
	if peakCount == 0 || len(snap.peaksMid) != peakCount || len(snap.peaksHigh) != peakCount {
		return r.img
	}

	viewStart, viewEnd := computeView(snap.position, snap.zoom)
	viewRange := viewEnd - viewStart
	if viewRange <= 0 {
		viewRange = 1.0
	}

	centerY := h / 2
	headX := int(((snap.position - viewStart) / viewRange) * float64(w))

	r.drawWaveform(snap, w, h, centerY, headX, viewStart, viewRange)
	r.drawLoop(snap, w, h, viewStart, viewRange)
	r.drawBeats(snap, w, h, viewStart, viewRange)
	r.drawCue(snap, w, h, viewStart, viewRange)
	r.drawPlayhead(headX, h)

	return r.img
}

func (r *deckStripRenderer) fillBackground() {
	bg := boomtheme.ColorWaveformBg
	pix := r.img.Pix
	stride := r.img.Stride
	for i := 0; i < stride; i += 4 {
		pix[i] = bg.R
		pix[i+1] = bg.G
		pix[i+2] = bg.B
		pix[i+3] = 255
	}
	for y := 1; y < r.img.Bounds().Dy(); y++ {
		copy(pix[y*stride:(y+1)*stride], pix[:stride])
	}
}

// drawWaveform paints the three stacked frequency layers into the strip.
func (r *deckStripRenderer) drawWaveform(snap stripSnapshot, w, h, centerY, headX int, viewStart, viewRange float64) {
	peakCount := len(snap.peaksLow)
	maxH := float64(centerY - 2)

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

		pLow := snap.peaksLow[idx0]*(1-frac) + snap.peaksLow[idx1]*frac
		pMid := snap.peaksMid[idx0]*(1-frac) + snap.peaksMid[idx1]*frac
		pHigh := snap.peaksHigh[idx0]*(1-frac) + snap.peaksHigh[idx1]*frac

		hLow := int(pLow * 0.50 * maxH)
		hMid := int(pMid * 0.30 * maxH)
		hHigh := int(pHigh * 0.20 * maxH)
		totalH := hLow + hMid + hHigh
		lowMidH := hLow + hMid

		if totalH <= 0 {
			continue
		}

		var cHigh, cMid, cLow color.RGBA
		if x < headX {
			cHigh = boomtheme.ColorWaveformHighDim
			cMid = boomtheme.ColorWaveformMidDim
			cLow = boomtheme.ColorWaveformLowDim
		} else {
			cHigh = boomtheme.ColorWaveformHigh
			cMid = boomtheme.ColorWaveformMid
			cLow = boomtheme.ColorWaveformLow
		}

		r.vLine(x, centerY-totalH, centerY+totalH, cHigh)
		if lowMidH > 0 {
			r.vLine(x, centerY-lowMidH, centerY+lowMidH, cMid)
		}
		if hLow > 0 {
			r.vLine(x, centerY-hLow, centerY+hLow, cLow)
		}
	}
}

// drawLoop paints the loop region under the beat markers so they stay legible.
func (r *deckStripRenderer) drawLoop(snap stripSnapshot, w, h int, viewStart, viewRange float64) {
	if snap.loopStart < 0 || snap.loopEnd <= snap.loopStart {
		return
	}
	xStartF := (snap.loopStart - viewStart) / viewRange * float64(w)
	xEndF := (snap.loopEnd - viewStart) / viewRange * float64(w)
	xStart := int(xStartF)
	xEnd := int(xEndF)
	if xEnd > w {
		xEnd = w
	}
	if xStart < 0 {
		xStart = 0
	}
	fill := boomtheme.ColorLoopFill
	if !snap.loopActive {
		fill.A = 30
	}
	for x := xStart; x < xEnd; x++ {
		r.vLineAlpha(x, 2, h-3, fill)
	}
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

// drawBeats paints beat markers, with downbeats brighter than off-beats.
func (r *deckStripRenderer) drawBeats(snap stripSnapshot, w, h int, viewStart, viewRange float64) {
	durSec := snap.duration.Seconds()
	if durSec <= 0 || len(snap.beatGrid) == 0 {
		return
	}
	for i, beatTime := range snap.beatGrid {
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

// drawCue paints the saved cue point as a 2-pixel orange line.
func (r *deckStripRenderer) drawCue(snap stripSnapshot, w, h int, viewStart, viewRange float64) {
	if snap.cuePoint < 0 {
		return
	}
	cueXF := (snap.cuePoint - viewStart) / viewRange * float64(w)
	if cueXF < 0 || cueXF >= float64(w) {
		return
	}
	cx := int(cueXF)
	cueColor := boomtheme.ColorCueActive
	r.vLineAlpha(cx, 0, h-1, cueColor)
	r.vLineAlpha(cx+1, 0, h-1, cueColor)
}

// drawPlayhead paints the 2-pixel-wide playhead.
func (r *deckStripRenderer) drawPlayhead(headX, h int) {
	playheadColor := boomtheme.ColorPlayhead
	r.vLineAlpha(headX-1, 0, h-1, playheadColor)
	r.vLineAlpha(headX, 0, h-1, playheadColor)
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
		return
	}
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
