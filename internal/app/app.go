package app

import (
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
	library   *library.Library
	store     *library.Store
	playlists *library.PlaylistService
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

	store, err := library.NewStore(cfg.DatabasePath, int64(cfg.Library.MMapSizeMB)*1024*1024)
	if err != nil {
		return nil, err
	}
	lib := library.NewLibrary(bus, store)
	playlists := library.NewPlaylistService(store, bus)
	analyzer := analysis.NewService(bus, store, cfg)

	engine, err := audio.NewEngine(bus, cfg.SampleRate, cfg.BufferSize,
		cfg.AudioOutputDevice, cfg.CueOutputDevice,
		newStoreWaveformCache(store))
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

	window := ui.NewWindow(bus, cfg, playlists)

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
		library:   lib,
		store:     store,
		playlists: playlists,
		analyzer: analyzer,
		plugins:  plugins,
		window:   window,
		loader:   loader,
		ledMgr:   ledMgr,
		stopCh:   make(chan struct{}),
	}

	app.subscribeCuePersistence()
	app.subscribePlayTracking()
	app.subscribeAnalysisHydration()
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

// subscribeAnalysisHydration restores cached analysis (beat grid, gain, …)
// onto a deck right after a track loads.
//
// The browser's track list comes from AllTracks()/Search()/etc, which
// intentionally skip the beat_grid blob so large list queries stay cheap.
// That means the *model.Track delivered via ActionLoadTrack has BPM+Key
// populated but BeatGrid is nil. On first ever load the analyzer fills it
// in, but on every subsequent load the analyzer sees BPM>0 && Key!="" and
// (correctly) skips, leaving the deck with an empty grid.
//
// We close that gap here: when a track load event arrives with no grid,
// fetch the full row from the store and republish as a synthetic
// AnalyzeComplete so the existing engine + UI handlers apply it the same
// way they would for a fresh analysis pass.
func (a *App) subscribeAnalysisHydration() {
	a.bus.Subscribe(event.TopicEngine, func(ev event.Event) error {
		if ev.Action != event.ActionTrackLoaded {
			return nil
		}
		track, _ := ev.Payload.(*model.Track)
		if track == nil || track.Path == "" {
			return nil
		}
		if len(track.BeatGrid) > 0 {
			return nil // already hydrated by an earlier pass
		}
		deckID := ev.DeckID
		path := track.Path
		go func() {
			full, err := a.store.GetByPath(path)
			if err != nil || full == nil {
				return
			}
			if full.BPM == 0 && len(full.BeatGrid) == 0 {
				return // nothing cached yet — analyzer will handle it
			}
			a.bus.Publish(event.Event{
				Topic:  event.TopicAnalysis,
				Action: event.ActionAnalyzeComplete,
				DeckID: deckID,
				Payload: &event.AnalysisResult{
					TrackID:  full.ID,
					BPM:      full.BPM,
					Key:      full.Key,
					Gain:     full.Gain,
					BeatGrid: full.BeatGrid,
					DeckID:   deckID,
				},
			})
		}()
		return nil
	})
}

// subscribePlayTracking bumps the play counter + last_played the first time
// a deck enters play state after a track is loaded. "Scrobbled-per-load"
// semantics: loading a different track resets the flag, so pause/resume of
// the same track counts as a single play.
func (a *App) subscribePlayTracking() {
	var scrobbled [3]string // index by deck ID 1..2; stores last track ID counted

	a.bus.Subscribe(event.TopicEngine, func(ev event.Event) error {
		switch ev.Action {
		case event.ActionTrackLoaded:
			if ev.DeckID < 1 || ev.DeckID >= len(scrobbled) {
				return nil
			}
			scrobbled[ev.DeckID] = "" // reset — new track is eligible to scrobble

		case event.ActionPlayState:
			if ev.DeckID < 1 || ev.DeckID >= len(scrobbled) {
				return nil
			}
			if ev.Value < 0.5 { // stopped / paused
				return nil
			}
			deck := a.engine.Deck(ev.DeckID)
			if deck == nil {
				return nil
			}
			track := deck.Track()
			if track == nil || track.ID == "" {
				return nil
			}
			if scrobbled[ev.DeckID] == track.ID {
				return nil
			}
			scrobbled[ev.DeckID] = track.ID
			trackID := track.ID
			go func() {
				if err := a.store.MarkPlayed(trackID, time.Now()); err != nil {
					log.Printf("play tracking: %v", err)
				}
			}()
		}
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
