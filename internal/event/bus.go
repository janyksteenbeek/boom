package event

import "sync"

// Bus provides publish-subscribe event routing between subsystems.
type Bus struct {
	mu       sync.RWMutex
	handlers map[Topic][]Handler
	global   []Handler
}

// New creates a new event bus.
func New() *Bus {
	return &Bus{
		handlers: make(map[Topic][]Handler),
	}
}

// Subscribe registers a handler for a specific topic.
func (b *Bus) Subscribe(topic Topic, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[topic] = append(b.handlers[topic], h)
}

// SubscribeAll registers a handler that receives all events.
func (b *Bus) SubscribeAll(h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.global = append(b.global, h)
}

// Publish sends an event synchronously to all matching handlers.
// Use this for latency-sensitive paths (audio engine).
func (b *Bus) Publish(e Event) {
	b.mu.RLock()
	handlers := b.handlers[e.Topic]
	globals := b.global
	b.mu.RUnlock()

	for _, h := range handlers {
		_ = h(e)
	}
	for _, h := range globals {
		_ = h(e)
	}
}

// PublishAsync sends an event asynchronously via a goroutine.
// Use this for non-latency-sensitive paths (UI updates).
func (b *Bus) PublishAsync(e Event) {
	go b.Publish(e)
}
