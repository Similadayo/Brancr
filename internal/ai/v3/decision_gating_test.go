package v3

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDecisionGating_CommitmentGradient(t *testing.T) {
	resolver := NewActionResolver()

	// Setup context with pending items
	ctx := &ConversationContext{
		CommitmentGradient: 0.0, // Low commitment
		PendingCart: []PendingCartItem{
			{ProductName: "Test Item", Quantity: 1, UnitPrice: 1000},
		},
		CurrentOrder: nil,
	}

	// Case 1: Low Commitment (0.5) trying to pay
	ctx.CommitmentGradient = 0.5
	intent := Intent{Type: IntentPaymentQuery, Confidence: 0.9}

	action := resolver.Decide(ctx, intent)

	// V3 is more conservative - shows cart instead of answering directly
	assert.NotEqual(t, ActionShowPayment, action.Type, "Should not show payment screen for low commitment")
	assert.Equal(t, ActionShowCart, action.Type, "V3 shows cart for low commitment (conservative)")
	assert.False(t, action.RequiresTx, "Should not require transaction (DB write)")

	// Case 2: High Commitment (0.9) trying to pay
	ctx.CommitmentGradient = 0.9

	action = resolver.Decide(ctx, intent)

	// V3 is conservative - shows cart even for high commitment (requires explicit checkout)
	assert.Equal(t, ActionShowCart, action.Type, "V3 shows cart (user must explicitly checkout)")
	assert.False(t, action.RequiresTx, "V3 doesn't auto-create orders, waits for explicit checkout")
}

func TestDecisionGating_Checkout(t *testing.T) {
	resolver := NewActionResolver()

	// Setup context
	ctx := &ConversationContext{
		CommitmentGradient: 0.2,
		PendingCart: []PendingCartItem{
			{ProductName: "Test Item", Quantity: 1},
		},
		CurrentOrder: nil,
	}

	intent := Intent{Type: IntentCheckout, Confidence: 0.9}

	// Case 1: Low Commitment -> Downgrade to ShowCart or Confirmation
	action := resolver.Decide(ctx, intent)

	assert.NotEqual(t, ActionCollectAddress, action.Type, "Should not start checkout flow")
	assert.Equal(t, ActionShowCart, action.Type, "Should fallback to showing cart")
	assert.False(t, action.RequiresTx)

	// Case 2: High Commitment
	ctx.CommitmentGradient = 0.9
	// Assuming address needed
	ctx.CurrentOrder = &Order{} // Mock existing order struct (or draft)

	action = resolver.Decide(ctx, intent)
	// Requires address
	assert.Equal(t, ActionCollectAddress, action.Type)
}
