package intent

func ShouldBypassLLM(i IntentType) bool {
	return i == IntentRequestOfferings || i == IntentPriceInquiry || i == IntentCancel || i == IntentCancelOrder || i == IntentRequestRecommendation
}
