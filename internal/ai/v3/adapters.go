package v3

import (
	"fmt"

	"github.com/similadayo/brancr/internal/domain"
	"github.com/similadayo/brancr/internal/types"
)

// ToResolvedContext converts the legacy V3 ConversationContext into the canonical types.ResolvedContext.
func ToResolvedContext(v3Ctx *ConversationContext) types.ResolvedContext {
	// 1. Map Order State
	var orderState *domain.OrderState
	if v3Ctx.CurrentOrder != nil {
		orderState = &domain.OrderState{
			OrderID: domain.OrderID(v3Ctx.CurrentOrder.ID.String()),
			TotalPrice: domain.Money{
				Amount:   int64(v3Ctx.CurrentOrder.Total * 100),
				Currency: v3Ctx.CurrentOrder.Currency,
			},
			CreatedAt: v3Ctx.CurrentOrder.CreatedAt,
			Items:     mapOrderItems(v3Ctx.CurrentOrder.Items),
			Payment: domain.PaymentState{
				Status: mapV3StateToPaymentStatus(v3Ctx.CurrentOrder.State),
			},
		}
		if v3Ctx.CurrentOrder.Snapshot != nil {
			orderState.Negotiation.Phase = domain.NegotiationLocked
		}
	} else if len(v3Ctx.PendingCart) > 0 {
		orderState = &domain.OrderState{
			Items: mapPendingItems(v3Ctx.PendingCart),
		}
	}

	// 2. Map Conversation State
	convState := domain.ConversationState{
		SessionID:    v3Ctx.ConversationID.String(),
		CustomerID:   v3Ctx.CustomerID.String(),
		LastIntent:   string(v3Ctx.CurrentIntent),
		MessageCount: len(v3Ctx.RecentMessages),
		// Phase 1: Initialize transaction state from context (Defaulted in Build)
		TransactionState: v3Ctx.TransactionState,
	}
	if v3Ctx.Obligations != nil {
		convState.HasAskedQualification = v3Ctx.Obligations.HasAskedQualification
		// Note: ReflectionUsed and ObjectionNormalized are typically transient for the response generation
		// but we map them if they exist in obligations/context for continuity.
		// Since they are new, we assume they default to false or come from v3Ctx if we tracked them there.
		// For now, we leave them as default (false) when building from context,
		// as they are set during the pipeline's response phase for the current turn.
	}
	if v3Ctx.Obligations != nil {
		convState.HasAskedQualification = v3Ctx.Obligations.HasAskedQualification
		// Note from Step 3: We don't have persistent source for ReflectionUsed/ObjectionNormalized yet
		// so they remain false by default here.
		// Step 4: IsRefusal also comes from context inference or obligations logic, assumed false unless persisted.
		// Step 5: IsBoundaryResponse is transient for the response generation.
		// Step 6: HasClosed is persisted to avoid repetition. HasPendingAction is transient.
		// Step 7: LastAIMessageAt and HasReengaged are persisted.
		convState.LastAIMessageAt = v3Ctx.Obligations.LastAIMessageAt
		convState.HasReengaged = v3Ctx.Obligations.HasReengaged
		// Step 8: NegotiationRounds is persisted.
		convState.NegotiationRounds = v3Ctx.Obligations.NegotiationRounds
		convState.HasShownOfferings = v3Ctx.Obligations.HasShownOfferings
		// Step 6: HasClosed is persisted.
		convState.HasClosed = v3Ctx.Obligations.HasClosed
	}

	// Find last user message time
	for i := len(v3Ctx.RecentMessages) - 1; i >= 0; i-- {
		msg := v3Ctx.RecentMessages[i]
		if msg.Role == "customer" {
			convState.LastMessageTime = msg.Timestamp
			break
		}
	}
	// Note: Negotiation state from v3Ctx is not mapped here because domain.ConversationState
	// does not support it. It is tracked on the OrderState level in domain.

	// 3. Map Customer Memory
	memory := domain.CustomerMemory{
		CustomerID: v3Ctx.CustomerID.String(),
	}
	if v3Ctx.CustomerHistory != nil {
		memory.Preferences = v3Ctx.CustomerHistory.PreferredProducts
	}

	// 4. Map Merchant Config
	merchant := domain.MerchantConfig{
		MerchantID:             v3Ctx.TenantID.String(),
		Currency:               "NGN",
		MaxNegotiationAttempts: 3, // Default
		TonePreset:             v3Ctx.BusinessTone,
		BusinessName:           v3Ctx.BusinessName,
		Industry:               v3Ctx.BusinessType,
		Description:            v3Ctx.BusinessDescription,
		AllowedPaymentMethods:  []string{"transfer", "card"}, // defaults
	}
	if v3Ctx.PaymentInfo != nil {
		merchant.AllowedPaymentMethods = []string{v3Ctx.PaymentInfo.Provider}
	}

	// 5. Map Catalog (Products & Services)
	var catalog []domain.Offering

	// Map Products
	for _, p := range v3Ctx.Products {
		price := fmt.Sprintf("%s %.2f", p.Currency, p.Price)
		if !p.InStock {
			price += " (Out of Stock)"
		}
		catalog = append(catalog, domain.Offering{
			Title:       p.Name,
			Description: p.Description,
			PriceText:   price,
		})
	}

	// Map Services
	for _, s := range v3Ctx.Services {
		price := "Contact for price"
		if s.Price > 0 {
			price = fmt.Sprintf("%s %.2f", s.Currency, s.Price)
		}
		catalog = append(catalog, domain.Offering{
			Title:       s.Name,
			Description: s.Description,
			PriceText:   price,
			Extras:      []string{s.Duration},
		})
	}

	return types.ResolvedContext{
		Order:        orderState,
		Conversation: convState,
		Memory:       memory,
		Merchant:     merchant,
		Catalog:      catalog,
	}
}

// Helpers

func mapV3StateToPaymentStatus(state OrderState) domain.PaymentStatus {
	switch state {
	case StatePaid, StateCompleted:
		return domain.PaymentCompleted
	case StateAwaitingPayment:
		return domain.PaymentRequested
	default:
		return domain.PaymentNotRequested
	}
}

func mapOrderItems(items []OrderItem) []domain.OrderItem {
	out := make([]domain.OrderItem, len(items))
	for i, item := range items {
		out[i] = domain.OrderItem{
			Name:      item.ProductName,
			Quantity:  item.Quantity,
			UnitPrice: domain.Money{Amount: int64(item.UnitPrice * 100)},
		}
	}
	return out
}

func mapPendingItems(items []PendingCartItem) []domain.OrderItem {
	out := make([]domain.OrderItem, len(items))
	for i, item := range items {
		out[i] = domain.OrderItem{
			Name:      item.ProductName,
			Quantity:  item.Quantity,
			UnitPrice: domain.Money{Amount: int64(item.UnitPrice * 100)},
		}
	}
	return out
}
