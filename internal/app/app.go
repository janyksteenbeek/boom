package app

import (
	"log"

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
	analyzer := analysis.NewService(bus, store, cfg)

	engine, err := audio.NewEngine(bus, cfg.SampleRate, cfg.BufferSize,
		cfg.AudioOutputDevice, cfg.CueOutputDevice)
	if err != nil {
		store.Close()
		return nil, err
	}
	applyEngineSettings(engine, cfg)

	midiMgr := boomidi.NewManager(bus)

	loader, ledMgr, err := setupController(cfg, bus, midiMgr)
	if err != nil {
		log.Printf("controller setup: %v", err)
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

	app.subscribeCuePersistence()
	app.wireSettingsSave()

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

	go a.vuMeterLoop()
	go a.ledFeedbackLoop()

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
	if a.loader != nil {
		a.loader.Close()
	}
	a.library.Close()
}

// applyEngineSettings pushes the relevant config sections into the engine.
// Called on startup and again whenever the user saves the settings dialog.
func applyEngineSettings(engine *audio.Engine, cfg *config.Config) {
	engine.SetAutoCue(cfg.AutoCue)
	engine.SetLoopOptions(audio.LoopOptions{
		Quantize:        cfg.Loop.Quantize,
		DefaultBeatLoop: cfg.Loop.DefaultBeatLoop,
		MinBeats:        cfg.Loop.MinBeats,
		MaxBeats:        cfg.Loop.MaxBeats,
		SmartLoop:       cfg.Loop.SmartLoop,
	})
	engine.SetJogOptions(audio.JogOptions{
		VinylMode:          cfg.Jog.VinylMode,
		ScratchSensitivity: cfg.Jog.ScratchSensitivity,
		PitchSensitivity:   cfg.Jog.PitchSensitivity,
	})
}

// setupController loads MIDI mappings, builds the action registry/LED manager,
// compiles the first available controller config, and installs a hot-reload
// hook so YAML edits get picked up live.
func setupController(cfg *config.Config, bus *event.Bus, midiMgr *boomidi.Manager) (*controller.Loader, *controller.LEDManager, error) {
	loader := controller.NewLoader(cfg.MIDIMappingDir)
	if err := loader.LoadAll(); err != nil {
		log.Printf("controller configs: %v", err)
	}

	registry := controller.NewActionRegistry()
	registerActions(registry, bus)

	ledMgr := controller.NewLEDManager(func(status, data1, data2 uint8) {
		// Silently ignore send errors (controller may not be connected)
		_ = midiMgr.SendMIDI(status, data1, data2)
	})

	for name, ctrlCfg := range loader.All() {
		compiled, err := controller.Compile(*ctrlCfg)
		if err != nil {
			log.Printf("compile controller %s: %v", name, err)
			continue
		}

		mapper := controller.NewMapper(compiled, registry)
		midiMgr.SetMapper(mapper)

		for _, b := range compiled.LEDBindings {
			ledMgr.AddBinding(b)
		}
		midiMgr.SetLEDManager(ledMgr)

		log.Printf("controller active: %s (%d LED bindings)", name, len(compiled.LEDBindings))
		break
	}

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

	return loader, ledMgr, nil
}

// subscribeCuePersistence persists cue point changes to the database. Play and
// cue LED state is driven by ledFeedbackLoop instead because both can be in a
// blinking state — event-based updates can't express that.
func (a *App) subscribeCuePersistence() {
	a.bus.Subscribe(event.TopicEngine, func(ev event.Event) error {
		if ev.Action != event.ActionCuePointChanged {
			return nil
		}
		trackID, _ := ev.Payload.(string)
		if trackID == "" {
			return nil
		}
		pos := ev.Value
		go func() {
			if err := a.store.UpdateCuePoint(trackID, pos); err != nil {
				log.Printf("cue: persist failed for %s: %v", trackID, err)
			}
		}()
		return nil
	})
}

// wireSettingsSave installs the on-save handler that re-applies the engine
// settings and rescans the library when the user changes their music dirs.
func (a *App) wireSettingsSave() {
	a.window.OnSettingsSave(func(updatedCfg *config.Config) {
		log.Printf("settings saved, rescanning music dirs: %v", updatedCfg.MusicDirs)
		applyEngineSettings(a.engine, updatedCfg)
		go func() {
			a.library.ScanDirs(updatedCfg.MusicDirs)
			tracks, err := a.library.AllTracks(0, 500)
			if err == nil {
				a.window.Browser().SetTracks(tracks)
			}
			genres, err := a.library.Genres()
			if err == nil {
				a.window.Browser().SetGenres(genres)
			}

			if updatedCfg.AutoAnalyzeOnImport {
				unanalyzed, err := a.store.UnanalyzedTracks(500)
				if err == nil && len(unanalyzed) > 0 {
					a.bus.PublishAsync(event.Event{
						Topic:   event.TopicAnalysis,
						Action:  event.ActionAnalyzeRequest,
						Payload: unanalyzed,
					})
				}
			}
		}()
	})
}
