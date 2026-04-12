package deck

import (
	"fmt"
	"math"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
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

	// Loop overlay state. loopStart/loopEnd are normalized 0..1; loopBeats is
	// the beat-length used for the text label; loopActive controls whether
	// playback is currently wrapping (label gets brighter when inactive to
	// hint that it's a stored-but-paused loop).
	loopStart  float64
	loopEnd    float64
	loopBeats  float64
	loopActive bool
}

func NewWaveformWidget(deckID int) *WaveformWidget {
	w := &WaveformWidget{
		deckID:    deckID,
		cuePoint:  -1,
		loopStart: -1,
		loopEnd:   -1,
	}
	w.ExtendBaseWidget(w)
	return w
}

// SetLoopState updates the loop overlay. Pass start<0 (or end<=start) to hide.
func (w *WaveformWidget) SetLoopState(start, end, beats float64, active bool) {
	w.mu.Lock()
	if w.loopStart == start && w.loopEnd == end && w.loopBeats == beats && w.loopActive == active {
		w.mu.Unlock()
		return
	}
	w.loopStart = start
	w.loopEnd = end
	w.loopBeats = beats
	w.loopActive = active
	w.mu.Unlock()
	fyne.Do(func() {
		w.Refresh()
	})
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

// labelForBeats returns the beat-length caption shown inside the loop region
// on the waveform.
func labelForBeats(beats float64) string {
	switch {
	case beats <= 0:
		return ""
	case beats >= 0.999:
		if beats == float64(int(beats)) {
			return fmt.Sprintf("%d Beats", int(beats))
		}
		return fmt.Sprintf("%.1f Beats", beats)
	case beats >= 0.49 && beats <= 0.51:
		return "1/2 Beat"
	case beats >= 0.24 && beats <= 0.26:
		return "1/4 Beat"
	case beats >= 0.124 && beats <= 0.126:
		return "1/8 Beat"
	case beats >= 0.062 && beats <= 0.063:
		return "1/16 Beat"
	case beats >= 0.031 && beats <= 0.032:
		return "1/32 Beat"
	default:
		return fmt.Sprintf("%.2f Beats", beats)
	}
}
