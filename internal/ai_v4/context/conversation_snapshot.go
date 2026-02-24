package context

// ConversationSnapshot holds the state of the conversation at a point in time.
type ConversationSnapshot struct {
	LastUserMessage      string  `json:"last_user_message"`
	LastAIReply          string  `json:"last_ai_reply"`
	LastDomain           string  `json:"last_domain"`
	ConversationStage    string  `json:"conversation_stage"`
	ConversationMode     string  `json:"conversation_mode"` // "general" | "ordering" | "support"
	ActiveItem           string  `json:"active_item"`       // Sticky focus item (e.g. for negotiation)
	NegotiationRound     int     `json:"negotiation_round"` // How many times user has negotiated
	LastOfferedPrice     float64 `json:"last_offered_price"`
	TurnCount            int     `json:"turn_count"`
	CloseoutState        string  `json:"closeout_state,omitempty"` // "open" | "closed"
	CloseoutMessageCount int     `json:"closeout_message_count,omitempty"`
	LastCloseoutAt       string  `json:"last_closeout_at,omitempty"` // RFC3339
}

// DefaultConversationSnapshot returns a fresh snapshot for a new conversation.
func DefaultConversationSnapshot() *ConversationSnapshot {
	return &ConversationSnapshot{
		ConversationStage: "intro",
		ConversationMode:  "general", // Start in general mode
		TurnCount:         0,
		CloseoutState:     "open",
	}
}
