package decision

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/action"
	"github.com/similadayo/brancr/internal/ai_contract"
	"go.uber.org/zap"
)

// =============================================================================
// DECISION ENGINE - The Authority Boundary
// =============================================================================
// This package is the ONLY place where AI suggestions become execution decisions.
//
// PHASE-5: This is the critical safety layer.
// AI suggests → Decision Engine decides → Action executes
//
// RULES:
// - NO AI calls allowed here
// - ALL suggestions must be validated against rules
// - ONE action selected (or none)
// - EVERY decision must be explainable
// - NO direct execution from AI output
// =============================================================================

// Engine evaluates AI suggestions and produces execution decisions.
type Engine struct {
	// rulesEngine rules.Engine // PHASE-5: Will be added when rules engine is ready
	logger *zap.Logger
}

// NewEngine creates a new decision engine.
// PHASE-5: rulesEngine parameter will be added later
func NewEngine(logger *zap.Logger) *Engine {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Engine{
		// rulesEngine: rulesEngine,
		logger: logger,
	}
}

// DecisionInput contains everything needed to make a decision.
type DecisionInput struct {
	// TenantID identifies the business
	TenantID uuid.UUID

	// ConversationID identifies the conversation
	ConversationID uuid.UUID

	// CustomerID identifies the customer
	CustomerID uuid.UUID

	// Intent is the detected user intent
	Intent string

	// Confidence is the AI's confidence in the intent
	Confidence float64

	// Suggestions are the AI's suggested actions
	Suggestions []ai_contract.AISuggestedAction

	// Context provides decision-making context
	Context DecisionContext
}

// DecisionContext provides state and rules context for decision-making.
type DecisionContext struct {
	// OrderStatus is the current order state (if any)
	OrderStatus string

	// CartItemCount is the number of items in cart
	CartItemCount int

	// TotalAmount is the current cart/order total
	TotalAmount float64

	// PaymentReceived indicates if payment has been confirmed
	PaymentReceived bool

	// ConversationTurnCount is the number of turns in this conversation
	ConversationTurnCount int

	// LastActionType is the last action that was executed
	LastActionType string

	// UserEmotion is the detected user emotion
	UserEmotion string

	// BusinessRules contains tenant-specific rules
	BusinessRules map[string]interface{}
}

// DecisionResult is the output of the decision engine.
type DecisionResult struct {
	// Action is the selected action to execute
	Action Action

	// Allowed indicates if the action is permitted
	Allowed bool

	// Reason explains why this decision was made
	Reason string

	// RuleTrace is the list of rules that were evaluated
	RuleTrace []string

	// RejectedActions are the suggestions that were not selected
	RejectedActions []RejectedAction

	// DecisionTime is when this decision was made
	DecisionTime time.Time
}

// Action represents the final action to execute.
type Action struct {
	// Type is the action type
	Type action.ActionType

	// Params contains action-specific parameters
	Params map[string]interface{}

	// Source indicates where this action came from
	Source string // "ai_suggestion", "rule_override", "fallback"
}

// RejectedAction represents a suggestion that was not selected.
type RejectedAction struct {
	// Type is the action type that was rejected
	Type string

	// Score was the AI's confidence in this action
	Score float64

	// Reason explains why it was rejected
	Reason string

	// RuleID identifies the rule that rejected it (if any)
	RuleID string
}

// =============================================================================
// DECISION ENGINE LOGIC
// =============================================================================

// Evaluate takes AI suggestions and produces a single execution decision.
// PHASE-5: This is the authority boundary - AI cannot bypass this.
func (e *Engine) Evaluate(ctx context.Context, input DecisionInput) (*DecisionResult, error) {
	start := time.Now()

	// PHASE-5 LOGGING: Log decision input
	e.logger.Info("decision_engine_evaluating",
		zap.String("intent", input.Intent),
		zap.Float64("confidence", input.Confidence),
		zap.Int("num_suggestions", len(input.Suggestions)),
	)

	result := &DecisionResult{
		RuleTrace:       make([]string, 0),
		RejectedActions: make([]RejectedAction, 0),
		DecisionTime:    time.Now(),
	}

	// PHASE-5: If no suggestions, return NO_ACTION
	if len(input.Suggestions) == 0 {
		result.Allowed = false
		result.Reason = "No actions suggested by AI"
		result.RuleTrace = append(result.RuleTrace, "NO_SUGGESTIONS")
		return result, nil
	}

	// PHASE-5: Evaluate each suggestion against rules
	var selectedAction *ai_contract.AISuggestedAction
	var selectedReason string

	for _, suggestion := range input.Suggestions {
		// Convert to action type
		actionType := action.ActionType(suggestion.Type)

		// PHASE-5: Validate against rules
		allowed, reason, ruleID := e.validateAction(ctx, actionType, input)

		result.RuleTrace = append(result.RuleTrace, fmt.Sprintf("%s: %s", ruleID, reason))

		if allowed {
			// Select this action
			selectedAction = &suggestion
			selectedReason = reason
			e.logger.Info("action_selected",
				zap.String("action", string(actionType)),
				zap.Float64("score", suggestion.Score),
				zap.String("reason", reason),
			)
			break
		} else {
			// Reject this action
			result.RejectedActions = append(result.RejectedActions, RejectedAction{
				Type:   suggestion.Type,
				Score:  suggestion.Score,
				Reason: reason,
				RuleID: ruleID,
			})
			e.logger.Info("action_rejected",
				zap.String("action", string(actionType)),
				zap.Float64("score", suggestion.Score),
				zap.String("reason", reason),
			)
		}
	}

	// PHASE-5: If no action was selected, deny
	if selectedAction == nil {
		result.Allowed = false
		result.Reason = "All suggested actions were rejected by rules"
		result.RuleTrace = append(result.RuleTrace, "ALL_REJECTED")

		// Log decision
		e.logger.Warn("decision_denied",
			zap.String("reason", result.Reason),
			zap.Int("rejected_count", len(result.RejectedActions)),
			zap.Duration("decision_time", time.Since(start)),
		)

		return result, nil
	}

	// PHASE-5: Action selected and allowed
	result.Allowed = true
	result.Reason = selectedReason
	result.Action = Action{
		Type:   action.ActionType(selectedAction.Type),
		Params: selectedAction.Params,
		Source: "ai_suggestion",
	}

	// Log decision
	e.logger.Info("decision_approved",
		zap.String("action", string(result.Action.Type)),
		zap.String("reason", result.Reason),
		zap.Int("rejected_count", len(result.RejectedActions)),
		zap.Duration("decision_time", time.Since(start)),
	)

	return result, nil
}

// validateAction checks if an action is allowed based on rules and state.
// PHASE-5: This is where business rules are enforced.
func (e *Engine) validateAction(
	ctx context.Context,
	actionType action.ActionType,
	input DecisionInput,
) (allowed bool, reason string, ruleID string) {

	// PHASE-5: Basic validation rules
	// These will be expanded with the full rules engine

	// Rule: Cannot place order if cart is empty
	if actionType == action.ActionPlaceOrder {
		if input.Context.CartItemCount == 0 {
			return false, "Cannot place order with empty cart", "EMPTY_CART"
		}
	}

	// Rule: Cannot cancel order if already paid
	if actionType == action.ActionCancelOrder {
		if input.Context.PaymentReceived {
			return false, "Cannot cancel order after payment received", "ORDER_PAID"
		}
	}

	// Rule: Cannot request payment if cart is empty
	if actionType == action.ActionRequestPayment {
		if input.Context.CartItemCount == 0 {
			return false, "Cannot request payment with empty cart", "EMPTY_CART"
		}
		if input.Context.TotalAmount <= 0 {
			return false, "Cannot request payment with zero total", "ZERO_TOTAL"
		}
	}

	// Rule: Low confidence actions require higher threshold
	if input.Confidence < 0.7 {
		// Only allow safe actions with low confidence
		safeActions := map[action.ActionType]bool{
			action.ActionClarifyIntent: true,
			action.ActionAskQuestion:   true,
			action.ActionGreet:         true,
			action.ActionAcknowledge:   true,
			action.ActionShowMenu:      true,
			action.ActionShowProducts:  true,
		}
		if !safeActions[actionType] {
			return false, fmt.Sprintf("Confidence too low (%.2f) for action", input.Confidence), "LOW_CONFIDENCE"
		}
	}

	// PHASE-5: Default allow (will be replaced with full rules engine)
	return true, "Action allowed by decision engine", "DECISION_ALLOWED"
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// GetPrimaryAction returns the highest-scored suggestion.
// This is a helper for decision-making, not the final decision.
func GetPrimaryAction(suggestions []ai_contract.AISuggestedAction) *ai_contract.AISuggestedAction {
	if len(suggestions) == 0 {
		return nil
	}

	primary := &suggestions[0]
	for i := range suggestions {
		if suggestions[i].Score > primary.Score {
			primary = &suggestions[i]
		}
	}
	return primary
}
