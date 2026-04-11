package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/internal/audio"
	"github.com/janyksteenbeek/boom/internal/config"
	"github.com/janyksteenbeek/boom/internal/event"
	"github.com/janyksteenbeek/boom/internal/ui/assets"
	"github.com/janyksteenbeek/boom/internal/ui/beatgrid"
	"github.com/janyksteenbeek/boom/internal/ui/browser"
	"github.com/janyksteenbeek/boom/internal/ui/deck"
	"github.com/janyksteenbeek/boom/internal/ui/mixer"
	"github.com/janyksteenbeek/boom/internal/ui/settings"
	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
	"github.com/janyksteenbeek/boom/pkg/model"
)

type Window struct {
	app      fyne.App
	window   fyne.Window
	bus      *event.Bus
	cfg      *config.Config
	deck1    *deck.DeckView
	deck2    *deck.DeckView
	mixer    *mixer.MixerView
	browser  *browser.BrowserView
	beatGrid *beatgrid.BeatGridStrip
	onSettingsSave func(*config.Config)
}

func NewWindow(bus *event.Bus, cfg *config.Config) *Window {
	a := app.NewWithID("dev.janyk.boom")
	a.SetIcon(fyne.NewStaticResource("app-icon.png", assets.AppIconPNG))
	a.Settings().SetTheme(boomtheme.New())

	w := a.NewWindow("Boom")
	w.Resize(fyne.NewSize(1400, 850))

	win := &Window{
		app:     a,
		window:  w,
		bus:     bus,
		cfg:     cfg,
		deck1:    deck.NewDeckView(1, bus),
		deck2:    deck.NewDeckView(2, bus),
		mixer:    mixer.NewMixerView(bus),
		browser:  browser.NewBrowserView(bus),
		beatGrid: beatgrid.NewBeatGridStrip(bus),
	}

	// Logo
	logoResource := fyne.NewStaticResource("logo-white.svg", assets.LogoWhiteSVG)
	logo := canvas.NewImageFromResource(logoResource)
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(80, 24))

	// Settings button (gear icon)
	settingsBtn := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
		settings.ShowSettingsDialog(win.window, win.cfg, func(updatedCfg *config.Config) {
			if win.onSettingsSave != nil {
				win.onSettingsSave(updatedCfg)
			}
		})
	})

	// Top toolbar: spacer-logo-spacer centered, settings far right
	toolbar := container.NewHBox(
		layout.NewSpacer(),
		logo,
		layout.NewSpacer(),
		settingsBtn,
	)

	toolbarSep := canvas.NewRectangle(boomtheme.ColorSeparator)
	toolbarSep.SetMinSize(fyne.NewSize(0, 1))

	// Deck layout
	vSepL := canvas.NewRectangle(boomtheme.ColorSeparator)
	vSepL.SetMinSize(fyne.NewSize(1, 0))
	vSepR := canvas.NewRectangle(boomtheme.ColorSeparator)
	vSepR.SetMinSize(fyne.NewSize(1, 0))
	hSep := canvas.NewRectangle(boomtheme.ColorSeparator)
	hSep.SetMinSize(fyne.NewSize(0, 1))

	mixerCol := container.NewHBox(vSepL, win.mixer, vSepR)
	rightSide := container.NewBorder(nil, nil, mixerCol, nil, win.deck2)
	decksRow := container.NewHSplit(win.deck1, rightSide)
	decksRow.SetOffset(0.5)

	mainContent := container.NewVSplit(
		decksRow,
		container.NewBorder(hSep, nil, nil, nil, win.browser),
	)
	mainContent.SetOffset(0.55)

	beatGridSep := canvas.NewRectangle(boomtheme.ColorSeparator)
	beatGridSep.SetMinSize(fyne.NewSize(0, 1))

	fullLayout := container.NewBorder(
		container.NewVBox(toolbar, toolbarSep, win.beatGrid, beatGridSep),
		nil, nil, nil,
		mainContent,
	)

	w.SetContent(fullLayout)
	win.subscribeEvents()
	return win
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
		}
		return nil
	})

	w.bus.Subscribe(event.TopicDeck, func(ev event.Event) error {
		// Handle FX events first — these can have DeckID=0 (master/global MIDI)
		switch ev.Action {
		case event.ActionFXTime:
			w.mixer.UpdateFXTime(ev.Value)
			return nil
		case event.ActionFXWetDry:
			if ev.DeckID == 0 {
				w.mixer.HandleMIDIFXWetDry(ev.Value)
			} else {
				w.mixer.UpdateFXWetDry(ev.Value)
			}
			return nil
		case event.ActionFXActivate:
			if ev.DeckID == 0 {
				w.mixer.HandleMIDIFXActivate()
			}
			return nil
		case event.ActionFXNext:
			w.mixer.HandleMIDIFXNext()
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
