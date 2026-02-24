package ai

// OrderState represents the lifecycle state of an order.
// Use these exact enum values everywhere - no string literals scattered in code.
type OrderState string

const (
	// OrderStateNone means no active order exists
	OrderStateNone OrderState = "NONE"

	// OrderStateBrowsing means user is asking questions, no cart yet
	OrderStateBrowsing OrderState = "BROWSING"

	// OrderStateCartActive means cart exists and is editable
	OrderStateCartActive OrderState = "CART_ACTIVE"

	// OrderStateCheckoutPending means order is locked, awaiting payment send
	OrderStateCheckoutPending OrderState = "CHECKOUT_PENDING"

	// OrderStatePaymentSent means payment details have been sent to customer
	OrderStatePaymentSent OrderState = "PAYMENT_SENT"

	// OrderStatePaid means payment has been confirmed
	OrderStatePaid OrderState = "PAID"

	// OrderStateFulfilled means order has been delivered/processed (terminal)
	OrderStateFulfilled OrderState = "FULFILLED"

	// OrderStateCancelled means order was cancelled (terminal)
	OrderStateCancelled OrderState = "CANCELLED"

	// OrderStateExpired means order timed out (terminal)
	OrderStateExpired OrderState = "EXPIRED"
)

// TerminalStates are states where the order is complete and cannot transition.
var TerminalStates = []OrderState{
	OrderStateFulfilled,
	OrderStateCancelled,
	OrderStateExpired,
}

// IsTerminal returns true if this state is a terminal (final) state.
func (s OrderState) IsTerminal() bool {
	for _, terminal := range TerminalStates {
		if s == terminal {
			return true
		}
	}
	return false
}

// IsActive returns true if this state represents an active (non-terminal) order.
func (s OrderState) IsActive() bool {
	return s != OrderStateNone && !s.IsTerminal()
}

// CanTransitionTo checks if a transition from this state to target is allowed.
func (s OrderState) CanTransitionTo(target OrderState) bool {
	allowed, ok := allowedTransitions[s]
	if !ok {
		return false
	}
	for _, valid := range allowed {
		if valid == target {
			return true
		}
	}
	return false
}

// allowedTransitions defines the state machine.
// Only these transitions are allowed - all others are blocked.
var allowedTransitions = map[OrderState][]OrderState{
	OrderStateNone:            {OrderStateBrowsing},
	OrderStateBrowsing:        {OrderStateCartActive},
	OrderStateCartActive:      {OrderStateCartActive, OrderStateCheckoutPending, OrderStateCancelled, OrderStateExpired},
	OrderStateCheckoutPending: {OrderStatePaymentSent, OrderStateCancelled, OrderStateExpired},
	OrderStatePaymentSent:     {OrderStatePaid, OrderStateCancelled},
	OrderStatePaid:            {OrderStateFulfilled},
	// Terminal states cannot transition
	OrderStateFulfilled: {},
	OrderStateCancelled: {},
	OrderStateExpired:   {},
}

// MapLegacyStatus maps old v1 status strings to v2 OrderState.
// This ensures backward compatibility during migration.
func MapLegacyStatus(legacyStatus string) OrderState {
	switch legacyStatus {
	case "draft", "building":
		return OrderStateCartActive
	case "pending", "awaiting_payment":
		return OrderStatePaymentSent
	case "confirmed", "paid":
		return OrderStatePaid
	case "fulfilled", "completed", "delivered":
		return OrderStateFulfilled
	case "cancelled":
		return OrderStateCancelled
	case "expired":
		return OrderStateExpired
	default:
		return OrderStateNone
	}
}

// ToLegacyStatus converts OrderState back to legacy status string for DB compatibility.
func (s OrderState) ToLegacyStatus() string {
	switch s {
	case OrderStateCartActive:
		return "draft"
	case OrderStateCheckoutPending:
		return "pending"
	case OrderStatePaymentSent:
		return "awaiting_payment"
	case OrderStatePaid:
		return "paid"
	case OrderStateFulfilled:
		return "fulfilled"
	case OrderStateCancelled:
		return "cancelled"
	case OrderStateExpired:
		return "expired"
	default:
		return ""
	}
}
