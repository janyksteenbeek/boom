package deck

import (
	"fmt"
	"log"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/internal/audio"
	"github.com/janyksteenbeek/boom/internal/event"
	"github.com/janyksteenbeek/boom/internal/ui/components"
	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
	"github.com/janyksteenbeek/boom/pkg/model"
)

type DeckView struct {
	widget.BaseWidget

	deckID         int
	bus            *event.Bus
	currentTrackID string
	waveform       *WaveformWidget

	playBtn *components.DJButton
	cueBtn  *components.DJButton
	syncBtn *components.DJButton

	loopInBtn    *components.DJButton
	loopOutBtn   *components.DJButton
	reloopBtn    *components.DJButton
	loopHalveBtn *components.DJButton
	loopDoubleBtn *components.DJButton

	volKnob *components.Knob
	hiKnob  *components.Knob
	midKnob *components.Knob
	loKnob  *components.Knob

	bpmMinusBtn *components.DJButton
	bpmPlusBtn  *components.DJButton
	bpmOrigTap  *tappableText

	deckLabel     *canvas.Text
	trackTitle    *canvas.Text
	trackArtist   *canvas.Text
	bpmText       *canvas.Text
	bpmLabel      *canvas.Text
	bpmOrigText   *canvas.Text
	timeText      *canvas.Text
	durText       *canvas.Text
	remainingText *canvas.Text

	// origBPM is the analyzed BPM as stored on the track. It's shown in
	// small text next to the current BPM whenever the user has nudged the
	// tempo away from that value.
	origBPM    float64
	tempoRatio float64       // current tempo multiplier, 1.0 = original
	duration   time.Duration // track duration once known

	content *fyne.Container
}

// bpmNudgeStep is the BPM adjustment applied per click on the -/+ buttons.
// Small enough to feel like a fine nudge, large enough to be useful without
// needing to spam the button.
const bpmNudgeStep = 0.1

func NewDeckView(deckID int, bus *event.Bus) *DeckView {
	d := &DeckView{deckID: deckID, bus: bus, tempoRatio: 1.0}
	deckColor := boomtheme.DeckColor(deckID)

	d.waveform = NewWaveformWidget(deckID)
	d.waveform.SetOnSeek(func(pos float64) {
		bus.Publish(event.Event{
			Topic: event.TopicDeck, Action: event.ActionSeek,
			DeckID: deckID, Value: pos,
		})
	})

	// Header info
	d.deckLabel = canvas.NewText(fmt.Sprintf("DECK %d", deckID), deckColor)
	d.deckLabel.TextSize = 10
	d.deckLabel.TextStyle = fyne.TextStyle{Bold: true}

	d.trackTitle = canvas.NewText("No Track Loaded", boomtheme.ColorLabel)
	d.trackTitle.TextSize = 13
	d.trackTitle.TextStyle = fyne.TextStyle{Bold: true}

	d.trackArtist = canvas.NewText("", boomtheme.ColorLabelSecondary)
	d.trackArtist.TextSize = 11

	d.bpmText = canvas.NewText("---", deckColor)
	d.bpmText.TextSize = 26
	d.bpmText.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
	d.bpmText.Alignment = fyne.TextAlignTrailing

	d.bpmOrigText = canvas.NewText("", boomtheme.ColorLabelTertiary)
	d.bpmOrigText.TextSize = 10
	d.bpmOrigText.TextStyle = fyne.TextStyle{Monospace: true}
	d.bpmOrigText.Alignment = fyne.TextAlignTrailing

	d.bpmLabel = canvas.NewText("BPM", boomtheme.ColorLabelTertiary)
	d.bpmLabel.TextSize = 9
	d.bpmLabel.Alignment = fyne.TextAlignTrailing

	d.bpmMinusBtn = components.NewDJButton("−", boomtheme.ColorLabel, func() {
		d.nudgeBPM(-bpmNudgeStep)
	})
	d.bpmPlusBtn = components.NewDJButton("+", boomtheme.ColorLabel, func() {
		d.nudgeBPM(bpmNudgeStep)
	})
	// Clicking the ghosted original BPM resets the tempo back to 1.0×.
	d.bpmOrigTap = newTappableText(d.bpmOrigText, func() {
		d.resetTempo()
	})

	d.timeText = canvas.NewText("0:00", boomtheme.ColorLabel)
	d.timeText.TextSize = 15
	d.timeText.TextStyle = fyne.TextStyle{Monospace: true}

	d.durText = canvas.NewText("/ 0:00", boomtheme.ColorLabelTertiary)
	d.durText.TextSize = 11
	d.durText.TextStyle = fyne.TextStyle{Monospace: true}

	d.remainingText = canvas.NewText("-0:00", boomtheme.ColorLabelSecondary)
	d.remainingText.TextSize = 15
	d.remainingText.TextStyle = fyne.TextStyle{Monospace: true}
	d.remainingText.Alignment = fyne.TextAlignTrailing

	// Transport buttons
	d.playBtn = components.NewDJButton("PLAY", boomtheme.ColorPlayActive, func() {
		bus.Publish(event.Event{Topic: event.TopicDeck, Action: event.ActionPlayPause, DeckID: deckID})
	})
	// CUE button: press/release for hold-to-preview, right-click to remove the cue point.
	d.cueBtn = components.NewDJButton("CUE", boomtheme.ColorCueActive, nil)
	d.cueBtn.OnPressed = func() {
		bus.Publish(event.Event{
			Topic: event.TopicDeck, Action: event.ActionCue,
			DeckID: deckID, Pressed: true,
		})
	}
	d.cueBtn.OnReleased = func() {
		bus.Publish(event.Event{
			Topic: event.TopicDeck, Action: event.ActionCue,
			DeckID: deckID, Pressed: false,
		})
	}
	d.cueBtn.OnSecondary = func() {
		bus.Publish(event.Event{
			Topic: event.TopicDeck, Action: event.ActionCueDelete,
			DeckID: deckID,
		})
	}
	d.syncBtn = components.NewDJButton("SYNC", boomtheme.ColorSyncActive, func() {
		bus.Publish(event.Event{Topic: event.TopicDeck, Action: event.ActionSync, DeckID: deckID})
	})

	// Compact loop controls — fire per-deck; no target switching needed.
	d.loopInBtn = components.NewDJButton("IN", boomtheme.ColorCueActive, func() {
		log.Printf("deck%d UI: loop_in click", deckID)
		bus.Publish(event.Event{Topic: event.TopicDeck, Action: event.ActionLoopIn, DeckID: deckID})
	})
	d.loopOutBtn = components.NewDJButton("OUT", boomtheme.ColorCueActive, func() {
		log.Printf("deck%d UI: loop_out click", deckID)
		bus.Publish(event.Event{Topic: event.TopicDeck, Action: event.ActionLoopOut, DeckID: deckID})
	})
	d.reloopBtn = components.NewDJButton("RELOOP", boomtheme.ColorCueActive, func() {
		log.Printf("deck%d UI: loop_toggle click", deckID)
		bus.Publish(event.Event{Topic: event.TopicDeck, Action: event.ActionLoopToggle, DeckID: deckID})
	})
	d.loopHalveBtn = components.NewDJButton("1/2×", boomtheme.ColorLabel, func() {
		log.Printf("deck%d UI: loop_halve click", deckID)
		bus.Publish(event.Event{Topic: event.TopicDeck, Action: event.ActionLoopHalve, DeckID: deckID})
	})
	d.loopDoubleBtn = components.NewDJButton("2×", boomtheme.ColorLabel, func() {
		log.Printf("deck%d UI: loop_double click", deckID)
		bus.Publish(event.Event{Topic: event.TopicDeck, Action: event.ActionLoopDouble, DeckID: deckID})
	})

	// Knobs
	d.volKnob = components.NewKnob("VOL", 0.8, deckColor, func(v float64) {
		bus.Publish(event.Event{Topic: event.TopicDeck, Action: event.ActionVolumeChange, DeckID: deckID, Value: v})
	})
	d.hiKnob = components.NewKnob("HI", 0.5, boomtheme.ColorLabel, func(v float64) {
		bus.Publish(event.Event{Topic: event.TopicDeck, Action: event.ActionEQHigh, DeckID: deckID, Value: v})
	})
	d.midKnob = components.NewKnob("MID", 0.5, boomtheme.ColorLabel, func(v float64) {
		bus.Publish(event.Event{Topic: event.TopicDeck, Action: event.ActionEQMid, DeckID: deckID, Value: v})
	})
	d.loKnob = components.NewKnob("LO", 0.5, boomtheme.ColorLabel, func(v float64) {
		bus.Publish(event.Event{Topic: event.TopicDeck, Action: event.ActionEQLow, DeckID: deckID, Value: v})
	})

	// --- Layout ---

	// Header row
	infoLeft := container.NewVBox(d.deckLabel, d.trackTitle, d.trackArtist)

	// Header right side: big BPM count on top, then a compact row beneath
	// containing the -/+ nudge buttons, the "BPM" label, and the ghosted
	// original BPM (clickable to reset).
	nudgeBtnSize := fyne.NewSize(22, 18)
	bpmSubRow := container.NewHBox(
		layout.NewSpacer(),
		d.bpmOrigTap,
		container.New(layout.NewGridWrapLayout(nudgeBtnSize), d.bpmMinusBtn),
		container.New(layout.NewGridWrapLayout(nudgeBtnSize), d.bpmPlusBtn),
		d.bpmLabel,
	)
	bpmRight := container.NewVBox(layout.NewSpacer(), d.bpmText, bpmSubRow)
	header := container.NewBorder(nil, nil, infoLeft, bpmRight)

	// Time row — current / total on the left, remaining time on the right
	timeLeft := container.NewHBox(d.timeText, d.durText)
	timeRow := container.NewBorder(nil, nil, timeLeft, d.remainingText)

	// Separator
	sep := canvas.NewRectangle(boomtheme.ColorSeparator)
	sep.SetMinSize(fyne.NewSize(0, 0.5))

	// Transport buttons - fixed size in a row
	btnSize := fyne.NewSize(72, 32)
	transportRow := container.NewHBox(
		container.New(layout.NewGridWrapLayout(btnSize), d.playBtn),
		container.New(layout.NewGridWrapLayout(btnSize), d.cueBtn),
		container.New(layout.NewGridWrapLayout(btnSize), d.syncBtn),
	)

	// Compact loop row — five small buttons below transport. The cell size
	// stays at or above DJButton.MinSize (60×28) so GridWrap doesn't clip
	// hit-testing on the smaller ones.
	loopBtnSize := fyne.NewSize(60, 28)
	loopRow := container.NewHBox(
		container.New(layout.NewGridWrapLayout(loopBtnSize), d.loopInBtn),
		container.New(layout.NewGridWrapLayout(loopBtnSize), d.loopOutBtn),
		container.New(layout.NewGridWrapLayout(loopBtnSize), d.reloopBtn),
		container.New(layout.NewGridWrapLayout(loopBtnSize), d.loopHalveBtn),
		container.New(layout.NewGridWrapLayout(loopBtnSize), d.loopDoubleBtn),
	)

	buttonsRow := container.NewVBox(transportRow, loopRow)

	// Knobs in a row with fixed size
	knobSize := fyne.NewSize(54, 72)
	knobsRow := container.NewHBox(
		container.New(layout.NewGridWrapLayout(knobSize), d.volKnob),
		container.New(layout.NewGridWrapLayout(knobSize), d.hiKnob),
		container.New(layout.NewGridWrapLayout(knobSize), d.midKnob),
		container.New(layout.NewGridWrapLayout(knobSize), d.loKnob),
	)

	// Controls section: buttons left, knobs right
	controlsRow := container.NewBorder(nil, nil, buttonsRow, knobsRow)

	// Use VSplit so waveform and controls both get enough space
	// Top part: header + waveform (expandable)
	// Bottom part: time + buttons/knobs (fixed height)
	topSection := container.NewBorder(header, nil, nil, nil, d.waveform)
	bottomSection := container.NewVBox(timeRow, sep, controlsRow)

	d.content = container.NewBorder(
		nil,
		bottomSection,
		nil, nil,
		topSection,
	)

	d.ExtendBaseWidget(d)
	return d
}

func (d *DeckView) UpdatePosition(pos float64) {
	d.waveform.SetPosition(pos)
	if d.duration <= 0 {
		return
	}
	if pos < 0 {
		pos = 0
	}
	if pos > 1 {
		pos = 1
	}
	current := time.Duration(float64(d.duration) * pos)
	remaining := d.duration - current
	if remaining < 0 {
		remaining = 0
	}
	// formatDuration is 1-Hz granular (M:SS). The position event fires at
	// ~30 Hz, so most updates produce identical strings — skip the Refresh
	// and the closure allocation when neither text changed.
	newCurrent := formatDuration(current)
	newRemaining := "-" + formatDuration(remaining)
	if newCurrent == d.timeText.Text && newRemaining == d.remainingText.Text {
		return
	}
	d.timeText.Text = newCurrent
	d.remainingText.Text = newRemaining
	fyne.Do(func() {
		d.timeText.Refresh()
		d.remainingText.Refresh()
	})
}

// UpdateCuePoint refreshes the visual cue marker on the waveform.
// Pass a negative value to hide it.
func (d *DeckView) UpdateCuePoint(pos float64) {
	d.waveform.SetCuePoint(pos)
}

// UpdateLoopState forwards engine loop state changes to the waveform overlay
// and the compact loop buttons. The RELOOP button lights up while the loop
// is wrapping and shows the beat count as its label (e.g. "4", "8", "1/2")
// so the user has a glanceable read of the active loop length.
func (d *DeckView) UpdateLoopState(state *event.LoopState) {
	if state == nil {
		d.waveform.SetLoopState(-1, -1, 0, false)
		d.reloopBtn.SetActive(false)
		d.reloopBtn.SetText("RELOOP")
		return
	}
	d.waveform.SetLoopState(state.Start, state.End, state.Beats, state.Active)
	d.reloopBtn.SetActive(state.Active)

	hasLoop := state.Start >= 0 && state.End > state.Start
	if hasLoop && state.Beats > 0 {
		d.reloopBtn.SetText(compactBeatLabel(state.Beats))
	} else {
		d.reloopBtn.SetText("RELOOP")
	}
}

// compactBeatLabel renders a beat count as a terse label that fits inside
// the compact reloop button ("4", "1/2", "1/32", etc.).
func compactBeatLabel(beats float64) string {
	switch {
	case beats >= 0.999:
		if beats == float64(int(beats)) {
			return fmt.Sprintf("%d", int(beats))
		}
		return fmt.Sprintf("%.1f", beats)
	case beats >= 0.49 && beats <= 0.51:
		return "1/2"
	case beats >= 0.24 && beats <= 0.26:
		return "1/4"
	case beats >= 0.124 && beats <= 0.126:
		return "1/8"
	case beats >= 0.062 && beats <= 0.063:
		return "1/16"
	case beats >= 0.031 && beats <= 0.032:
		return "1/32"
	default:
		return fmt.Sprintf("%.2f", beats)
	}
}

func (d *DeckView) SetWaveformData(data *audio.WaveformData) {
	d.waveform.SetFrequencyPeaks(data.PeaksLow, data.PeaksMid, data.PeaksHigh)
	if data.Duration > 0 {
		d.duration = data.Duration
		fyne.Do(func() {
			d.durText.Text = fmt.Sprintf("/ %s", formatDuration(d.duration))
			d.durText.Refresh()
			d.remainingText.Text = "-" + formatDuration(d.duration)
			d.remainingText.Refresh()
		})
	}
}

func (d *DeckView) SetTrack(track *model.Track) {
	d.tempoRatio = 1.0
	if track == nil {
		d.currentTrackID = ""
		d.origBPM = 0
		d.duration = 0
		d.trackTitle.Text = "No Track Loaded"
		d.trackArtist.Text = ""
		d.bpmText.Text = "---"
		d.bpmOrigText.Text = ""
		d.timeText.Text = "0:00"
		d.durText.Text = "/ 0:00"
		d.remainingText.Text = "-0:00"
	} else {
		d.currentTrackID = track.ID
		d.origBPM = track.BPM
		d.duration = track.Duration
		d.trackTitle.Text = track.Title
		if d.trackTitle.Text == "" {
			d.trackTitle.Text = "Unknown"
		}
		d.trackArtist.Text = track.Artist
		if track.BPM > 0 {
			d.bpmText.Text = fmt.Sprintf("%.1f", track.BPM)
		} else {
			d.bpmText.Text = "---"
		}
		d.bpmOrigText.Text = ""
		d.timeText.Text = "0:00"
		d.durText.Text = fmt.Sprintf("/ %s", formatDuration(track.Duration))
		if track.Duration > 0 {
			d.remainingText.Text = "-" + formatDuration(track.Duration)
		} else {
			d.remainingText.Text = "-0:00"
		}
	}
	fyne.Do(func() {
		d.trackTitle.Refresh()
		d.trackArtist.Refresh()
		d.bpmText.Refresh()
		d.bpmOrigText.Refresh()
		d.timeText.Refresh()
		d.durText.Refresh()
		d.remainingText.Refresh()
	})
}

// UpdateAnalysis updates the BPM display when analysis completes for the loaded track.
func (d *DeckView) UpdateAnalysis(trackID string, bpm float64, key string) {
	if d.currentTrackID != trackID {
		return
	}
	if bpm <= 0 {
		return
	}
	d.origBPM = bpm
	d.refreshBPMDisplay()
}

// UpdateVolume sets the volume knob from an external source (MIDI).
func (d *DeckView) UpdateVolume(v float64) {
	d.volKnob.SetValue(v)
}

// UpdateGain is a no-op on the deck view now that gain lives on the mixer
// channel fader. Kept so the window event dispatcher can still call it
// unconditionally.
func (d *DeckView) UpdateGain(v float64) {}

// UpdateTempo is called when an external source (MIDI) changes the deck tempo.
// The bus value uses the same 0..1 → 0.5..2.0 mapping as ActionTempoChange.
func (d *DeckView) UpdateTempo(v float64) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	d.tempoRatio = 0.5 + v*1.5
	d.refreshBPMDisplay()
}

// nudgeBPM shifts the effective BPM by deltaBPM and republishes the new
// tempo via the standard ActionTempoChange value (0..1 → 0.5..2.0 ratio).
// A nudge only makes sense once we know the track's original BPM.
func (d *DeckView) nudgeBPM(deltaBPM float64) {
	if d.origBPM <= 0 {
		return
	}
	newBPM := d.origBPM*d.tempoRatio + deltaBPM
	if newBPM < d.origBPM*0.5 {
		newBPM = d.origBPM * 0.5
	}
	if newBPM > d.origBPM*2.0 {
		newBPM = d.origBPM * 2.0
	}
	d.tempoRatio = newBPM / d.origBPM
	value := (d.tempoRatio - 0.5) / 1.5
	d.refreshBPMDisplay()
	d.bus.Publish(event.Event{
		Topic: event.TopicDeck, Action: event.ActionTempoChange,
		DeckID: d.deckID, Value: value,
	})
}

// refreshBPMDisplay paints the current BPM in the header, showing the
// original analyzed BPM as a small ghost label whenever the user (or MIDI)
// has nudged the tempo away from 1.0×.
func (d *DeckView) refreshBPMDisplay() {
	if d.origBPM <= 0 {
		d.bpmText.Text = "---"
		d.bpmOrigText.Text = ""
	} else {
		current := d.origBPM * d.tempoRatio
		d.bpmText.Text = fmt.Sprintf("%.1f", current)
		if nearlyEqual(current, d.origBPM) {
			d.bpmOrigText.Text = ""
		} else {
			d.bpmOrigText.Text = fmt.Sprintf("%.1f", d.origBPM)
		}
	}
	fyne.Do(func() {
		d.bpmText.Refresh()
		d.bpmOrigText.Refresh()
	})
}

func nearlyEqual(a, b float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < 0.05
}

// resetTempo restores the original analyzed BPM (tempo ratio 1.0) and
// republishes the tempo via the standard event-bus mapping. Invoked when
// the user clicks on the ghosted original BPM.
func (d *DeckView) resetTempo() {
	if d.origBPM <= 0 {
		return
	}
	d.tempoRatio = 1.0
	value := (1.0 - 0.5) / 1.5
	d.refreshBPMDisplay()
	d.bus.Publish(event.Event{
		Topic: event.TopicDeck, Action: event.ActionTempoChange,
		DeckID: d.deckID, Value: value,
	})
}

// tappableText wraps a canvas.Text in a tappable widget so it can act as a
// tiny click target. Fyne's canvas primitives don't implement Tappable on
// their own, and pulling in widget.Button would come with unwanted padding
// and a full button frame.
type tappableText struct {
	widget.BaseWidget
	text  *canvas.Text
	onTap func()
}

var _ fyne.Tappable = (*tappableText)(nil)

func newTappableText(text *canvas.Text, onTap func()) *tappableText {
	t := &tappableText{text: text, onTap: onTap}
	t.ExtendBaseWidget(t)
	return t
}

func (t *tappableText) Tapped(_ *fyne.PointEvent) {
	if t.onTap != nil {
		t.onTap()
	}
}

func (t *tappableText) MinSize() fyne.Size {
	return t.text.MinSize()
}

func (t *tappableText) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.text)
}

// UpdateEQHigh sets the HI EQ knob from an external source (MIDI).
func (d *DeckView) UpdateEQHigh(v float64) {
	d.hiKnob.SetValue(v)
}

// UpdateEQMid sets the MID EQ knob from an external source (MIDI).
func (d *DeckView) UpdateEQMid(v float64) {
	d.midKnob.SetValue(v)
}

// UpdateEQLow sets the LO EQ knob from an external source (MIDI).
func (d *DeckView) UpdateEQLow(v float64) {
	d.loKnob.SetValue(v)
}

func (d *DeckView) SetPlaying(playing bool) {
	d.playBtn.SetActive(playing)
	if playing {
		d.playBtn.SetText("PAUSE")
	} else {
		d.playBtn.SetText("PLAY")
	}
}

func (d *DeckView) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(d.content)
}

func formatDuration(d time.Duration) string {
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%d:%02d", m, s)
}
