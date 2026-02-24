package intent

func ClassifyIntent(i IntentType) IntentClass {
	switch i {

	case IntentRequestMenu,
		IntentRequestOfferings,
		IntentRequestPrices,
		IntentPriceInquiry,
		IntentItemInquiry: // Including Item Inquiry as Content
		return IntentClassContent

	case IntentCancelOrder,
		IntentCancel, // Alias
		IntentModifyOrder,
		IntentPlaceOrder,
		IntentConfirmOrder,
		IntentOrderStart:
		return IntentClassAction

	// Note: DelegateDecision not strictly defined in types.go yet, mapping Recommendation
	case IntentRequestRecommendation,
		IntentDelegateDecision: // Fix 10.1
		return IntentClassDecision // Recommendation is a form of decision delegation ("Help me choose")

	case IntentCheckOrder,
		IntentRequestPayment: // Payment status check often lumped here
		return IntentClassQuestion

	default:
		return IntentClassSystem
	}
}

// UserAskedAboutOrder determines if the user explicitly referenced the order.
// If NOT, the AI is forbidden from mentioning order/payment context.
func UserAskedAboutOrder(i IntentType) bool {
	return i == IntentCheckOrder ||
		i == IntentCancelOrder ||
		i == IntentCancel ||
		i == IntentModifyOrder ||
		i == IntentConfirmOrder ||
		i == IntentRequestPayment ||
		i == IntentConfirmPayment
}
