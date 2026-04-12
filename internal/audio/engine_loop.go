package audio

import (
	"log"
	"math"

	"github.com/janyksteenbeek/boom/internal/event"
)

// applyBeatLoop creates a beat-length loop of `beats` starting at the nearest
// beat before the current playhead. Returns true if the loop was applied.
func (e *Engine) applyBeatLoop(deck *Deck, beats float64) bool {
	pcmLen := len(deck.PCMSamples())
	if pcmLen == 0 {
		return false
	}
	spb := deck.SamplesPerBeat()
	if spb <= 0 {
		log.Printf("engine: beat loop deck %d — no BPM metadata", deck.ID())
		return false
	}
	opts := e.loopOptions()
	if beats < opts.MinBeats {
		beats = opts.MinBeats
	}
	if beats > opts.MaxBeats {
		beats = opts.MaxBeats
	}
	start := deck.NearestBeatBefore(deck.Position())
	if start < 0 {
		start = 0
	}
	startSamples := start * float64(pcmLen)
	endSamples := startSamples + beats*spb
	if endSamples > float64(pcmLen) {
		if !opts.SmartLoop {
			return false
		}
		endSamples = float64(pcmLen)
		// Recompute beats from clamped length so halve/double stay sensible.
		beats = (endSamples - startSamples) / spb
		if beats <= 0 {
			return false
		}
	}
	end := endSamples / float64(pcmLen)
	deck.SetManualLoop(start, end, beats)
	return true
}

// resizeLoopBeats keeps the current loop start but recomputes the end
// position for the new beat length. Returns true when the loop was updated.
func (e *Engine) resizeLoopBeats(deck *Deck, beats float64) bool {
	pcmLen := len(deck.PCMSamples())
	if pcmLen == 0 {
		return false
	}
	spb := deck.SamplesPerBeat()
	if spb <= 0 || beats <= 0 {
		return false
	}
	start := deck.LoopStart()
	if math.IsNaN(start) {
		return false
	}
	startSamples := start * float64(pcmLen)
	endSamples := startSamples + beats*spb
	opts := e.loopOptions()
	if endSamples > float64(pcmLen) {
		if !opts.SmartLoop {
			return false
		}
		endSamples = float64(pcmLen)
		beats = (endSamples - startSamples) / spb
		if beats <= 0 {
			return false
		}
	}
	end := endSamples / float64(pcmLen)
	wasActive := deck.IsLoopActive()
	deck.SetManualLoop(start, end, beats)
	if !wasActive {
		deck.SetLoopActive(false)
	}
	return true
}

// samplesToBeats converts a normalized (start,end) pair into a beat count.
func (e *Engine) samplesToBeats(deck *Deck, start, end float64) float64 {
	pcmLen := len(deck.PCMSamples())
	if pcmLen == 0 {
		return 0
	}
	spb := deck.SamplesPerBeat()
	if spb <= 0 {
		return 0
	}
	return (end - start) * float64(pcmLen) / spb
}

// publishLoopState broadcasts the deck's current loop configuration so the
// UI (waveform overlay, loop bar) can reflect it.
func (e *Engine) publishLoopState(deck *Deck) {
	start := deck.LoopStart()
	end := deck.LoopEnd()
	if math.IsNaN(start) {
		start = -1
	}
	if math.IsNaN(end) {
		end = -1
	}
	state := &event.LoopState{
		Start:  start,
		End:    end,
		Beats:  deck.LoopBeats(),
		Active: deck.IsLoopActive(),
	}
	e.bus.PublishAsync(event.Event{
		Topic:   event.TopicEngine,
		Action:  event.ActionLoopStateUpdate,
		DeckID:  deck.ID(),
		Payload: state,
	})
}

// publishCuePointChanged broadcasts a cue point update so the UI can refresh
// the marker and the app layer can persist it to the database.
func (e *Engine) publishCuePointChanged(deck *Deck, pos float64) {
	var trackID string
	if t := deck.Track(); t != nil {
		trackID = t.ID
	}
	e.bus.PublishAsync(event.Event{
		Topic:   event.TopicEngine,
		Action:  event.ActionCuePointChanged,
		DeckID:  deck.ID(),
		Value:   pos,
		Payload: trackID,
	})
}
