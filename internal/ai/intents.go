package ai

// CanonicalIntent represents the finite set of allowed intents.
// AI must classify messages into one of these intents only.
// No free-text intents. No invented intents. One message → one intent.
type CanonicalIntent string

const (
	// Core Intents (Global, All Industries)
	IntentGreeting CanonicalIntent = "greeting"
	IntentGoodbye  CanonicalIntent = "goodbye"
	IntentAffirm   CanonicalIntent = "affirm"
	IntentReject   CanonicalIntent = "reject"
	IntentClarify  CanonicalIntent = "clarify"
	IntentUnknown  CanonicalIntent = "unknown"

	// E-commerce Domain Intents
	IntentBrowseProducts   CanonicalIntent = "browse_products"
	IntentAskProductInfo   CanonicalIntent = "ask_product_info"
	IntentAskPrice         CanonicalIntent = "ask_price"
	IntentAddToCart        CanonicalIntent = "add_to_cart"
	IntentRemoveFromCart   CanonicalIntent = "remove_from_cart"
	IntentViewCart         CanonicalIntent = "view_cart"
	IntentCheckout         CanonicalIntent = "checkout"
	IntentAskPaymentMethod CanonicalIntent = "ask_payment_method"
	IntentConfirmPayment   CanonicalIntent = "confirm_payment"
	IntentCancelOrder      CanonicalIntent = "cancel_order"
	IntentTrackOrder       CanonicalIntent = "track_order"
)

// AllCanonicalIntents is the exhaustive list of valid intents.
// Used for validation - any intent not in this list is invalid.
var AllCanonicalIntents = []CanonicalIntent{
	IntentGreeting,
	IntentGoodbye,
	IntentAffirm,
	IntentReject,
	IntentClarify,
	IntentUnknown,
	IntentBrowseProducts,
	IntentAskProductInfo,
	IntentAskPrice,
	IntentAddToCart,
	IntentRemoveFromCart,
	IntentViewCart,
	IntentCheckout,
	IntentAskPaymentMethod,
	IntentConfirmPayment,
	IntentCancelOrder,
	IntentTrackOrder,
}

// IsValidIntent checks if an intent is in the canonical list.
func IsValidIntent(intent string) bool {
	for _, valid := range AllCanonicalIntents {
		if string(valid) == intent {
			return true
		}
	}
	return false
}

// IntentRequiresOrder returns true if this intent needs an active order context.
func (i CanonicalIntent) RequiresOrder() bool {
	switch i {
	case IntentAddToCart, IntentRemoveFromCart, IntentViewCart,
		IntentCheckout, IntentAskPaymentMethod, IntentConfirmPayment,
		IntentCancelOrder, IntentTrackOrder:
		return true
	default:
		return false
	}
}

// IntentCreatesOrder returns true if this intent can create a new order.
func (i CanonicalIntent) CreatesOrder() bool {
	return i == IntentAddToCart
}

// ConfidenceThreshold is the minimum confidence required to act on an intent.
// Below this threshold, intent is treated as IntentUnknown.
const ConfidenceThreshold = 0.6
