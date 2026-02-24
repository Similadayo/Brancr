package conversation

import (
	"fmt"

	"github.com/similadayo/brancr/internal/types"
)

// PostPaymentAcknowledgement generates a clean, reassuring message after payment confirmation.
// PHASE 7: This is language-only, not logic. No upsells, no questions.
func PostPaymentAcknowledgement(ctx *types.ResolvedContext) string {
	if ctx.Order == nil {
		return "Payment confirmed ✅ Your order is being processed."
	}

	return fmt.Sprintf(
		"Payment confirmed ✅\nYour order #%s is being processed. I'll update you once it's ready.",
		ctx.Order.OrderID,
	)
}
