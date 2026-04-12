package audio

import (
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/janyksteenbeek/boom/internal/audio/output"
	"github.com/janyksteenbeek/boom/internal/event"
)

const (
	NumDecks               = 2
	positionUpdateInterval = 16 * time.Millisecond

	// feederBlockFrames is the number of frames the engine generates per
	// mixer tick. Small enough that the producer comfortably stays ahead
	// of any sane hardware buffer (default 256–2048 frames), big enough
	// that the per-tick smoothing/EQ overhead doesn't dominate.
	feederBlockFrames = 512
)

// LoopOptions mirrors config.LoopSettings so the audio package can honor
// the user's loop preferences without importing the config package.
type LoopOptions struct {
	Quantize        bool
	DefaultBeatLoop float64
	MinBeats        float64
	MaxBeats        float64
	SmartLoop       bool
}

// JogOptions mirrors config.JogSettings — same reason.
type JogOptions struct {
	VinylMode          bool
	ScratchSensitivity float64
	PitchSensitivity   float64
}

type Engine struct {
	bus        *event.Bus
	decks      [NumDecks]*Deck
	master     *MasterMixer
	sampleRate int

	stopOnce sync.Once
	stopCh   chan struct{}
	feederWG sync.WaitGroup

	autoCue atomic.Bool // auto-cue on load (fallback cue = first audio frame)
	loopOpt atomic.Pointer[LoopOptions]

	backend      output.Backend
	masterStream output.Stream
	cueStream    output.Stream // nil when no cue device configured
}

// SetAutoCue toggles auto-cue-on-load. When enabled, tracks without a saved
// manual cue seek to the first audible sample rather than sample 0.
func (e *Engine) SetAutoCue(v bool) { e.autoCue.Store(v) }

// SetLoopOptions publishes a snapshot of the loop preferences. Called on
// startup and whenever the user saves the settings dialog.
func (e *Engine) SetLoopOptions(opts LoopOptions) {
	if opts.MinBeats <= 0 {
		opts.MinBeats = 1.0 / 32.0
	}
	if opts.MaxBeats <= 0 {
		opts.MaxBeats = 32
	}
	if opts.DefaultBeatLoop <= 0 {
		opts.DefaultBeatLoop = 4
	}
	e.loopOpt.Store(&opts)
}

// SetJogOptions pushes vinyl mode + scratch/pitch sensitivities to all decks.
func (e *Engine) SetJogOptions(opts JogOptions) {
	for _, d := range e.decks {
		d.SetVinylMode(opts.VinylMode)
		d.SetJogScratchSensitivity(opts.ScratchSensitivity)
		d.SetJogPitchSensitivity(opts.PitchSensitivity)
	}
}

func (e *Engine) loopOptions() LoopOptions {
	if opt := e.loopOpt.Load(); opt != nil {
		return *opt
	}
	return LoopOptions{
		Quantize:        true,
		DefaultBeatLoop: 4,
		MinBeats:        1.0 / 32.0,
		MaxBeats:        32,
		SmartLoop:       true,
	}
}

// NewEngine builds the audio engine and opens the output streams.
//
// outputDevice is the master/main output device (Device.ID, "" = default).
// cueDevice is the optional headphone/cue device (Device.ID, "" = none).
// When cueDevice is non-empty, a second output.Stream is opened on that
// device and the mixer produces a parallel cue mix on every tick. The
// cue stream is non-blocking so it cannot starve the master output if
// the two devices have slightly different clocks.
func NewEngine(bus *event.Bus, sampleRate int, bufferSize int, outputDevice, cueDevice string, wfCache WaveformCache) (*Engine, error) {
	log.Printf("audio: initializing engine at %d Hz, buffer %d", sampleRate, bufferSize)

	backend, err := output.New()
	if err != nil {
		return nil, fmt.Errorf("audio backend: %w", err)
	}

	e := &Engine{
		bus:        bus,
		sampleRate: sampleRate,
		stopCh:     make(chan struct{}),
		backend:    backend,
	}

	deckSlice := make([]*Deck, NumDecks)
	for i := range e.decks {
		e.decks[i] = NewDeck(i+1, sampleRate, bus, wfCache)
		deckSlice[i] = e.decks[i]
	}
	e.master = NewMasterMixer(deckSlice, sampleRate)

	masterStream, err := openWithFallback(backend, "master", outputDevice, output.StreamConfig{
		SampleRate:   sampleRate,
		BufferFrames: bufferSize,
		NumChannels:  2,
		BlockOnFull:  true, // master paces the producer
	})
	if err != nil {
		_ = backend.Close()
		return nil, fmt.Errorf("open master output: %w", err)
	}
	e.masterStream = masterStream
	log.Printf("audio: master stream opened (%d Hz, %d ch, buf %d frames)",
		masterStream.SampleRate(), masterStream.NumChannels(), bufferSize)

	if cueDevice != "" {
		cueStream, err := openWithFallback(backend, "cue", cueDevice, output.StreamConfig{
			SampleRate:   sampleRate,
			BufferFrames: bufferSize,
			NumChannels:  2,
			BlockOnFull:  false, // cue is best-effort; never blocks master
		})
		if err != nil {
			log.Printf("audio: cue output unavailable, continuing without it: %v", err)
		} else {
			e.cueStream = cueStream
			log.Printf("audio: cue stream opened (%d Hz, %d ch)",
				cueStream.SampleRate(), cueStream.NumChannels())
		}
	}

	e.feederWG.Add(1)
	go e.feederLoop()

	e.subscribeEvents()
	go e.positionUpdateLoop()
	return e, nil
}

// feederLoop is the engine's producer goroutine. It generates audio in
// fixed-size blocks, runs the mixer once per block to fill master and
// (optionally) cue buffers, and writes the result to the output streams.
// The master stream is opened with BlockOnFull=true, so its Write call
// is the natural pacer — when the master ring is full the goroutine
// sleeps inside output.Write until the audio thread drains it.
func (e *Engine) feederLoop() {
	defer e.feederWG.Done()

	const block = feederBlockFrames
	masterBuf := make([][2]float32, block)
	cueBuf := make([][2]float32, block)
	flatMaster := make([]float32, block*2)
	flatCue := make([]float32, block*2)

	for {
		select {
		case <-e.stopCh:
			return
		default:
		}

		// StreamPair overwrites both buffers — no pre-clear needed.
		if e.cueStream != nil {
			e.master.StreamPair(masterBuf, cueBuf)
		} else {
			e.master.Stream(masterBuf)
		}

		for i, fr := range masterBuf {
			flatMaster[i*2] = clampF32(fr[0])
			flatMaster[i*2+1] = clampF32(fr[1])
		}
		if _, err := e.masterStream.Write(flatMaster); err != nil {
			if err == output.ErrStreamClosed {
				return
			}
			log.Printf("audio: master write: %v", err)
			return
		}

		if e.cueStream != nil {
			for i, fr := range cueBuf {
				flatCue[i*2] = clampF32(fr[0])
				flatCue[i*2+1] = clampF32(fr[1])
			}
			if _, err := e.cueStream.Write(flatCue); err != nil && err != output.ErrStreamClosed {
				log.Printf("audio: cue write: %v", err)
			}
		}
	}
}

func (e *Engine) Deck(id int) *Deck {
	if id < 1 || id > NumDecks {
		return nil
	}
	return e.decks[id-1]
}

func (e *Engine) Stop() {
	e.stopOnce.Do(func() {
		close(e.stopCh)
		if e.masterStream != nil {
			_ = e.masterStream.Close()
		}
		if e.cueStream != nil {
			_ = e.cueStream.Close()
		}
		// Wait for the feeder to exit before tearing down the backend so
		// it can't try to Write into a freed C stream.
		e.feederWG.Wait()
		if e.backend != nil {
			_ = e.backend.Close()
		}
	})
}

func (e *Engine) positionUpdateLoop() {
	ticker := time.NewTicker(positionUpdateInterval)
	defer ticker.Stop()
	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			for _, d := range e.decks {
				if d.IsPlaying() {
					e.bus.PublishAsync(event.Event{
						Topic: event.TopicEngine, Action: event.ActionPositionUpdate,
						DeckID: d.ID(), Value: d.Position(),
					})
				}
			}
		}
	}
}

// openWithFallback tries to open a named device, then resolves the
// configured ID against the current device list (so a stale config
// holding the old name-based format still finds the right device), and
// finally falls back to the system default. The two intermediate steps
// are about config compatibility: pre-miniaudio Boom persisted device
// names; the new config persists hex-encoded ma_device_id blobs. Either
// representation should keep playback working.
func openWithFallback(backend output.Backend, label, configID string, base output.StreamConfig) (output.Stream, error) {
	cfg := base
	cfg.DeviceID = configID
	if stream, err := backend.OpenStream(cfg); err == nil {
		return stream, nil
	} else if configID == "" {
		// No configured ID and we still failed — surface the original error.
		return nil, err
	} else {
		log.Printf("audio: %s device %q not opened directly (%v); resolving by name", label, configID, err)
	}

	// Maybe the config holds a legacy device name. Match it against the
	// current device list and retry with the canonical ID.
	if devs, derr := backend.ListDevices(); derr == nil {
		for _, d := range devs {
			if d.Name == configID {
				cfg.DeviceID = d.ID
				if stream, err := backend.OpenStream(cfg); err == nil {
					log.Printf("audio: %s device matched %q by name → %s",
						label, configID, shortID(d.ID))
					return stream, nil
				}
			}
		}
	}

	// Last resort: open the system default and warn loudly so the user
	// notices their selection didn't take effect.
	log.Printf("audio: %s device %q unavailable, falling back to system default", label, configID)
	cfg.DeviceID = ""
	return backend.OpenStream(cfg)
}

func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12] + "…"
}

func clampF32(v float32) float32 {
	if v > 1.0 {
		return 1.0
	}
	if v < -1.0 {
		return -1.0
	}
	if v != v {
		return 0
	}
	return v
}

func boolToFloat(b bool) float64 {
	if b {
		return 1.0
	}
	return 0.0
}
