package fxbar

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

// FXBarView is a horizontal secondary bar above the library containing the
// Beat FX controls (type select, target select, time/wet-dry knobs, FX on).
type FXBarView struct {
	widget.BaseWidget

	bus     *event.Bus
	content *fyne.Container

	mu       sync.Mutex
	fxType   audio.FXType
	fxTarget int // 0=master, 1=deck1, 2=deck2
	fxActive bool

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

func NewFXBarView(bus *event.Bus) *FXBarView {
	f := &FXBarView{
		bus:      bus,
		fxTarget: 0,
		fxType:   audio.FXNone,
	}

	f.fxBtnEcho = components.NewDJButton("ECH", boomtheme.ColorPurple, func() {
		f.selectFXType(audio.FXEcho)
	})
	f.fxBtnFlanger = components.NewDJButton("FLN", boomtheme.ColorPurple, func() {
		f.selectFXType(audio.FXFlanger)
	})
	f.fxBtnReverb = components.NewDJButton("REV", boomtheme.ColorPurple, func() {
		f.selectFXType(audio.FXReverb)
	})

	f.fxBtnD1 = components.NewDJButton("D1", boomtheme.ColorDeck1, func() {
		f.selectFXTarget(1)
	})
	f.fxBtnMst = components.NewDJButton("MST", boomtheme.ColorLabel, func() {
		f.selectFXTarget(0)
	})
	f.fxBtnMst.SetActive(true)
	f.fxBtnD2 = components.NewDJButton("D2", boomtheme.ColorDeck2, func() {
		f.selectFXTarget(2)
	})

	f.fxBtnOn = components.NewDJButton("FX ON", boomtheme.ColorGreen, func() {
		f.toggleFXActive()
	})

	f.fxTime = components.NewKnob("TIME", 0.5, boomtheme.ColorPurple, func(v float64) {
		f.mu.Lock()
		target := f.fxTarget
		f.mu.Unlock()
		bus.Publish(event.Event{
			Topic: event.TopicDeck, Action: event.ActionFXTime,
			DeckID: target, Value: v,
		})
	})

	f.fxWetDry = components.NewKnob("W/D", 0.5, boomtheme.ColorPurple, func(v float64) {
		f.mu.Lock()
		target := f.fxTarget
		f.mu.Unlock()
		bus.Publish(event.Event{
			Topic: event.TopicDeck, Action: event.ActionFXWetDry,
			DeckID: target, Value: v,
		})
	})

	fxLabel := canvas.NewText("BEAT FX", boomtheme.ColorLabelTertiary)
	fxLabel.TextSize = 9
	fxLabel.TextStyle = fyne.TextStyle{Bold: true}

	typeRow := container.NewGridWithColumns(3, f.fxBtnEcho, f.fxBtnFlanger, f.fxBtnReverb)
	targetRow := container.NewGridWithColumns(3, f.fxBtnD1, f.fxBtnMst, f.fxBtnD2)
	knobRow := container.NewHBox(
		container.NewCenter(f.fxTime),
		container.NewCenter(f.fxWetDry),
	)

	f.content = container.NewHBox(
		layout.NewSpacer(),
		container.NewCenter(fxLabel),
		newVSep(),
		container.NewCenter(typeRow),
		newVSep(),
		container.NewCenter(targetRow),
		newVSep(),
		container.NewCenter(knobRow),
		newVSep(),
		container.NewCenter(f.fxBtnOn),
		layout.NewSpacer(),
	)

	f.ExtendBaseWidget(f)
	return f
}

func newVSep() *canvas.Rectangle {
	s := canvas.NewRectangle(boomtheme.ColorSeparator)
	s.SetMinSize(fyne.NewSize(1, 28))
	return s
}

func (f *FXBarView) MinSize() fyne.Size {
	return fyne.NewSize(600, 56)
}

func (f *FXBarView) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(f.content)
}

// selectFXType sets the active FX type and publishes the select event.
func (f *FXBarView) selectFXType(t audio.FXType) {
	f.mu.Lock()
	f.fxType = t
	target := f.fxTarget
	f.mu.Unlock()

	f.fxBtnEcho.SetActive(t == audio.FXEcho)
	f.fxBtnFlanger.SetActive(t == audio.FXFlanger)
	f.fxBtnReverb.SetActive(t == audio.FXReverb)

	f.bus.Publish(event.Event{
		Topic: event.TopicDeck, Action: event.ActionFXSelect,
		DeckID: target, Value: float64(t),
	})
}

// selectFXTarget switches the FX target and re-sends current FX state to the new target.
func (f *FXBarView) selectFXTarget(target int) {
	f.mu.Lock()
	oldTarget := f.fxTarget
	f.fxTarget = target
	fxType := f.fxType
	active := f.fxActive
	f.mu.Unlock()

	f.fxBtnD1.SetActive(target == 1)
	f.fxBtnMst.SetActive(target == 0)
	f.fxBtnD2.SetActive(target == 2)

	if oldTarget != target {
		f.bus.Publish(event.Event{
			Topic: event.TopicDeck, Action: event.ActionFXActivate,
			DeckID: oldTarget, Value: 0.0,
		})
	}

	f.bus.Publish(event.Event{
		Topic: event.TopicDeck, Action: event.ActionFXSelect,
		DeckID: target, Value: float64(fxType),
	})
	if active {
		f.bus.Publish(event.Event{
			Topic: event.TopicDeck, Action: event.ActionFXActivate,
			DeckID: target, Value: 1.0,
		})
	}
}

// toggleFXActive toggles the FX on/off state.
func (f *FXBarView) toggleFXActive() {
	f.mu.Lock()
	f.fxActive = !f.fxActive
	active := f.fxActive
	target := f.fxTarget
	f.mu.Unlock()

	f.fxBtnOn.SetActive(active)

	val := 0.0
	if active {
		val = 1.0
	}
	f.bus.Publish(event.Event{
		Topic: event.TopicDeck, Action: event.ActionFXActivate,
		DeckID: target, Value: val,
	})
}

// HandleMIDIFXWetDry handles the FX wet/dry from MIDI (routes to current target).
func (f *FXBarView) HandleMIDIFXWetDry(v float64) {
	f.fxWetDry.SetValue(v)
	f.mu.Lock()
	target := f.fxTarget
	f.mu.Unlock()
	f.bus.Publish(event.Event{
		Topic: event.TopicDeck, Action: event.ActionFXWetDry,
		DeckID: target, Value: v,
	})
}

// HandleMIDIFXActivate handles the FX on/off toggle from MIDI.
func (f *FXBarView) HandleMIDIFXActivate() {
	f.toggleFXActive()
}

// HandleMIDIFXNext cycles to the next effect type from MIDI.
func (f *FXBarView) HandleMIDIFXNext() {
	f.mu.Lock()
	next := f.fxType + 1
	if next > audio.FXReverb {
		next = audio.FXEcho
	}
	f.mu.Unlock()
	f.selectFXType(next)
}

// UpdateFXTime sets the FX time knob from an external source (MIDI).
func (f *FXBarView) UpdateFXTime(v float64) {
	f.fxTime.SetValue(v)
}

// UpdateFXWetDry sets the FX wet/dry knob from an external source (MIDI).
func (f *FXBarView) UpdateFXWetDry(v float64) {
	f.fxWetDry.SetValue(v)
}
