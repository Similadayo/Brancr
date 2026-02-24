package conversation

import "strings"

func AdjustForConfidence(text string, level ConfidenceLevel) string {
	switch level {

	case ConfidenceMedium:
		return soften(text)

	case ConfidenceLow:
		return softenMore(text)

	default:
		return text
	}
}

// soften adds optionality ("might", "can") and extends flow
func soften(text string) string {
	// Simple heuristic adjustments
	// In a real NLP system, this would be more sophisticated.
	// We'll append a softener clause if not present.
	if strings.Contains(text, ".") {
		return strings.Replace(text, ".", ", and I can walk you through it.", 1)
	}
	return text + ", if that works for you."
}

// softenMore slows pacing and reassures
func softenMore(text string) string {
	// Prepend reassurance + soften logic
	return "We can take this step by step — " + text + " No rush."
}
