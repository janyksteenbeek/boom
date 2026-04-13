package mixer

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/internal/event"
	"github.com/janyksteenbeek/boom/internal/ui/components"
	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
)

// MixerView is a narrow center panel with master volume, cue volume, and crossfader.
type MixerView struct {
	widget.BaseWidget

	bus        *event.Bus
	masterVol  *components.Fader
	cueVol     *components.Knob
	gain1      *components.Fader
	gain2      *components.Fader
	peak1      *components.PeakMeter
	peak2      *components.PeakMeter
	crossfader *components.Fader
	content    *fyne.Container
}

func NewMixerView(bus *event.Bus) *MixerView {
	m := &MixerView{bus: bus}

	m.masterVol = components.NewFader(true, 0.8, boomtheme.ColorLabel, func(v float64) {
		bus.Publish(event.Event{Topic: event.TopicMixer, Action: event.ActionMasterVolume, Value: v})
	})

	m.peak1 = components.NewPeakMeter()
	m.peak2 = components.NewPeakMeter()

	m.cueVol = components.NewKnob("CUE", 0.8, boomtheme.ColorCueActive, func(v float64) {
		bus.Publish(event.Event{Topic: event.TopicMixer, Action: event.ActionCueVolume, Value: v})
	})

	// Per-deck channel gain sliders flank the CUE knob. They map 0..1 to
	// the same range the deck GAIN knob publishes (the engine already
	// handles ActionGainChange for both paths).
	m.gain1 = components.NewFader(true, 0.5, boomtheme.ColorDeck1, func(v float64) {
		bus.Publish(event.Event{
			Topic: event.TopicDeck, Action: event.ActionGainChange,
			DeckID: 1, Value: v,
		})
	})
	m.gain2 = components.NewFader(true, 0.5, boomtheme.ColorDeck2, func(v float64) {
		bus.Publish(event.Event{
			Topic: event.TopicDeck, Action: event.ActionGainChange,
			DeckID: 2, Value: v,
		})
	})

	m.crossfader = components.NewFader(false, 0.5, boomtheme.ColorLabel, func(v float64) {
		bus.Publish(event.Event{Topic: event.TopicMixer, Action: event.ActionCrossfader, Value: v})
	})

	masterLabel := canvas.NewText("MASTER", boomtheme.ColorLabelTertiary)
	masterLabel.TextSize = 9
	masterLabel.TextStyle = fyne.TextStyle{Bold: true}
	masterLabel.Alignment = fyne.TextAlignCenter

	cueLabel := canvas.NewText("CUE VOL", boomtheme.ColorLabelTertiary)
	cueLabel.TextSize = 9
	cueLabel.TextStyle = fyne.TextStyle{Bold: true}
	cueLabel.Alignment = fyne.TextAlignCenter

	aLabel := canvas.NewText("A", boomtheme.ColorDeck1)
	aLabel.TextSize = 9
	aLabel.TextStyle = fyne.TextStyle{Bold: true}
	bLabel := canvas.NewText("B", boomtheme.ColorDeck2)
	bLabel.TextSize = 9
	bLabel.TextStyle = fyne.TextStyle{Bold: true}
	bLabel.Alignment = fyne.TextAlignTrailing

	abRow := container.NewBorder(nil, nil, aLabel, bLabel)

	sep := canvas.NewRectangle(boomtheme.ColorSeparator)
	sep.SetMinSize(fyne.NewSize(0, 0.5))

	sep2 := canvas.NewRectangle(boomtheme.ColorSeparator)
	sep2.SetMinSize(fyne.NewSize(0, 0.5))

	// Tiny "1" / "2" labels above each channel-gain fader.
	gain1Label := canvas.NewText("1", boomtheme.ColorDeck1)
	gain1Label.TextSize = 10
	gain1Label.TextStyle = fyne.TextStyle{Bold: true}
	gain1Label.Alignment = fyne.TextAlignCenter
	gain2Label := canvas.NewText("2", boomtheme.ColorDeck2)
	gain2Label.TextSize = 10
	gain2Label.TextStyle = fyne.TextStyle{Bold: true}
	gain2Label.Alignment = fyne.TextAlignCenter

	// CUE row: gain1 fader | CUE knob | gain2 fader. Each side is a small
	// VBox stacking the channel label above its fader. Fixed-width cells
	// plus horizontal padding keep the cue knob centered and give the row
	// breathing room.
	faderCell := fyne.NewSize(22, 90)
	gain1Col := container.NewVBox(
		gain1Label,
		container.New(layout.NewGridWrapLayout(faderCell), m.gain1),
	)
	gain2Col := container.NewVBox(
		gain2Label,
		container.New(layout.NewGridWrapLayout(faderCell), m.gain2),
	)
	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(12, 0))
	spacer2 := canvas.NewRectangle(color.Transparent)
	spacer2.SetMinSize(fyne.NewSize(12, 0))
	cueRow := container.NewHBox(
		gain1Col,
		spacer,
		container.NewCenter(m.cueVol),
		spacer2,
		gain2Col,
	)

	// Master row: peak meter | master fader | peak meter. Tiny "1"/"2"
	// labels above each meter mark which deck it's monitoring.
	meterCell := fyne.NewSize(14, 110)
	masterCell := fyne.NewSize(28, 130)
	peak1Label := canvas.NewText("1", boomtheme.ColorDeck1)
	peak1Label.TextSize = 9
	peak1Label.TextStyle = fyne.TextStyle{Bold: true}
	peak1Label.Alignment = fyne.TextAlignCenter
	peak2Label := canvas.NewText("2", boomtheme.ColorDeck2)
	peak2Label.TextSize = 9
	peak2Label.TextStyle = fyne.TextStyle{Bold: true}
	peak2Label.Alignment = fyne.TextAlignCenter

	peak1Col := container.NewVBox(
		peak1Label,
		container.New(layout.NewGridWrapLayout(meterCell), m.peak1),
	)
	peak2Col := container.NewVBox(
		peak2Label,
		container.New(layout.NewGridWrapLayout(meterCell), m.peak2),
	)
	masterRow := container.NewHBox(
		peak1Col,
		container.New(layout.NewGridWrapLayout(masterCell), m.masterVol),
		peak2Col,
	)

	m.content = container.NewVBox(
		layout.NewSpacer(),
		container.NewCenter(masterLabel),
		container.NewCenter(masterRow),
		layout.NewSpacer(),
		sep,
		layout.NewSpacer(),
		container.NewCenter(cueLabel),
		container.NewCenter(cueRow),
		layout.NewSpacer(),
		sep2,
		layout.NewSpacer(),
		abRow,
		m.crossfader,
		layout.NewSpacer(),
	)

	m.ExtendBaseWidget(m)
	return m
}

// UpdateCrossfader sets the crossfader position from an external source (MIDI).
func (m *MixerView) UpdateCrossfader(v float64) {
	m.crossfader.SetValue(v)
}

// UpdateMasterVolume sets the master volume from an external source (MIDI).
func (m *MixerView) UpdateMasterVolume(v float64) {
	m.masterVol.SetValue(v)
}

// UpdateCueVolume sets the cue volume knob from an external source (MIDI).
func (m *MixerView) UpdateCueVolume(v float64) {
	m.cueVol.SetValue(v)
}

// UpdateGain reflects a deck gain change (from MIDI or the deck's own
// knob) on the mixer's channel gain slider.
func (m *MixerView) UpdateGain(deckID int, v float64) {
	switch deckID {
	case 1:
		m.gain1.SetValue(v)
	case 2:
		m.gain2.SetValue(v)
	}
}

// UpdatePeakLevel feeds the per-deck output peak (0..1) into the master
// peak meters. Called from the engine VU loop via the bus.
func (m *MixerView) UpdatePeakLevel(deckID int, v float64) {
	switch deckID {
	case 1:
		m.peak1.SetLevel(v)
	case 2:
		m.peak2.SetLevel(v)
	}
}

func (m *MixerView) MinSize() fyne.Size {
	return fyne.NewSize(220, 260)
}

func (m *MixerView) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(m.content)
}
