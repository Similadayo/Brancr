package memory

import (
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai_v4/types"
)

// =============================================================================
// CONVERSATION MEMORY (Narrative)
// =============================================================================

// ConversationMemory represents the narrative state of the conversation.
// It tracks what was said and where we conceptually are.
type ConversationMemory struct {
	ConversationID uuid.UUID
	Messages       []types.Message
	LastIntent     types.Intent
	Mode           types.ConversationMode
	State          types.ConversationState // e.g. "active", "closed"
	IsEscalated    bool
	Metadata       map[string]any
}

// =============================================================================
// OPERATIONAL MEMORY (Business Process)
// =============================================================================

// OperationalMemory represents the hard business state.
// It tracks orders, payments, and system overrides.
type OperationalMemory struct {
	ActiveOrderID   *uuid.UUID
	PaymentState    *PaymentState
	EscalationState *EscalationState
	OrderSnapshot   *types.OrderSnapshot // Optional, nullable
}

// PaymentState tracks the status of a payment flow.
type PaymentState struct {
	Status      string // "none", "requested", "proof_received", "confirmed"
	LastUpdated time.Time
}

// EscalationState tracks the status of a human handoff.
type EscalationState struct {
	Active    bool
	Reason    string
	Priority  types.EscalationPriority
	StartedAt time.Time
}
