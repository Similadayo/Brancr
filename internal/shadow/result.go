package shadow

import "time"

type ShadowResult struct {
	Reply string

	SuggestedAction  string
	ConversationGoal string
	ResponseMode     string

	DurationMs int64
	Timestamp  time.Time
}
