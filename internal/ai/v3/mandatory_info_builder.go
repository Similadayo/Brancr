package v3

import "strings"

// BuildMandatoryInfo constructs the mandatory facts from the current context.
// This is DETERMINISTIC logic. No AI.
func BuildMandatoryInfo(ctx *ConversationContext) MandatoryInfo {
	info := MandatoryInfo{}

	// Default to all unsafe
	// But we only populate Products if the intent demands it.

	// 1. Determine Needs based on Intent
	switch ctx.CurrentIntent {
	case IntentProductInquiry:
		info.Products = filterRejected(ctx.Products, ctx.Obligations.RejectedProducts)
		info.PricesRequired = true
	case IntentProductAdvisory: // "recommendation"
		info.RecommendationRequired = true
		info.Products = filterRejected(ctx.Products, ctx.Obligations.RejectedProducts)
	}
	// Note: Payment/Delivery intents are handled via Obligations or Contextual intent mapping below if needed.

	// 2. Determine Needs based on Obligations (Override Intent)
	if ctx.Obligations != nil {
		for _, ob := range ctx.Obligations.PendingObligations {
			if !ob.Answered {
				if ob.QuestionType == "menu_inquiry" || ob.QuestionType == "price_inquiry" {
					info.Products = filterRejected(ctx.Products, ctx.Obligations.RejectedProducts)
					info.PricesRequired = true
				}
				if ob.QuestionType == "recommendation_request" {
					info.RecommendationRequired = true
					info.Products = filterRejected(ctx.Products, ctx.Obligations.RejectedProducts)
				}
				if ob.QuestionType == "payment_inquiry" {
					info.PaymentInfo = true
				}
				if ob.QuestionType == "delivery_inquiry" {
					info.DeliveryInfo = true
				}
			}
		}
	}
	// 3. Populate Forbidden Terms
	if ctx.Obligations != nil {
		info.ForbiddenTerms = append(info.ForbiddenTerms, ctx.Obligations.RejectedProducts...)
	}
	// Add System Phrases to ban
	info.ForbiddenTerms = append(info.ForbiddenTerms,
		"I'll connect you with someone",
		"Here are the options",
		"unhandled intent",
		"as an ai",
	)

	return info
}

// filterRejected removes products that the user has explicitly rejected.
func filterRejected(candidates []Product, rejected []string) []Product {
	if len(rejected) == 0 {
		return candidates
	}

	var safe []Product
	for _, p := range candidates {
		isRejected := false
		for _, r := range rejected {
			if strings.EqualFold(p.Name, r) {
				isRejected = true
				break
			}
		}
		if !isRejected {
			safe = append(safe, p)
		}
	}
	return safe
}
