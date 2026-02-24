package decision

import (
	"strings"

	"github.com/similadayo/brancr/internal/ai_v4/types"
	"go.uber.org/zap"
)

// PolicyEvaluator enforces business rules and safety guardrails.
// It filters the raw Decision produced by the Engine.
type PolicyEvaluator struct {
	logger *zap.Logger
}

// NewPolicyEvaluator creates a new Policy Evaluator.
func NewPolicyEvaluator(logger *zap.Logger) *PolicyEvaluator {
	return &PolicyEvaluator{
		logger: logger,
	}
}

// Apply enforces constraints on the decision.
// 1. Forbidden Promises
// 2. Forbidden Topics
// 3. Action Constraints
// 4. Safety Freeze
func (p *PolicyEvaluator) Apply(
	d *Decision,
	input *DecisionInput,
) *Decision {

	// STEP 1: FORBIDDEN PROMISES
	// If intent or facts suggest a promise that is forbidden
	for _, promise := range input.Role.ForbiddenPromises {
		// Heuristic check in message for simplicity now, ideally Intent would flag this.
		if strings.Contains(strings.ToLower(input.Snapshot.Message), strings.ToLower(promise)) {
			// Aggressive blocking
			d.AllowedActions = []types.ActionType{}
			d.Reason = "blocked_forbidden_promise"
			return d
		}
	}

	// STEP 2: FORBIDDEN TOPICS
	// If message touches forbidden topics
	for _, topic := range input.Role.ForbiddenTopics {
		if strings.Contains(strings.ToLower(input.Snapshot.Message), strings.ToLower(topic)) {
			d.AllowedActions = []types.ActionType{}
			// Allow clarification/redirection if we had specific actions for it.
			// Ideally we rely on the specific fallback response generator to handle "I cannot discuss X".
			d.Reason = "blocked_forbidden_topic"
			return d
		}
	}

	// STEP 3: ACTION CONSTRAINTS
	// Remove blocked actions from AllowedActions
	allowed := []types.ActionType{}
	for _, action := range d.AllowedActions {
		isBlocked := false
		for _, blocked := range d.BlockedActions {
			if action == blocked {
				isBlocked = true
				break
			}
		}

		if !isBlocked {
			// Additional granular checks could go here
			allowed = append(allowed, action)
		}
	}
	d.AllowedActions = allowed

	// STEP 4: SAFETY FREEZE
	// If no actions remain and confidence is low, freeze ordering
	if len(d.AllowedActions) == 0 && input.Intent.Confidence < 0.4 {
		d.FreezeOrders = true
		d.Reason = "safety_freeze_low_confidence"
	}

	return d
}
