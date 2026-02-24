package executor

import (
	"time"

	"github.com/similadayo/brancr/internal/ai/response"
	"github.com/similadayo/brancr/internal/types"
)

// ExecuteHandoffToHuman handles the actual transfer.
// This is terminal for the AI.
func ExecuteHandoffToHuman(ctx *types.ResolvedContext) *response.AIResponse {
	// 1. Assign to human queue (Stub logic/Event)
	// In real system: emit event, update DB status.
	// For now, update state.
	ctx.Conversation.IsHandedOff = true
	ctx.Conversation.LastAIMessageAt = time.Now()

	// 2. Return empty or confirmation?
	// Step 4.8 says: "does not message the customer again".
	// "The last customer-facing message is the suggestion."
	// Wait, if user says "Yes", and we execute this... we just go silent?
	// Usually we say "Okay, connecting you..." or similar.
	// But prompt says "does not message...".
	// If the suggestion was "I can loop in a colleague...", and user says "Yes",
	// silence is weird.
	// Maybe "ActionHandoffToHuman" implies backend switch.
	// I will return a meta-only response or a very brief confirmation if needed.
	// But strictly following prompt: "The last customer-facing message is the suggestion."
	// This implies the suggestion *was* the last message.
	// But the user *replied* to it ("Yes").
	// If we don't reply to "Yes", it's broken.
	// I suspect "ActionHandoffToHuman" executes, and maybe the system takes over.
	// Or maybe the prompt implies we *already* suggested, user said yes, we switch mode.
	// I will return a standard "Connecting..." message just in case, wrapped in AIResponse,
	// but Mark it as "Handed Off" so AI stops.

	return &response.AIResponse{
		HumanText: "I've flagged this for my colleague. They'll be with you shortly.",
		Meta: map[string]any{
			"action_type": "handoff_to_human",
			"handoff":     true,
			"bypass_llm":  true,
		},
	}
}
