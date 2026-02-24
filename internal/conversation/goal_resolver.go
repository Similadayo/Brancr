package conversation

import "github.com/similadayo/brancr/internal/intent"

// ResolveGoal determines the conversational trajectory from intent.
// This function MUST remain deterministic and GPT-free.
func ResolveGoal(i intent.IntentType) Goal {
	switch i {

	// --- EXPLORE ---
	case intent.IntentGreeting,
		intent.IntentItemInquiry,
		intent.IntentPriceInquiry,
		intent.IntentUnknown:
		return GoalExplore

	// --- DECIDE ---
	case intent.IntentNegotiatePrice,
		intent.IntentRejectPrice:
		return GoalDecide

	// --- COMMIT ---
	case intent.IntentAcceptPrice,
		intent.IntentRequestPayment,
		intent.IntentConfirmOrder:
		return GoalCommit

	// --- RESOLVE ---
	case intent.IntentConfirmPayment:
		return GoalResolve

	default:
		return GoalExplore
	}
}
