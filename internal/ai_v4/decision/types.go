package decision

import (
	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"

	"github.com/similadayo/brancr/internal/ai_v4/emotion"
	"github.com/similadayo/brancr/internal/ai_v4/role"
	"github.com/similadayo/brancr/internal/ai_v4/signals"
	"github.com/similadayo/brancr/internal/ai_v4/types"
)

// Decision represents the authoritative output of the Decision Engine.
// It dictates what MUST happen next.
type Decision struct {
	// Control flags
	FreezeAI     bool // Stop AI from generating further responses (e.g., handed off)
	FreezeOrders bool // Stop any order mutation (e.g., during escalation)

	// Conversation progression
	NextMode types.ConversationMode

	// Action governance
	AllowedActions []types.ActionType
	BlockedActions []types.ActionType

	// Escalation (optional - if set, escalation determines flow)
	Escalation *types.EscalationRequest

	// Internal reasoning (debug only)
	Reason string
}

// DecisionInput aggregates all necessary context for making a decision.
type DecisionInput struct {
	Snapshot     *v4ctx.ContextSnapshot
	Facts        *signals.SemanticFacts
	Intent       *types.IntentResult
	Semantic     *types.SemanticUnderstanding
	Emotion      *emotion.EmotionState
	Role         *role.RoleContract
	Capabilities role.Capabilities
}
