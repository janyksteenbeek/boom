package app

import (
	"fmt"
	"log"

	"github.com/janyksteenbeek/boom/internal/controller"
	"github.com/janyksteenbeek/boom/internal/event"
)

// registerActions maps standard DJ actions to event bus events.
func registerActions(registry *controller.ActionRegistry, bus *event.Bus) {
	registerDeckTriggers(registry, bus)
	registerCueActions(registry, bus)
	registerContinuousDeck(registry, bus)
	registerJogActions(registry, bus)
	registerMixerActions(registry, bus)
	registerLibraryActions(registry, bus)
	registerLoadTrackActions(registry, bus)
	registerFXActions(registry, bus)
	registerBeatLoopAction(registry, bus)
	registerStubActions(registry)
}

// registerDeckTriggers wires the press-only deck buttons that fire one event
// per push (play/pause, sync, loop edit, etc.).
func registerDeckTriggers(registry *controller.ActionRegistry, bus *event.Bus) {
	for _, action := range []string{
		event.ActionPlayPause, event.ActionPlay, event.ActionPause,
		event.ActionSync,
		event.ActionLoopIn, event.ActionLoopOut, event.ActionLoopToggle,
		event.ActionLoopHalve, event.ActionLoopDouble,
	} {
		a := action
		registry.Register(a, controller.ActionDescriptor{
			Name: a, Type: controller.ActionTypeTrigger,
		}, func(ctx controller.ActionContext) {
			if !ctx.Pressed {
				return
			}
			log.Printf("MIDI action: %s deck=%d", a, ctx.Deck)
			bus.Publish(event.Event{
				Topic:  event.TopicDeck,
				Action: a,
				DeckID: ctx.Deck,
			})
		})
	}
}

// registerCueActions handles the CUE button and its shifted variants. CUE
// itself needs both press and release for hold-to-preview / cue-release latch.
func registerCueActions(registry *controller.ActionRegistry, bus *event.Bus) {
	registry.Register(event.ActionCue, controller.ActionDescriptor{
		Name: event.ActionCue, Type: controller.ActionTypeTrigger,
	}, func(ctx controller.ActionContext) {
		bus.Publish(event.Event{
			Topic:   event.TopicDeck,
			Action:  event.ActionCue,
			DeckID:  ctx.Deck,
			Pressed: ctx.Pressed,
		})
	})

	// CUE delete: removes the saved manual cue point. Press-only.
	registry.Register(event.ActionCueDelete, controller.ActionDescriptor{
		Name: event.ActionCueDelete, Type: controller.ActionTypeTrigger,
	}, func(ctx controller.ActionContext) {
		if !ctx.Pressed {
			return
		}
		bus.Publish(event.Event{
			Topic:  event.TopicDeck,
			Action: event.ActionCueDelete,
			DeckID: ctx.Deck,
		})
	})

	// SHIFT + CUE: jump to track start, paused. Press-only.
	registry.Register(event.ActionCueGoStart, controller.ActionDescriptor{
		Name: event.ActionCueGoStart, Type: controller.ActionTypeTrigger,
	}, func(ctx controller.ActionContext) {
		if !ctx.Pressed {
			return
		}
		bus.Publish(event.Event{
			Topic:  event.TopicDeck,
			Action: event.ActionCueGoStart,
			DeckID: ctx.Deck,
		})
	})
}

// registerContinuousDeck wires per-deck continuous controls (volume, tempo,
// EQ, gain) plus the YAML alias names that map to them.
func registerContinuousDeck(registry *controller.ActionRegistry, bus *event.Bus) {
	for _, action := range []string{
		event.ActionVolumeChange, event.ActionTempoChange,
		event.ActionEQHigh, event.ActionEQMid, event.ActionEQLow,
		event.ActionGainChange,
	} {
		a := action
		registry.Register(a, controller.ActionDescriptor{
			Name: a, Type: controller.ActionTypeContinuous,
		}, func(ctx controller.ActionContext) {
			bus.Publish(event.Event{
				Topic:  event.TopicDeck,
				Action: a,
				DeckID: ctx.Deck,
				Value:  ctx.Value,
			})
		})
	}

	actionAliases := map[string]string{
		"volume":  event.ActionVolumeChange,
		"tempo":   event.ActionTempoChange,
		"eq_high": event.ActionEQHigh,
		"eq_mid":  event.ActionEQMid,
		"eq_low":  event.ActionEQLow,
		"gain":    event.ActionGainChange,
	}
	for alias, target := range actionAliases {
		t := target
		registry.Register(alias, controller.ActionDescriptor{
			Name: alias, Type: controller.ActionTypeContinuous,
		}, func(ctx controller.ActionContext) {
			bus.Publish(event.Event{
				Topic:  event.TopicDeck,
				Action: t,
				DeckID: ctx.Deck,
				Value:  ctx.Value,
			})
		})
	}
}

// registerJogActions wires the jog wheel encoders plus the touch and vinyl
// mode buttons that change how those encoder ticks are interpreted.
func registerJogActions(registry *controller.ActionRegistry, bus *event.Bus) {
	for _, action := range []string{event.ActionJogScratch, event.ActionJogPitch} {
		a := action
		registry.Register(a, controller.ActionDescriptor{
			Name: a, Type: controller.ActionTypeRelative,
		}, func(ctx controller.ActionContext) {
			bus.Publish(event.Event{
				Topic:  event.TopicDeck,
				Action: a,
				DeckID: ctx.Deck,
				Value:  ctx.Delta,
			})
		})
	}

	// jog_touch: trigger with press AND release — release matters for
	// snapping back to the captured play state in vinyl mode.
	registry.Register(event.ActionJogTouch, controller.ActionDescriptor{
		Name: event.ActionJogTouch, Type: controller.ActionTypeTrigger,
	}, func(ctx controller.ActionContext) {
		bus.Publish(event.Event{
			Topic:   event.TopicDeck,
			Action:  event.ActionJogTouch,
			DeckID:  ctx.Deck,
			Pressed: ctx.Pressed,
		})
	})

	// vinyl_mode: press-only toggle.
	registry.Register(event.ActionVinylMode, controller.ActionDescriptor{
		Name: event.ActionVinylMode, Type: controller.ActionTypeTrigger,
	}, func(ctx controller.ActionContext) {
		if !ctx.Pressed {
			return
		}
		bus.Publish(event.Event{
			Topic:  event.TopicDeck,
			Action: event.ActionVinylMode,
			DeckID: ctx.Deck,
		})
	})
}

// registerMixerActions wires the global mixer controls (crossfader and the
// master/cue volume knobs).
func registerMixerActions(registry *controller.ActionRegistry, bus *event.Bus) {
	for _, action := range []string{
		event.ActionCrossfader, event.ActionMasterVolume, event.ActionCueVolume,
	} {
		a := action
		registry.Register(a, controller.ActionDescriptor{
			Name: a, Type: controller.ActionTypeContinuous,
		}, func(ctx controller.ActionContext) {
			bus.Publish(event.Event{
				Topic:  event.TopicMixer,
				Action: a,
				Value:  ctx.Value,
			})
		})
	}
}

// registerLibraryActions wires the browser scroll/select wheel.
func registerLibraryActions(registry *controller.ActionRegistry, bus *event.Bus) {
	registry.Register(event.ActionBrowseScroll, controller.ActionDescriptor{
		Name: event.ActionBrowseScroll, Type: controller.ActionTypeRelative,
	}, func(ctx controller.ActionContext) {
		bus.Publish(event.Event{
			Topic:  event.TopicLibrary,
			Action: event.ActionBrowseScroll,
			Value:  ctx.Delta,
		})
	})

	registry.Register(event.ActionBrowseSelect, controller.ActionDescriptor{
		Name: event.ActionBrowseSelect, Type: controller.ActionTypeTrigger,
	}, func(ctx controller.ActionContext) {
		if !ctx.Pressed {
			return
		}
		bus.Publish(event.Event{
			Topic:  event.TopicLibrary,
			Action: event.ActionBrowseSelect,
		})
	})
}

// registerLoadTrackActions wires the global "load to deck N" buttons.
func registerLoadTrackActions(registry *controller.ActionRegistry, bus *event.Bus) {
	for deckID := 1; deckID <= 2; deckID++ {
		id := deckID
		name := fmt.Sprintf("load_track_%d", id)
		registry.Register(name, controller.ActionDescriptor{
			Name: name, Type: controller.ActionTypeTrigger,
		}, func(ctx controller.ActionContext) {
			if !ctx.Pressed {
				return
			}
			bus.Publish(event.Event{
				Topic:  event.TopicDeck,
				Action: event.ActionLoadTrack,
				DeckID: id,
			})
		})
	}
}

// registerFXActions wires the beat-FX section. DeckID 0 routes to the master
// FX bus; 1/2 route to per-deck inserts.
func registerFXActions(registry *controller.ActionRegistry, bus *event.Bus) {
	registry.Register(event.ActionFXSelect, controller.ActionDescriptor{
		Name: event.ActionFXSelect, Type: controller.ActionTypeTrigger,
	}, func(ctx controller.ActionContext) {
		if !ctx.Pressed {
			return
		}
		bus.Publish(event.Event{
			Topic:  event.TopicDeck,
			Action: event.ActionFXSelect,
			DeckID: ctx.Deck,
			Value:  ctx.Value,
		})
	})

	registry.Register(event.ActionFXActivate, controller.ActionDescriptor{
		Name: event.ActionFXActivate, Type: controller.ActionTypeTrigger,
	}, func(ctx controller.ActionContext) {
		if !ctx.Pressed {
			return
		}
		deckID := ctx.Deck
		if deckID == 0 {
			deckID = event.DeckIDUnresolved
		}
		bus.Publish(event.Event{
			Topic:  event.TopicDeck,
			Action: event.ActionFXActivate,
			DeckID: deckID,
			Value:  1.0,
		})
	})

	registry.Register(event.ActionFXNext, controller.ActionDescriptor{
		Name: event.ActionFXNext, Type: controller.ActionTypeTrigger,
	}, func(ctx controller.ActionContext) {
		if !ctx.Pressed {
			return
		}
		bus.Publish(event.Event{
			Topic:  event.TopicDeck,
			Action: event.ActionFXNext,
			DeckID: ctx.Deck,
		})
	})

	for _, action := range []string{event.ActionFXWetDry, event.ActionFXTime} {
		a := action
		registry.Register(a, controller.ActionDescriptor{
			Name: a, Type: controller.ActionTypeContinuous,
		}, func(ctx controller.ActionContext) {
			deckID := ctx.Deck
			// fx_wetdry without a deck context routes via the UI's current
			// FX target to avoid a publish-loop between window and mixer.
			if a == event.ActionFXWetDry && deckID == 0 {
				deckID = event.DeckIDUnresolved
			}
			bus.Publish(event.Event{
				Topic:  event.TopicDeck,
				Action: a,
				DeckID: deckID,
				Value:  ctx.Value,
			})
		})
	}
}

// registerBeatLoopAction wires the parameterized beat-length loop action.
// Mappings specify the beat count in the YAML "value" field; the action
// registry forwards it via ctx.Value. Fallback is the engine's configured
// DefaultBeatLoop.
func registerBeatLoopAction(registry *controller.ActionRegistry, bus *event.Bus) {
	registry.Register(event.ActionBeatLoop, controller.ActionDescriptor{
		Name: event.ActionBeatLoop, Type: controller.ActionTypeContinuous,
	}, func(ctx controller.ActionContext) {
		if !ctx.Pressed && ctx.Value == 0 {
			return
		}
		bus.Publish(event.Event{
			Topic:  event.TopicDeck,
			Action: event.ActionBeatLoop,
			DeckID: ctx.Deck,
			Value:  ctx.Value,
		})
	})
}

// registerStubActions registers placeholder handlers for actions defined in
// YAML mappings but not yet implemented in the engine. They log on press so
// new mappings can be debugged without crashing the dispatcher.
func registerStubActions(registry *controller.ActionRegistry) {
	stubs := []string{
		"stutter", "headphone_cue",
		"browse_back",
	}
	for i := 1; i <= 8; i++ {
		stubs = append(stubs, fmt.Sprintf("hotcue_%d", i))
		stubs = append(stubs, fmt.Sprintf("hotcue_%d_delete", i))
	}
	for _, name := range stubs {
		n := name
		registry.Register(n, controller.ActionDescriptor{
			Name: n, Type: controller.ActionTypeTrigger,
		}, func(ctx controller.ActionContext) {
			if ctx.Pressed {
				log.Printf("stub action: %s deck=%d", n, ctx.Deck)
			}
		})
	}
}
