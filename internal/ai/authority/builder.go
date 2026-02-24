package authority

import "github.com/similadayo/brancr/internal/types"

func BuildAuthority(ctx *types.ResolvedContext, intent string) ResponseAuthority {
	a := ResponseAuthority{
		AllowedFacts: make(map[string]bool),
	}

	switch intent {

	case "IntentRequestMenu", "REQUEST_OFFERINGS", "REQUEST_MENU": // Supporting multiple variations
		a.CanSendMenu = true
		a.AllowFollowUpQuestions = false
		a.AllowExplanations = false

	case "IntentRequestPrices", "PRICE_INQUIRY":
		a.CanSendPrices = true
		a.AllowFollowUpQuestions = false
		a.AllowExplanations = false

	case "IntentCancelOrder", "CANCEL_ORDER", "CANCEL":
		// Check if order exists (assuming ctx.Order is not nil means it exists, or check status)
		if ctx.Order != nil && ctx.Order.Payment.Status != "COMPLETED" { // Simplified existence check
			a.CanCancelOrder = true
			a.AllowedFacts["order_exists"] = true
			a.AllowedFacts["order_total"] = true
		}
		a.AllowFollowUpQuestions = true

	case "IntentCheckOrder":
		a.CanCheckOrder = true
		a.AllowedFacts["order_exists"] = true
		a.AllowFollowUpQuestions = false
		a.AllowExplanations = false

	default:
		// Safe neutral mode
		a.AllowFollowUpQuestions = true
	}

	return a
}
