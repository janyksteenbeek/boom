package event

import "sync"

// Bus provides publish-subscribe event routing between subsystems.
type Bus struct {
	mu       sync.RWMutex
	handlers map[Topic][]Handler
	global   []Handler
	asyncCh  chan Event
	stopCh   chan struct{}
}

const asyncWorkers = 4
const asyncBufSize = 256

// New creates a new event bus.
func New() *Bus {
	b := &Bus{
		handlers: make(map[Topic][]Handler),
		asyncCh:  make(chan Event, asyncBufSize),
		stopCh:   make(chan struct{}),
	}
	for i := 0; i < asyncWorkers; i++ {
		go b.asyncWorker()
	}
	return b
}

// Stop shuts down async workers.
func (b *Bus) Stop() {
	close(b.stopCh)
}

func (b *Bus) asyncWorker() {
	for {
		select {
		case e := <-b.asyncCh:
			b.Publish(e)
		case <-b.stopCh:
			return
		}
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

// PublishAsync sends an event asynchronously via a fixed worker pool.
// Non-blocking: drops events if the channel is full (better than blocking audio).
func (b *Bus) PublishAsync(e Event) {
	select {
	case b.asyncCh <- e:
	default:
		// Channel full — drop event to avoid blocking callers
	}
}
