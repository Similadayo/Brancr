package events

import "time"

type Event struct {
	ID        string
	TenantID  string
	Type      EventType
	Payload   any
	Timestamp time.Time
}
