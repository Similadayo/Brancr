package rules

import (
	"github.com/similadayo/brancr/internal/conversation"
	"github.com/similadayo/brancr/internal/domain"
	"github.com/similadayo/brancr/internal/types"
)

// ShouldRecommendHuman determines if we should offer a human handoff.
// Escalation is allowed ONLY if specific conditions are met.
func ShouldRecommendHuman(ctx *types.ResolvedContext) bool {

	// 1. Explicit Request
	if ctx.Conversation.ExplicitHumanRequest {
		return true
	}

	// FIX 4.3 - Degraded State Check
	// If insights are stale (e.g. timeout), do NOT escalate aggressively.
	// We prefer a calm, neutral fallback over a "blind" escalation.
	if ctx.Conversation.InsightStatus == domain.InsightStatusStale {
		// Only escalate if explicitly asked (the user knows best)
		if ctx.Conversation.ExplicitHumanRequest {
			return true
		}
		// Otherwise, suppress frustration/risk based checks to avoid false positives
		return false
	}

	// 2. Emotional Load (Frustration Persistence)
	// We check for "FRUSTRATED" status.
	// We compare string value from conversation package.
	if ctx.Conversation.Emotion == string(conversation.EmotionFrustrated) &&
		ctx.Conversation.FrustrationTurns >= 2 {
		return true
	}

	// 3. Negotiation Deadlock
	if ctx.Conversation.NegotiationRounds >= 4 &&
		!ctx.Conversation.HasProgress {
		return true
	}

	// 4. High Risk
	if ctx.Conversation.IsHighRiskTopic {
		return true
	}

	// 5. Low Confidence
	if ctx.Conversation.LowConfidence {
		return true
	}

	return false
}
