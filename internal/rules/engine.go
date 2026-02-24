package rules

import (
	"github.com/similadayo/brancr/internal/conversation"
	"github.com/similadayo/brancr/internal/intent"
	"github.com/similadayo/brancr/internal/types"
)

// Engine defines the interface for the deterministic rules engine.
// It acts as the authority on what actions are permissible given the current context and intent.
type Engine interface {
	Evaluate(
		ctx types.ResolvedContext,
		intent intent.IntentResult,
		goal conversation.Goal,
	) RuleResult
}
