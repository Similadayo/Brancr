package commitment

import (
	"strings"
)

// DetectCommitment returns true if the message contains strong buying signals
// that should trigger an Immediate Order Initialization.
func DetectCommitment(message string) bool {
	lower := strings.ToLower(message)

	return containsAny(lower, []string{
		"i'll take",
		"i will take",
		"i want this",
		"i want to buy",
		"book me",
		"let's go ahead",
		"confirm order",
		"yes, that one",
		"add to cart",
		"add to order",
		"send me the bill",
		"how do i pay",
	})
}

func containsAny(s string, substrings []string) bool {
	for _, sub := range substrings {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
