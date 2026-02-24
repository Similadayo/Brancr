package events

import (
	"sync"
)

// InMemoryBus implements a synchronous, in-memory EventBus.
type InMemoryBus struct {
	mu       sync.RWMutex
	handlers map[EventType][]EventHandler
}

// NewInMemoryBus creates a new instance of InMemoryBus.
func NewInMemoryBus() *InMemoryBus {
	return &InMemoryBus{
		handlers: make(map[EventType][]EventHandler),
	}
}

// Subscribe adds a handler for a specific event type.
func (b *InMemoryBus) Subscribe(eventType EventType, handler EventHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], handler)
}

// Publish executes all subscribers for the event's type synchronously.
// If any handler returns an error, execution continues but the error is collected (simple implementation).
// For strict Phase 3 compliance: "No retries, synchronous".
func (b *InMemoryBus) Publish(event Event) error {
	b.mu.RLock()
	handlers, ok := b.handlers[event.Type]
	b.mu.RUnlock()

	if !ok {
		return nil // No listeners, no error
	}

	for _, handler := range handlers {
		if err := handler(event); err != nil {
			return err // Fail fast or collect? Phase 3 says "No retries". Returning first error seems safest.
		}
	}

	return nil
}
