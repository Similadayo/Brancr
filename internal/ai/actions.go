package ai

// V2ActionType represents the action to be executed by Stage 4 (Executor).
// The Decision Resolver (Stage 3) decides the action, Executor performs it.
// Prefixed with V2 to avoid collision with v1 Action type during migration.
type V2ActionType string

const (
	// V2ActionNone means no action required (e.g., greeting, info query)
	V2ActionNone V2ActionType = "NO_ACTION"

	// V2ActionCreateOrder creates a new order in CART_ACTIVE state
	V2ActionCreateOrder V2ActionType = "CREATE_ORDER"

	// V2ActionUpdateOrder modifies items in an existing order
	V2ActionUpdateOrder V2ActionType = "UPDATE_ORDER"

	// V2ActionLockOrder transitions order to CHECKOUT_PENDING
	V2ActionLockOrder V2ActionType = "LOCK_ORDER"

	// V2ActionSendPayment generates and sends payment instructions
	V2ActionSendPayment V2ActionType = "SEND_PAYMENT"

	// V2ActionConfirmPayment marks order as PAID
	V2ActionConfirmPayment V2ActionType = "CONFIRM_PAYMENT"

	// V2ActionCancelOrder cancels the order
	V2ActionCancelOrder V2ActionType = "CANCEL_ORDER"

	// V2ActionFulfillOrder marks order as FULFILLED
	V2ActionFulfillOrder V2ActionType = "FULFILL_ORDER"

	// V2ActionAskClarification asks user for more information
	V2ActionAskClarification V2ActionType = "ASK_CLARIFICATION"

	// V2ActionRejectIntent rejects the intent due to invalid state
	V2ActionRejectIntent V2ActionType = "REJECT_INTENT"
)

// IntentToV2Action maps canonical intents to their default v2 actions.
// The Decision Resolver may override based on current state.
var IntentToV2Action = map[CanonicalIntent]V2ActionType{
	IntentGreeting:         V2ActionNone,
	IntentGoodbye:          V2ActionNone,
	IntentAffirm:           V2ActionNone, // Context-dependent
	IntentReject:           V2ActionNone, // Context-dependent
	IntentClarify:          V2ActionNone,
	IntentUnknown:          V2ActionAskClarification,
	IntentBrowseProducts:   V2ActionNone,
	IntentAskProductInfo:   V2ActionNone,
	IntentAskPrice:         V2ActionNone,
	IntentAddToCart:        V2ActionCreateOrder, // Or UPDATE_ORDER if order exists
	IntentRemoveFromCart:   V2ActionUpdateOrder,
	IntentViewCart:         V2ActionNone,
	IntentCheckout:         V2ActionLockOrder,
	IntentAskPaymentMethod: V2ActionSendPayment,
	IntentConfirmPayment:   V2ActionConfirmPayment,
	IntentCancelOrder:      V2ActionCancelOrder,
	IntentTrackOrder:       V2ActionNone,
}
