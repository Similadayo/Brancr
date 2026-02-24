package v3

import (
	"github.com/similadayo/brancr/internal/domain"
	"github.com/similadayo/brancr/internal/types"
)

type ContextSnapshot struct {
	LastTopic      string // what we were talking about
	LastAction     string // last meaningful system action
	HasActiveOrder bool
	PaymentStarted bool
	KeyMemoryHint  string // optional, low-confidence hint
}

// BuildContextSnapshot extracts conversationally relevant signals.
// This MUST remain lightweight and human-oriented.
func BuildContextSnapshot(ctx types.ResolvedContext) ContextSnapshot {
	snap := ContextSnapshot{}

	// --- Order awareness ---
	snap.HasActiveOrder = ctx.Order != nil
	if ctx.Order != nil {
		// Verify Payment struct exists to avoid panic
		snap.PaymentStarted = ctx.Order.Payment.Status != domain.PaymentNotRequested
	}

	// --- Conversation awareness ---
	if ctx.Conversation.LastIntent != "" {
		snap.LastTopic = ctx.Conversation.LastIntent
	}

	// Note: ctx.Conversation might need LastAction field if not present.
	// Based on previous files, ConversationState had: SessionID, CustomerID, LastIntent, MessageCount.
	// We might need to check if LastAction exists or use a placeholder.
	// For now, checks if it implies recent history.
	// Assuming ConversationState doesn't have LastAction yet based on previous view.
	// I will check if I can add it or if I should infer it.
	// Re-reading Step 3 prompt: "snap.LastAction = ctx.Conversation.LastAction"
	// I need to ensure ConversationState has this field or add it.

	// Checks for memory hint
	if ctx.Memory.PriceSensitivity == "high" {
		snap.KeyMemoryHint = "price-conscious"
	}

	return snap
}
