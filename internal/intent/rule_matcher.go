package intent

import (
	"strings"
)

// MatchByRules attempts to identify intent using strict keyword/regex matching.
// Returns success = true if a definitive rule matches.
func MatchByRules(message string) (IntentResult, bool) {
	msg := strings.ToLower(message)

	// Note: In a real production system, these rules would be more complex and managed via config.
	// For Phase 5 "Deterministic Matcher" demonstration:

	switch {
	// FIX 2.1: Strong Intent RequestOfferings (Highest Priority)
	case IsRequestOfferings(msg):
		return IntentResult{Intent: IntentRequestOfferings, Confidence: 0.99, Source: "RULE"}, true

	// Greeting
	case containsAny(msg, "hello", "hi ", "hey "):
		return IntentResult{Intent: IntentGreeting, Confidence: 0.99, Source: "RULE"}, true

	// Price
	case containsAny(msg, "how much", "price", "cost"):
		return IntentResult{Intent: IntentPriceInquiry, Confidence: 0.9, Source: "RULE"}, true

	// Negotiation / Agreement
	case containsAny(msg, "i agree", "deal", "sounds good", "ok", "sure", "yes"):
		// Context sensitive heavily, but rule matcher is "Strict".
		// "i agree" is strong. "ok" is ambiguous, might need context guard later or just low confidence here?
		// User instructions: "strings.Contains 'i agree' -> IntentAcceptPrice"
		if strings.Contains(msg, "i agree") || strings.Contains(msg, "deal") {
			return IntentResult{Intent: IntentAcceptPrice, Confidence: 0.9, Source: "RULE"}, true
		}
		// "ok"/"sure"/"yes" might be IntentConfirmOrder or IntentAcceptPrice depending on state.
		// Rule Matcher here treats them as specific intents or falls back to AI if ambiguous?
		// For now, strict explicit keywords.

	// Payment
	case containsAny(msg, "send account", "payment details", "how to pay"):
		return IntentResult{Intent: IntentRequestPayment, Confidence: 0.95, Source: "RULE"}, true

	// Lifecycle
	case containsAny(msg, "cancel order", "stop"):
		return IntentResult{Intent: IntentCancelOrder, Confidence: 0.99, Source: "RULE"}, true
	}

	return IntentResult{}, false
}

func containsAny(s string, substrings ...string) bool {
	for _, sub := range substrings {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
