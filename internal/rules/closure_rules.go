package rules

import (
	"github.com/similadayo/brancr/internal/conversation"
	"github.com/similadayo/brancr/internal/types"
)

func AllowClosure(ctx types.ResolvedContext, goal conversation.Goal) bool {

	if goal != conversation.GoalResolve {
		return false
	}

	if ctx.Conversation.HasPendingAction {
		return false
	}

	if ctx.Conversation.HasAskedQualification {
		return false
	}

	if ctx.Conversation.IsBoundaryResponse {
		return false
	}

	return true
}
