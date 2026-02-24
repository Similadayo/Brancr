package rules

import (
	"time"

	"github.com/similadayo/brancr/internal/types"
)

func AllowReengagement(ctx types.ResolvedContext, silenceAfter time.Duration) bool {

	if ctx.Conversation.HasReengaged {
		return false
	}

	if time.Since(ctx.Conversation.LastAIMessageAt) < silenceAfter {
		return false
	}

	if ctx.Conversation.HasPendingAction {
		return false
	}

	return true
}
