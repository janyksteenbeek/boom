package audio

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/janyksteenbeek/boom/internal/audio/output"
	"github.com/janyksteenbeek/boom/internal/event"
	"github.com/janyksteenbeek/boom/pkg/model"
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

// NewEngine builds the audio engine and opens the output streams.
//
// outputDevice is the master/main output device (Device.ID, "" = default).
// cueDevice is the optional headphone/cue device (Device.ID, "" = none).
// When cueDevice is non-empty, a second output.Stream is opened on that
// device and the mixer produces a parallel cue mix on every tick. The
// cue stream is non-blocking so it cannot starve the master output if
// the two devices have slightly different clocks.
func NewEngine(bus *event.Bus, sampleRate int, bufferSize int, outputDevice, cueDevice string) (*Engine, error) {
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
		e.decks[i] = NewDeck(i+1, sampleRate, bus)
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

func (e *Engine) subscribeEvents() {
	e.bus.Subscribe(event.TopicDeck, func(ev event.Event) error {
		// Temporary trace: every deck event that reaches the engine. Loop-
		// related issues should show up as missing log lines.
		if ev.Action == event.ActionLoopIn || ev.Action == event.ActionLoopOut ||
			ev.Action == event.ActionLoopToggle || ev.Action == event.ActionLoopHalve ||
			ev.Action == event.ActionLoopDouble || ev.Action == event.ActionBeatLoop {
			log.Printf("engine RX: action=%s deckID=%d pressed=%v value=%v",
				ev.Action, ev.DeckID, ev.Pressed, ev.Value)
		}
		if ev.DeckID < 1 || ev.DeckID > NumDecks {
			return nil
		}
		deck := e.decks[ev.DeckID-1]

		switch ev.Action {
		case event.ActionPlayPause:
			// CUE+PLAY = cue release latch: kap de preview-snapback en forceer playing
			if deck.CueHeld() {
				deck.SetCuePreview(false)
				if !deck.IsPlaying() {
					deck.Play()
				}
				e.bus.PublishAsync(event.Event{
					Topic: event.TopicEngine, Action: event.ActionPlayState,
					DeckID: ev.DeckID, Value: 1.0,
				})
			} else {
				deck.TogglePlay()
				e.bus.PublishAsync(event.Event{
					Topic: event.TopicEngine, Action: event.ActionPlayState,
					DeckID: ev.DeckID, Value: boolToFloat(deck.IsPlaying()),
				})
			}

		case event.ActionPlay:
			if deck.CueHeld() {
				deck.SetCuePreview(false) // latch
			}
			deck.Play()
			e.bus.PublishAsync(event.Event{
				Topic: event.TopicEngine, Action: event.ActionPlayState,
				DeckID: ev.DeckID, Value: 1.0,
			})

		case event.ActionPause:
			deck.Pause()
			e.bus.PublishAsync(event.Event{
				Topic: event.TopicEngine, Action: event.ActionPlayState,
				DeckID: ev.DeckID, Value: 0.0,
			})

		case event.ActionCue:
			// Rekordbox cue behavior:
			// - Playing + press → back-cue: seek to effective cue, enter cue_preview.
			// - Paused  + press → set manual cue at current playhead, enter cue_preview.
			// - Release while in preview → pause + seek back to cue.
			if deck.Track() == nil {
				return nil
			}
			if ev.Pressed {
				deck.SetCueHeld(true)
				if deck.IsPlaying() {
					// Back-cue: jump to effective cue, keep audio running as preview.
					deck.Seek(deck.EffectiveCue())
					deck.SetCuePreview(true)
				} else {
					// Paused: SET cue at playhead (replaces any previous manual cue),
					// then start preview playback from that point.
					newPos := deck.Position()
					deck.SetCuePoint(newPos)
					e.publishCuePointChanged(deck, newPos)
					deck.Play()
					deck.SetCuePreview(true)
					e.bus.PublishAsync(event.Event{
						Topic: event.TopicEngine, Action: event.ActionPlayState,
						DeckID: ev.DeckID, Value: 1.0,
					})
				}
			} else {
				deck.SetCueHeld(false)
				if deck.CuePreview() {
					deck.Pause()
					deck.Seek(deck.EffectiveCue())
					deck.SetCuePreview(false)
					e.bus.PublishAsync(event.Event{
						Topic: event.TopicEngine, Action: event.ActionPlayState,
						DeckID: ev.DeckID, Value: 0.0,
					})
				}
			}

		case event.ActionCueDelete:
			deck.ClearCue()
			e.publishCuePointChanged(deck, -1)

		case event.ActionCueGoStart:
			// SHIFT + CUE: jump to track start, paused. Does not touch the cue point.
			if deck.Track() == nil {
				return nil
			}
			deck.Pause()
			deck.Seek(0)
			deck.SetCuePreview(false)
			e.bus.PublishAsync(event.Event{
				Topic: event.TopicEngine, Action: event.ActionPlayState,
				DeckID: ev.DeckID, Value: 0.0,
			})

		case event.ActionSeek:
			deck.Seek(ev.Value)

		case event.ActionVolumeChange:
			deck.SetVolume(ev.Value)

		case event.ActionGainChange:
			deck.SetGain(float32(ev.Value * 2.0))

		case event.ActionEQHigh:
			deck.SetEQHigh(ev.Value)
		case event.ActionEQMid:
			deck.SetEQMid(ev.Value)
		case event.ActionEQLow:
			deck.SetEQLow(ev.Value)

		case event.ActionTempoChange:
			ratio := 0.5 + ev.Value*1.5
			deck.SetTempo(ratio)

		case event.ActionSync:
			otherID := 2
			if ev.DeckID == 2 {
				otherID = 1
			}
			other := e.decks[otherID-1]
			deckTrack := deck.Track()
			otherTrack := other.Track()
			if deckTrack == nil || otherTrack == nil || deckTrack.BPM <= 0 || otherTrack.BPM <= 0 {
				log.Printf("engine: sync deck %d — missing BPM data", ev.DeckID)
				return nil
			}
			otherBPM := other.EffectiveBPM()
			newRatio := otherBPM / deckTrack.BPM
			deck.SetTempo(newRatio)
			log.Printf("engine: sync deck %d → %.1f BPM (matched deck %d at %.1f BPM, ratio %.3f)",
				ev.DeckID, deckTrack.BPM*newRatio, otherID, otherBPM, newRatio)

		case event.ActionLoopIn:
			if deck.Track() == nil {
				return nil
			}
			pos := deck.Position()
			if e.loopOptions().Quantize {
				if snapped := deck.NearestBeatBefore(pos); snapped > 0 {
					pos = snapped
				}
			}
			deck.SetLoopStart(pos)
			e.publishLoopState(deck)

		case event.ActionLoopOut:
			if deck.Track() == nil {
				return nil
			}
			start := deck.LoopStart()
			if math.IsNaN(start) {
				return nil
			}
			end := deck.Position()
			if e.loopOptions().Quantize {
				if snapped := deck.NearestBeatBefore(end); snapped > start {
					end = snapped
				}
			}
			if end <= start {
				return nil
			}
			beats := e.samplesToBeats(deck, start, end)
			deck.SetManualLoop(start, end, beats)
			e.publishLoopState(deck)

		case event.ActionLoopToggle:
			// DDJ-FLX4 "4 Beat / Exit Loop" button: while a loop is active
			// this exits AND clears the stored boundaries, so the next press
			// always starts a fresh auto beat loop from the current playhead.
			if deck.Track() == nil {
				return nil
			}
			if deck.IsLoopActive() || deck.HasLoop() {
				deck.ClearLoop()
				e.publishLoopState(deck)
				return nil
			}
			opts := e.loopOptions()
			if e.applyBeatLoop(deck, opts.DefaultBeatLoop) {
				e.publishLoopState(deck)
			}

		case event.ActionLoopHalve:
			log.Printf("engine: loop_halve deck=%d hasLoop=%v beats=%.4f start=%.4f end=%.4f",
				ev.DeckID, deck.HasLoop(), deck.LoopBeats(), deck.LoopStart(), deck.LoopEnd())
			if !deck.HasLoop() {
				return nil
			}
			opts := e.loopOptions()
			beats := deck.LoopBeats()
			if beats <= 0 {
				beats = e.samplesToBeats(deck, deck.LoopStart(), deck.LoopEnd())
			}
			beats = beats / 2
			if beats < opts.MinBeats {
				beats = opts.MinBeats
			}
			ok := e.resizeLoopBeats(deck, beats)
			log.Printf("engine: loop_halve → newBeats=%.4f resized=%v", beats, ok)
			if ok {
				e.publishLoopState(deck)
			}

		case event.ActionLoopDouble:
			log.Printf("engine: loop_double deck=%d hasLoop=%v beats=%.4f",
				ev.DeckID, deck.HasLoop(), deck.LoopBeats())
			if !deck.HasLoop() {
				return nil
			}
			opts := e.loopOptions()
			beats := deck.LoopBeats()
			if beats <= 0 {
				beats = e.samplesToBeats(deck, deck.LoopStart(), deck.LoopEnd())
			}
			beats = beats * 2
			if beats > opts.MaxBeats {
				beats = opts.MaxBeats
			}
			ok := e.resizeLoopBeats(deck, beats)
			log.Printf("engine: loop_double → newBeats=%.4f resized=%v", beats, ok)
			if ok {
				e.publishLoopState(deck)
			}

		case event.ActionBeatLoop:
			if deck.Track() == nil || ev.Value <= 0 {
				return nil
			}
			if e.applyBeatLoop(deck, ev.Value) {
				e.publishLoopState(deck)
			}

		case event.ActionLoadTrack:
			track, ok := ev.Payload.(*model.Track)
			if !ok {
				return fmt.Errorf("expected *model.Track")
			}
			deckID := ev.DeckID
			go func() {
				if err := deck.LoadTrack(track); err != nil {
					log.Printf("engine: load failed deck %d: %v", deckID, err)
					return
				}
				// Fallback cue = first audio frame if auto-cue is on, else 0.
				var fallback float64
				if e.autoCue.Load() {
					fallback = deck.FirstAudioFrame()
				}
				deck.SetFallbackCue(fallback)
				// Restore manual cue from DB (if saved).
				if track.HasCuePoint() {
					deck.SetCuePoint(track.CuePoint)
				} else {
					deck.ClearCue()
				}
				e.publishCuePointChanged(deck, deck.CuePoint())
				e.publishLoopState(deck)
				// Seek to effective cue, stay paused (no auto-play).
				deck.Seek(deck.EffectiveCue())
				e.bus.PublishAsync(event.Event{
					Topic: event.TopicEngine, Action: event.ActionPlayState,
					DeckID: deckID, Value: 0.0,
				})
			}()
		}
		return nil
	})

	// Analysis results → refresh the deck's cached Track so loop/sync
	// features pick up the new BPM and beat grid without needing a reload.
	e.bus.Subscribe(event.TopicAnalysis, func(ev event.Event) error {
		if ev.Action != event.ActionAnalyzeComplete {
			return nil
		}
		res, ok := ev.Payload.(*event.AnalysisResult)
		if !ok || res == nil {
			return nil
		}
		beatGridJSON := ""
		if len(res.BeatGrid) > 0 {
			if data, mErr := json.Marshal(res.BeatGrid); mErr == nil {
				beatGridJSON = string(data)
			}
		}
		for _, d := range e.decks {
			if d.UpdateTrackAnalysis(res.TrackID, res.BPM, res.Key, beatGridJSON) {
				log.Printf("engine: deck %d track analysis applied (bpm=%.2f)", d.ID(), res.BPM)
			}
		}
		return nil
	})

	e.bus.Subscribe(event.TopicMixer, func(ev event.Event) error {
		switch ev.Action {
		case event.ActionCrossfader:
			e.master.SetCrossfader(ev.Value)
		case event.ActionMasterVolume:
			e.master.SetMasterVolume(ev.Value)
		case event.ActionCueVolume:
			e.master.SetCueVolume(ev.Value)
		}
		return nil
	})

	// Beat FX — route to deck or master based on DeckID (0=master, 1/2=deck)
	e.bus.Subscribe(event.TopicDeck, func(ev event.Event) error {
		switch ev.Action {
		case event.ActionFXSelect:
			fxType := FXType(int32(ev.Value))
			if ev.DeckID == 0 {
				e.master.SetBeatFXType(fxType)
			} else if ev.DeckID >= 1 && ev.DeckID <= NumDecks {
				e.decks[ev.DeckID-1].SetBeatFXType(fxType)
			}
		case event.ActionFXActivate:
			on := ev.Value > 0.5
			if ev.DeckID == 0 {
				e.master.SetBeatFXActive(on)
			} else if ev.DeckID >= 1 && ev.DeckID <= NumDecks {
				e.decks[ev.DeckID-1].SetBeatFXActive(on)
			}
		case event.ActionFXWetDry:
			v := float32(ev.Value)
			if ev.DeckID == 0 {
				e.master.SetBeatFXWetDry(v)
			} else if ev.DeckID >= 1 && ev.DeckID <= NumDecks {
				e.decks[ev.DeckID-1].SetBeatFXWetDry(v)
			}
		case event.ActionFXTime:
			// Map 0.0–1.0 to 50–2000ms range
			ms := float32(50 + ev.Value*1950)
			if ev.DeckID == 0 {
				e.master.SetBeatFXTime(ms)
			} else if ev.DeckID >= 1 && ev.DeckID <= NumDecks {
				e.decks[ev.DeckID-1].SetBeatFXTime(ms)
			}
		}
		return nil
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

// applyBeatLoop creates a beat-length loop of `beats` starting at the nearest
// beat before the current playhead. Returns true if the loop was applied.
func (e *Engine) applyBeatLoop(deck *Deck, beats float64) bool {
	pcmLen := len(deck.PCMSamples())
	if pcmLen == 0 {
		return false
	}
	spb := deck.SamplesPerBeat()
	if spb <= 0 {
		log.Printf("engine: beat loop deck %d — no BPM metadata", deck.ID())
		return false
	}
	opts := e.loopOptions()
	if beats < opts.MinBeats {
		beats = opts.MinBeats
	}
	if beats > opts.MaxBeats {
		beats = opts.MaxBeats
	}
	start := deck.NearestBeatBefore(deck.Position())
	if start < 0 {
		start = 0
	}
	startSamples := start * float64(pcmLen)
	endSamples := startSamples + beats*spb
	if endSamples > float64(pcmLen) {
		if !opts.SmartLoop {
			return false
		}
		endSamples = float64(pcmLen)
		// Recompute beats from clamped length so halve/double stay sensible.
		beats = (endSamples - startSamples) / spb
		if beats <= 0 {
			return false
		}
	}
	end := endSamples / float64(pcmLen)
	deck.SetManualLoop(start, end, beats)
	return true
}

// resizeLoopBeats keeps the current loop start but recomputes the end
// position for the new beat length. Returns true when the loop was updated.
func (e *Engine) resizeLoopBeats(deck *Deck, beats float64) bool {
	pcmLen := len(deck.PCMSamples())
	if pcmLen == 0 {
		return false
	}
	spb := deck.SamplesPerBeat()
	if spb <= 0 || beats <= 0 {
		return false
	}
	start := deck.LoopStart()
	if math.IsNaN(start) {
		return false
	}
	startSamples := start * float64(pcmLen)
	endSamples := startSamples + beats*spb
	opts := e.loopOptions()
	if endSamples > float64(pcmLen) {
		if !opts.SmartLoop {
			return false
		}
		endSamples = float64(pcmLen)
		beats = (endSamples - startSamples) / spb
		if beats <= 0 {
			return false
		}
	}
	end := endSamples / float64(pcmLen)
	// Keep wrapping active if it already was.
	wasActive := deck.IsLoopActive()
	deck.SetManualLoop(start, end, beats)
	if !wasActive {
		deck.SetLoopActive(false)
	}
	return true
}

// samplesToBeats converts a normalized (start,end) pair into a beat count.
func (e *Engine) samplesToBeats(deck *Deck, start, end float64) float64 {
	pcmLen := len(deck.PCMSamples())
	if pcmLen == 0 {
		return 0
	}
	spb := deck.SamplesPerBeat()
	if spb <= 0 {
		return 0
	}
	return (end - start) * float64(pcmLen) / spb
}

// publishLoopState broadcasts the deck's current loop configuration so the
// UI (waveform overlay, loop bar) can reflect it.
func (e *Engine) publishLoopState(deck *Deck) {
	start := deck.LoopStart()
	end := deck.LoopEnd()
	if math.IsNaN(start) {
		start = -1
	}
	if math.IsNaN(end) {
		end = -1
	}
	state := &event.LoopState{
		Start:  start,
		End:    end,
		Beats:  deck.LoopBeats(),
		Active: deck.IsLoopActive(),
	}
	e.bus.PublishAsync(event.Event{
		Topic:   event.TopicEngine,
		Action:  event.ActionLoopStateUpdate,
		DeckID:  deck.ID(),
		Payload: state,
	})
}

// publishCuePointChanged broadcasts a cue point update so the UI can refresh
// the marker and the app layer can persist it to the database.
func (e *Engine) publishCuePointChanged(deck *Deck, pos float64) {
	var trackID string
	if t := deck.Track(); t != nil {
		trackID = t.ID
	}
	e.bus.PublishAsync(event.Event{
		Topic:   event.TopicEngine,
		Action:  event.ActionCuePointChanged,
		DeckID:  deck.ID(),
		Value:   pos,
		Payload: trackID,
	})
}
