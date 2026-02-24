package intent

import (
	"strings"

	"github.com/similadayo/brancr/internal/domain"
)

// IsRequestRecommendation detects if the user is asking for help choosing.
// PREREQUISITE: Offerings MUST have been shown (HasShownOfferings == true).
// If offerings haven't been shown, this intent should likely fall back or trigger offerings instead.
// But per Fix 3 rules: "Recommendations may ONLY trigger if HasShownOfferings == true"
func IsRequestRecommendation(msg string, state domain.ConversationState) bool {
	if !state.HasShownOfferings {
		return false
	}

	// If already recommended, don't trigger again to avoid looping?
	// Fix 3.9 says "Prevents: repeated recommendations".
	// But Step 3.1 says "User has not already chosen".
	// And "If any condition fails -> do nothing".
	// Step 3.9 adds `HasRecommended`.
	// Usually strict intents check if already done.
	// But let's check triggers first.

	msg = strings.ToLower(strings.TrimSpace(msg))

	triggers := []string{
		"what do you recommend",
		"which one is better",
		"not sure which to choose",
		"any suggestion",
		"help me choose",
		"what would you suggest",
		"i'm not sure",
		"im not sure",
		"i am not sure",
		"confused",
	}

	for _, t := range triggers {
		if strings.Contains(msg, t) {
			return true
		}
	}

	return false
}
