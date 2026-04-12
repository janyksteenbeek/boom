package audio

import "log"

func (d *Deck) Play() {
	if d.pcm.Load() == nil {
		return
	}
	log.Printf("deck %d: Play()", d.id)
	d.playing.Store(true)
	d.pendingFade.Store(1) // audio thread will trigger fade-in
}

func (d *Deck) Pause() {
	d.pendingFade.Store(2) // audio thread will trigger fade-out
	// playing will be set to false when fade-out completes in Stream()
}

func (d *Deck) TogglePlay() {
	if d.IsPlaying() {
		d.Pause()
	} else {
		d.Play()
	}
}

func (d *Deck) IsPlaying() bool { return d.playing.Load() }

func (d *Deck) SetVolume(v float64) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	d.volume.Set(float32(v))
}

// SetGain sets the trim/gain multiplier (0.0 = mute, 1.0 = unity, 2.0 = +6dB).
func (d *Deck) SetGain(v float32) {
	if v < 0 {
		v = 0
	}
	if v > 2 {
		v = 2
	}
	d.gain.Set(v)
}

func (d *Deck) SetEQHigh(v float64) { d.eq.SetHigh(v) }
func (d *Deck) SetEQMid(v float64)  { d.eq.SetMid(v) }
func (d *Deck) SetEQLow(v float64)  { d.eq.SetLow(v) }

func (d *Deck) SetBeatFXType(t FXType)    { d.beatFX.SetFXType(t) }
func (d *Deck) SetBeatFXActive(on bool)   { d.beatFX.SetActive(on) }
func (d *Deck) SetBeatFXWetDry(v float32) { d.beatFX.SetWetDry(v) }
func (d *Deck) SetBeatFXTime(ms float32)  { d.beatFX.SetTime(ms) }

func (d *Deck) SetTempo(ratio float64) {
	if ratio <= 0 {
		ratio = 0.01
	}
	if ratio > 4 {
		ratio = 4
	}
	d.storeFloat(&d.tempoBits, ratio)
}

func (d *Deck) Tempo() float64 { return d.loadFloat(&d.tempoBits) }

func (d *Deck) Seek(pos float64) {
	if d.pcm.Load() == nil {
		return
	}
	if pos < 0 {
		pos = 0
	}
	if pos > 1 {
		pos = 1
	}
	d.storeFloat(&d.pendingSeek, pos) // audio thread will apply
}

func (d *Deck) Position() float64 {
	return d.loadFloat(&d.posSnapshot)
}

// EffectiveBPM returns the track's BPM adjusted by the current tempo ratio.
// Returns 0 if no track is loaded or the track has no BPM metadata.
func (d *Deck) EffectiveBPM() float64 {
	t := d.track.Load()
	if t == nil || t.BPM <= 0 {
		return 0
	}
	return t.BPM * d.Tempo()
}
