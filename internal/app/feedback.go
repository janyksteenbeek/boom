package app

import (
	"math"
	"time"

	"github.com/janyksteenbeek/boom/internal/audio"
	"github.com/janyksteenbeek/boom/internal/event"
)

// ledFeedbackLoop drives the per-deck PLAY and CUE LEDs with software blink:
//
//	no track             → both OFF
//	playing              → PLAY solid, CUE OFF
//	cue_preview          → PLAY OFF,   CUE solid
//	paused at eff. cue   → PLAY blink, CUE solid
//	paused away from cue → PLAY blink, CUE blink
//
// PLAY blinks at 1 Hz (~500 ms period) when the deck is paused — this is the
// "ready to play" indicator. CUE blinks at 2 Hz (~250 ms period) when paused
// away from the cue — a faster "press to (re)cue here" invitation that's
// visually distinct from PLAY. The loop polls at 50 ms and only sends MIDI on
// edge transitions to keep controller traffic low.
func (a *App) ledFeedbackLoop() {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	var lastPlay, lastCue [audio.NumDecks]bool
	var lastLoopIn, lastLoopOut [audio.NumDecks]bool
	var initialized [audio.NumDecks]bool

	for {
		select {
		case <-a.stopCh:
			return
		case <-ticker.C:
			ms := time.Now().UnixMilli()
			playBlink := (ms/500)%2 == 0 // 1 Hz
			cueBlink := (ms/250)%2 == 0  // 2 Hz
			for i := 0; i < audio.NumDecks; i++ {
				deck := a.engine.Deck(i + 1)
				if deck == nil {
					continue
				}
				hasTrack := deck.Track() != nil
				playing := deck.IsPlaying()
				preview := deck.CuePreview()
				atCue := deck.AtEffectiveCue()

				var playOn, cueOn bool
				switch {
				case !hasTrack:
					playOn = false
				case preview:
					playOn = false
				case playing:
					playOn = true
				default:
					playOn = playBlink
				}

				switch {
				case !hasTrack:
					cueOn = false
				case preview:
					cueOn = true
				case playing:
					cueOn = false
				case atCue:
					cueOn = true
				default:
					cueOn = cueBlink
				}

				if !initialized[i] || playOn != lastPlay[i] {
					lastPlay[i] = playOn
					a.ledMgr.Update("play_pause", i+1, playOn)
				}
				if !initialized[i] || cueOn != lastCue[i] {
					lastCue[i] = cueOn
					a.ledMgr.Update("cue", i+1, cueOn)
				}

				// Loop IN: solid when a full loop is stored/active, flashing
				// while the user is "recording" (start set but no end yet),
				// off otherwise. Loop OUT: solid only while the loop is
				// actually wrapping playback.
				var loopInOn bool
				switch {
				case !hasTrack:
					loopInOn = false
				case deck.HasLoop():
					loopInOn = true
				case !math.IsNaN(deck.LoopStart()):
					loopInOn = cueBlink // 2 Hz flash while recording
				}
				loopOutOn := hasTrack && deck.IsLoopActive()

				if !initialized[i] || loopInOn != lastLoopIn[i] {
					lastLoopIn[i] = loopInOn
					a.ledMgr.Update("loop_in", i+1, loopInOn)
				}
				if !initialized[i] || loopOutOn != lastLoopOut[i] {
					lastLoopOut[i] = loopOutOn
					a.ledMgr.Update("loop_out", i+1, loopOutOn)
				}

				initialized[i] = true
			}
		}
	}
}

// vuMeterLoop polls each deck's smoothed output peak at ~20 Hz, publishes it
// on the bus for UI meters to consume, and forwards the same level to the
// controller as a CC value. The DDJ-FLX4 expects CC 2 on ch0/ch1 with
// values in the 37–123 range; we use a perceptual sqrt curve so quiet
// passages light a few segments instead of nothing.
const (
	vuMidiCC      uint8 = 2
	vuMidiMin     uint8 = 37
	vuMidiMax     uint8 = 123
	vuMidiStatus  uint8 = 0xB0
)

func (a *App) vuMeterLoop() {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	// silenceTicks counts consecutive ticks where both the raw peak and the
	// perceptual display are effectively zero. After ~silenceGraceTicks
	// (~1 s) the peak meter's envelope follower has fully decayed and the
	// MIDI VU LED is already at minimum, so we can stop publishing events
	// entirely until audio returns.
	const silenceThreshold = 0.001
	const silenceGraceTicks = 20
	var silenceTicks [2]int

	var lastMidi [2]uint8
	for {
		select {
		case <-a.stopCh:
			return
		case <-ticker.C:
			for i := 0; i < 2; i++ {
				deck := a.engine.Deck(i + 1)
				if deck == nil {
					continue
				}
				peak := deck.PeakLevel()
				// Perceptual curve — meters track loudness more than amplitude.
				display := math.Sqrt(peak)
				if display < 0 {
					display = 0
				}
				if display > 1 {
					display = 1
				}

				if display < silenceThreshold {
					silenceTicks[i]++
					if silenceTicks[i] > silenceGraceTicks {
						continue
					}
				} else {
					silenceTicks[i] = 0
				}

				a.bus.PublishAsync(event.Event{
					Topic:  event.TopicEngine,
					Action: event.ActionVULevel,
					DeckID: i + 1,
					Value:  display,
				})

				vuValue := vuMidiMin + uint8(display*float64(vuMidiMax-vuMidiMin)+0.5)
				if vuValue != lastMidi[i] {
					lastMidi[i] = vuValue
					channel := uint8(i)
					_ = a.midi.SendMIDI(vuMidiStatus|channel, vuMidiCC, vuValue)
				}
			}
		}
	}
}
