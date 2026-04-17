package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	fynelayout "fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/internal/audio"
	"github.com/janyksteenbeek/boom/internal/config"
	"github.com/janyksteenbeek/boom/internal/event"
	"github.com/janyksteenbeek/boom/internal/library"
	"github.com/janyksteenbeek/boom/internal/ui/assets"
	"github.com/janyksteenbeek/boom/internal/ui/beatgrid"
	"github.com/janyksteenbeek/boom/internal/ui/browser"
	"github.com/janyksteenbeek/boom/internal/ui/deck"
	"github.com/janyksteenbeek/boom/internal/ui/fxbar"
	"github.com/janyksteenbeek/boom/internal/ui/layout"
	"github.com/janyksteenbeek/boom/internal/ui/mixer"
	"github.com/janyksteenbeek/boom/internal/ui/settings"
	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
	"github.com/janyksteenbeek/boom/pkg/model"
)

type Window struct {
	app            fyne.App
	window         fyne.Window
	bus            *event.Bus
	cfg            *config.Config
	opts           Options
	deck1          *deck.DeckView
	deck2          *deck.DeckView
	mixer          *mixer.MixerView
	fxBar          *fxbar.FXBarView
	browser        *browser.BrowserView
	beatGrid       *beatgrid.BeatGridStrip
	onSettingsSave func(*config.Config)
}

// NewWindow is kept for callers that don't care about Options (tests,
// legacy entry points). Prefer NewWindowWithOptions for anything that
// might run in mini / kiosk mode.
func NewWindow(bus *event.Bus, cfg *config.Config, playlists *library.PlaylistService) *Window {
	return NewWindowWithOptions(bus, cfg, playlists, Options{})
}

// NewWindowWithOptions builds the window, chooses a layout based on opts,
// and applies window-level options (fullscreen, force-size, kiosk).
func NewWindowWithOptions(bus *event.Bus, cfg *config.Config, playlists *library.PlaylistService, opts Options) *Window {
	a := app.NewWithID("dev.janyk.boom")
	a.SetIcon(fyne.NewStaticResource("app-icon.png", assets.AppIconPNG))
	a.Settings().SetTheme(boomtheme.New())

	w := a.NewWindow("Boom")

	// Default window size depends on the chosen layout.
	defaultW, defaultH := float32(1400), float32(850)
	if opts.Layout == "mini" {
		defaultW, defaultH = 800, 480
	}
	if opts.ForceWidth > 0 && opts.ForceHeight > 0 {
		defaultW = float32(opts.ForceWidth)
		defaultH = float32(opts.ForceHeight)
	}
	w.Resize(fyne.NewSize(defaultW, defaultH))

	deckOpts := deck.Options{Compact: opts.Layout == "mini"}
	win := &Window{
		app:      a,
		window:   w,
		bus:      bus,
		cfg:      cfg,
		opts:     opts,
		deck1:    deck.NewDeckViewWithOptions(1, bus, deckOpts),
		deck2:    deck.NewDeckViewWithOptions(2, bus, deckOpts),
		mixer:    mixer.NewMixerView(bus),
		fxBar:    fxbar.NewFXBarView(bus),
		beatGrid: beatgrid.NewBeatGridStrip(bus),
	}

	// Browser: mini-mode uses a compact variant (no toolbar, no column
	// header, folder-style navigation) suitable for a fullscreen overlay.
	if opts.Layout == "mini" {
		win.browser = browser.NewBrowserViewWithOpts(bus, playlists, browser.BrowserOpts{
			HideToolbar:      true,
			HideColumnHeader: true,
			SidebarMode:      browser.SidebarModeFolder,
		})
	} else {
		win.browser = browser.NewBrowserView(bus, playlists)
	}

	toolbar := win.buildToolbar()

	deps := layout.Deps{
		Deck1:    win.deck1,
		Deck2:    win.deck2,
		Mixer:    win.mixer,
		FXBar:    win.fxBar,
		Browser:  win.browser,
		BeatGrid: win.beatGrid,
		Toolbar:  toolbar,
		Window:   w,
		Bus:      bus,
	}

	var l layout.Layout
	switch opts.Layout {
	case "mini":
		l = layout.NewMini()
	default:
		l = layout.NewDesktop()
	}

	w.SetContent(l.Build(deps))

	if opts.Fullscreen {
		w.SetFullScreen(true)
	}
	if opts.Kiosk {
		w.SetMainMenu(nil)
	}

	win.subscribeEvents()
	win.installShortcuts(playlists)
	return win
}

// buildToolbar composes the top toolbar with or without the settings
// gear depending on kiosk mode. Kiosk mode intentionally hides the gear
// so an unattended Pi deployment can't accidentally be reconfigured.
func (win *Window) buildToolbar() fyne.CanvasObject {
	logoResource := fyne.NewStaticResource("logo-white.svg", assets.LogoWhiteSVG)
	logo := canvas.NewImageFromResource(logoResource)
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(80, 24))

	if win.opts.Kiosk {
		return container.NewHBox(fynelayout.NewSpacer(), logo, fynelayout.NewSpacer())
	}

	settingsBtn := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
		settings.ShowSettingsDialog(win.window, win.cfg, func(updatedCfg *config.Config) {
			if win.onSettingsSave != nil {
				win.onSettingsSave(updatedCfg)
			}
		})
	})
	return container.NewHBox(
		fynelayout.NewSpacer(),
		logo,
		fynelayout.NewSpacer(),
		settingsBtn,
	)
}

// installShortcuts wires the browser-scoped keyboard shortcuts for playlist
// management. Shortcuts are only acted on when the user is focussed on the
// browser track list with a manual playlist open, so they never interfere
// with deck controls.
func (w *Window) installShortcuts(playlists *library.PlaylistService) {
	if playlists == nil {
		return
	}
	tl := w.browser.TrackList()

	reorderBy := func(step int) {
		pid := tl.PlaylistID()
		if pid == "" {
			return
		}
		sel := tl.SelectedTracks()
		if len(sel) == 0 {
			return
		}
		// Move the primary selection; multi-move is cheap because
		// ReorderMany is already a single transaction.
		ids := make([]string, len(sel))
		for i, t := range sel {
			ids[i] = t.ID
		}
		tracks := tl.Tracks()
		firstIdx := -1
		for i, t := range tracks {
			for _, s := range sel {
				if t.ID == s.ID {
					firstIdx = i
					break
				}
			}
			if firstIdx >= 0 {
				break
			}
		}
		if firstIdx < 0 {
			return
		}
		newIdx := firstIdx + step
		if newIdx < 0 {
			newIdx = 0
		}
		_ = playlists.ReorderMany(pid, ids, newIdx)
	}

	remove := func() {
		pid := tl.PlaylistID()
		if pid == "" {
			return
		}
		sel := tl.SelectedTracks()
		if len(sel) == 0 {
			return
		}
		ids := make([]string, len(sel))
		for i, t := range sel {
			ids[i] = t.ID
		}
		_ = playlists.RemoveTracks(pid, ids)
	}

	canvas := w.window.Canvas()

	// Keyboard fallbacks — mirrors what a MIDI controller publishes so
	// mini-mode is usable without hardware. SetOnTypedKey fires on any
	// keypress regardless of focus, which AddShortcut does not.
	//
	//   Enter        → browse_select  (open overlay / cycle focus)
	//   Up / Down    → browse_scroll  (move selection up / down)
	//   1 / 2        → load_track     (load selected into deck 1 / 2)
	//   Escape       → (overlay closes via its own subscriber)
	//   Delete/Back  → remove track from playlist (desktop)
	//
	// The browse_scroll handler inverts sign (CW encoder = advance), so
	// positive Value maps to "up" and negative maps to "down" here.
	canvas.SetOnTypedKey(func(ev *fyne.KeyEvent) {
		switch ev.Name {
		case fyne.KeyDelete, fyne.KeyBackspace:
			remove()
		case fyne.KeyReturn, fyne.KeyEnter:
			w.bus.Publish(event.Event{Topic: event.TopicLibrary, Action: event.ActionBrowseSelect})
		case fyne.KeyUp:
			w.bus.Publish(event.Event{Topic: event.TopicLibrary, Action: event.ActionBrowseScroll, Value: 1})
		case fyne.KeyDown:
			w.bus.Publish(event.Event{Topic: event.TopicLibrary, Action: event.ActionBrowseScroll, Value: -1})
		case fyne.Key1:
			w.bus.Publish(event.Event{Topic: event.TopicDeck, Action: event.ActionLoadTrack, DeckID: 1})
		case fyne.Key2:
			w.bus.Publish(event.Event{Topic: event.TopicDeck, Action: event.ActionLoadTrack, DeckID: 2})
		}
	})

	// Cmd/Ctrl + Up/Down reorder tracks within the open manual playlist.
	canvas.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyUp, Modifier: fyne.KeyModifierShortcutDefault}, func(fyne.Shortcut) {
		reorderBy(-1)
	})
	canvas.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyDown, Modifier: fyne.KeyModifierShortcutDefault}, func(fyne.Shortcut) {
		reorderBy(1)
	})
}

// OnSettingsSave sets a callback for when settings are saved.
func (w *Window) OnSettingsSave(fn func(*config.Config)) {
	w.onSettingsSave = fn
}

func (w *Window) Run() {
	w.window.ShowAndRun()
}

func (w *Window) Browser() *browser.BrowserView {
	return w.browser
}

func (w *Window) deckForID(id int) *deck.DeckView {
	if id == 1 {
		return w.deck1
	}
	if id == 2 {
		return w.deck2
	}
	return nil
}

func (w *Window) subscribeEvents() {
	w.bus.Subscribe(event.TopicEngine, func(ev event.Event) error {
		d := w.deckForID(ev.DeckID)
		switch ev.Action {
		case event.ActionPositionUpdate:
			if d != nil {
				d.UpdatePosition(ev.Value)
			}
			w.beatGrid.UpdatePosition(ev.DeckID, ev.Value)
		case event.ActionWaveformReady:
			data, ok := ev.Payload.(*audio.WaveformData)
			if ok {
				if d != nil {
					d.SetWaveformData(data)
				}
				w.beatGrid.SetWaveformData(ev.DeckID, data)
			}
		case event.ActionTrackLoaded:
			track, ok := ev.Payload.(*model.Track)
			if ok {
				if d != nil {
					d.SetTrack(track)
				}
				w.beatGrid.SetTrack(ev.DeckID, track)
			}
		case event.ActionPlayState:
			if d != nil {
				d.SetPlaying(ev.Value > 0.5)
			}
		case event.ActionCuePointChanged:
			if d != nil {
				d.UpdateCuePoint(ev.Value)
			}
			w.beatGrid.SetCuePoint(ev.DeckID, ev.Value)
		case event.ActionLoopStateUpdate:
			state, _ := ev.Payload.(*event.LoopState)
			if d != nil {
				d.UpdateLoopState(state)
			}
			w.beatGrid.SetLoopState(ev.DeckID, state)
		case event.ActionVULevel:
			w.mixer.UpdatePeakLevel(ev.DeckID, ev.Value)
		}
		return nil
	})

	w.bus.Subscribe(event.TopicDeck, func(ev event.Event) error {
		// FX events may arrive unresolved from MIDI (DeckIDUnresolved) —
		// in that case route through the mixer's current FX target.
		switch ev.Action {
		case event.ActionFXTime:
			w.fxBar.UpdateFXTime(ev.Value)
			return nil
		case event.ActionFXWetDry:
			if ev.DeckID == event.DeckIDUnresolved {
				w.fxBar.HandleMIDIFXWetDry(ev.Value)
			} else {
				w.fxBar.UpdateFXWetDry(ev.Value)
			}
			return nil
		case event.ActionFXActivate:
			if ev.DeckID == event.DeckIDUnresolved {
				w.fxBar.HandleMIDIFXActivate()
			}
			return nil
		case event.ActionFXNext:
			w.fxBar.HandleMIDIFXNext()
			return nil
		}

		// Per-deck events require a valid deck
		d := w.deckForID(ev.DeckID)
		if d == nil {
			return nil
		}
		switch ev.Action {
		case event.ActionVolumeChange:
			d.UpdateVolume(ev.Value)
		case event.ActionTempoChange:
			d.UpdateTempo(ev.Value)
		case event.ActionEQHigh:
			d.UpdateEQHigh(ev.Value)
		case event.ActionEQMid:
			d.UpdateEQMid(ev.Value)
		case event.ActionEQLow:
			d.UpdateEQLow(ev.Value)
		case event.ActionGainChange:
			d.UpdateGain(ev.Value)
			w.mixer.UpdateGain(ev.DeckID, ev.Value)
		}
		return nil
	})

	// Analysis results: update deck BPM displays + beat grid
	w.bus.Subscribe(event.TopicAnalysis, func(ev event.Event) error {
		if ev.Action == event.ActionAnalyzeComplete {
			result, ok := ev.Payload.(*event.AnalysisResult)
			if ok {
				w.deck1.UpdateAnalysis(result.TrackID, result.BPM, result.Key)
				w.deck2.UpdateAnalysis(result.TrackID, result.BPM, result.Key)
				if result.DeckID > 0 && len(result.BeatGrid) > 0 {
					w.beatGrid.SetBeatGrid(result.DeckID, result.BeatGrid)
				}
			}
		}
		return nil
	})

	w.bus.Subscribe(event.TopicMixer, func(ev event.Event) error {
		switch ev.Action {
		case event.ActionCrossfader:
			w.mixer.UpdateCrossfader(ev.Value)
		case event.ActionMasterVolume:
			w.mixer.UpdateMasterVolume(ev.Value)
		case event.ActionCueVolume:
			w.mixer.UpdateCueVolume(ev.Value)
		}
		return nil
	})
}
