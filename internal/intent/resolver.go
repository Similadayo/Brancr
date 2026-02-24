package intent

import (
	"fmt"

	"github.com/similadayo/brancr/internal/types"
)

var aiClassifier AIIntentClassifier

func SetAIClassifier(c AIIntentClassifier) {
	aiClassifier = c
}

// ResolveIntent determines the user's intent from the message and context.
// Flow: Rules -> AI Fallback.
func ResolveIntent(ctx types.ResolvedContext, message string) (IntentResult, error) {
	// 1. Try Deterministic Rules
	if result, ok := MatchByRules(message); ok {
		return result, nil
	}

	// 2. Fallback to AI Classification
	if aiClassifier != nil {
		// Mock context summary for Phase 5. Real implementation would stringify relevant context.
		// "summarizeContext(ctx)" logic would go here or be a helper.
		contextSummary := fmt.Sprintf("OrderID=%v Negotiation=%v Payment=%v",
			ctx.Order != nil, // Simplified summary
			ctx.Order != nil && ctx.Order.Negotiation.Phase != "NONE",
			ctx.Order != nil && ctx.Order.Payment.Status != "NOT_REQUESTED",
		)
		return aiClassifier.Classify(message, contextSummary)
	}

	// 3. Default (should likely not happen if AI is configured, but return Unknown)
	return IntentResult{Intent: IntentUnknown, Confidence: 0, Source: "NONE"}, nil
}
