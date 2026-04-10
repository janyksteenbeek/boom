package audio

import (
	"fmt"
	"log"
	"runtime/debug"
	"time"
	"unsafe"

	"github.com/gen2brain/malgo"

	"github.com/janyksteenbeek/boom/internal/event"
	"github.com/janyksteenbeek/boom/pkg/model"
)

const (
	NumDecks               = 2
	positionUpdateInterval = 33 * time.Millisecond
)

type Engine struct {
	bus        *event.Bus
	decks      [NumDecks]*Deck
	master     *MasterMixer
	sampleRate int
	stopCh     chan struct{}

	malgoCtx *malgo.AllocatedContext
	device   *malgo.Device
}

func NewEngine(bus *event.Bus, sampleRate int, bufferSize int, outputDevice string) (*Engine, error) {
	log.Printf("audio: initializing engine at %d Hz, buffer %d", sampleRate, bufferSize)

	// Reduce GC frequency once at engine startup for lower audio jitter
	debug.SetGCPercent(800)

	e := &Engine{
		bus:        bus,
		sampleRate: sampleRate,
		stopCh:     make(chan struct{}),
	}

	deckSlice := make([]*Deck, NumDecks)
	for i := range e.decks {
		e.decks[i] = NewDeck(i+1, sampleRate, bus)
		deckSlice[i] = e.decks[i]
	}
	e.master = NewMasterMixer(deckSlice, sampleRate)

	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("malgo init context: %w", err)
	}
	e.malgoCtx = ctx

	devices, _ := ctx.Devices(malgo.Playback)
	for _, d := range devices {
		marker := " "
		if d.IsDefault != 0 {
			marker = "*"
		}
		log.Printf("audio: [%s] %s", marker, d.Name())
	}

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format = malgo.FormatF32
	deviceConfig.Playback.Channels = 2
	deviceConfig.SampleRate = uint32(sampleRate)
	deviceConfig.PeriodSizeInFrames = uint32(bufferSize)
	deviceConfig.Periods = 2

	if outputDevice != "" && outputDevice != "System Default" {
		for _, d := range devices {
			if d.Name() == outputDevice {
				log.Printf("audio: selecting output: %s", outputDevice)
				deviceConfig.Playback.DeviceID = d.ID.Pointer()
				break
			}
		}
	}

	onSamples := func(pOutputSamples, pInputSamples []byte, frameCount uint32) {
		frames := int(frameCount)
		if frames == 0 {
			return
		}

		// Reinterpret malgo's byte buffer as [][2]float32 — same memory, zero copy.
		// Safe on all target platforms (x86, x86_64, ARM64) which are little-endian
		// and malgo's FormatF32 outputs native-endian float32.
		stereo := unsafe.Slice((*[2]float32)(unsafe.Pointer(&pOutputSamples[0])), frames)
		e.master.Stream(stereo)

		// Clamp output to [-1.0, 1.0] to prevent DAC clipping distortion
		for i := 0; i < frames; i++ {
			stereo[i][0] = clampF32(stereo[i][0])
			stereo[i][1] = clampF32(stereo[i][1])
		}
	}

	device, err := malgo.InitDevice(ctx.Context, deviceConfig, malgo.DeviceCallbacks{Data: onSamples})
	if err != nil {
		ctx.Uninit()
		ctx.Free()
		return nil, fmt.Errorf("malgo init device: %w", err)
	}
	e.device = device

	if err := device.Start(); err != nil {
		device.Uninit()
		ctx.Uninit()
		ctx.Free()
		return nil, fmt.Errorf("malgo start: %w", err)
	}
	log.Printf("audio: device started")

	e.subscribeEvents()
	go e.positionUpdateLoop()
	return e, nil
}

func (e *Engine) Deck(id int) *Deck {
	if id < 1 || id > NumDecks {
		return nil
	}
	return e.decks[id-1]
}

func (e *Engine) Stop() {
	close(e.stopCh)
	if e.device != nil {
		e.device.Uninit()
	}
	if e.malgoCtx != nil {
		_ = e.malgoCtx.Uninit()
		e.malgoCtx.Free()
	}
}

func (e *Engine) subscribeEvents() {
	e.bus.Subscribe(event.TopicDeck, func(ev event.Event) error {
		if ev.DeckID < 1 || ev.DeckID > NumDecks {
			return nil
		}
		deck := e.decks[ev.DeckID-1]

		switch ev.Action {
		case event.ActionPlayPause:
			deck.TogglePlay()
			e.bus.PublishAsync(event.Event{
				Topic: event.TopicEngine, Action: event.ActionPlayState,
				DeckID: ev.DeckID, Value: boolToFloat(deck.IsPlaying()),
			})

		case event.ActionPlay:
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

		case event.ActionSeek:
			deck.Seek(ev.Value)

		case event.ActionVolumeChange:
			deck.SetVolume(ev.Value)

		case event.ActionGainChange:
			// Map normalized 0.0-1.0 to gain multiplier 0.0-2.0 (0.5 = unity)
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

		case event.ActionLoadTrack:
			track, ok := ev.Payload.(*model.Track)
			if !ok {
				return fmt.Errorf("expected *model.Track")
			}
			// Decode on background goroutine to avoid blocking UI
			deckID := ev.DeckID
			go func() {
				err := deck.LoadTrack(track)
				if err == nil {
					deck.Play()
					e.bus.PublishAsync(event.Event{
						Topic: event.TopicEngine, Action: event.ActionPlayState,
						DeckID: deckID, Value: 1.0,
					})
				} else {
					log.Printf("engine: load failed deck %d: %v", deckID, err)
				}
			}()
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
	return v
}

func boolToFloat(b bool) float64 {
	if b {
		return 1.0
	}
	return 0.0
}
