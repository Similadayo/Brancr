package shadow

import "time"

type ShadowRecord struct {
	TenantID       string
	ConversationID string

	UserMessage string

	ActiveReply string
	ShadowReply string

	ActiveAction string
	ShadowAction string

	ShadowGoal string
	ShadowMode string

	LatencyMs int64
	Timestamp time.Time
}
