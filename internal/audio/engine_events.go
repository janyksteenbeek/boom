package audio

import (
	"encoding/json"
	"fmt"
	"log"
	"math"

	"github.com/janyksteenbeek/boom/internal/event"
	"github.com/janyksteenbeek/boom/pkg/model"
)

// subscribeEvents wires the engine to all bus topics it cares about: deck
// commands, mixer changes, beat-FX, and analysis results.
func (e *Engine) subscribeEvents() {
	e.bus.Subscribe(event.TopicDeck, e.handleDeckEvent)
	e.bus.Subscribe(event.TopicAnalysis, e.handleAnalysisEvent)
	e.bus.Subscribe(event.TopicMixer, e.handleMixerEvent)
	e.bus.Subscribe(event.TopicDeck, e.handleBeatFXEvent)
}

func (e *Engine) handleDeckEvent(ev event.Event) error {
	// Temporary trace: every loop-related deck event that reaches the engine.
	// Loop bugs typically show up as missing log lines here.
	if ev.Action == event.ActionLoopIn || ev.Action == event.ActionLoopOut ||
		ev.Action == event.ActionLoopToggle || ev.Action == event.ActionLoopHalve ||
		ev.Action == event.ActionLoopDouble || ev.Action == event.ActionBeatLoop {
		log.Printf("engine RX: action=%s deckID=%d pressed=%v value=%v",
			ev.Action, ev.DeckID, ev.Pressed, ev.Value)
	}
	if ev.DeckID < 1 || ev.DeckID > NumDecks {
		return nil
	}
	deck := e.decks[ev.DeckID-1]

	switch ev.Action {
	case event.ActionPlayPause:
		e.handlePlayPause(deck, ev)
	case event.ActionPlay:
		e.handlePlay(deck, ev)
	case event.ActionPause:
		e.handlePause(deck, ev)
	case event.ActionCue:
		e.handleCue(deck, ev)
	case event.ActionCueDelete:
		deck.ClearCue()
		e.publishCuePointChanged(deck, -1)
	case event.ActionCueGoStart:
		e.handleCueGoStart(deck, ev)
	case event.ActionSeek:
		deck.Seek(ev.Value)
	case event.ActionVolumeChange:
		deck.SetVolume(ev.Value)
	case event.ActionGainChange:
		deck.SetGain(float32(ev.Value * 2.0))
	case event.ActionEQHigh:
		deck.SetEQHigh(ev.Value)
	case event.ActionEQMid:
		deck.SetEQMid(ev.Value)
	case event.ActionEQLow:
		deck.SetEQLow(ev.Value)
	case event.ActionTempoChange:
		deck.SetTempo(0.5 + ev.Value*1.5)
	case event.ActionSync:
		e.handleSync(deck, ev)
	case event.ActionLoopIn:
		e.handleLoopIn(deck)
	case event.ActionLoopOut:
		e.handleLoopOut(deck)
	case event.ActionLoopToggle:
		e.handleLoopToggle(deck)
	case event.ActionLoopHalve:
		e.handleLoopResize(deck, ev, 0.5)
	case event.ActionLoopDouble:
		e.handleLoopResize(deck, ev, 2.0)
	case event.ActionBeatLoop:
		if deck.Track() == nil || ev.Value <= 0 {
			return nil
		}
		if e.applyBeatLoop(deck, ev.Value) {
			e.publishLoopState(deck)
		}
	case event.ActionVinylMode:
		deck.ToggleVinylMode()
		e.bus.PublishAsync(event.Event{
			Topic: event.TopicEngine, Action: event.ActionVinylModeChanged,
			DeckID: ev.DeckID, Value: boolToFloat(deck.VinylMode()),
		})
	case event.ActionJogTouch:
		deck.SetJogTouch(ev.Pressed)
	case event.ActionJogScratch:
		// Vinyl mode + top touch → scratch velocity. Otherwise (jog mode
		// or side-only touch) the same encoder feeds the pitch bend.
		if deck.VinylMode() && deck.JogTouched() {
			deck.AddJogScratchDelta(ev.Value)
		} else {
			deck.AddJogPitchDelta(ev.Value)
		}
	case event.ActionJogPitch:
		deck.AddJogPitchDelta(ev.Value)
	case event.ActionLoadTrack:
		return e.handleLoadTrack(deck, ev)
	}
	return nil
}

func (e *Engine) handlePlayPause(deck *Deck, ev event.Event) {
	// CUE+PLAY = cue release latch: cancel preview snap-back and force playing.
	if deck.CueHeld() {
		deck.SetCuePreview(false)
		if !deck.IsPlaying() {
			deck.Play()
		}
		e.bus.PublishAsync(event.Event{
			Topic: event.TopicEngine, Action: event.ActionPlayState,
			DeckID: ev.DeckID, Value: 1.0,
		})
		return
	}
	deck.TogglePlay()
	e.bus.PublishAsync(event.Event{
		Topic: event.TopicEngine, Action: event.ActionPlayState,
		DeckID: ev.DeckID, Value: boolToFloat(deck.IsPlaying()),
	})
}

func (e *Engine) handlePlay(deck *Deck, ev event.Event) {
	if deck.CueHeld() {
		deck.SetCuePreview(false) // latch
	}
	deck.Play()
	e.bus.PublishAsync(event.Event{
		Topic: event.TopicEngine, Action: event.ActionPlayState,
		DeckID: ev.DeckID, Value: 1.0,
	})
}

func (e *Engine) handlePause(deck *Deck, ev event.Event) {
	deck.Pause()
	e.bus.PublishAsync(event.Event{
		Topic: event.TopicEngine, Action: event.ActionPlayState,
		DeckID: ev.DeckID, Value: 0.0,
	})
}

// handleCue implements the standard CUE button behavior:
//
//   - Playing + press → back-cue: seek to effective cue, enter cue_preview.
//   - Paused  + press → set manual cue at current playhead, enter cue_preview.
//   - Release while in preview → pause + seek back to cue.
func (e *Engine) handleCue(deck *Deck, ev event.Event) {
	if deck.Track() == nil {
		return
	}
	if ev.Pressed {
		deck.SetCueHeld(true)
		if deck.IsPlaying() {
			deck.Seek(deck.EffectiveCue())
			deck.SetCuePreview(true)
			return
		}
		// Paused: SET cue at playhead (replaces any previous manual cue),
		// then start preview playback from that point.
		newPos := deck.Position()
		deck.SetCuePoint(newPos)
		e.publishCuePointChanged(deck, newPos)
		deck.Play()
		deck.SetCuePreview(true)
		e.bus.PublishAsync(event.Event{
			Topic: event.TopicEngine, Action: event.ActionPlayState,
			DeckID: ev.DeckID, Value: 1.0,
		})
		return
	}
	deck.SetCueHeld(false)
	if deck.CuePreview() {
		deck.Pause()
		deck.Seek(deck.EffectiveCue())
		deck.SetCuePreview(false)
		e.bus.PublishAsync(event.Event{
			Topic: event.TopicEngine, Action: event.ActionPlayState,
			DeckID: ev.DeckID, Value: 0.0,
		})
	}
}

// handleCueGoStart implements SHIFT + CUE: jump to track start, paused. Does
// not touch the saved cue point.
func (e *Engine) handleCueGoStart(deck *Deck, ev event.Event) {
	if deck.Track() == nil {
		return
	}
	deck.Pause()
	deck.Seek(0)
	deck.SetCuePreview(false)
	e.bus.PublishAsync(event.Event{
		Topic: event.TopicEngine, Action: event.ActionPlayState,
		DeckID: ev.DeckID, Value: 0.0,
	})
}

func (e *Engine) handleSync(deck *Deck, ev event.Event) {
	otherID := 2
	if ev.DeckID == 2 {
		otherID = 1
	}
	other := e.decks[otherID-1]
	deckTrack := deck.Track()
	otherTrack := other.Track()
	if deckTrack == nil || otherTrack == nil || deckTrack.BPM <= 0 || otherTrack.BPM <= 0 {
		log.Printf("engine: sync deck %d — missing BPM data", ev.DeckID)
		return
	}
	otherBPM := other.EffectiveBPM()
	newRatio := otherBPM / deckTrack.BPM
	deck.SetTempo(newRatio)
	log.Printf("engine: sync deck %d → %.1f BPM (matched deck %d at %.1f BPM, ratio %.3f)",
		ev.DeckID, deckTrack.BPM*newRatio, otherID, otherBPM, newRatio)
}

func (e *Engine) handleLoopIn(deck *Deck) {
	if deck.Track() == nil {
		return
	}
	pos := deck.Position()
	if e.loopOptions().Quantize {
		if snapped := deck.NearestBeatBefore(pos); snapped > 0 {
			pos = snapped
		}
	}
	deck.SetLoopStart(pos)
	e.publishLoopState(deck)
}

func (e *Engine) handleLoopOut(deck *Deck) {
	if deck.Track() == nil {
		return
	}
	start := deck.LoopStart()
	if math.IsNaN(start) {
		return
	}
	end := deck.Position()
	if e.loopOptions().Quantize {
		if snapped := deck.NearestBeatBefore(end); snapped > start {
			end = snapped
		}
	}
	if end <= start {
		return
	}
	beats := e.samplesToBeats(deck, start, end)
	deck.SetManualLoop(start, end, beats)
	e.publishLoopState(deck)
}

// handleLoopToggle implements the "4 Beat / Exit Loop" button: while a loop
// is active this exits AND clears the stored boundaries, so the next press
// always starts a fresh auto beat loop from the current playhead.
func (e *Engine) handleLoopToggle(deck *Deck) {
	if deck.Track() == nil {
		return
	}
	if deck.IsLoopActive() || deck.HasLoop() {
		deck.ClearLoop()
		e.publishLoopState(deck)
		return
	}
	if e.applyBeatLoop(deck, e.loopOptions().DefaultBeatLoop) {
		e.publishLoopState(deck)
	}
}

// handleLoopResize halves or doubles the active loop length, clamped to the
// configured min/max beat counts.
func (e *Engine) handleLoopResize(deck *Deck, ev event.Event, factor float64) {
	verb := "double"
	if factor < 1 {
		verb = "halve"
	}
	log.Printf("engine: loop_%s deck=%d hasLoop=%v beats=%.4f start=%.4f end=%.4f",
		verb, ev.DeckID, deck.HasLoop(), deck.LoopBeats(), deck.LoopStart(), deck.LoopEnd())
	if !deck.HasLoop() {
		return
	}
	opts := e.loopOptions()
	beats := deck.LoopBeats()
	if beats <= 0 {
		beats = e.samplesToBeats(deck, deck.LoopStart(), deck.LoopEnd())
	}
	beats *= factor
	if beats < opts.MinBeats {
		beats = opts.MinBeats
	}
	if beats > opts.MaxBeats {
		beats = opts.MaxBeats
	}
	ok := e.resizeLoopBeats(deck, beats)
	log.Printf("engine: loop_%s → newBeats=%.4f resized=%v", verb, beats, ok)
	if ok {
		e.publishLoopState(deck)
	}
}

func (e *Engine) handleLoadTrack(deck *Deck, ev event.Event) error {
	track, ok := ev.Payload.(*model.Track)
	if !ok {
		return fmt.Errorf("expected *model.Track")
	}
	deckID := ev.DeckID
	go func() {
		if err := deck.LoadTrack(track); err != nil {
			log.Printf("engine: load failed deck %d: %v", deckID, err)
			return
		}
		// Fallback cue = first audio frame if auto-cue is on, else 0.
		var fallback float64
		if e.autoCue.Load() {
			fallback = deck.FirstAudioFrame()
		}
		deck.SetFallbackCue(fallback)
		if track.HasCuePoint() {
			deck.SetCuePoint(track.CuePoint)
		} else {
			deck.ClearCue()
		}
		e.publishCuePointChanged(deck, deck.CuePoint())
		e.publishLoopState(deck)
		// Seek to effective cue, stay paused (no auto-play).
		deck.Seek(deck.EffectiveCue())
		e.bus.PublishAsync(event.Event{
			Topic: event.TopicEngine, Action: event.ActionPlayState,
			DeckID: deckID, Value: 0.0,
		})
	}()
	return nil
}

// handleAnalysisEvent refreshes each deck's cached Track when a new analysis
// result arrives, so loop/sync features pick up the new BPM and beat grid
// without needing a reload.
func (e *Engine) handleAnalysisEvent(ev event.Event) error {
	if ev.Action != event.ActionAnalyzeComplete {
		return nil
	}
	res, ok := ev.Payload.(*event.AnalysisResult)
	if !ok || res == nil {
		return nil
	}
	beatGridJSON := ""
	if len(res.BeatGrid) > 0 {
		if data, mErr := json.Marshal(res.BeatGrid); mErr == nil {
			beatGridJSON = string(data)
		}
	}
	for _, d := range e.decks {
		if d.UpdateTrackAnalysis(res.TrackID, res.BPM, res.Key, beatGridJSON) {
			log.Printf("engine: deck %d track analysis applied (bpm=%.2f)", d.ID(), res.BPM)
		}
	}
	return nil
}

func (e *Engine) handleMixerEvent(ev event.Event) error {
	switch ev.Action {
	case event.ActionCrossfader:
		e.master.SetCrossfader(ev.Value)
	case event.ActionMasterVolume:
		e.master.SetMasterVolume(ev.Value)
	case event.ActionCueVolume:
		e.master.SetCueVolume(ev.Value)
	}
	return nil
}

// handleBeatFXEvent routes beat-FX events to per-deck inserts (DeckID 1/2) or
// the master FX bus (DeckID 0).
func (e *Engine) handleBeatFXEvent(ev event.Event) error {
	switch ev.Action {
	case event.ActionFXSelect:
		fxType := FXType(int32(ev.Value))
		if ev.DeckID == 0 {
			e.master.SetBeatFXType(fxType)
		} else if ev.DeckID >= 1 && ev.DeckID <= NumDecks {
			e.decks[ev.DeckID-1].SetBeatFXType(fxType)
		}
	case event.ActionFXActivate:
		on := ev.Value > 0.5
		if ev.DeckID == 0 {
			e.master.SetBeatFXActive(on)
		} else if ev.DeckID >= 1 && ev.DeckID <= NumDecks {
			e.decks[ev.DeckID-1].SetBeatFXActive(on)
		}
	case event.ActionFXWetDry:
		v := float32(ev.Value)
		if ev.DeckID == 0 {
			e.master.SetBeatFXWetDry(v)
		} else if ev.DeckID >= 1 && ev.DeckID <= NumDecks {
			e.decks[ev.DeckID-1].SetBeatFXWetDry(v)
		}
	case event.ActionFXTime:
		// Map 0.0–1.0 to 50–2000ms range
		ms := float32(50 + ev.Value*1950)
		if ev.DeckID == 0 {
			e.master.SetBeatFXTime(ms)
		} else if ev.DeckID >= 1 && ev.DeckID <= NumDecks {
			e.decks[ev.DeckID-1].SetBeatFXTime(ms)
		}
	}
	return nil
}
