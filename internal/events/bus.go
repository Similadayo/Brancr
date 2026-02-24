package events

// EventHandler processes an event and returns an error if processing fails.
type EventHandler func(Event) error

// EventBus defines the contract for dispatching system events.
type EventBus interface {
	Publish(event Event) error
	Subscribe(eventType EventType, handler EventHandler)
}
