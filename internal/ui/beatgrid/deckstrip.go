package beatgrid

import (
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
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

	// layoutVersion is bumped by anything the raster draw() depends on
	// except the playhead position. Lets the renderer skip full redraws
	// when only the playhead moved a sub-pixel amount.
	layoutVersion uint64

	// onSeek is invoked with a normalized 0..1 position whenever the user
	// drags horizontally on the strip to scrub. Nil = non-interactive.
	onSeek func(float64)
}

var _ fyne.Draggable = (*DeckStrip)(nil)

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
	d.layoutVersion++
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
	d.layoutVersion++
	d.mu.Unlock()
	fyne.Do(func() { d.Refresh() })
}

func (d *DeckStrip) SetFrequencyPeaks(low, mid, high []float64) {
	d.mu.Lock()
	d.peaksLow = low
	d.peaksMid = mid
	d.peaksHigh = high
	d.layoutVersion++
	d.mu.Unlock()
	fyne.Do(func() { d.Refresh() })
}

func (d *DeckStrip) SetPosition(pos float64) {
	d.mu.Lock()
	if d.position == pos {
		d.mu.Unlock()
		return
	}
	d.position = pos
	d.mu.Unlock()
	fyne.Do(func() { d.Refresh() })
}

func (d *DeckStrip) SetZoom(zoom float64) {
	d.mu.Lock()
	if d.zoom == zoom {
		d.mu.Unlock()
		return
	}
	d.zoom = zoom
	d.layoutVersion++
	d.mu.Unlock()
	fyne.Do(func() { d.Refresh() })
}

func (d *DeckStrip) SetBeatGrid(beats []float64) {
	d.mu.Lock()
	d.beatGrid = beats
	d.layoutVersion++
	d.mu.Unlock()
	fyne.Do(func() { d.Refresh() })
}

func (d *DeckStrip) SetDuration(dur time.Duration) {
	d.mu.Lock()
	d.duration = dur
	d.mu.Unlock()
}

// SetOnSeek installs the callback fired while the user drags horizontally
// on the strip to scrub. Receives a normalized 0..1 position.
func (d *DeckStrip) SetOnSeek(fn func(float64)) {
	d.onSeek = fn
}

// Dragged applies a relative jog based on the horizontal drag delta. The
// scrolling strip is centered on the playhead, so dragging *right* is
// analogous to pushing a vinyl platter right: the waveform scrolls right
// (older content is revealed) and the playback position moves *backward*.
func (d *DeckStrip) Dragged(ev *fyne.DragEvent) {
	if d.onSeek == nil {
		return
	}
	w := float64(d.Size().Width)
	if w <= 0 {
		return
	}
	d.mu.Lock()
	if d.zoom <= 0 {
		d.mu.Unlock()
		return
	}
	delta := -float64(ev.Dragged.DX) / w * d.zoom
	d.position += delta
	if d.position < 0 {
		d.position = 0
	}
	if d.position > 1 {
		d.position = 1
	}
	newPos := d.position
	d.mu.Unlock()
	fyne.Do(func() { d.Refresh() })
	d.onSeek(newPos)
}

func (d *DeckStrip) DragEnd() {}

func (d *DeckStrip) MinSize() fyne.Size {
	return fyne.NewSize(100, 56)
}

func (d *DeckStrip) CreateRenderer() fyne.WidgetRenderer {
	r := &deckStripRenderer{widget: d}
	r.buildObjects()
	return r
}
