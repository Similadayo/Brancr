package admin

import (
	"sync"
)

type EventBroker struct {
	mu   sync.Mutex
	subs map[chan string]struct{}
}

func NewEventBroker() *EventBroker {
	return &EventBroker{
		subs: make(map[chan string]struct{}),
	}
}

func (b *EventBroker) Subscribe() chan string {
	ch := make(chan string, 10)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *EventBroker) Unsubscribe(ch chan string) {
	b.mu.Lock()
	delete(b.subs, ch)
	close(ch)
	b.mu.Unlock()
}

func (b *EventBroker) Publish(msg string) {
	b.mu.Lock()
	for ch := range b.subs {
		select {
		case ch <- msg:
		default:
		}
	}
	b.mu.Unlock()
}
