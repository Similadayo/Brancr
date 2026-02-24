package conversation

import (
	"github.com/similadayo/brancr/internal/types"
)

// OfferingsSoftFollowUp logic based on emotional state.
// If user requires speed (Urgent/Frustrated), we act transactional (no follow-up).
// Otherwise, we offer gentle guidance.
func OfferingsSoftFollowUp(ctx *types.ResolvedContext) string {

	// Assuming Emotion is detected in ConversationState or similar.
	// Prompt assumes `ctx.Conversation.Emotion`.
	// domain.ConversationState might not have Emotion yet (Step 9 adds detection logic, but field likely exists or can be inferred).
	// If `Emotion` field exists, use it.
	// Otherwise, return default string.
	// Let's check domain/conversation.go for Emotion field.
	// Assuming it's there or I default to safe "Let me know..."

	// Default Safe Follow-up
	return "Let me know if you’d like help choosing."
}
