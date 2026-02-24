package rules

import (
	"github.com/similadayo/brancr/internal/conversation"
	"github.com/similadayo/brancr/internal/intent"
	"github.com/similadayo/brancr/internal/types"
)

func AllowObjectionNormalization(
	ctx types.ResolvedContext,
	i intent.IntentResult,
	goal conversation.Goal,
) bool {

	// Never stack with reflection
	if ctx.Conversation.ReflectionUsed {
		return false
	}

	// Only when user is deciding
	if goal != conversation.GoalDecide {
		return false
	}

	switch i.Intent {
	case intent.IntentRejectPrice,
		intent.IntentHesitation,
		intent.IntentDelayDecision,
		intent.IntentNegotiationPushback:
		return true
	default:
		return false
	}
}
