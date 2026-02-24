package v3

import (
	"strings"

	"github.com/google/uuid"
)

// =============================================================================
// NO SURPRISE ORDERS - SYSTEM CONTRACT GUARDS
// =============================================================================

// GuardNoMutationAfterConfirm effectively locks the order state.
// If the order is confirmed, ANY attempt to mutate it (Add/Remove) triggers a clarification.
func GuardNoMutationAfterConfirm(ctx *ConversationContext) *Action {
	if ctx.CurrentOrder == nil {
		return nil
	}

	// LOCKED STATES
	if ctx.CurrentOrder.State == StateConfirmed ||
		ctx.CurrentOrder.State == StateAwaitingPayment ||
		ctx.CurrentOrder.State == StatePaid ||
		ctx.CurrentOrder.State == StateCompleted {

		return &Action{
			Type:    ActionAskClarification,
			Allowed: true,
			Reason:  "Order locked — no mutation allowed",
			Data: ActionData{
				Message: "Your order is already confirmed. Would you like to proceed to payment or cancel the order to start over?",
			},
		}
	}

	return nil
}

// GuardDuplicateAdd checks if an item is already in the cart to prevent accidental inflation.
// If it returns true, the caller should ask for confirmation before adding quantity.
func GuardDuplicateAdd(ctx *ConversationContext, productID uuid.UUID) bool {
	// Check PendingCart (Draft)
	for _, item := range ctx.PendingCart {
		if item.ProductID == productID {
			return true // Already exists
		}
	}

	// Check CurrentOrder items (if synced)
	if ctx.CurrentOrder != nil {
		for _, item := range ctx.CurrentOrder.Items {
			if item.ProductID == productID {
				return true
			}
		}
	}

	return false
}

// IsAffirmationOnly detects if a message is purely agreement, which should NEVER trigger mutation.
// Used to prevent "Yes" -> "Add Item" hallucinations.
func IsAffirmationOnly(msg string) bool {
	switch strings.TrimSpace(strings.ToLower(msg)) {
	case "yes", "yeah", "ok", "okay", "correct", "that's right", "sure", "yep", "proceed", "confirm", "good":
		return true
	}
	return false
}

// CanReferencePastRecommendation ensures the AI doesn't hallucinate "As I mentioned".
// Use this to strip "As I said" phrases if we haven't actually made a recommendation.
func CanReferencePastRecommendation(ctx *ConversationContext) bool {
	return ctx.Obligations != nil &&
		ctx.Obligations.LastRecommendation != ""
}

// RequiresExplicitData checks if an intent MUST have structured data extracted.
// If the composer fails to extract data for these intents, we should fail fast.
func RequiresExplicitData(intentType IntentType) bool {
	return intentType == IntentPriceInquiry ||
		intentType == IntentProductInquiry ||
		intentType == IntentPaymentQuery
}

// MustUseSnapshot enforces that Confirmed orders MUST use the snapshot rules.
func MustUseSnapshot(ctx *ConversationContext) bool {
	return ctx.CurrentOrder != nil &&
		ctx.CurrentOrder.Snapshot != nil
}
