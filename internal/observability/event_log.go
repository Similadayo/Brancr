package observability

// EventLog is the immutable, append-only record of a system event.
// It serves as the ground truth for replaying history.
type EventLog struct {
	EventID    string
	EventType  string
	SessionID  string
	Timestamp  int64
	PayloadRef string // Pointer to payload (e.g., JSON blob or storage ref), not raw
}
