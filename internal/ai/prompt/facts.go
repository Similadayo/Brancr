package prompt

import (
	"fmt"

	"github.com/similadayo/brancr/internal/types"
)

func FactsBlock(ctx *types.ResolvedContext, allowed map[string]bool) string {
	out := "FACTS:\n"

	if allowed["order_exists"] {
		exists := "No"
		if ctx.Order != nil {
			exists = "Yes"
		}
		out += "- Order exists: " + exists + "\n"
	}

	if allowed["order_total"] && ctx.Order != nil {
		// formatting money manually or using a helper if available.
		// Assuming TotalPrice in minor units (e.g. kobo/cents) based on previous file views
		amount := float64(ctx.Order.TotalPrice.Amount) / 100.0
		out += fmt.Sprintf("- Order total: %s %.2f\n", ctx.Order.TotalPrice.Currency, amount)
	}

	return out
}
