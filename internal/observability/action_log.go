package observability

// ActionExecutionLog records the actual execution result of an action.
// Distinguishes "AI Suggested" (Audit) from "System Executed" (Log).
type ActionExecutionLog struct {
	SessionID string
	Action    string
	Success   bool
	Error     string // Empty if success
	Timestamp int64
}
