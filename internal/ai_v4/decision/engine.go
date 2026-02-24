package decision

import (
	"context"
	"fmt"

	"github.com/similadayo/brancr/internal/ai_v4/types"
	"go.uber.org/zap"
)

// Engine is the authoritative brain of the AI.
// It determines what happens next based on all inputs.
type Engine struct {
	logger *zap.Logger
	policy *PolicyEvaluator
}

// NewEngine creates a new Decision Engine.
func NewEngine(logger *zap.Logger) *Engine {
	return &Engine{
		logger: logger,
		policy: NewPolicyEvaluator(logger),
	}
}

// Decide evaluates the inputs and produces a binding Decision.
// It follows a strict strict evaluation order:
// 1. Hard Escalation
// 2. Payment Escalation
// 3. Order Flow Locking
// 4. Capability Enforcement
// 5. Mode Transition (Default)
// 6. Failsafe
func (e *Engine) Decide(
	ctx context.Context,
	input *DecisionInput,
) (*Decision, error) {

	// Initialize robust default decision
	d := &Decision{
		FreezeAI:       false,
		FreezeOrders:   false,
		NextMode:       input.Snapshot.Conversation.Mode, // Default to current
		AllowedActions: []types.ActionType{},
		BlockedActions: []types.ActionType{},
		Reason:         "default_flow",
	}

	// =========================================================================
	// STEP 1: HARD ESCALATION (TERMINAL)
	// =========================================================================
	escalationReason := ""
	intent := input.Intent.Primary

	if intent == types.IntentRefund {
		escalationReason = "customer_requested_refund"
	} else if intent == types.IntentComplaint {
		escalationReason = "customer_complaint"
	} else if intent == types.IntentHumanRequest {
		escalationReason = "customer_requested_human"
	} else if input.Emotion.IsAngry && input.Role.MustEscalateOnAnger {
		escalationReason = "customer_anger_detected"
	}

	if escalationReason != "" {
		d.Escalation = &types.EscalationRequest{
			Reason:   escalationReason,
			Priority: types.PriorityHigh,
		}
		d.FreezeAI = true
		d.FreezeOrders = true
		d.NextMode = types.ModeEscalation
		d.Reason = "hard_escalation_triggered"
		return d, nil
	}

	// =========================================================================
	// STEP 2: ESCALATION BY PAYMENT RULES (TERMINAL)
	// =========================================================================
	// If facts indicate payment proof and role requires escalation
	if input.Facts.MentionsPayment && input.Role.MustEscalateOnPayment {
		// We treat MentionsPayment loosely here as "payment interaction".
		// Ideally we'd look for "PaymentProofReceived" signal if we had one explicit,
		// but facts.MentionsPayment is the closest observational fact.
		// Use stronger signal if available in future, for now complying with instructions.
		d.Escalation = &types.EscalationRequest{
			Reason:   "active_payment_requires_human",
			Priority: types.PriorityHigh,
		}
		d.FreezeAI = true
		d.FreezeOrders = true // Don't mutate orders while human verifies
		d.NextMode = types.ModeEscalation
		d.Reason = "payment_escalation_rule"
		return d, nil
	}

	// =========================================================================
	// STEP 3: ORDER FLOW LOCKING
	// =========================================================================
	hasActiveOrder := input.Snapshot.Operational.ActiveOrderID != nil

	if hasActiveOrder {
		d.Reason = "order_flow_locked"

		// Freeze intent drift: Ignore unrelated questions typically, or treat them as
		// secondary to the order.
		// Block non-order actions.
		d.FreezeOrders = false // We need to mutate the active order!

		// Default mode when order is active
		if d.NextMode != types.ModeOrderCollection && d.NextMode != types.ModePaymentAwareness {
			d.NextMode = types.ModeOrderCollection
		}

		// If we are already in payment, stay there or check logic
		if input.Snapshot.Conversation.Mode == types.ModePaymentAwareness {
			d.NextMode = types.ModePaymentAwareness
		}
	}

	// =========================================================================
	// STEP 4: CAPABILITY ENFORCEMENT & ACTION ALLOWANCE
	// =========================================================================
	// Populate allowed actions based on Intent + Capabilities
	// This is a simplified allowance. The ActionComposer usually proposes,
	// but Decision Engine gates them.

	// Ordering
	// STRICT: Only allow ordering if tenant actually has offerings loaded (Step 5)
	hasOfferings := false
	if input.Snapshot.TenantKnowledge != nil {
		hasOfferings = input.Snapshot.TenantKnowledge.HasProducts ||
			input.Snapshot.TenantKnowledge.HasMenu ||
			input.Snapshot.TenantKnowledge.HasServices
	}

	if input.Capabilities.OrderingEnabled && hasOfferings {
		d.AllowedActions = append(d.AllowedActions,
			types.ActionSendOfferings,
			types.ActionInitializeOrder,
			types.ActionAddToOrder,
			types.ActionConfirmOrder,
			types.ActionUpdateOrder,
		)
	} else {
		d.BlockedActions = append(d.BlockedActions,
			types.ActionInitializeOrder,
			types.ActionAddToOrder,
			types.ActionConfirmOrder,
		)
		// SendOfferings also blocked impliedly as it's not added
	}

	// Payment
	if input.Capabilities.PaymentEnabled {
		d.AllowedActions = append(d.AllowedActions, types.ActionRequestPayment)
	} else {
		d.BlockedActions = append(d.BlockedActions, types.ActionRequestPayment)
	}

	// Negotiation
	if input.Capabilities.NegotiationEnabled {
		// Explicit negotiation actions would go here
	} else {
		// Block negotiation
	}

	// =========================================================================
	// STEP 5: MODE TRANSITION
	// =========================================================================
	// If not already locked by order, determine mode
	if !hasActiveOrder {
		switch intent {
		case types.IntentPurchase, types.IntentConfirmOrder:
			if input.Capabilities.OrderingEnabled {
				d.NextMode = types.ModeOrderCollection
				d.Reason = "intent_purchase_ordering"
			} else if input.Capabilities.SalesEnabled {
				d.NextMode = types.ModeSales // Leads capture?
				d.Reason = "intent_purchase_sales"
			}
		case types.IntentInquiry, types.IntentPriceCheck, types.IntentOrderStatus:
			if input.Snapshot.Conversation.Mode != types.ModeSupport {
				d.NextMode = types.ModeSupport
				d.Reason = "intent_inquiry_support"
			}
		case types.IntentGreeting:
			if input.Capabilities.SalesEnabled {
				d.NextMode = types.ModeSales
			} else {
				d.NextMode = types.ModeDiscovery
			}
			d.Reason = "intent_greeting"
		}

		// SEMANTIC OVERRIDE (Step 3) - Meaning Drives Behavior
		// If we have high semantic confidence, we prioritize it over intent hints.
		if input.Semantic != nil && input.Semantic.Confidence > 0.7 {

			switch input.Semantic.UserGoal {
			case "greeting":
				d.NextMode = types.ModeSupport
				d.Reason = "semantic_greeting"
				// Hint: If intent was also greeting, great. If unknown, we still greet.

			case "business_info":
				d.NextMode = types.ModeSupport
				d.AllowedActions = append(d.AllowedActions, types.ActionSendOfferings)
				d.Reason = "semantic_business_info" // Part C: Semantic Reason

			case "support":
				d.NextMode = types.ModeSupport
				d.Reason = "semantic_support"

			case "understand_business": // Explicit User Request
				d.NextMode = types.ModeSupport
				d.AllowedActions = append(d.AllowedActions, types.ActionSendOfferings)
				d.Reason = "semantic_business_intro"

			// 🆕 FIX: Handle explicit order requests
			case "order":
				// Semantic understanding detected the customer wants to order
				if input.Capabilities.OrderingEnabled && hasOfferings {
					d.NextMode = types.ModeOrderCollection
					// Allow order-related actions
					d.AllowedActions = append(d.AllowedActions,
						types.ActionInitializeOrder,
						types.ActionAddToOrder,
					)
					d.Reason = "semantic_order_request"
				} else {
					// Ordering not available, provide support instead
					d.NextMode = types.ModeSupport
					d.Reason = "semantic_order_unsupported"
				}

			// 🆕 FIX: Handle payment-related messages
			case "payment":
				// Semantic understanding detected payment intent
				if hasActiveOrder {
					// There's an active order, move to payment awareness
					d.NextMode = types.ModePaymentAwareness
					if input.Capabilities.PaymentEnabled {
						d.AllowedActions = append(d.AllowedActions, types.ActionRequestPayment)
					}
					d.Reason = "semantic_payment_intent"
				} else {
					// No active order, customer might be asking about payment methods
					d.NextMode = types.ModeSupport
					d.Reason = "semantic_payment_inquiry"
				}

			default:
				// If semantic meaning is strong but not a specific case, trust semantic guidance
				if input.Semantic.SuggestedIntent != "UNKNOWN" {
					// We can use SuggestedIntent as a fallback if primary intent failed
					if input.Intent.Primary == types.IntentUnknown {
						// Upgrade intent based on semantic suggestion
						// Note: We can't change input.Intent here, but we can act as if.
						d.Reason = fmt.Sprintf("semantic_suggestion_%s", input.Semantic.SuggestedIntent)
					}
				}
			}
		}
	} else {
		// Active order exists
		if intent == types.IntentConfirmOrder {
			d.NextMode = types.ModePaymentAwareness
			d.Reason = "order_committed_payment"
		}
	}

	// =========================================================================
	// STEP 6: AI CONFIDENCE FAILSAFE
	// =========================================================================
	isGreeting := input.Intent.Primary == types.IntentGreeting

	// Check semantic safety before freezing
	hasSemanticSafety := input.Semantic != nil && input.Semantic.Confidence > 0.7

	if !isGreeting && !hasSemanticSafety && input.Emotion.IsConfused && input.Intent.Confidence < 0.4 {
		d.FreezeOrders = true // Safety freeze
		d.Reason = "failsafe_confusion_low_confidence"
		// Restrict actions to nothing (force clarification)
		d.AllowedActions = []types.ActionType{}
	}

	// APPLY POLICY
	d = e.policy.Apply(d, input)

	return d, nil
}
