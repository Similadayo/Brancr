package rules

import (
	"github.com/similadayo/brancr/internal/conversation"
	"github.com/similadayo/brancr/internal/domain"
	"github.com/similadayo/brancr/internal/intent"
	"github.com/similadayo/brancr/internal/types"
)

type BaseEngine struct{}

func NewBaseEngine() *BaseEngine {
	return &BaseEngine{}
}

func (e *BaseEngine) Evaluate(ctx types.ResolvedContext, i intent.IntentResult, goal conversation.Goal) RuleResult {
	// Default to clarifying if nothing matches
	actions := []ActionType{}
	reason := "default fallback"

	// 1. Goal-Based Orientation (Step 1 Foundation)
	switch goal {
	case conversation.GoalExplore:
		actions = append(actions, ActionExplainPrice, ActionClarifyIntent, ActionAcknowledge, ActionResumeConversation, ActionSuggestNextStep)
	case conversation.GoalDecide:
		actions = append(actions, ActionProposeDiscount, ActionRejectDiscount, ActionClarifyIntent, ActionAcknowledge, ActionSuggestNextStep)
	case conversation.GoalCommit:
		actions = append(actions, ActionRequestPayment, ActionConfirmOrder, ActionAcknowledge)
	case conversation.GoalResolve:
		actions = append(actions, ActionClarifyIntent, ActionAcknowledge) // Stub for resolve
	}

	// No Order Logic
	if ctx.Order == nil {
		switch i.Intent {
		case intent.IntentPriceInquiry, intent.IntentItemInquiry, intent.IntentType("PRODUCT_INQUIRY"), intent.IntentType("SERVICE_INQUIRY"):
			// Allow starting an order or just explaining
			actions = append(actions, ActionInitializeOrder, ActionExplainPrice)
			reason = "inquiry implies potential order"
		case intent.IntentGreeting:
			actions = append(actions, ActionExplainPrice) // "Hi" -> "I sell cakes"
			reason = "greeting"
		case intent.IntentPlaceOrder, intent.IntentOrderStart, intent.IntentType("ADD_ITEM"):
			// Allow starting order implicitly
			actions = append(actions, ActionInitializeOrder, ActionSuggestNextStep)
			reason = "order initialization"
		default:
			actions = append(actions, ActionClarifyIntent)
			reason = "unknown intent without order"
		}
		return RuleResult{AllowedActions: actions, Reason: reason}
	}

	// Active Order Logic
	order := ctx.Order

	switch i.Intent {
	case intent.IntentPriceInquiry:
		// Always allow explaining price
		actions = append(actions, ActionExplainPrice)
		if order.Negotiation.Phase == domain.NegotiationNone || order.Negotiation.Phase == domain.NegotiationStarted {
			// Can propose discount if config allows (logic stub)
			actions = append(actions, ActionProposeDiscount)
		}
		reason = "price inquiry handling"

	case intent.IntentNegotiatePrice: // "Can you do cheaper?"
		// Check limits
		if order.Negotiation.Phase == domain.NegotiationLocked || order.Negotiation.Phase == domain.NegotiationAccepted {
			actions = append(actions, ActionRejectDiscount) // Already locked
			reason = "negotiation locked"
		} else if order.Negotiation.Attempts >= ctx.Merchant.MaxNegotiationAttempts {
			actions = append(actions, ActionRejectDiscount) // Max attempts reached
			reason = "max attempts reached"
		} else {
			actions = append(actions, ActionProposeDiscount, ActionRejectDiscount) // Choice is up to Strategy (later phase), Rules allow both
			reason = "negotiation active"
		}

	case intent.IntentAcceptPrice:
		if order.Negotiation.Phase != domain.NegotiationLocked {
			actions = append(actions, ActionLockPrice, ActionRequestPayment)
			reason = "price accepted"
		} else {
			actions = append(actions, ActionRequestPayment)
			reason = "already locked"
		}

	case intent.IntentRequestPayment:
		// Strict check: Negotiation Phase MUST be Accepted or Locked (or None if fixed price)
		// Or PaymentStatus not completed
		if order.Payment.Status == domain.PaymentCompleted {
			actions = append(actions, ActionConfirmOrder)
			reason = "already paid"
		} else if isPriceAgreed(order) {
			actions = append(actions, ActionRequestPayment)
			reason = "payment request allowed"
		} else {
			// Premature payment request
			actions = append(actions, ActionExplainPrice)
			reason = "price not agreed yet"
		}

	case intent.IntentConfirmPayment:
		if order.Payment.Status == domain.PaymentRequested || order.Payment.Status == domain.PaymentPending {
			// Allow manual confirmation check (stub)
			actions = append(actions, ActionConfirmOrder) // In lieu of CheckPaymentStatus
			reason = "payment verification"
		} else {
			actions = append(actions, ActionRequestPayment) // Re-prompt
			reason = "payment not active"
		}

	case intent.IntentCancelOrder:
		actions = append(actions, ActionCancelOrder)
		reason = "user cancellation"

	// FIX 33: V1 Emulation - Allow Order Modification in Active State
	// "Just Do It" - If user wants to add items, let them.
	case intent.IntentPlaceOrder, intent.IntentOrderStart, intent.IntentType("ADD_ITEM"):
		actions = append(actions, ActionInitializeOrder, ActionSuggestNextStep)
		reason = "augmenting active order"

	case intent.IntentType("PRODUCT_INQUIRY"), intent.IntentType("SERVICE_INQUIRY"), intent.IntentItemInquiry:
		// User asking about other items while ordering
		actions = append(actions, ActionExplainPrice, ActionSuggestNextStep)
		reason = "mid-order inquiry"

	default:
		// FIX 33.2: Soften the Default Fallback (Blind Trust)
		// If we are unsure, allow Resume/Suggest instead of forcing Clarify
		actions = append(actions, ActionResumeConversation, ActionSuggestNextStep, ActionClarifyIntent)
		reason = "flexible fallback"
	}

	// Step 2: Qualification (Sales Behavior)
	// FIX 19.1: Disabled Rigid Rule. Moving to Fluid Intent (LLM Decision).
	// if AllowQualification(ctx, goal, i.Intent) {
	// 	actions = append(actions, ActionQualifyUser)
	// }

	return RuleResult{AllowedActions: actions, Reason: reason}
}

func isPriceAgreed(order *domain.OrderState) bool {
	p := order.Negotiation.Phase
	// If Phase is None, it implies standard price (agreed by default context).
	// If Phase is Accepted/Locked, agreed.
	return p == domain.NegotiationNone || p == domain.NegotiationAccepted || p == domain.NegotiationLocked
}
