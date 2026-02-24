package types

// StructuredReply represents AI output with delivery instructions
// Core principle: AI generates once, sends many (if needed)
// AI declares structure, delivery layer handles sequencing
type StructuredReply struct {
	ReplyMode  string   `json:"reply_mode"`           // "single" or "sequence"
	Messages   []string `json:"messages"`             // Messages to send
	Confidence string   `json:"confidence"`           // "high", "medium", "low"
	Escalation *string  `json:"escalation"`           // null or escalation reason
	MediaURLs  []string `json:"media_urls,omitempty"` // Optional images to send
	Variant    string   `json:"variant,omitempty"`    // A/B test variant (e.g., "control", "style_a")
}
