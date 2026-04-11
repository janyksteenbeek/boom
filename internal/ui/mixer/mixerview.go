package mixer

import (
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/internal/audio"
	"github.com/janyksteenbeek/boom/internal/event"
	"github.com/janyksteenbeek/boom/internal/ui/components"
	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
)

// MixerView is a narrow center panel with master volume, Beat FX controls, and crossfader.
type MixerView struct {
	widget.BaseWidget

	bus        *event.Bus
	masterVol  *components.Fader
	cueVol     *components.Knob
	crossfader *components.Fader
	content    *fyne.Container

	// Beat FX state
	mu       sync.Mutex
	fxType   audio.FXType // currently selected effect type
	fxTarget int          // 0=master, 1=deck1, 2=deck2
	fxActive bool

	// FX UI elements
	fxBtnEcho    *components.DJButton
	fxBtnFlanger *components.DJButton
	fxBtnReverb  *components.DJButton
	fxBtnD1      *components.DJButton
	fxBtnMst     *components.DJButton
	fxBtnD2      *components.DJButton
	fxBtnOn      *components.DJButton
	fxTime       *components.Knob
	fxWetDry     *components.Knob
}

func NewMixerView(bus *event.Bus) *MixerView {
	m := &MixerView{
		bus:      bus,
		fxTarget: 0, // default: master
		fxType:   audio.FXNone,
	}

	m.masterVol = components.NewFader(true, 0.8, boomtheme.ColorLabel, func(v float64) {
		bus.Publish(event.Event{Topic: event.TopicMixer, Action: event.ActionMasterVolume, Value: v})
	})

	m.cueVol = components.NewKnob("CUE", 0.8, boomtheme.ColorCueActive, func(v float64) {
		bus.Publish(event.Event{Topic: event.TopicMixer, Action: event.ActionCueVolume, Value: v})
	})

	m.crossfader = components.NewFader(false, 0.5, boomtheme.ColorLabel, func(v float64) {
		bus.Publish(event.Event{Topic: event.TopicMixer, Action: event.ActionCrossfader, Value: v})
	})

	// Beat FX controls
	m.fxBtnEcho = components.NewDJButton("ECH", boomtheme.ColorPurple, func() {
		m.selectFXType(audio.FXEcho)
	})
	m.fxBtnFlanger = components.NewDJButton("FLN", boomtheme.ColorPurple, func() {
		m.selectFXType(audio.FXFlanger)
	})
	m.fxBtnReverb = components.NewDJButton("REV", boomtheme.ColorPurple, func() {
		m.selectFXType(audio.FXReverb)
	})

	m.fxBtnD1 = components.NewDJButton("D1", boomtheme.ColorDeck1, func() {
		m.selectFXTarget(1)
	})
	m.fxBtnMst = components.NewDJButton("MST", boomtheme.ColorLabel, func() {
		m.selectFXTarget(0)
	})
	m.fxBtnMst.SetActive(true) // default target
	m.fxBtnD2 = components.NewDJButton("D2", boomtheme.ColorDeck2, func() {
		m.selectFXTarget(2)
	})

	m.fxBtnOn = components.NewDJButton("FX ON", boomtheme.ColorGreen, func() {
		m.toggleFXActive()
	})

	m.fxTime = components.NewKnob("TIME", 0.5, boomtheme.ColorPurple, func(v float64) {
		m.mu.Lock()
		target := m.fxTarget
		m.mu.Unlock()
		bus.Publish(event.Event{
			Topic: event.TopicDeck, Action: event.ActionFXTime,
			DeckID: target, Value: v,
		})
	})

	m.fxWetDry = components.NewKnob("W/D", 0.5, boomtheme.ColorPurple, func(v float64) {
		m.mu.Lock()
		target := m.fxTarget
		m.mu.Unlock()
		bus.Publish(event.Event{
			Topic: event.TopicDeck, Action: event.ActionFXWetDry,
			DeckID: target, Value: v,
		})
	})

	masterLabel := canvas.NewText("MASTER", boomtheme.ColorLabelTertiary)
	masterLabel.TextSize = 9
	masterLabel.TextStyle = fyne.TextStyle{Bold: true}
	masterLabel.Alignment = fyne.TextAlignCenter

	cueLabel := canvas.NewText("CUE VOL", boomtheme.ColorLabelTertiary)
	cueLabel.TextSize = 9
	cueLabel.TextStyle = fyne.TextStyle{Bold: true}
	cueLabel.Alignment = fyne.TextAlignCenter

	fxLabel := canvas.NewText("BEAT FX", boomtheme.ColorLabelTertiary)
	fxLabel.TextSize = 9
	fxLabel.TextStyle = fyne.TextStyle{Bold: true}
	fxLabel.Alignment = fyne.TextAlignCenter

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

	sep3 := canvas.NewRectangle(boomtheme.ColorSeparator)
	sep3.SetMinSize(fyne.NewSize(0, 0.5))

	// FX type row: ECH | FLN | REV
	fxTypeRow := container.NewGridWithColumns(3, m.fxBtnEcho, m.fxBtnFlanger, m.fxBtnReverb)

	// FX target row: D1 | MST | D2
	fxTargetRow := container.NewGridWithColumns(3, m.fxBtnD1, m.fxBtnMst, m.fxBtnD2)

	// FX knobs row
	fxKnobRow := container.NewGridWithColumns(2,
		container.NewCenter(m.fxTime),
		container.NewCenter(m.fxWetDry),
	)

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
		container.NewCenter(fxLabel),
		fxTypeRow,
		fxTargetRow,
		fxKnobRow,
		container.NewCenter(m.fxBtnOn),
		sep3,
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

// selectFXType sets the active FX type and publishes the select event.
func (m *MixerView) selectFXType(t audio.FXType) {
	m.mu.Lock()
	m.fxType = t
	target := m.fxTarget
	m.mu.Unlock()

	m.fxBtnEcho.SetActive(t == audio.FXEcho)
	m.fxBtnFlanger.SetActive(t == audio.FXFlanger)
	m.fxBtnReverb.SetActive(t == audio.FXReverb)

	m.bus.Publish(event.Event{
		Topic: event.TopicDeck, Action: event.ActionFXSelect,
		DeckID: target, Value: float64(t),
	})
}

// selectFXTarget switches the FX target and re-sends current FX state to the new target.
func (m *MixerView) selectFXTarget(target int) {
	m.mu.Lock()
	oldTarget := m.fxTarget
	m.fxTarget = target
	fxType := m.fxType
	active := m.fxActive
	m.mu.Unlock()

	m.fxBtnD1.SetActive(target == 1)
	m.fxBtnMst.SetActive(target == 0)
	m.fxBtnD2.SetActive(target == 2)

	// Deactivate FX on old target
	if oldTarget != target {
		m.bus.Publish(event.Event{
			Topic: event.TopicDeck, Action: event.ActionFXActivate,
			DeckID: oldTarget, Value: 0.0,
		})
	}

	// Send current state to new target
	m.bus.Publish(event.Event{
		Topic: event.TopicDeck, Action: event.ActionFXSelect,
		DeckID: target, Value: float64(fxType),
	})
	if active {
		m.bus.Publish(event.Event{
			Topic: event.TopicDeck, Action: event.ActionFXActivate,
			DeckID: target, Value: 1.0,
		})
	}
}

// toggleFXActive toggles the FX on/off state.
func (m *MixerView) toggleFXActive() {
	m.mu.Lock()
	m.fxActive = !m.fxActive
	active := m.fxActive
	target := m.fxTarget
	m.mu.Unlock()

	m.fxBtnOn.SetActive(active)

	val := 0.0
	if active {
		val = 1.0
	}
	m.bus.Publish(event.Event{
		Topic: event.TopicDeck, Action: event.ActionFXActivate,
		DeckID: target, Value: val,
	})
}

// HandleMIDIFXWetDry handles the FX wet/dry from MIDI (routes to current target).
func (m *MixerView) HandleMIDIFXWetDry(v float64) {
	m.fxWetDry.SetValue(v)
	m.mu.Lock()
	target := m.fxTarget
	m.mu.Unlock()
	m.bus.Publish(event.Event{
		Topic: event.TopicDeck, Action: event.ActionFXWetDry,
		DeckID: target, Value: v,
	})
}

// HandleMIDIFXActivate handles the FX on/off toggle from MIDI.
func (m *MixerView) HandleMIDIFXActivate() {
	m.toggleFXActive()
}

// HandleMIDIFXNext cycles to the next effect type from MIDI.
func (m *MixerView) HandleMIDIFXNext() {
	m.mu.Lock()
	next := m.fxType + 1
	if next > audio.FXReverb {
		next = audio.FXEcho
	}
	m.mu.Unlock()
	m.selectFXType(next)
}

// UpdateFXTime sets the FX time knob from an external source (MIDI).
func (m *MixerView) UpdateFXTime(v float64) {
	m.fxTime.SetValue(v)
}

// UpdateFXWetDry sets the FX wet/dry knob from an external source (MIDI).
func (m *MixerView) UpdateFXWetDry(v float64) {
	m.fxWetDry.SetValue(v)
}

func (m *MixerView) MinSize() fyne.Size {
	return fyne.NewSize(130, 200)
}

func (m *MixerView) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(m.content)
}
