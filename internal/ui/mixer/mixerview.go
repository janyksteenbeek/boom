package mixer

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/internal/event"
	"github.com/janyksteenbeek/boom/internal/ui/components"
	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
)

// MixerView is a narrow center panel with master volume and crossfader.
type MixerView struct {
	widget.BaseWidget

	bus        *event.Bus
	masterVol  *components.Fader
	cueVol     *components.Knob
	crossfader *components.Fader
	content    *fyne.Container
}

func NewMixerView(bus *event.Bus) *MixerView {
	m := &MixerView{bus: bus}

	m.masterVol = components.NewFader(true, 0.8, boomtheme.ColorLabel, func(v float64) {
		bus.Publish(event.Event{Topic: event.TopicMixer, Action: event.ActionMasterVolume, Value: v})
	})

	m.cueVol = components.NewKnob("CUE", 0.8, boomtheme.ColorCueActive, func(v float64) {
		bus.Publish(event.Event{Topic: event.TopicMixer, Action: event.ActionCueVolume, Value: v})
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

	// Deck indicators
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

	m.content = container.NewVBox(
		layout.NewSpacer(),
		container.NewCenter(masterLabel),
		container.NewCenter(m.masterVol),
		layout.NewSpacer(),
		sep,
		layout.NewSpacer(),
		container.NewCenter(cueLabel),
		container.NewCenter(m.cueVol),
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

func (m *MixerView) MinSize() fyne.Size {
	return fyne.NewSize(80, 200)
}

func (m *MixerView) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(m.content)
}
