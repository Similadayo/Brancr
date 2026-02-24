package foundation

type EventType string

type Event struct {
	Type      EventType
	Payload   any
	Timestamp int64
}
