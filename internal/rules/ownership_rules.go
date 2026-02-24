package rules

import (
	"github.com/similadayo/brancr/internal/types"
)

func AllowOwnership(ctx types.ResolvedContext, action string) bool {

	if ctx.Conversation.IsRefusal {
		return false
	}

	switch action {
	case "SUGGEST_NEXT_STEP",
		"REQUEST_PAYMENT",
		"BOOK_SERVICE",
		"SHARE_OPTIONS":
		return true
	default:
		return false
	}
}
