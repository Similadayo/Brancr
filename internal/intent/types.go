package intent

type IntentType string

const (
	IntentUnknown IntentType = "UNKNOWN"

	// Discovery
	IntentGreeting              IntentType = "GREETING"
	IntentRequestOfferings      IntentType = "REQUEST_OFFERINGS"
	IntentRequestMenu           IntentType = "REQUEST_MENU"           // Fix 8.1
	IntentRequestRecommendation IntentType = "REQUEST_RECOMMENDATION" // Fix 3
	IntentDelegateDecision      IntentType = "DELEGATE_DECISION"      // Fix 10.1
	IntentPriceInquiry          IntentType = "PRICE_INQUIRY"
	IntentRequestPrices         IntentType = "REQUEST_PRICES" // Alias for Price Inquiry
	IntentItemInquiry           IntentType = "ITEM_INQUIRY"

	// Negotiation
	IntentNegotiatePrice IntentType = "NEGOTIATE_PRICE"
	IntentAcceptPrice    IntentType = "ACCEPT_PRICE"
	IntentRejectPrice    IntentType = "REJECT_PRICE"

	// Payment
	IntentRequestPayment IntentType = "REQUEST_PAYMENT"
	IntentConfirmPayment IntentType = "CONFIRM_PAYMENT"

	// Order lifecycle
	IntentCancelOrder  IntentType = "CANCEL_ORDER"
	IntentModifyOrder  IntentType = "MODIFY_ORDER"
	IntentPlaceOrder   IntentType = "PLACE_ORDER"
	IntentOrderStart   IntentType = "ORDER_START" // New session
	IntentCheckOrder   IntentType = "CHECK_ORDER"
	IntentCancel       IntentType = "CANCEL" // Alias for V3 compatibility?
	IntentConfirmOrder IntentType = "CONFIRM_ORDER"

	// Objection / Hesitation
	IntentHesitation          IntentType = "HESITATION"
	IntentDelayDecision       IntentType = "DELAY_DECISION"
	IntentNegotiationPushback IntentType = "NEGOTIATION_PUSHBACK"
)
