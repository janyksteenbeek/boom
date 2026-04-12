package audio

import "math"

// CuePoint returns the manual cue point as a normalized 0..1 fraction,
// or a negative value if no manual cue is set.
func (d *Deck) CuePoint() float64 { return d.loadFloat(&d.cuePoint) }

// SetCuePoint stores a new manual cue point. Pass a negative value (or call
// ClearCue) to mark the manual cue as unset.
func (d *Deck) SetCuePoint(p float64) {
	if p > 1 {
		p = 1
	}
	d.storeFloat(&d.cuePoint, p)
}

// HasCue reports whether a manual cue point is currently set.
func (d *Deck) HasCue() bool { return d.CuePoint() >= 0 }

// ClearCue removes the manual cue point. The fallback cue remains available
// via EffectiveCue().
func (d *Deck) ClearCue() { d.storeFloat(&d.cuePoint, -1) }

// FallbackCue returns the auto-cue / track-start position computed on load.
// Always a valid 0..1 value (defaults to 0 when not explicitly set).
func (d *Deck) FallbackCue() float64 { return d.loadFloat(&d.fallbackCue) }

// SetFallbackCue stores the auto-cue / track-start position.
func (d *Deck) SetFallbackCue(p float64) {
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	d.storeFloat(&d.fallbackCue, p)
}

// EffectiveCue returns the manual cue if one is set, otherwise the fallback
// (auto-cue or track start).
func (d *Deck) EffectiveCue() float64 {
	if d.HasCue() {
		return d.CuePoint()
	}
	return d.FallbackCue()
}

// CueHeld reports whether the CUE button is currently held down.
func (d *Deck) CueHeld() bool { return d.cueHeld.Load() }

// SetCueHeld marks the CUE button as held / released.
func (d *Deck) SetCueHeld(v bool) { d.cueHeld.Store(v) }

// CuePreview reports whether playback is currently a cue-hold preview
// (will snap back to cue on release unless latched by PLAY).
func (d *Deck) CuePreview() bool { return d.cuePreview.Load() }

// SetCuePreview marks the current playback as a cue-hold preview.
func (d *Deck) SetCuePreview(v bool) { d.cuePreview.Store(v) }

// FirstAudioFrame scans the PCM buffer for the first sample whose absolute
// amplitude exceeds a silence threshold (~-60 dBFS) and returns the position
// as a normalized 0..1 fraction of the full track. Must only be called once
// the streaming decode has completed (i.e. after <-deck.DecodeDone()) so
// the normalization denominator matches the UI's position scale.
func (d *Deck) FirstAudioFrame() float64 {
	p := d.pcm.Load()
	if p == nil {
		return 0
	}
	pLen := p.Len()
	pTotal := p.Total()
	if pLen == 0 || pTotal == 0 {
		return 0
	}
	const threshold float32 = 0.001 // ~-60 dBFS
	samples := p.samples[:pLen]
	for i := 0; i < pLen; i++ {
		l := samples[i][0]
		r := samples[i][1]
		if l < 0 {
			l = -l
		}
		if r < 0 {
			r = -r
		}
		if l > threshold || r > threshold {
			return float64(i) / float64(pTotal)
		}
	}
	return 0
}

// AtEffectiveCue reports whether the playhead is at the effective cue point
// (manual cue if set, else fallback) within a ~50 ms tolerance. Returns false
// when no track is loaded.
func (d *Deck) AtEffectiveCue() bool {
	p := d.pcm.Load()
	if p == nil {
		return false
	}
	pTotal := p.Total()
	if pTotal == 0 {
		return false
	}
	eps := float64(d.sampleRate) * 0.05 / float64(pTotal)
	return math.Abs(d.Position()-d.EffectiveCue()) <= eps
}
