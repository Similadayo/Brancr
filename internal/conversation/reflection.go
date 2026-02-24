package conversation

import "github.com/similadayo/brancr/internal/intent"

// ReflectionForIntent returns a short, human-like reflection
// that mirrors the user's intent without adding information.
func ReflectionForIntent(i intent.IntentType) string {
	switch i {

	case intent.IntentGreeting:
		return "Hi there."

	case intent.IntentItemInquiry:
		return "Okay — you’re exploring the options."

	case intent.IntentPriceInquiry:
		return "Got it — you’re checking on pricing."

	case intent.IntentNegotiatePrice:
		return "I hear you — price is the main concern."

	case intent.IntentRejectPrice:
		return "That makes sense — you’re weighing the cost."

	case intent.IntentUnknown:
		return "Alright — let’s take a look together."

	default:
		return ""
	}
}
