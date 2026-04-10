package audio

import (
	"log"
	"math"
	"runtime"
	"sync/atomic"

	"github.com/gopxl/beep/v2"

	"github.com/janyksteenbeek/boom/internal/event"
	"github.com/janyksteenbeek/boom/pkg/model"
)

// pcmBuffer holds decoded audio samples at the target sample rate.
type pcmBuffer struct {
	samples [][2]float32
	len     int
}

// Deck represents one playback channel.
// Audio is decoded fully upfront into a PCM buffer.
// The audio callback only reads from this buffer — zero allocations, zero I/O.
type Deck struct {
	id         int
	sampleRate int
	bus        *event.Bus

	// PCM buffer — set on LoadTrack, read from audio thread
	pcm atomic.Pointer[pcmBuffer]

	// Playback position — ONLY modified by audio thread in Stream()
	pos  int
	fpos float64 // fractional position accumulator for tempo (drift-free)

	// EQ — lock-free (uses atomic coefficient swap internally)
	eq *ThreeBandEQ

	// Smoothed volume & gain — Set() from any goroutine, Tick() from audio thread
	volume SmoothParam
	gain   SmoothParam // Trim/gain knob: 0.0-2.0 multiplier (1.0 = unity)

	// Fade envelope for click-free play/stop
	fade FadeEnvelope

	// Atomic control values
	playing  atomic.Bool
	tempoBits atomic.Uint64 // float64 as bits, ratio

	track    atomic.Pointer[model.Track]
	waveform atomic.Pointer[WaveformData]
}

func NewDeck(id int, sampleRate int, bus *event.Bus) *Deck {
	d := &Deck{
		id:         id,
		sampleRate: sampleRate,
		bus:        bus,
		eq:         NewThreeBandEQ(sampleRate),
		volume:     NewSmoothParam(0.8, sampleRate, 0.005),
		gain:       NewSmoothParam(1.0, sampleRate, 0.005),
		fade:       NewFadeEnvelope(sampleRate, 0.01),
	}
	d.storeFloat(&d.tempoBits, 1.0)
	return d
}

func (d *Deck) ID() int { return d.id }

// LoadTrack decodes the entire file to a PCM buffer at the target sample rate.
// This runs on the calling goroutine (NOT the audio thread). May take a moment.
func (d *Deck) LoadTrack(track *model.Track) error {
	log.Printf("deck %d: decoding '%s'...", d.id, track.Title)

	src, format, err := Decode(track.Path)
	if err != nil {
		return err
	}

	// Build offline decode chain: source → resample (if needed)
	var streamer beep.Streamer = src
	needsResample := format.SampleRate != beep.SampleRate(d.sampleRate)
	if needsResample {
		log.Printf("deck %d: resampling %d → %d Hz", d.id, format.SampleRate, d.sampleRate)
		streamer = beep.Resample(6, format.SampleRate, beep.SampleRate(d.sampleRate), src)
	}

	// Pre-allocate PCM buffer based on known track length to avoid append/realloc
	estimatedSamples := src.Len()
	if needsResample {
		estimatedSamples = int(float64(estimatedSamples) * float64(d.sampleRate) / float64(format.SampleRate))
	}
	pcm := make([][2]float32, 0, estimatedSamples+8192) // +margin

	// Decode in chunks — beep returns float64, we convert to float32
	buf := make([][2]float64, 8192)
	for {
		n, ok := streamer.Stream(buf)
		if n > 0 {
			for i := 0; i < n; i++ {
				pcm = append(pcm, [2]float32{float32(buf[i][0]), float32(buf[i][1])})
			}
		}
		if !ok {
			break
		}
	}
	src.Close()

	log.Printf("deck %d: decoded %d samples (%.1f sec)", d.id, len(pcm), float64(len(pcm))/float64(d.sampleRate))

	// Force GC now to clean up decode buffers before playback starts
	runtime.GC()

	// Atomic swap — audio thread picks this up on next Stream() call
	d.pcm.Store(&pcmBuffer{samples: pcm, len: len(pcm)})
	d.pos = 0
	d.fpos = 0
	d.playing.Store(false)
	d.track.Store(track)

	// Reset EQ and fade state for new track
	d.eq.Reset()
	d.fade = NewFadeEnvelope(d.sampleRate, 0.01)

	go d.generateWaveform()

	d.bus.PublishAsync(event.Event{
		Topic: event.TopicEngine, Action: event.ActionTrackLoaded,
		DeckID: d.id, Payload: track,
	})
	return nil
}

func (d *Deck) Play() {
	if d.pcm.Load() == nil {
		return
	}
	log.Printf("deck %d: Play()", d.id)
	d.playing.Store(true)
	d.fade.TriggerFadeIn()
}

func (d *Deck) Pause() {
	d.fade.TriggerFadeOut()
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

func (d *Deck) SetTempo(ratio float64) {
	if ratio <= 0 {
		ratio = 0.01
	}
	if ratio > 4 {
		ratio = 4
	}
	d.storeFloat(&d.tempoBits, ratio)
}

func (d *Deck) Seek(pos float64) {
	p := d.pcm.Load()
	if p == nil {
		return
	}
	newPos := int(pos * float64(p.len))
	if newPos < 0 {
		newPos = 0
	}
	if newPos >= p.len {
		newPos = p.len - 1
	}
	d.pos = newPos
	d.fpos = float64(newPos)
}

func (d *Deck) Position() float64 {
	p := d.pcm.Load()
	if p == nil || p.len == 0 {
		return 0
	}
	return float64(d.pos) / float64(p.len)
}

func (d *Deck) Track() *model.Track     { return d.track.Load() }
func (d *Deck) Waveform() *WaveformData { return d.waveform.Load() }
func (d *Deck) SampleRate() int          { return d.sampleRate }

// Stream fills the output buffer with audio samples.
// Called ONLY from the malgo audio callback thread.
// Zero allocations, zero I/O, zero locks — just array reads and math.
func (d *Deck) Stream(samples [][2]float32) {
	p := d.pcm.Load()
	if p == nil {
		for i := range samples {
			samples[i] = [2]float32{}
		}
		return
	}

	// If not playing and fade is silent, zero-fill
	if !d.playing.Load() && d.fade.State() == FadeSilent {
		for i := range samples {
			samples[i] = [2]float32{}
		}
		return
	}

	tempo := d.loadFloat(&d.tempoBits)
	n := len(samples)

	if tempo == 1.0 {
		// Fast path: no tempo change, just copy
		remaining := p.len - d.pos
		if remaining <= 0 {
			for i := range samples {
				samples[i] = [2]float32{}
			}
			d.playing.Store(false)
			d.fade = NewFadeEnvelope(d.sampleRate, 0.01)
			return
		}
		count := n
		if count > remaining {
			count = remaining
		}
		copy(samples[:count], p.samples[d.pos:d.pos+count])
		d.pos += count
		d.fpos = float64(d.pos)
		// Zero fill remainder
		for i := count; i < n; i++ {
			samples[i] = [2]float32{}
		}
	} else {
		// Tempo: linear interpolation with float64 accumulator for drift-free tracking
		d.fpos = float64(d.pos)
		for i := 0; i < n; i++ {
			idx := int(d.fpos)
			if idx >= p.len-1 {
				samples[i] = [2]float32{}
				continue
			}
			frac := float32(d.fpos - float64(idx))
			samples[i][0] = p.samples[idx][0]*(1-frac) + p.samples[idx+1][0]*frac
			samples[i][1] = p.samples[idx][1]*(1-frac) + p.samples[idx+1][1]*frac
			d.fpos += tempo
		}
		d.pos = int(d.fpos)
		if d.pos >= p.len {
			d.playing.Store(false)
			d.fade = NewFadeEnvelope(d.sampleRate, 0.01)
		}
	}

	// Apply EQ in-place (lock-free biquad filters)
	d.eq.ProcessBuffer(samples, n)

	// Apply gain and volume in a single pass (biquad filters are linear, so
	// gain*EQ(x) == EQ(gain*x) — safe to combine post-EQ)
	for i := 0; i < n; i++ {
		gv := d.gain.Tick() * d.volume.Tick()
		samples[i][0] *= gv
		samples[i][1] *= gv
	}

	// Apply fade envelope for click-free start/stop
	state := d.fade.Process(samples)
	if state == FadeSilent && !d.playing.Load() {
		// Fade-out complete — already paused
	} else if state == FadeSilent {
		d.playing.Store(false)
	}
}

func (d *Deck) generateWaveform() {
	p := d.pcm.Load()
	if p == nil {
		return
	}
	data := GenerateWaveformFromPCM(p.samples, d.sampleRate)
	d.waveform.Store(data)
	d.bus.PublishAsync(event.Event{
		Topic: event.TopicEngine, Action: event.ActionWaveformReady,
		DeckID: d.id, Payload: data,
	})
}

func (d *Deck) storeFloat(a *atomic.Uint64, v float64) {
	a.Store(math.Float64bits(v))
}

func (d *Deck) loadFloat(a *atomic.Uint64) float64 {
	return math.Float64frombits(a.Load())
}
