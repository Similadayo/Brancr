package context

import (
	"time"

	"github.com/similadayo/brancr/internal/ai_v4/memory"
	"github.com/similadayo/brancr/internal/ai_v4/types"
)

// ContextSnapshot represents the authoritative, read-only state of the world
// at the beginning of an AI turn. It is the single source of truth.
type ContextSnapshot struct {
	// Request Context
	Message   string
	Platform  string
	Timestamp time.Time

	// Business Context (Authoritative)
	Business  types.BusinessProfile
	Offerings []types.Offering
	Payment   types.PaymentConfig

	// Customer Context
	Customer types.CustomerInfo

	// Memory (Pointer to current state)
	Conversation *memory.ConversationMemory
	Operational  *memory.OperationalMemory

	// Order Snapshot (Read-Only)
	// If an order is active, this contains its last known state.
	// It is NOT synthesized here; it is loaded.
	ActiveOrder *types.OrderSnapshot

	// Tenant Knowledge (REQUIRED)
	// Must be loaded before intent classification.
	TenantKnowledge *TenantKnowledgeSnapshot
}
