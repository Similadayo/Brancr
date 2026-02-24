package intent

import "strings"

// WARNING:
// IntentRequestOfferings must trigger immediate sending of offerings.
// Do NOT add clarification, GPT calls, or narration.
// Violations reintroduce chatbot behavior.

func IsRequestOfferings(message string) bool {
	msg := strings.ToLower(message)

	triggers := []string{
		"what do you sell",
		"what do you have",
		"what do you offer",
		"what are you selling",
		"menu",
		"catalog",
		"catalogue",
		"products",
		"services",
		"offerings",
		"what's available",
		"what do you guys have",
		"what do you guys sell",
		"what you got",
		"what's on the menu",
	}

	for _, t := range triggers {
		if strings.Contains(msg, t) {
			return true
		}
	}

	return false
}
