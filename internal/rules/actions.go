package rules

type ActionType string

const (
	// Conversational (no state change)
	ActionExplainPrice       ActionType = "EXPLAIN_PRICE"
	ActionClarifyIntent      ActionType = "CLARIFY_INTENT"
	ActionSendOfferings      ActionType = "send_offerings" // Fix 2.2
	ActionAcknowledge        ActionType = "ACKNOWLEDGE"
	ActionResumeConversation ActionType = "RESUME_CONVERSATION"
	ActionSuggestNextStep    ActionType = "SUGGEST_NEXT_STEP"
	ActionBookService        ActionType = "BOOK_SERVICE"
	ActionShareOptions       ActionType = "SHARE_OPTIONS"
	ActionRecommend          ActionType = "RECOMMEND"        // Fix 3
	ActionHandoffToHuman     ActionType = "HANDOFF_TO_HUMAN" // Fix 4

	// Qualification (Step 2)
	ActionQualifyUser ActionType = "QUALIFY_USER"

	// Negotiation
	ActionProposeDiscount ActionType = "PROPOSE_DISCOUNT"
	ActionRejectDiscount  ActionType = "REJECT_DISCOUNT"

	// Order lifecycle
	ActionInitializeOrder ActionType = "INITIALIZE_ORDER"
	ActionLockPrice       ActionType = "LOCK_PRICE"
	ActionCancelOrder     ActionType = "CANCEL_ORDER"

	// Payment
	ActionRequestPayment ActionType = "REQUEST_PAYMENT"

	// Fulfillment
	ActionConfirmOrder ActionType = "CONFIRM_ORDER"
)
