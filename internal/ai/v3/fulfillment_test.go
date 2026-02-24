package v3

import (
	"testing"
)

// TestFulfillmentPhase verifies the transition to PhaseFulfillment and status checks
func TestFulfillmentPhase(t *testing.T) {
	evaluator := NewStateEvaluator()

	t.Run("Transition to Fulfillment on Confirmed Payment", func(t *testing.T) {
		ctx := &ConversationContext{
			UserSignals: UserSignals{
				PrimaryIntent: Intent{Type: IntentConfirmPayment},
			},
			CommitmentGradient: 1.0, // Fully committed/paid
			PendingCart: []PendingCartItem{
				{ProductName: "Test Item", Quantity: 1, UnitPrice: 100},
			},
		}

		// Previous phase was payment
		newPhase := evaluator.derivePhase(ctx, PhasePayment)
		if newPhase != PhaseFulfillment {
			t.Errorf("Expected transition to PhaseFulfillment, got %s", newPhase)
		}
	})

	t.Run("Stay in Fulfillment Phase", func(t *testing.T) {
		ctx := &ConversationContext{
			UserSignals: UserSignals{
				PrimaryIntent: Intent{Type: IntentStatusInquiry},
			},
			CommitmentGradient: 1.0,
		}

		// Already in fulfillment
		newPhase := evaluator.derivePhase(ctx, PhaseFulfillment)
		if newPhase != PhaseFulfillment {
			t.Errorf("Expected to stay in PhaseFulfillment, got %s", newPhase)
		}
	})

	t.Run("Stay in Payment if not fully paid", func(t *testing.T) {
		ctx := &ConversationContext{
			UserSignals: UserSignals{
				PrimaryIntent: Intent{Type: IntentCheckout},
			},
			CommitmentGradient: 0.9,
		}

		newPhase := evaluator.derivePhase(ctx, PhasePayment)
		// Should stay in payment (or move back if logic dictates, but definitely not fulfillment)
		if newPhase == PhaseFulfillment {
			t.Errorf("Unexpected transition to PhaseFulfillment with gradient 0.9")
		}
	})
}
