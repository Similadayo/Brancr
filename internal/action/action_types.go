package action

// =============================================================================
// CANONICAL ACTION TYPES - Advisory Labels Only
// =============================================================================
// These are the canonical action types that the AI can suggest.
//
// PHASE-4: These are LABELS, not permissions.
// The AI suggests actions; Go's business rules decide if they're allowed.
//
// IMPORTANT:
// - These are advisory only
// - No execution logic here
// - No business rules here
// - Just type definitions
// =============================================================================

type ActionType string

const (
	// Cart Operations
	ActionAddItem    ActionType = "ADD_ITEM"
	ActionRemoveItem ActionType = "REMOVE_ITEM"
	ActionViewCart   ActionType = "VIEW_CART"
	ActionClearCart  ActionType = "CLEAR_CART"

	// Order Lifecycle
	ActionPlaceOrder   ActionType = "PLACE_ORDER"
	ActionCancelOrder  ActionType = "CANCEL_ORDER"
	ActionModifyOrder  ActionType = "MODIFY_ORDER"
	ActionConfirmOrder ActionType = "CONFIRM_ORDER"
	ActionCheckOrder   ActionType = "CHECK_ORDER"

	// Information
	ActionShowMenu     ActionType = "SHOW_MENU"
	ActionShowProducts ActionType = "SHOW_PRODUCTS"
	ActionShowServices ActionType = "SHOW_SERVICES"
	ActionExplainPrice ActionType = "EXPLAIN_PRICE"
	ActionShowPayment  ActionType = "SHOW_PAYMENT"
	ActionProvideInfo  ActionType = "PROVIDE_INFO"

	// Recommendations
	ActionRecommendProduct ActionType = "RECOMMEND_PRODUCT"
	ActionOfferAlternative ActionType = "OFFER_ALTERNATIVE"
	ActionSuggestUpsell    ActionType = "SUGGEST_UPSELL"

	// Pricing & Discounts
	ActionApplyDiscount  ActionType = "APPLY_DISCOUNT"
	ActionNegotiatePrice ActionType = "NEGOTIATE_PRICE"
	ActionAcceptPrice    ActionType = "ACCEPT_PRICE"
	ActionRejectPrice    ActionType = "REJECT_PRICE"

	// Payment
	ActionRequestPayment ActionType = "REQUEST_PAYMENT"
	ActionConfirmPayment ActionType = "CONFIRM_PAYMENT"

	// Clarification
	ActionClarifyIntent ActionType = "CLARIFY_INTENT"
	ActionAskQuestion   ActionType = "ASK_QUESTION"

	// Escalation
	ActionEscalateToHuman ActionType = "ESCALATE_TO_HUMAN"
	ActionRequestCallback ActionType = "REQUEST_CALLBACK"

	// Social
	ActionGreet       ActionType = "GREET"
	ActionAcknowledge ActionType = "ACKNOWLEDGE"
	ActionThankUser   ActionType = "THANK_USER"

	// Fallback
	ActionNoAction ActionType = "NO_ACTION"
	ActionWait     ActionType = "WAIT"
)

// =============================================================================
// HELPER FUNCTIONS (Read-Only)
// =============================================================================

// IsValidActionType checks if a string is a valid action type.
// PHASE-4: This is for validation, not enforcement.
func IsValidActionType(action string) bool {
	validActions := map[string]bool{
		string(ActionAddItem):          true,
		string(ActionRemoveItem):       true,
		string(ActionViewCart):         true,
		string(ActionClearCart):        true,
		string(ActionPlaceOrder):       true,
		string(ActionCancelOrder):      true,
		string(ActionModifyOrder):      true,
		string(ActionConfirmOrder):     true,
		string(ActionCheckOrder):       true,
		string(ActionShowMenu):         true,
		string(ActionShowProducts):     true,
		string(ActionShowServices):     true,
		string(ActionExplainPrice):     true,
		string(ActionShowPayment):      true,
		string(ActionProvideInfo):      true,
		string(ActionRecommendProduct): true,
		string(ActionOfferAlternative): true,
		string(ActionSuggestUpsell):    true,
		string(ActionApplyDiscount):    true,
		string(ActionNegotiatePrice):   true,
		string(ActionAcceptPrice):      true,
		string(ActionRejectPrice):      true,
		string(ActionRequestPayment):   true,
		string(ActionConfirmPayment):   true,
		string(ActionClarifyIntent):    true,
		string(ActionAskQuestion):      true,
		string(ActionEscalateToHuman):  true,
		string(ActionRequestCallback):  true,
		string(ActionGreet):            true,
		string(ActionAcknowledge):      true,
		string(ActionThankUser):        true,
		string(ActionNoAction):         true,
		string(ActionWait):             true,
	}
	return validActions[action]
}

// AllActionTypes returns all valid action types.
// PHASE-4: For reference and validation.
func AllActionTypes() []ActionType {
	return []ActionType{
		ActionAddItem,
		ActionRemoveItem,
		ActionViewCart,
		ActionClearCart,
		ActionPlaceOrder,
		ActionCancelOrder,
		ActionModifyOrder,
		ActionConfirmOrder,
		ActionCheckOrder,
		ActionShowMenu,
		ActionShowProducts,
		ActionShowServices,
		ActionExplainPrice,
		ActionShowPayment,
		ActionProvideInfo,
		ActionRecommendProduct,
		ActionOfferAlternative,
		ActionSuggestUpsell,
		ActionApplyDiscount,
		ActionNegotiatePrice,
		ActionAcceptPrice,
		ActionRejectPrice,
		ActionRequestPayment,
		ActionConfirmPayment,
		ActionClarifyIntent,
		ActionAskQuestion,
		ActionEscalateToHuman,
		ActionRequestCallback,
		ActionGreet,
		ActionAcknowledge,
		ActionThankUser,
		ActionNoAction,
		ActionWait,
	}
}
