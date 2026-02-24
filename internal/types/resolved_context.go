package types

import (
	"github.com/similadayo/brancr/internal/domain"
)

// ResolvedContext represents the validated, single source of truth for the current interaction.
// It is assembled from various domain aggregates.
type ResolvedContext struct {
	Order        *domain.OrderState
	Conversation domain.ConversationState
	Memory       domain.CustomerMemory
	Merchant     domain.MerchantConfig
	Catalog      []domain.Offering
}
