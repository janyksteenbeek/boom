package event

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestBusPublishSubscribe(t *testing.T) {
	bus := New()
	var received int32

	bus.Subscribe(TopicDeck, func(e Event) error {
		atomic.AddInt32(&received, 1)
		return nil
	})

	bus.Publish(Event{Topic: TopicDeck, Action: ActionPlay, DeckID: 1})
	bus.Publish(Event{Topic: TopicDeck, Action: ActionPause, DeckID: 1})

	if got := atomic.LoadInt32(&received); got != 2 {
		t.Errorf("expected 2 events, got %d", got)
	}
}

func TestBusTopicFiltering(t *testing.T) {
	bus := New()
	var deckCount, mixerCount int32

	bus.Subscribe(TopicDeck, func(e Event) error {
		atomic.AddInt32(&deckCount, 1)
		return nil
	})
	bus.Subscribe(TopicMixer, func(e Event) error {
		atomic.AddInt32(&mixerCount, 1)
		return nil
	})

	bus.Publish(Event{Topic: TopicDeck, Action: ActionPlay})
	bus.Publish(Event{Topic: TopicMixer, Action: ActionCrossfader})
	bus.Publish(Event{Topic: TopicDeck, Action: ActionPause})

	if got := atomic.LoadInt32(&deckCount); got != 2 {
		t.Errorf("deck events: expected 2, got %d", got)
	}
	if got := atomic.LoadInt32(&mixerCount); got != 1 {
		t.Errorf("mixer events: expected 1, got %d", got)
	}
}

func TestBusSubscribeAll(t *testing.T) {
	bus := New()
	var count int32

	bus.SubscribeAll(func(e Event) error {
		atomic.AddInt32(&count, 1)
		return nil
	})

	bus.Publish(Event{Topic: TopicDeck})
	bus.Publish(Event{Topic: TopicMixer})
	bus.Publish(Event{Topic: TopicLibrary})

	if got := atomic.LoadInt32(&count); got != 3 {
		t.Errorf("expected 3 events, got %d", got)
	}
}

func TestBusPublishAsync(t *testing.T) {
	bus := New()
	var received int32

	bus.Subscribe(TopicDeck, func(e Event) error {
		atomic.AddInt32(&received, 1)
		return nil
	})

	bus.PublishAsync(Event{Topic: TopicDeck, Action: ActionPlay})

	// Wait for async delivery
	time.Sleep(50 * time.Millisecond)

	if got := atomic.LoadInt32(&received); got != 1 {
		t.Errorf("expected 1 event, got %d", got)
	}
}
