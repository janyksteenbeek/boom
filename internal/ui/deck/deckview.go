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

	loopInBtn     *components.DJButton
	loopOutBtn    *components.DJButton
	reloopBtn     *components.DJButton
	loopHalveBtn  *components.DJButton
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
	tempoPctText  *canvas.Text
	keyText       *canvas.Text
	keyBadge      *fyne.Container
	timeText      *canvas.Text
	durText       *canvas.Text
	remainingText *canvas.Text

	// Composed sub-rows exposed via accessors. Keeping them as fields lets
	// alternative layouts (e.g. mini-mode) pick individual rows rather than
	// embedding the whole DeckView.
	headerRow    fyne.CanvasObject
	timeRow      fyne.CanvasObject
	transportRow fyne.CanvasObject
	loopRow      fyne.CanvasObject
	knobsRow     fyne.CanvasObject

	// origBPM is the analyzed BPM as stored on the track. It's shown in
	// small text next to the current BPM whenever the user has nudged the
	// tempo away from that value.
	origBPM    float64
	tempoRatio float64       // current tempo multiplier, 1.0 = original
	duration   time.Duration // track duration once known

	// showRemainingPrimary flips the prominent vs secondary reading in the
	// time row: when true, the left (large) slot shows remaining time and
	// the right (smaller) slot shows elapsed — the reverse of the default.
	// Tapping the time row toggles it.
	showRemainingPrimary bool

	content *fyne.Container
}

// bpmNudgeStep is the BPM adjustment applied per click on the -/+ buttons.
// Small enough to feel like a fine nudge, large enough to be useful without
// needing to spam the button.
const bpmNudgeStep = 0.1

// Options tunes which DeckView sub-elements are visible. Mini-mode uses
// Compact to drop the redundant "DECK N" label, the on-screen BPM
// nudge buttons (hardware covers that), and the "BPM" text label so
// the header breathes on an 800-px-wide screen.
type Options struct {
	Compact bool
}

// NewDeckView constructs the default (desktop) deck view.
func NewDeckView(deckID int, bus *event.Bus) *DeckView {
	return NewDeckViewWithOptions(deckID, bus, Options{})
}

// NewDeckViewWithOptions constructs a deck view with tunable chrome.
func NewDeckViewWithOptions(deckID int, bus *event.Bus, opts Options) *DeckView {
	d := &DeckView{deckID: deckID, bus: bus, tempoRatio: 1.0}
	compact := opts.Compact
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
	if compact {
		d.bpmText.TextSize = 20
	} else {
		d.bpmText.TextSize = 26
	}
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

	// Tempo-nudge percentage (e.g. "+2.4%"); shown as a compact ghost label
	// next to the BPM. Empty when the deck is at 1.0x.
	d.tempoPctText = canvas.NewText("", boomtheme.ColorLabelTertiary)
	d.tempoPctText.TextSize = 10
	d.tempoPctText.TextStyle = fyne.TextStyle{Monospace: true}
	d.tempoPctText.Alignment = fyne.TextAlignTrailing

	// Musical key badge (e.g. "5A" in Camelot, "Am" in traditional).
	// Rendered as a small tinted pill next to the BPM so harmonic-mixing
	// info is glanceable across the room. Empty when analysis hasn't
	// produced a key yet.
	d.keyText = canvas.NewText("", boomtheme.ColorLabel)
	d.keyText.TextSize = 10
	d.keyText.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
	d.keyText.Alignment = fyne.TextAlignCenter
	keyBg := canvas.NewRectangle(boomtheme.ColorBackgroundTertiary)
	keyBg.CornerRadius = 3
	d.keyBadge = container.NewStack(keyBg, container.NewPadded(d.keyText))
	d.keyBadge.Hide()

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

	// Header left. Compact mode drops the redundant "DECK N" label —
	// the mini-card's accent strip already encodes that — so the title
	// gets top billing and there's room to breathe.
	var infoLeft *fyne.Container
	if compact {
		infoLeft = container.NewVBox(d.trackTitle, d.trackArtist)
	} else {
		infoLeft = container.NewVBox(d.deckLabel, d.trackTitle, d.trackArtist)
	}

	// Header right side: big BPM count on top, then a compact row beneath
	// containing the -/+ nudge buttons, the "BPM" label, and the ghosted
	// original BPM (clickable to reset). Compact mode drops the nudge
	// buttons and the "BPM" text label (hardware-driven pitch fader
	// covers that).
	nudgeBtnSize := fyne.NewSize(22, 18)
	bpmSubRow := container.NewHBox(layout.NewSpacer(), d.keyBadge, d.bpmOrigTap, d.tempoPctText)
	if !compact {
		bpmSubRow.Add(container.New(layout.NewGridWrapLayout(nudgeBtnSize), d.bpmMinusBtn))
		bpmSubRow.Add(container.New(layout.NewGridWrapLayout(nudgeBtnSize), d.bpmPlusBtn))
		bpmSubRow.Add(d.bpmLabel)
	}
	bpmRight := container.NewVBox(layout.NewSpacer(), d.bpmText, bpmSubRow)
	d.headerRow = container.NewBorder(nil, nil, infoLeft, bpmRight)

	// Time row — current / total on the left, remaining time on the right.
	// Wrapped in a tappable so clicking the row toggles which reading gets
	// the prominent slot (elapsed vs remaining). Useful on small / mini
	// layouts where the user wants the remaining time at a glance.
	timeLeft := container.NewHBox(d.timeText, d.durText)
	timeBorder := container.NewBorder(nil, nil, timeLeft, d.remainingText)
	d.timeRow = newTappableContainer(timeBorder, func() { d.toggleTimeMode() })

	// Separator
	sep := canvas.NewRectangle(boomtheme.ColorSeparator)
	sep.SetMinSize(fyne.NewSize(0, 0.5))

	// Transport buttons - fixed size in a row
	btnSize := fyne.NewSize(72, 32)
	d.transportRow = container.NewHBox(
		container.New(layout.NewGridWrapLayout(btnSize), d.playBtn),
		container.New(layout.NewGridWrapLayout(btnSize), d.cueBtn),
		container.New(layout.NewGridWrapLayout(btnSize), d.syncBtn),
	)

	// Compact loop row — five small buttons below transport. The cell size
	// stays at or above DJButton.MinSize (60×28) so GridWrap doesn't clip
	// hit-testing on the smaller ones.
	loopBtnSize := fyne.NewSize(60, 28)
	d.loopRow = container.NewHBox(
		container.New(layout.NewGridWrapLayout(loopBtnSize), d.loopInBtn),
		container.New(layout.NewGridWrapLayout(loopBtnSize), d.loopOutBtn),
		container.New(layout.NewGridWrapLayout(loopBtnSize), d.reloopBtn),
		container.New(layout.NewGridWrapLayout(loopBtnSize), d.loopHalveBtn),
		container.New(layout.NewGridWrapLayout(loopBtnSize), d.loopDoubleBtn),
	)

	buttonsRow := container.NewVBox(d.transportRow, d.loopRow)

	// Knobs in a row with fixed size
	knobSize := fyne.NewSize(54, 72)
	d.knobsRow = container.NewHBox(
		container.New(layout.NewGridWrapLayout(knobSize), d.volKnob),
		container.New(layout.NewGridWrapLayout(knobSize), d.hiKnob),
		container.New(layout.NewGridWrapLayout(knobSize), d.midKnob),
		container.New(layout.NewGridWrapLayout(knobSize), d.loKnob),
	)

	// Controls section: buttons left, knobs right
	controlsRow := container.NewBorder(nil, nil, buttonsRow, d.knobsRow)

	// Use VSplit so waveform and controls both get enough space
	// Top part: header + waveform (expandable)
	// Bottom part: time + buttons/knobs (fixed height)
	topSection := container.NewBorder(d.headerRow, nil, nil, nil, d.waveform)
	bottomSection := container.NewVBox(d.timeRow, sep, controlsRow)

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
	var newPrimary, newSecondary string
	if d.showRemainingPrimary {
		newPrimary = "-" + formatDuration(remaining)
		newSecondary = formatDuration(current)
	} else {
		newPrimary = formatDuration(current)
		newSecondary = "-" + formatDuration(remaining)
	}
	if newPrimary == d.timeText.Text && newSecondary == d.remainingText.Text {
		return
	}
	d.timeText.Text = newPrimary
	d.remainingText.Text = newSecondary
	fyne.Do(func() {
		d.timeText.Refresh()
		d.remainingText.Refresh()
	})
}

// toggleTimeMode flips the time row between "big = elapsed" and "big =
// remaining". The secondary slot always shows whichever reading isn't
// primary. Triggered by tapping the time row.
func (d *DeckView) toggleTimeMode() {
	d.showRemainingPrimary = !d.showRemainingPrimary
	if d.duration <= 0 {
		if d.showRemainingPrimary {
			d.timeText.Text = "-0:00"
			d.remainingText.Text = "0:00"
		} else {
			d.timeText.Text = "0:00"
			d.remainingText.Text = "-0:00"
		}
		fyne.Do(func() {
			d.timeText.Refresh()
			d.remainingText.Refresh()
		})
		return
	}
	d.UpdatePosition(d.waveform.PlayPosition())
}

// Header returns the title/artist/BPM row as a standalone canvas object so
// alternative layouts (e.g. mini-mode) can compose it without pulling in
// the whole DeckView content tree.
func (d *DeckView) Header() fyne.CanvasObject { return d.headerRow }

// TimeRow returns the elapsed/remaining row.
func (d *DeckView) TimeRow() fyne.CanvasObject { return d.timeRow }

// TransportRow returns the PLAY/CUE/SYNC row.
func (d *DeckView) TransportRow() fyne.CanvasObject { return d.transportRow }

// LoopRow returns the compact IN/OUT/RELOOP/½/2× row.
func (d *DeckView) LoopRow() fyne.CanvasObject { return d.loopRow }

// KnobsRow returns the VOL/HI/MID/LO knob row.
func (d *DeckView) KnobsRow() fyne.CanvasObject { return d.knobsRow }

// WaveformWidget returns the per-deck full-track overview widget.
func (d *DeckView) WaveformWidget() *WaveformWidget { return d.waveform }

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
	if track != nil {
		d.setKey(track.Key)
	} else {
		d.setKey("")
	}
	if track == nil {
		d.currentTrackID = ""
		d.origBPM = 0
		d.duration = 0
		d.trackTitle.Text = "No Track Loaded"
		d.trackArtist.Text = ""
		d.bpmText.Text = "---"
		d.bpmOrigText.Text = ""
		d.tempoPctText.Text = ""
		if d.showRemainingPrimary {
			d.timeText.Text = "-0:00"
			d.remainingText.Text = "0:00"
		} else {
			d.timeText.Text = "0:00"
			d.remainingText.Text = "-0:00"
		}
		d.durText.Text = "/ 0:00"
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
		d.tempoPctText.Text = ""
		d.durText.Text = fmt.Sprintf("/ %s", formatDuration(track.Duration))
		if d.showRemainingPrimary {
			if track.Duration > 0 {
				d.timeText.Text = "-" + formatDuration(track.Duration)
			} else {
				d.timeText.Text = "-0:00"
			}
			d.remainingText.Text = "0:00"
		} else {
			d.timeText.Text = "0:00"
			if track.Duration > 0 {
				d.remainingText.Text = "-" + formatDuration(track.Duration)
			} else {
				d.remainingText.Text = "-0:00"
			}
		}
	}
	fyne.Do(func() {
		d.trackTitle.Refresh()
		d.trackArtist.Refresh()
		d.bpmText.Refresh()
		d.bpmOrigText.Refresh()
		d.tempoPctText.Refresh()
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
	if bpm > 0 {
		d.origBPM = bpm
		d.refreshBPMDisplay()
	}
	d.setKey(key)
}

// setKey renders (or hides) the musical-key badge. Updates are routed
// onto the Fyne thread so callers don't have to marshal themselves.
func (d *DeckView) setKey(key string) {
	if key == "" {
		d.keyText.Text = ""
		fyne.Do(func() {
			d.keyText.Refresh()
			d.keyBadge.Hide()
		})
		return
	}
	d.keyText.Text = key
	fyne.Do(func() {
		d.keyText.Refresh()
		d.keyBadge.Show()
	})
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
		d.tempoPctText.Text = ""
	} else {
		current := d.origBPM * d.tempoRatio
		d.bpmText.Text = fmt.Sprintf("%.1f", current)
		if nearlyEqual(current, d.origBPM) {
			d.bpmOrigText.Text = ""
			d.tempoPctText.Text = ""
		} else {
			d.bpmOrigText.Text = fmt.Sprintf("%.1f", d.origBPM)
			pct := (d.tempoRatio - 1.0) * 100.0
			if pct >= 0 {
				d.tempoPctText.Text = fmt.Sprintf("+%.1f%%", pct)
			} else {
				d.tempoPctText.Text = fmt.Sprintf("%.1f%%", pct)
			}
		}
	}
	fyne.Do(func() {
		d.bpmText.Refresh()
		d.bpmOrigText.Refresh()
		d.tempoPctText.Refresh()
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

// tappableContainer wraps any CanvasObject in a tappable widget. Used to
// make a whole row (e.g. the time display) click-toggle without changing
// the layout of its children.
type tappableContainer struct {
	widget.BaseWidget
	child fyne.CanvasObject
	onTap func()
}

var _ fyne.Tappable = (*tappableContainer)(nil)

func newTappableContainer(child fyne.CanvasObject, onTap func()) *tappableContainer {
	t := &tappableContainer{child: child, onTap: onTap}
	t.ExtendBaseWidget(t)
	return t
}

func (t *tappableContainer) Tapped(_ *fyne.PointEvent) {
	if t.onTap != nil {
		t.onTap()
	}
}

func (t *tappableContainer) MinSize() fyne.Size {
	return t.child.MinSize()
}

func (t *tappableContainer) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.child)
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
