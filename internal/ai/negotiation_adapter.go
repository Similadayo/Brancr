package ai

import (
	"context"

	v3 "github.com/similadayo/brancr/internal/ai/v3"
)

// NegotiationAdapter implements v3.Negotiator
// V3 ONLY - All V2 dependencies removed
type NegotiationAdapter struct{}

func NewNegotiationAdapter() *NegotiationAdapter {
	return &NegotiationAdapter{}
}

// Decide implements v3.Negotiator.Decide
// Simplified V3-only implementation
func (n *NegotiationAdapter) Decide(ctx context.Context, convCtx *v3.ConversationContext, offer float64, productPrice float64) (v3.NegotiationResult, error) {
	// Simple negotiation logic without V2 dependencies
	// V3 pipeline will handle the full negotiation logic internally

	// Default: accept reasonable offers (within 10% of product price)
	discountPct := (productPrice - offer) / productPrice

	if discountPct <= 0.10 {
		// Accept offers within 10% discount
		return v3.NegotiationResult{
			Phase:   "accept",
			Offer:   offer,
			Style:   "friendly",
			Message: "Offer accepted",
		}, nil
	}

	// Counter with a smaller discount
	counterOffer := productPrice * 0.95 // 5% discount
	return v3.NegotiationResult{
		Phase:   "counter",
		Offer:   counterOffer,
		Style:   "professional",
		Message: "Counter offer",
	}, nil
}
