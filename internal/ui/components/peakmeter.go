package components

import (
	"image/color"
	"math"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"

	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
)

// PeakMeter is a vertical LED-style level meter. It renders as a stack of
// segments that light up from the bottom — green for the lower band, yellow
// for the middle, red for the top — plus a peak-hold marker that decays
// slowly so transients stay visible.
//
// The widget keeps its own envelope-followed display level, so callers can
// hand it raw measurements at whatever rate is convenient (the engine VU
// loop runs at ~20 Hz) and the meter still animates smoothly.
type PeakMeter struct {
	widget.BaseWidget

	mu sync.RWMutex

	// rawLevel is the most recent value passed to SetLevel.
	rawLevel float64
	// displayLevel trails rawLevel via an envelope follower for smooth motion.
	displayLevel float64
	// peakHold is the recent maximum, decaying slowly so transients linger.
	peakHold     float64
	peakHoldTime time.Time
	lastUpdate   time.Time
}

// peakMeterSegments is the number of vertical LED segments. Higher counts
// look smoother but cost a few more canvas objects per meter.
const peakMeterSegments = 24

// peakHoldFallPerSec is how quickly (in normalized units) the peak hold
// marker drifts back down once nothing fresh is exceeding it.
const peakHoldFallPerSec = 0.45

// peakHoldHangSeconds is how long the peak hold sits at its high-water
// mark before it starts falling.
const peakHoldHangSeconds = 0.6

// peakRiseTau / peakFallTau control the envelope follower used to smooth
// the displayed level. Rise is near-instant so the meter snaps to a hot
// transient; fall is slower so the eye can read the level.
const (
	peakRiseTau = 0.02
	peakFallTau = 0.18
)

func NewPeakMeter() *PeakMeter {
	m := &PeakMeter{}
	m.lastUpdate = time.Now()
	m.peakHoldTime = m.lastUpdate
	m.ExtendBaseWidget(m)
	return m
}

// SetLevel feeds a new normalized 0..1 reading into the meter. Out-of-range
// values are clamped. Safe to call from any goroutine.
func (m *PeakMeter) SetLevel(v float64) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	m.mu.Lock()
	now := time.Now()
	dt := now.Sub(m.lastUpdate).Seconds()
	if dt <= 0 {
		dt = 0.001
	}
	if dt > 0.5 {
		dt = 0.5
	}
	m.lastUpdate = now
	m.rawLevel = v

	// Envelope follower: rise fast, fall slow.
	if v > m.displayLevel {
		alpha := 1 - math.Exp(-dt/peakRiseTau)
		m.displayLevel += (v - m.displayLevel) * alpha
	} else {
		alpha := 1 - math.Exp(-dt/peakFallTau)
		m.displayLevel += (v - m.displayLevel) * alpha
	}

	if m.displayLevel >= m.peakHold {
		m.peakHold = m.displayLevel
		m.peakHoldTime = now
	} else if now.Sub(m.peakHoldTime).Seconds() > peakHoldHangSeconds {
		fall := peakHoldFallPerSec * dt
		m.peakHold -= fall
		if m.peakHold < m.displayLevel {
			m.peakHold = m.displayLevel
		}
		if m.peakHold < 0 {
			m.peakHold = 0
		}
	}
	m.mu.Unlock()

	fyne.Do(func() { m.Refresh() })
}

func (m *PeakMeter) MinSize() fyne.Size {
	return fyne.NewSize(14, 80)
}

func (m *PeakMeter) CreateRenderer() fyne.WidgetRenderer {
	r := &peakMeterRenderer{meter: m}
	r.build()
	return r
}

type peakMeterRenderer struct {
	meter *PeakMeter

	bg       *canvas.Rectangle
	segments [peakMeterSegments]*canvas.Rectangle
	peakBar  *canvas.Rectangle

	allObjs []fyne.CanvasObject
}

func (r *peakMeterRenderer) build() {
	r.bg = canvas.NewRectangle(color.RGBA{R: 12, G: 12, B: 14, A: 255})
	r.bg.StrokeColor = color.RGBA{R: 50, G: 50, B: 55, A: 255}
	r.bg.StrokeWidth = 1
	r.bg.CornerRadius = 3

	for i := range r.segments {
		seg := canvas.NewRectangle(color.Transparent)
		seg.CornerRadius = 1
		r.segments[i] = seg
	}

	r.peakBar = canvas.NewRectangle(color.RGBA{R: 255, G: 255, B: 255, A: 230})
	r.peakBar.CornerRadius = 1

	r.allObjs = make([]fyne.CanvasObject, 0, 2+peakMeterSegments)
	r.allObjs = append(r.allObjs, r.bg)
	for i := range r.segments {
		r.allObjs = append(r.allObjs, r.segments[i])
	}
	r.allObjs = append(r.allObjs, r.peakBar)

	r.Refresh()
}

func (r *peakMeterRenderer) Layout(size fyne.Size) { r.layout(size) }
func (r *peakMeterRenderer) MinSize() fyne.Size    { return r.meter.MinSize() }
func (r *peakMeterRenderer) Destroy()              {}
func (r *peakMeterRenderer) Objects() []fyne.CanvasObject {
	return r.allObjs
}

func (r *peakMeterRenderer) Refresh() { r.layout(r.meter.Size()) }

func (r *peakMeterRenderer) layout(size fyne.Size) {
	if size.Width <= 0 || size.Height <= 0 {
		return
	}

	r.meter.mu.RLock()
	level := r.meter.displayLevel
	hold := r.meter.peakHold
	r.meter.mu.RUnlock()

	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))

	const (
		insetX     = float32(2)
		topPadding = float32(3)
		botPadding = float32(3)
		gapPx      = float32(1)
	)

	innerX := insetX
	innerW := size.Width - 2*insetX
	innerY := topPadding
	innerH := size.Height - topPadding - botPadding
	if innerW < 1 || innerH < 1 {
		return
	}
	segH := (innerH - gapPx*float32(peakMeterSegments-1)) / float32(peakMeterSegments)
	if segH < 1 {
		segH = 1
	}

	activeUpTo := int(math.Round(level * float64(peakMeterSegments)))
	if activeUpTo > peakMeterSegments {
		activeUpTo = peakMeterSegments
	}

	for i := 0; i < peakMeterSegments; i++ {
		seg := r.segments[i]
		// Bottom segment is index 0, top segment is index peakMeterSegments-1.
		y := innerY + innerH - float32(i+1)*segH - float32(i)*gapPx
		seg.Resize(fyne.NewSize(innerW, segH))
		seg.Move(fyne.NewPos(innerX, y))

		base := segmentColor(i)
		if i < activeUpTo {
			seg.FillColor = base
		} else {
			seg.FillColor = dimColor(base, 0.18)
		}
		seg.Refresh()
	}

	// Peak hold: thin bright bar sitting at the hold level.
	if hold > 0.01 {
		idx := int(math.Round(hold*float64(peakMeterSegments))) - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= peakMeterSegments {
			idx = peakMeterSegments - 1
		}
		holdY := innerY + innerH - float32(idx+1)*segH - float32(idx)*gapPx
		barH := float32(2)
		r.peakBar.FillColor = peakHoldColor(idx)
		r.peakBar.Resize(fyne.NewSize(innerW, barH))
		r.peakBar.Move(fyne.NewPos(innerX, holdY-barH+segH/2))
		r.peakBar.Show()
		r.peakBar.Refresh()
	} else {
		r.peakBar.Hide()
	}

	r.bg.Refresh()
}

// segmentColor returns the base color for the i-th LED segment (0 at the
// bottom, peakMeterSegments-1 at the top). Lower segments are green,
// middle yellow, top red — same convention as analog mixer meters.
func segmentColor(i int) color.Color {
	frac := float64(i) / float64(peakMeterSegments-1)
	switch {
	case frac < 0.7:
		return boomtheme.ColorGreen
	case frac < 0.9:
		return boomtheme.ColorYellow
	default:
		return boomtheme.ColorRed
	}
}

func peakHoldColor(i int) color.Color {
	c := segmentColor(i)
	r, g, b, _ := c.RGBA()
	return color.RGBA{
		R: uint8(r >> 8),
		G: uint8(g >> 8),
		B: uint8(b >> 8),
		A: 255,
	}
}

func dimColor(c color.Color, alpha float64) color.Color {
	r, g, b, _ := c.RGBA()
	return color.RGBA{
		R: uint8(float64(r>>8) * alpha),
		G: uint8(float64(g>>8) * alpha),
		B: uint8(float64(b>>8) * alpha),
		A: 255,
	}
}
