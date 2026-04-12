package audio

import (
	"encoding/json"
	"math"
)

// LoopStart returns the normalized loop-start position, or NaN if unset.
func (d *Deck) LoopStart() float64 { return d.loadFloat(&d.loopStart) }

// LoopEnd returns the normalized loop-end position, or NaN if unset.
func (d *Deck) LoopEnd() float64 { return d.loadFloat(&d.loopEnd) }

// LoopBeats returns the beat-length of the current loop (0 = manual/unknown).
func (d *Deck) LoopBeats() float64 { return d.loadFloat(&d.loopBeats) }

// IsLoopActive reports whether the audio thread is currently wrapping
// playback inside the loop boundaries.
func (d *Deck) IsLoopActive() bool { return d.loopActive.Load() }

// HasLoop reports whether both loop boundaries are set to a valid range.
func (d *Deck) HasLoop() bool {
	s := d.LoopStart()
	e := d.LoopEnd()
	return !math.IsNaN(s) && !math.IsNaN(e) && e > s && s >= 0
}

// ClearLoop removes any configured loop and deactivates wrapping.
func (d *Deck) ClearLoop() {
	d.loopActive.Store(false)
	d.storeFloat(&d.loopStart, math.NaN())
	d.storeFloat(&d.loopEnd, math.NaN())
	d.storeFloat(&d.loopBeats, 0)
}

// SetLoopStart stores the loop-start position and deactivates any active
// loop — the out-point must be set before the loop wraps.
func (d *Deck) SetLoopStart(pos float64) {
	if pos < 0 {
		pos = 0
	}
	if pos > 1 {
		pos = 1
	}
	d.loopActive.Store(false)
	d.storeFloat(&d.loopStart, pos)
	d.storeFloat(&d.loopEnd, math.NaN())
	d.storeFloat(&d.loopBeats, 0)
}

// SetManualLoop stores an in/out pair and activates the loop. beats is the
// measured length (or 0 if unknown).
func (d *Deck) SetManualLoop(start, end, beats float64) {
	if end <= start {
		return
	}
	if start < 0 {
		start = 0
	}
	if end > 1 {
		end = 1
	}
	d.storeFloat(&d.loopStart, start)
	d.storeFloat(&d.loopEnd, end)
	d.storeFloat(&d.loopBeats, beats)
	d.loopActive.Store(true)
}

// SetLoopActive toggles the wrap flag without touching the boundaries.
func (d *Deck) SetLoopActive(v bool) { d.loopActive.Store(v) }

// SamplesPerBeat returns the number of samples in one beat at the track's
// native (non-tempo-adjusted) BPM. Using the native BPM keeps halve/double
// musically meaningful when the user later changes tempo.
func (d *Deck) SamplesPerBeat() float64 {
	t := d.track.Load()
	if t == nil || t.BPM <= 0 {
		return 0
	}
	return float64(d.sampleRate) * 60.0 / t.BPM
}

// NearestBeatBefore snaps a normalized position to the nearest beat at or
// before it, using Track.BeatGrid (JSON []float64 seconds) when available,
// falling back to a computed grid from BPM, and finally returning pos
// unchanged if neither is available.
func (d *Deck) NearestBeatBefore(pos float64) float64 {
	p := d.pcm.Load()
	if p == nil || p.len == 0 {
		return pos
	}
	t := d.track.Load()
	if t == nil {
		return pos
	}
	totalSeconds := float64(p.len) / float64(d.sampleRate)
	if totalSeconds <= 0 {
		return pos
	}
	posSeconds := pos * totalSeconds

	if t.BeatGrid != "" {
		var beats []float64
		if err := json.Unmarshal([]byte(t.BeatGrid), &beats); err == nil && len(beats) > 0 {
			best := beats[0]
			for _, b := range beats {
				if b <= posSeconds {
					best = b
				} else {
					break
				}
			}
			return best / totalSeconds
		}
	}
	if t.BPM > 0 {
		beatPeriod := 60.0 / t.BPM
		n := math.Floor(posSeconds / beatPeriod)
		return (n * beatPeriod) / totalSeconds
	}
	return pos
}

// UpdateTrackAnalysis refreshes the deck's cached track metadata after an
// analysis pass completes. Without this the deck keeps the BPM=0 / empty
// beatgrid snapshot captured at LoadTrack time, and beat-based features
// (loops, sync) silently no-op on newly analyzed tracks.
func (d *Deck) UpdateTrackAnalysis(trackID string, bpm float64, key string, beatGridJSON string) bool {
	t := d.track.Load()
	if t == nil || t.ID != trackID {
		return false
	}
	updated := *t
	updated.BPM = bpm
	updated.Key = key
	updated.BeatGrid = beatGridJSON
	d.track.Store(&updated)
	return true
}
