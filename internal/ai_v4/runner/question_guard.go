package runner

import "strings"

// looksLikeQuestionStandalone checks if a message is a question, including Nigerian English patterns.
// This is critical for distinguishing between "Am I getting this for 100k?" (Question) vs "I'll pay 100k" (Offer).
func looksLikeQuestionStandalone(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return false
	}
	lower := strings.ToLower(t)

	// Standard English Question Prefixes
	standardPrefixes := []string{
		"what ", "why ", "how ", "when ", "where ", "which ",
		"can you ", "could you ", "do you ", "did ", "have you ",
		"is it ", "are you ", "should ", "will i ", "am i ", "does ", "was ",
		"is the ", "are the ", "the one ", "that one ",
	}

	for _, p := range standardPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}

	// Nigerian English Question Prefixes
	nigerianPrefixes := []string{
		"shey ", "abi ", "na ",
	}
	for _, p := range nigerianPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}

	// Embedded Question Patterns (often used in rhetorical or clarification questions with numbers)
	questionPatterns := []string{
		"am i getting", "will i get", "is it possible", "does it come",
		"is there", "are there", "what's the difference", "compared to",
		"better than", "worse than", "last price", // "last price?" is a question in this market
	}
	for _, p := range questionPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}

	// Simple Question Mark Check (expanded length to catch longer questions)
	if strings.Contains(t, "?") {
		// If it has a question mark and isn't a massive paragraph, treat as question safety-net
		if len(t) < 200 {
			return true // Safer to assume question than to assume offer
		}
	}

	return false
}

// customerAcceptsLastOffer checks if the user is explicitly agreeing to a deal.
// This guards against the AI continuing to negotiate (or lowering price) after the user says "deal".
func customerAcceptsLastOffer(s string) bool {
	t := strings.ToLower(strings.TrimSpace(s))

	// Direct agreement patterns
	agreements := []string{
		"deal", "i agree", "i accept", "that works", "that is fine", "thats fine",
		"ok i will paying", "ok i will pay", "alright i will pay",
		"send account", "send details", "payment details",
	}

	for _, a := range agreements {
		if strings.Contains(t, a) {
			return true
		}
	}

	// "Okay/Alright" by itself is ambiguous, but if it's "Okay <price>", it's an offer/agreement.
	// Current logic: if it's short and affirmative, treatment depends on context,
	// but here we are strict to avoid false positives.
	return false
}
