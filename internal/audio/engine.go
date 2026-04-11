package audio

import (
	"fmt"
	"log"
	"time"
	"unsafe"

	"github.com/ebitengine/oto/v3"

	"github.com/janyksteenbeek/boom/internal/event"
	"github.com/janyksteenbeek/boom/pkg/model"
)

const (
	NumDecks               = 2
	positionUpdateInterval = 16 * time.Millisecond
)

type Engine struct {
	bus        *event.Bus
	decks      [NumDecks]*Deck
	master     *MasterMixer
	sampleRate int
	stopCh     chan struct{}

	otoCtx *oto.Context
	player *oto.Player
}

// audioReader implements io.Reader and feeds mixed audio to the oto player.
// All processing is allocation-free: pre-decoded PCM, pre-allocated buffers.
type audioReader struct {
	engine *Engine
	buf    [][2]float32
}

func (r *audioReader) Read(p []byte) (int, error) {
	frames := len(p) / 8 // 8 bytes per stereo float32 frame
	if frames == 0 {
		return 0, nil
	}
	if frames > len(r.buf) {
		frames = len(r.buf)
	}

	buf := r.buf[:frames]
	for i := range buf {
		buf[i] = [2]float32{}
	}

	r.engine.master.Stream(buf)

	for i := range buf {
		buf[i][0] = clampF32(buf[i][0])
		buf[i][1] = clampF32(buf[i][1])
	}

	n := frames * 8
	src := unsafe.Slice((*byte)(unsafe.Pointer(&buf[0])), n)
	copy(p[:n], src)
	return n, nil
}

func NewEngine(bus *event.Bus, sampleRate int, bufferSize int, outputDevice string) (*Engine, error) {
	log.Printf("audio: initializing engine at %d Hz, buffer %d", sampleRate, bufferSize)

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

	// Initialize oto audio context
	bufDuration := time.Duration(float64(bufferSize) / float64(sampleRate) * float64(time.Second))
	otoCtx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   sampleRate,
		ChannelCount: 2,
		Format:       oto.FormatFloat32LE,
		BufferSize:   bufDuration,
	})
	if err != nil {
		return nil, fmt.Errorf("oto init: %w", err)
	}
	<-ready
	e.otoCtx = otoCtx

	reader := &audioReader{
		engine: e,
		buf:    make([][2]float32, 8192),
	}

	player := otoCtx.NewPlayer(reader)
	player.SetBufferSize(bufferSize * 8) // bytes: frames × 2ch × 4 bytes
	player.Play()
	e.player = player

	log.Printf("audio: device started (oto)")

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
	if e.player != nil {
		e.player.Pause()
		e.player.Close()
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

		case event.ActionLoadTrack:
			track, ok := ev.Payload.(*model.Track)
			if !ok {
				return fmt.Errorf("expected *model.Track")
			}
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
