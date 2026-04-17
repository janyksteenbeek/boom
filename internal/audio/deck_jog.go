package audio

import "math"

// VinylMode reports whether the deck is in vinyl mode (top touch enables
// scratching). When false the deck is in plain "jog mode" — all rotation
// is pitch bend.
func (d *Deck) VinylMode() bool { return d.vinylMode.Load() }

// SetVinylMode toggles vinyl mode. Switching to jog mode while the platter
// is touched also clears the captured play state and any pending scratch
// velocity so the deck snaps back to normal playback cleanly.
func (d *Deck) SetVinylMode(on bool) {
	d.vinylMode.Store(on)
	if !on {
		d.storeFloat(&d.jogScratchVel, 0)
	}
}

// ToggleVinylMode flips vinyl mode and returns the new value.
func (d *Deck) ToggleVinylMode() bool {
	d.SetVinylMode(!d.VinylMode())
	return d.VinylMode()
}

// JogTouched reports whether the top sensor of the platter is currently held.
func (d *Deck) JogTouched() bool { return d.jogTouched.Load() }

// SetJogTouch is called from the engine on platter top-touch press/release.
// In vinyl mode the press captures the current play state and forces playing
// = true so Stream() runs while the user scratches; the release restores
// the captured state. In jog mode it's just a flag bookkeeping operation.
func (d *Deck) SetJogTouch(on bool) {
	if on {
		if !d.jogTouched.Load() && d.vinylMode.Load() {
			d.prevPlayingJog.Store(d.playing.Load())
			d.playing.Store(true)
		}
		d.jogTouched.Store(true)
		return
	}
	wasTouched := d.jogTouched.Swap(false)
	if !wasTouched || !d.vinylMode.Load() {
		return
	}
	d.storeFloat(&d.jogScratchVel, 0)
	if !d.prevPlayingJog.Load() {
		d.Pause()
	}
}

// SetJogScratchSensitivity adjusts the per-tick scratch gain (samples-per-
// output-sample contribution per encoder tick). Default 0.4. Higher = more
// audio movement per platter increment.
func (d *Deck) SetJogScratchSensitivity(g float64) {
	if g <= 0 {
		g = defaultJogScratchSensitivity
	}
	d.storeFloat(&d.jogScratchGainBits, g)
}

// SetJogPitchSensitivity adjusts the per-tick pitch bend gain. Default 0.04.
func (d *Deck) SetJogPitchSensitivity(g float64) {
	if g <= 0 {
		g = defaultJogPitchSensitivity
	}
	d.storeFloat(&d.jogPitchGainBits, g)
}

// AddJogScratchDelta accumulates a scratch velocity contribution from one
// MIDI encoder tick. Uses a CAS loop so concurrent ticks (e.g. fast platter
// movement firing several events between Stream() calls) compose additively
// without locking.
func (d *Deck) AddJogScratchDelta(delta float64) {
	contrib := delta * d.loadFloat(&d.jogScratchGainBits)
	for {
		old := d.jogScratchVel.Load()
		newV := math.Float64frombits(old) + contrib
		if d.jogScratchVel.CompareAndSwap(old, math.Float64bits(newV)) {
			return
		}
	}
}

// AddJogPitchDelta accumulates a pitch-bend tempo offset from one MIDI
// encoder tick. Same CAS pattern as AddJogScratchDelta.
func (d *Deck) AddJogPitchDelta(delta float64) {
	contrib := delta * d.loadFloat(&d.jogPitchGainBits)
	for {
		old := d.jogPitchOffset.Load()
		newV := math.Float64frombits(old) + contrib
		if d.jogPitchOffset.CompareAndSwap(old, math.Float64bits(newV)) {
			return
		}
	}
}
