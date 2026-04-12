package audio

import (
	"log"
	"math"
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

// Jog wheel decay tunables. Halflives are kept as constants — they affect
// the *feel* of platter inertia and pitch bend rebound and rarely need user
// tuning. The per-tick gains are exposed as per-deck atomics so the engine
// can apply user settings.
const (
	jogScratchHalflifeMs = 100.0 // platter inertia decay
	jogPitchHalflifeMs   = 220.0 // pitch bend rubber-band

	defaultJogScratchSensitivity = 0.4 // dimensionless gain (matches tempo units)
	defaultJogPitchSensitivity   = 0.04
)

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

	// Beat FX — lock-free effect processor (echo/flanger/reverb)
	beatFX *BeatFX

	// Smoothed volume & gain — Set() from any goroutine, Tick() from audio thread
	volume SmoothParam
	gain   SmoothParam // Trim/gain knob: 0.0-2.0 multiplier (1.0 = unity)

	// Fade envelope for click-free play/stop — ONLY accessed from audio thread
	fade FadeEnvelope

	// Atomic control values
	playing   atomic.Bool
	tempoBits atomic.Uint64 // float64 as bits, ratio

	// Cue point state — written by UI/MIDI threads via the engine.
	// cuePoint holds the manual cue (persisted to DB); negative = unset.
	// fallbackCue is the auto-cue / track-start position computed on load
	// and is always a valid value (0..1).
	cuePoint    atomic.Uint64 // float64 bits, normalized 0..1; negative = unset
	fallbackCue atomic.Uint64 // float64 bits, 0..1
	cueHeld     atomic.Bool   // CUE button currently held down
	cuePreview  atomic.Bool   // playing because of CUE-hold preview (release → snap back)

	// Loop state. Positions are normalized 0..1 stored as float64 bits.
	// A NaN in loopStart/loopEnd means "unset". loopActive controls whether
	// Stream() wraps playback between the points. loopBeats is the beat
	// length used for halve/double math (0 = manual loop).
	loopStart  atomic.Uint64
	loopEnd    atomic.Uint64
	loopBeats  atomic.Uint64
	loopActive atomic.Bool

	// Jog wheel state — written by engine, consumed by Stream().
	// vinylMode true = top touch enables scratch; false = all rotation is pitch bend.
	// jogScratchVel is samples-per-output-sample (signed, decays each block).
	// jogPitchOffset is an additive tempo delta (signed, decays each block).
	// prevPlayingJog snapshots playing state on touch-down so release can restore it.
	vinylMode          atomic.Bool
	jogTouched         atomic.Bool
	jogScratchVel      atomic.Uint64
	jogPitchOffset     atomic.Uint64
	jogScratchGainBits atomic.Uint64 // float64 bits, samples-per-sample per tick
	jogPitchGainBits   atomic.Uint64 // float64 bits, tempo delta per tick
	prevPlayingJog     atomic.Bool

	// Pending commands from non-audio threads (written by UI/MIDI, consumed by audio thread)
	pendingSeek  atomic.Uint64 // float64 bits; NaN = no pending seek
	pendingFade  atomic.Int32  // 0=none, 1=fade-in, 2=fade-out
	pendingReset atomic.Bool   // set by LoadTrack, audio thread resets all state

	// Position snapshot for UI (written by audio thread, read by UI/position loop)
	posSnapshot atomic.Uint64 // float64 bits, fractional position 0.0-1.0

	track    atomic.Pointer[model.Track]
	waveform atomic.Pointer[WaveformData]
}

func NewDeck(id int, sampleRate int, bus *event.Bus) *Deck {
	d := &Deck{
		id:         id,
		sampleRate: sampleRate,
		bus:        bus,
		eq:         NewThreeBandEQ(sampleRate),
		beatFX:     NewBeatFX(sampleRate),
		fade:       NewFadeEnvelope(sampleRate, 0.01),
	}
	d.volume.Init(0.8, sampleRate, 0.005)
	d.gain.Init(1.0, sampleRate, 0.005)
	d.storeFloat(&d.tempoBits, 1.0)
	d.storeFloat(&d.pendingSeek, math.NaN()) // sentinel: no pending seek
	d.storeFloat(&d.cuePoint, -1)            // sentinel: no cue set
	d.storeFloat(&d.loopStart, math.NaN())
	d.storeFloat(&d.loopEnd, math.NaN())
	d.storeFloat(&d.loopBeats, 0)
	d.storeFloat(&d.jogScratchVel, 0)
	d.storeFloat(&d.jogPitchOffset, 0)
	d.storeFloat(&d.jogScratchGainBits, defaultJogScratchSensitivity)
	d.storeFloat(&d.jogPitchGainBits, defaultJogPitchSensitivity)
	d.vinylMode.Store(true) // default: classic CDJ scratch behavior
	return d
}

func (d *Deck) ID() int                 { return d.id }
func (d *Deck) SampleRate() int          { return d.sampleRate }
func (d *Deck) Track() *model.Track     { return d.track.Load() }
func (d *Deck) Waveform() *WaveformData { return d.waveform.Load() }

// PCMSamples returns a read-only reference to the decoded PCM buffer.
// The returned slice must not be modified. Returns nil if no track is loaded.
func (d *Deck) PCMSamples() [][2]float32 {
	p := d.pcm.Load()
	if p == nil {
		return nil
	}
	return p.samples[:p.len]
}

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
		streamer = beep.Resample(4, format.SampleRate, beep.SampleRate(d.sampleRate), src)
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

	// Stop playback BEFORE swapping the PCM buffer so the callback never
	// reads the new buffer at the old position (race window elimination).
	d.playing.Store(false)
	d.pendingFade.Store(0)                   // clear any pending fade
	d.storeFloat(&d.pendingSeek, math.NaN()) // clear any pending seek
	d.cueHeld.Store(false)
	d.cuePreview.Store(false)
	// Discard any loop from the previous track — loop points are per-session
	// and anchored to absolute sample indices of a specific PCM buffer.
	d.loopActive.Store(false)
	d.storeFloat(&d.loopStart, math.NaN())
	d.storeFloat(&d.loopEnd, math.NaN())
	d.storeFloat(&d.loopBeats, 0)
	// cuePoint is set by the engine after LoadTrack returns (from track.CuePoint).
	d.pcm.Store(&pcmBuffer{samples: pcm, len: len(pcm)}) // swap buffer
	d.track.Store(track)
	d.pendingReset.Store(true) // signal audio thread to reset pos/fade/EQ

	go d.generateWaveform()

	d.bus.PublishAsync(event.Event{
		Topic: event.TopicEngine, Action: event.ActionTrackLoaded,
		DeckID: d.id, Payload: track,
	})
	return nil
}

func (d *Deck) storeFloat(a *atomic.Uint64, v float64) {
	a.Store(math.Float64bits(v))
}

func (d *Deck) loadFloat(a *atomic.Uint64) float64 {
	return math.Float64frombits(a.Load())
}
