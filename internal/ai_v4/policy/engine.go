package policy

import (
	"context"
	"fmt"
	"strings"

	"github.com/similadayo/brancr/internal/ai_v4/types"
	"go.uber.org/zap"
)

// Engine evaluates business rules and determines what AI can do
type Engine interface {
	Evaluate(ctx context.Context, resolved *types.ResolvedContext, intent *types.IntentResult) (*types.PolicyDecision, error)
	ValidateSafety(ctx context.Context, resolved *types.ResolvedContext, response *types.AIResponse) error
}

// DefaultEngine implements policy evaluation with hard-coded escalation rules
type DefaultEngine struct {
	logger *zap.Logger
}

// NewEngine creates a new policy engine
func NewEngine(logger *zap.Logger) *DefaultEngine {
	return &DefaultEngine{logger: logger}
}

// Evaluate applies business rules and returns policy decision
func (e *DefaultEngine) Evaluate(ctx context.Context, resolved *types.ResolvedContext, intent *types.IntentResult) (*types.PolicyDecision, error) {
	decision := &types.PolicyDecision{
		AllowedActions: make([]types.ActionType, 0),
		BlockedActions: make([]types.ActionType, 0),
		Constraints:    &types.ActionConstraints{},
	}

	// Check mandatory escalation rules (NON-NEGOTIABLE)
	if mustEscalate, reason := e.checkMandatoryEscalation(resolved, intent); mustEscalate {
		decision.MustEscalate = true
		decision.EscalateReason = reason
		e.logger.Info("policy: mandatory escalation triggered",
			zap.String("reason", reason),
			zap.String("intent", string(intent.Primary)))
		return decision, nil
	}

	// Determine allowed actions based on intent
	decision.AllowedActions = e.getAllowedActions(intent.Primary, resolved)

	// Set constraints from business settings
	decision.Constraints = e.getConstraints(resolved)

	return decision, nil
}

// checkMandatoryEscalation implements hard-coded escalation rules
// These are NON-NEGOTIABLE and policy-driven, not model-driven
func (e *DefaultEngine) checkMandatoryEscalation(resolved *types.ResolvedContext, intent *types.IntentResult) (bool, string) {
	// Rule 1: Customer explicitly requests human (ALWAYS honor)
	if intent.Primary == types.IntentHumanRequest {
		return true, "Customer requested human agent"
	}

	// Rule 2: Complaints ALWAYS escalate (protect brand reputation)
	if intent.Primary == types.IntentComplaint {
		return true, "Customer complaint requires human attention"
	}

	// Rule 3: Refund requests ALWAYS escalate (financial decision)
	if intent.Primary == types.IntentRefund {
		return true, "Refund request requires human approval"
	}

	// Rule 4: Low confidence - DISABLED for normal order/conversation flows
	// Only escalate if truly unknown intent and not in any order-related context
	if intent.Confidence < 0.3 && intent.Primary == types.IntentUnknown {
		// Don't escalate if in ordering state or has active order
		if resolved.Memory != nil &&
			(resolved.Memory.State == types.StateOrdering ||
				resolved.Memory.State == types.StateAwaitingPayment ||
				resolved.Memory.ActiveOrderID != nil) {
			return false, ""
		}
		// Don't escalate for purchase, inquiry, greeting, price check, confirm order
		safeIntents := []types.Intent{
			types.IntentPurchase, types.IntentInquiry, types.IntentGreeting,
			types.IntentPriceCheck, types.IntentConfirmOrder, types.IntentOrderStatus,
			types.IntentThankYou, types.IntentGoodbye,
		}
		for _, safe := range safeIntents {
			if intent.Primary == safe {
				return false, ""
			}
		}
		// Only escalate truly unknown intents with very low confidence
		// return true, "Unable to understand request - need human judgment"
		// DISABLED: Let AI try to handle it instead of escalating
		return false, ""
	}

	// Rule 5: Negative sentiment with complaint
	if intent.Primary == types.IntentComplaint && resolved.Signals.DetectedEmotion == types.EmotionHappy {
		// Paradox - complaint but happy? Probably handled.
	}

	// Rule 6: Repeated failures (cycle detection handled by state machine usually)

	// Rule 7: Payment stage escalation (Fix 7: Only escalate on issues)
	if resolved.Memory != nil && resolved.Memory.State == types.StateAwaitingPayment {
		if intent.Primary == types.IntentRefund || intent.Primary == types.IntentComplaint {
			return true, "Payment issue or dispute"
		}
		// Otherwise, stay in payment flow (silent escalation handled by action if proof received)
		return false, ""
	}

	// Original Rule 5: Strong negative emotion (from signals) - now Rule 8
	if resolved.Signals != nil && resolved.Signals.DetectedEmotion == types.EmotionAngry {
		return true, "Customer appears angry - human intervention needed"
	}

	// Original Rule 6: Prolonged negotiation (>3 rounds without agreement) - now Rule 9
	if resolved.Memory != nil && resolved.Memory.NegotiationRounds > 3 {
		return true, "Prolonged negotiation - human decision needed"
	}

	return false, ""
}

// getAllowedActions returns actions allowed for a given intent
func (e *DefaultEngine) getAllowedActions(intent types.Intent, resolved *types.ResolvedContext) []types.ActionType {
	actions := make([]types.ActionType, 0)

	switch intent {
	case types.IntentGreeting, types.IntentInquiry:
		actions = append(actions, types.ActionSendOfferings, types.ActionNoAction)

	case types.IntentPriceCheck:
		actions = append(actions, types.ActionSendOfferings, types.ActionNoAction)

	case types.IntentPurchase:
		actions = append(actions,
			types.ActionInitializeOrder,
			types.ActionAddToOrder,
			types.ActionConfirmOrder,
			types.ActionRequestPayment,
		)

	case types.IntentNegotiation:
		actions = append(actions, types.ActionNoAction) // Negotiation handled separately

	case types.IntentOrderStatus:
		actions = append(actions, types.ActionNoAction)

	case types.IntentThankYou, types.IntentGoodbye:
		actions = append(actions, types.ActionNoAction)

	default:
		actions = append(actions, types.ActionNoAction)
	}

	return actions
}

// getConstraints returns action constraints based on business settings
func (e *DefaultEngine) getConstraints(resolved *types.ResolvedContext) *types.ActionConstraints {
	constraints := &types.ActionConstraints{
		MaxDiscount:     0.10, // Default 10% max discount
		RequireApproval: []types.ActionType{},
		ForbiddenTopics: []string{},
	}

	// Find minimum discount from offerings
	// (Business can set NegotiationMinPrice on products)
	for _, offering := range resolved.Offerings {
		if offering.NegotiationMinPrice > 0 && offering.Price > 0 {
			discount := (offering.Price - offering.NegotiationMinPrice) / offering.Price
			if discount > constraints.MaxDiscount {
				constraints.MaxDiscount = discount
			}
		}
	}

	return constraints
}

// ValidateSafety checks the generated response for policy violations
func (e *DefaultEngine) ValidateSafety(ctx context.Context, resolved *types.ResolvedContext, response *types.AIResponse) error {
	// 1. Check for forbidden content patterns (hallucinations, sensitive data requests)
	if err := e.checkForbiddenContent(response.ReplyText); err != nil {
		e.logger.Warn("safety_validation_failed",
			zap.String("reason", err.Error()),
			zap.String("reply", response.ReplyText))
		return err
	}

	// 2. Validate proposed actions against offerings
	if err := e.validateActions(resolved, response.ProposedActions); err != nil {
		e.logger.Warn("action_validation_failed",
			zap.String("reason", err.Error()))
		return err
	}

	return nil
}

func (e *DefaultEngine) checkForbiddenContent(text string) error {
	lower := strings.ToLower(text)

	// "Never Do" List
	forbidden := map[string]string{
		"send me your credit card": "requesting_credit_card",
		"send me your cv":          "requesting_cvv", // heuristic
		"cvv":                      "requesting_cvv",
		"password":                 "requesting_password",
		"i promise":                "making_promises",
		"guarantee":                "making_guarantees",
		"100%":                     "making_absolute_claims",
	}

	for phrase, reason := range forbidden {
		if strings.Contains(lower, phrase) {
			return fmt.Errorf("policy violation: %s", reason)
		}
	}
	return nil
}

func (e *DefaultEngine) validateActions(resolved *types.ResolvedContext, actions []types.ProposedAction) error {
	for _, action := range actions {
		// Verify InitializeOrder actions references valid products
		if action.Type == types.ActionInitializeOrder {
			// This would require checking payload against resolved.Offerings
			// Taking a lightweight approach for now
		}
	}
	return nil
}
