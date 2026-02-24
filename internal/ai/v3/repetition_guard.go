package v3

import "strings"

// IsRepeat checks if the new response is effectively a repetition of the last one.
// This prevents the AI from looping or repeating explanations.
func IsRepeat(lastResponse, newResponse string) bool {
	if lastResponse == "" || newResponse == "" {
		return false
	}
	last := strings.ToLower(lastResponse)
	curr := strings.ToLower(newResponse)

	// Simple containment check - robust enough for verbatim repetition
	return strings.Contains(curr, last) || strings.Contains(last, curr)
}
