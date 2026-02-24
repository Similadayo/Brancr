package conversation

import "github.com/similadayo/brancr/internal/domain"

func EvaluateConfidence(ctx *domain.ConversationState) domain.ConfidenceLevel {

	// Long conversations soften confidence
	if ctx.TurnCount >= 10 {
		return domain.ConfidenceLow
	}

	// Negotiation fatigue (using existing field)
	if ctx.NegotiationRounds >= 3 {
		return domain.ConfidenceLow
	}

	// Mild decay
	if ctx.TurnCount >= 6 {
		return domain.ConfidenceMedium
	}

	return domain.ConfidenceHigh
}
