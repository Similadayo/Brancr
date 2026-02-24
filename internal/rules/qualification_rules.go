package rules

import (
	"github.com/similadayo/brancr/internal/conversation"
	"github.com/similadayo/brancr/internal/intent"
	"github.com/similadayo/brancr/internal/types"
)

// AllowQualification checks if we should ask a qualification question.
func AllowQualification(ctx types.ResolvedContext, goal conversation.Goal, currentIntent intent.IntentType) bool {
	// 1. One-time check (State from domain conversation)
	if ctx.Conversation.HasAskedQualification {
		return false
	}

	// 2. Don't ask if order exists (User already committed)
	if ctx.Order != nil {
		return false
	}

	// 3. Intent Check (Fix 16.2: Contextual Fluidity)
	// Don't interrupt natural questions/exploration with qualification scripts.
	switch currentIntent {
	case intent.IntentRequestMenu,
		intent.IntentRequestPrices,
		intent.IntentRequestOfferings,
		intent.IntentItemInquiry,
		intent.IntentPriceInquiry:
		return false
	}

	// 4. Goal check
	switch goal {
	case conversation.GoalExplore, conversation.GoalDecide:
		return true
	default:
		return false
	}
}
