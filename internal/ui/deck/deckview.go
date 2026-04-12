package deck

import (
	"fmt"
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

	volKnob   *components.Knob
	gainKnob  *components.Knob
	tempoKnob *components.Knob
	hiKnob    *components.Knob
	midKnob   *components.Knob
	loKnob    *components.Knob

	deckLabel   *canvas.Text
	trackTitle  *canvas.Text
	trackArtist *canvas.Text
	bpmText     *canvas.Text
	bpmLabel    *canvas.Text
	timeText    *canvas.Text
	durText     *canvas.Text

	content *fyne.Container
}

func NewDeckView(deckID int, bus *event.Bus) *DeckView {
	d := &DeckView{deckID: deckID, bus: bus}
	deckColor := boomtheme.DeckColor(deckID)

	d.waveform = NewWaveformWidget(deckID)

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

	d.bpmLabel = canvas.NewText("BPM", boomtheme.ColorLabelTertiary)
	d.bpmLabel.TextSize = 9
	d.bpmLabel.Alignment = fyne.TextAlignTrailing

	d.timeText = canvas.NewText("0:00", boomtheme.ColorLabel)
	d.timeText.TextSize = 15
	d.timeText.TextStyle = fyne.TextStyle{Monospace: true}

	d.durText = canvas.NewText("/ 0:00", boomtheme.ColorLabelTertiary)
	d.durText.TextSize = 11
	d.durText.TextStyle = fyne.TextStyle{Monospace: true}

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

	// Knobs
	d.volKnob = components.NewKnob("VOL", 0.8, deckColor, func(v float64) {
		bus.Publish(event.Event{Topic: event.TopicDeck, Action: event.ActionVolumeChange, DeckID: deckID, Value: v})
	})
	d.gainKnob = components.NewKnob("GAIN", 0.5, deckColor, func(v float64) {
		bus.Publish(event.Event{Topic: event.TopicDeck, Action: event.ActionGainChange, DeckID: deckID, Value: v})
	})
	d.tempoKnob = components.NewKnob("TEMPO", 0.5, deckColor, func(v float64) {
		bus.Publish(event.Event{Topic: event.TopicDeck, Action: event.ActionTempoChange, DeckID: deckID, Value: v})
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
	bpmRight := container.NewVBox(layout.NewSpacer(), d.bpmText, d.bpmLabel)
	header := container.NewBorder(nil, nil, infoLeft, bpmRight)

	// Time row
	timeRow := container.NewHBox(d.timeText, d.durText)

	// Separator
	sep := canvas.NewRectangle(boomtheme.ColorSeparator)
	sep.SetMinSize(fyne.NewSize(0, 0.5))

	// Transport buttons - fixed size in a row
	btnSize := fyne.NewSize(72, 32)
	buttonsRow := container.NewHBox(
		container.New(layout.NewGridWrapLayout(btnSize), d.playBtn),
		container.New(layout.NewGridWrapLayout(btnSize), d.cueBtn),
		container.New(layout.NewGridWrapLayout(btnSize), d.syncBtn),
	)

	// Knobs in a row with fixed size
	knobSize := fyne.NewSize(54, 72)
	knobsRow := container.NewHBox(
		container.New(layout.NewGridWrapLayout(knobSize), d.gainKnob),
		container.New(layout.NewGridWrapLayout(knobSize), d.volKnob),
		container.New(layout.NewGridWrapLayout(knobSize), d.tempoKnob),
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
}

// UpdateCuePoint refreshes the visual cue marker on the waveform.
// Pass a negative value to hide it.
func (d *DeckView) UpdateCuePoint(pos float64) {
	d.waveform.SetCuePoint(pos)
}

func (d *DeckView) SetWaveformData(data *audio.WaveformData) {
	d.waveform.SetFrequencyPeaks(data.PeaksLow, data.PeaksMid, data.PeaksHigh)
}

func (d *DeckView) SetTrack(track *model.Track) {
	if track == nil {
		d.currentTrackID = ""
		d.trackTitle.Text = "No Track Loaded"
		d.trackArtist.Text = ""
		d.bpmText.Text = "---"
		d.timeText.Text = "0:00"
		d.durText.Text = "/ 0:00"
	} else {
		d.currentTrackID = track.ID
		d.trackTitle.Text = track.Title
		if d.trackTitle.Text == "" {
			d.trackTitle.Text = "Unknown"
		}
		d.trackArtist.Text = track.Artist
		if track.BPM > 0 {
			d.bpmText.Text = fmt.Sprintf("%.0f", track.BPM)
		} else {
			d.bpmText.Text = "---"
		}
		d.timeText.Text = "0:00"
		d.durText.Text = fmt.Sprintf("/ %s", formatDuration(track.Duration))
	}
	fyne.Do(func() {
		d.trackTitle.Refresh()
		d.trackArtist.Refresh()
		d.bpmText.Refresh()
		d.timeText.Refresh()
		d.durText.Refresh()
	})
}

// UpdateAnalysis updates the BPM display when analysis completes for the loaded track.
func (d *DeckView) UpdateAnalysis(trackID string, bpm float64, key string) {
	if d.currentTrackID != trackID {
		return
	}
	fyne.Do(func() {
		if bpm > 0 {
			d.bpmText.Text = fmt.Sprintf("%.0f", bpm)
			d.bpmText.Refresh()
		}
	})
}

// UpdateVolume sets the volume knob from an external source (MIDI).
func (d *DeckView) UpdateVolume(v float64) {
	d.volKnob.SetValue(v)
}

// UpdateGain sets the gain/trim knob from an external source (MIDI).
func (d *DeckView) UpdateGain(v float64) {
	d.gainKnob.SetValue(v)
}

// UpdateTempo sets the tempo knob from an external source (MIDI).
func (d *DeckView) UpdateTempo(v float64) {
	d.tempoKnob.SetValue(v)
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

func (d *DeckView) UpdateTime(current, total time.Duration) {
	d.timeText.Text = formatDuration(current)
	d.durText.Text = fmt.Sprintf("/ %s", formatDuration(total))
	d.timeText.Refresh()
	d.durText.Refresh()
}

func (d *DeckView) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(d.content)
}

func formatDuration(d time.Duration) string {
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%d:%02d", m, s)
}
