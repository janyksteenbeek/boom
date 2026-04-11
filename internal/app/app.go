package app

import (
	"fmt"
	"log"
	"time"

	"github.com/janyksteenbeek/boom/internal/analysis"
	"github.com/janyksteenbeek/boom/internal/audio"
	"github.com/janyksteenbeek/boom/internal/config"
	"github.com/janyksteenbeek/boom/internal/controller"
	"github.com/janyksteenbeek/boom/internal/event"
	"github.com/janyksteenbeek/boom/internal/library"
	boomidi "github.com/janyksteenbeek/boom/internal/midi"
	"github.com/janyksteenbeek/boom/internal/plugin"
	"github.com/janyksteenbeek/boom/internal/ui"
	"github.com/janyksteenbeek/boom/pkg/model"
)

// App wires all subsystems together.
type App struct {
	bus      *event.Bus
	cfg      *config.Config
	engine   *audio.Engine
	midi     *boomidi.Manager
	library  *library.Library
	store    *library.Store
	analyzer *analysis.Service
	plugins  *plugin.Registry
	window   *ui.Window
	loader   *controller.Loader
	ledMgr   *controller.LEDManager
	stopCh   chan struct{}
}

func New() (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	bus := event.New()
	plugins := plugin.NewRegistry()

	store, err := library.NewStore(cfg.DatabasePath)
	if err != nil {
		return nil, err
	}
	lib := library.NewLibrary(bus, store)

	// Analysis service
	analyzer := analysis.NewService(bus, store, cfg)

	engine, err := audio.NewEngine(bus, cfg.SampleRate, cfg.BufferSize, cfg.AudioOutputDevice)
	if err != nil {
		store.Close()
		return nil, err
	}

	midiMgr := boomidi.NewManager(bus)

	// Controller mapping
	loader := controller.NewLoader(cfg.MIDIMappingDir)
	if err := loader.LoadAll(); err != nil {
		log.Printf("controller configs: %v", err)
	}

	registry := controller.NewActionRegistry()
	registerActions(registry, bus)

	// LED manager — sends MIDI output to controller
	ledMgr := controller.NewLEDManager(func(status, data1, data2 uint8) {
		if err := midiMgr.SendMIDI(status, data1, data2); err != nil {
			// Silently ignore send errors (controller may not be connected)
		}
	})

	// Compile first available controller config
	for name, ctrlCfg := range loader.All() {
		compiled, err := controller.Compile(*ctrlCfg)
		if err != nil {
			log.Printf("compile controller %s: %v", name, err)
			continue
		}

		mapper := controller.NewMapper(compiled, registry)
		midiMgr.SetMapper(mapper)

		// Register LED bindings from the compiled mapping
		for _, b := range compiled.LEDBindings {
			ledMgr.AddBinding(b)
		}
		midiMgr.SetLEDManager(ledMgr)

		log.Printf("controller active: %s (%d LED bindings)", name, len(compiled.LEDBindings))
		break
	}

	// Hot-reload
	loader.OnReload(func(name string, ctrlCfg *controller.ControllerConfig) {
		compiled, err := controller.Compile(*ctrlCfg)
		if err != nil {
			log.Printf("hot-reload compile %s: %v", name, err)
			return
		}
		mapper := controller.NewMapper(compiled, registry)
		midiMgr.SetMapper(mapper)
		log.Printf("hot-reloaded controller: %s", name)
	})
	if err := loader.Watch(); err != nil {
		log.Printf("controller watch: %v", err)
	}

	window := ui.NewWindow(bus, cfg)

	// Wire library search results to browser
	bus.Subscribe(event.TopicLibrary, func(ev event.Event) error {
		if ev.Action == event.ActionSearchResults {
			tracks, ok := ev.Payload.([]model.Track)
			if ok {
				window.Browser().SetTracks(tracks)
			}
		}
		return nil
	})

	app := &App{
		bus:      bus,
		cfg:      cfg,
		engine:   engine,
		midi:     midiMgr,
		library:  lib,
		store:    store,
		analyzer: analyzer,
		plugins:  plugins,
		window:   window,
		loader:   loader,
		ledMgr:   ledMgr,
		stopCh:   make(chan struct{}),
	}

	// Wire LED feedback: when play state changes, update controller LEDs
	bus.Subscribe(event.TopicEngine, func(ev event.Event) error {
		switch ev.Action {
		case event.ActionPlayState:
			playing := ev.Value > 0.5
			ledMgr.Update("play_pause", ev.DeckID, playing)
			log.Printf("LED: play_pause deck=%d playing=%v", ev.DeckID, playing)
		}
		return nil
	})

	// Wire settings save: rescan library when music dirs change
	window.OnSettingsSave(func(updatedCfg *config.Config) {
		log.Printf("settings saved, rescanning music dirs: %v", updatedCfg.MusicDirs)
		go func() {
			app.library.ScanDirs(updatedCfg.MusicDirs)
			tracks, err := app.library.AllTracks(0, 500)
			if err == nil {
				app.window.Browser().SetTracks(tracks)
			}
			genres, err := app.library.Genres()
			if err == nil {
				app.window.Browser().SetGenres(genres)
			}

			// Auto-analyze on import if enabled
			if updatedCfg.AutoAnalyzeOnImport {
				unanalyzed, err := app.store.UnanalyzedTracks(500)
				if err == nil && len(unanalyzed) > 0 {
					app.bus.PublishAsync(event.Event{
						Topic:   event.TopicAnalysis,
						Action:  event.ActionAnalyzeRequest,
						Payload: unanalyzed,
					})
				}
			}
		}()
	})

	return app, nil
}

func (a *App) Run() {
	if err := a.midi.Start(); err != nil {
		log.Printf("MIDI start: %v", err)
	}

	// Scan music in background, then load tracks and genres
	go func() {
		a.library.ScanDirs(a.cfg.MusicDirs)

		tracks, err := a.library.AllTracks(0, 500)
		if err != nil {
			log.Printf("initial track list: %v", err)
			return
		}
		a.window.Browser().SetTracks(tracks)

		genres, err := a.library.Genres()
		if err != nil {
			log.Printf("load genres: %v", err)
			return
		}
		a.window.Browser().SetGenres(genres)
	}()

	// Start VU meter output to controller
	go a.vuMeterLoop()

	// Run UI (blocks)
	a.window.Run()

	a.shutdown()
}

func (a *App) shutdown() {
	close(a.stopCh)
	if a.ledMgr != nil {
		a.ledMgr.ClearAll()
	}
	a.analyzer.Stop()
	a.engine.Stop()
	a.midi.Stop()
	a.loader.Close()
	a.library.Close()
}

// vuMeterLoop sends VU meter levels to the DDJ-FLX4 at ~20Hz.
// The DDJ-FLX4 expects CC 2 on ch0/ch1 with values 37-123.
func (a *App) vuMeterLoop() {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-a.stopCh:
			return
		case <-ticker.C:
			for i := 0; i < 2; i++ {
				deck := a.engine.Deck(i + 1)
				if deck == nil {
					continue
				}
				var vuValue uint8
				if deck.IsPlaying() {
					vuValue = 80 // Mid-level when playing (will be replaced with real RMS later)
				} else {
					vuValue = 37 // Minimum = off
				}
				channel := uint8(i) // ch0 for deck1, ch1 for deck2
				_ = a.midi.SendMIDI(0xB0|channel, 2, vuValue)
			}
		}
	}
}

// registerActions maps standard DJ actions to event bus events.
func registerActions(registry *controller.ActionRegistry, bus *event.Bus) {
	// Deck trigger actions
	for _, action := range []string{
		event.ActionPlayPause, event.ActionPlay, event.ActionPause,
		event.ActionCue, event.ActionSync,
		event.ActionLoopIn, event.ActionLoopOut, event.ActionLoopToggle,
	} {
		a := action
		registry.Register(a, controller.ActionDescriptor{
			Name: a, Type: controller.ActionTypeTrigger,
		}, func(ctx controller.ActionContext) {
			if !ctx.Pressed {
				return
			}
			bus.Publish(event.Event{
				Topic:  event.TopicDeck,
				Action: a,
				DeckID: ctx.Deck,
			})
		})
	}

	// Continuous deck actions
	for _, action := range []string{
		event.ActionVolumeChange, event.ActionTempoChange,
		event.ActionEQHigh, event.ActionEQMid, event.ActionEQLow,
		event.ActionGainChange,
	} {
		a := action
		registry.Register(a, controller.ActionDescriptor{
			Name: a, Type: controller.ActionTypeContinuous,
		}, func(ctx controller.ActionContext) {
			bus.Publish(event.Event{
				Topic:  event.TopicDeck,
				Action: a,
				DeckID: ctx.Deck,
				Value:  ctx.Value,
			})
		})
	}

	// Action name aliases (YAML action names -> event bus actions)
	actionAliases := map[string]string{
		"volume":  event.ActionVolumeChange,
		"tempo":   event.ActionTempoChange,
		"eq_high": event.ActionEQHigh,
		"eq_mid":  event.ActionEQMid,
		"eq_low":  event.ActionEQLow,
		"gain":    event.ActionGainChange,
	}
	for alias, target := range actionAliases {
		t := target
		registry.Register(alias, controller.ActionDescriptor{
			Name: alias, Type: controller.ActionTypeContinuous,
		}, func(ctx controller.ActionContext) {
			bus.Publish(event.Event{
				Topic:  event.TopicDeck,
				Action: t,
				DeckID: ctx.Deck,
				Value:  ctx.Value,
			})
		})
	}

	// Relative actions (jog wheel)
	for _, action := range []string{event.ActionJogScratch, event.ActionJogPitch} {
		a := action
		registry.Register(a, controller.ActionDescriptor{
			Name: a, Type: controller.ActionTypeRelative,
		}, func(ctx controller.ActionContext) {
			bus.Publish(event.Event{
				Topic:  event.TopicDeck,
				Action: a,
				DeckID: ctx.Deck,
				Value:  ctx.Delta,
			})
		})
	}

	// Mixer actions
	registry.Register(event.ActionCrossfader, controller.ActionDescriptor{
		Name: event.ActionCrossfader, Type: controller.ActionTypeContinuous,
	}, func(ctx controller.ActionContext) {
		bus.Publish(event.Event{
			Topic:  event.TopicMixer,
			Action: event.ActionCrossfader,
			Value:  ctx.Value,
		})
	})

	registry.Register(event.ActionMasterVolume, controller.ActionDescriptor{
		Name: event.ActionMasterVolume, Type: controller.ActionTypeContinuous,
	}, func(ctx controller.ActionContext) {
		bus.Publish(event.Event{
			Topic:  event.TopicMixer,
			Action: event.ActionMasterVolume,
			Value:  ctx.Value,
		})
	})

	registry.Register(event.ActionCueVolume, controller.ActionDescriptor{
		Name: event.ActionCueVolume, Type: controller.ActionTypeContinuous,
	}, func(ctx controller.ActionContext) {
		bus.Publish(event.Event{
			Topic:  event.TopicMixer,
			Action: event.ActionCueVolume,
			Value:  ctx.Value,
		})
	})

	// Library actions
	registry.Register(event.ActionBrowseScroll, controller.ActionDescriptor{
		Name: event.ActionBrowseScroll, Type: controller.ActionTypeRelative,
	}, func(ctx controller.ActionContext) {
		bus.Publish(event.Event{
			Topic:  event.TopicLibrary,
			Action: event.ActionBrowseScroll,
			Value:  ctx.Delta,
		})
	})

	registry.Register(event.ActionBrowseSelect, controller.ActionDescriptor{
		Name: event.ActionBrowseSelect, Type: controller.ActionTypeTrigger,
	}, func(ctx controller.ActionContext) {
		if !ctx.Pressed {
			return
		}
		bus.Publish(event.Event{
			Topic:  event.TopicLibrary,
			Action: event.ActionBrowseSelect,
		})
	})

	// Load track to specific deck (from global buttons on ch6)
	registry.Register("load_track_1", controller.ActionDescriptor{
		Name: "load_track_1", Type: controller.ActionTypeTrigger,
	}, func(ctx controller.ActionContext) {
		if !ctx.Pressed {
			return
		}
		bus.Publish(event.Event{
			Topic:  event.TopicDeck,
			Action: event.ActionLoadTrack,
			DeckID: 1,
		})
	})

	registry.Register("load_track_2", controller.ActionDescriptor{
		Name: "load_track_2", Type: controller.ActionTypeTrigger,
	}, func(ctx controller.ActionContext) {
		if !ctx.Pressed {
			return
		}
		bus.Publish(event.Event{
			Topic:  event.TopicDeck,
			Action: event.ActionLoadTrack,
			DeckID: 2,
		})
	})

	// Beat FX actions (DeckID: 0=master, 1=deck1, 2=deck2)
	registry.Register(event.ActionFXSelect, controller.ActionDescriptor{
		Name: event.ActionFXSelect, Type: controller.ActionTypeTrigger,
	}, func(ctx controller.ActionContext) {
		if !ctx.Pressed {
			return
		}
		bus.Publish(event.Event{
			Topic:  event.TopicDeck,
			Action: event.ActionFXSelect,
			DeckID: ctx.Deck,
			Value:  ctx.Value,
		})
	})

	registry.Register(event.ActionFXActivate, controller.ActionDescriptor{
		Name: event.ActionFXActivate, Type: controller.ActionTypeTrigger,
	}, func(ctx controller.ActionContext) {
		if !ctx.Pressed {
			return
		}
		bus.Publish(event.Event{
			Topic:  event.TopicDeck,
			Action: event.ActionFXActivate,
			DeckID: ctx.Deck,
			Value:  1.0,
		})
	})

	registry.Register(event.ActionFXNext, controller.ActionDescriptor{
		Name: event.ActionFXNext, Type: controller.ActionTypeTrigger,
	}, func(ctx controller.ActionContext) {
		if !ctx.Pressed {
			return
		}
		bus.Publish(event.Event{
			Topic:  event.TopicDeck,
			Action: event.ActionFXNext,
			DeckID: ctx.Deck,
		})
	})

	for _, action := range []string{
		event.ActionFXWetDry, event.ActionFXTime,
	} {
		a := action
		registry.Register(a, controller.ActionDescriptor{
			Name: a, Type: controller.ActionTypeContinuous,
		}, func(ctx controller.ActionContext) {
			bus.Publish(event.Event{
				Topic:  event.TopicDeck,
				Action: a,
				DeckID: ctx.Deck,
				Value:  ctx.Value,
			})
		})
	}

	// Stub actions for things defined in YAML but not yet implemented
	stubs := []string{
		"stutter", "jog_touch", "headphone_cue",
		"loop_halve", "loop_double", "browse_back",
	}
	for i := 1; i <= 8; i++ {
		stubs = append(stubs, fmt.Sprintf("hotcue_%d", i))
		stubs = append(stubs, fmt.Sprintf("hotcue_%d_delete", i))
	}
	for _, name := range stubs {
		n := name
		registry.Register(n, controller.ActionDescriptor{
			Name: n, Type: controller.ActionTypeTrigger,
		}, func(ctx controller.ActionContext) {
			if ctx.Pressed {
				log.Printf("stub action: %s deck=%d", n, ctx.Deck)
			}
		})
	}
}
