package ai

import "github.com/similadayo/brancr/internal/intent"

// ModelForIntent determines the appropriate cost/performance tier for a given intent.
// This allows dynamic optimization (e.g. cheap model for greetings, smart model for negotiation).
func ModelForIntent(i intent.IntentType) string {
	switch i {
	case intent.IntentNegotiatePrice:
		return ModelFull // Requires high reasoning to adhere to limits and strategy
	case intent.IntentRequestPayment:
		return ModelMini // Requires accuracy but less "creative" reasoning
	case intent.IntentGreeting, intent.IntentUnknown:
		return ModelNano // Very simple or needs clarification
	default:
		return ModelMini // Default to balanced tier
	}
}

// ModelSelector is a compatibility struct for V2 architecture
type ModelSelector struct {
	DefaultModel string
}

func NewModelSelector() *ModelSelector {
	return &ModelSelector{
		DefaultModel: ModelMini,
	}
}

func (s *ModelSelector) SelectModelForTask(
	intentStr string,
	message string,
	isNegotiating bool,
	negotiationRound int,
	customerOffer float64,
	productPrice float64,
) string {
	// Simple mapping back to intent constants
	var i intent.IntentType
	switch intentStr {
	case "price_negotiation", "negotiate_price":
		i = intent.IntentNegotiatePrice
	case "payment_request", "request_payment":
		i = intent.IntentRequestPayment
	case "greeting":
		i = intent.IntentGreeting
	default:
		i = intent.IntentUnknown
	}
	return ModelForIntent(i)
}
