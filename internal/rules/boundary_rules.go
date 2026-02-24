package rules

import "github.com/similadayo/brancr/internal/types"

func RequiresBoundary(ctx types.ResolvedContext, actionBlocked bool) bool {
	return actionBlocked
}
