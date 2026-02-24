package ai

import "strings"

// truncate truncates a string to a maximum length
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// removeEmDashes removes em-dashes from text
func removeEmDashes(text string) string {
	// Remove em-dashes (—) and replace with regular dashes (-)
	text = strings.ReplaceAll(text, "—", "-")
	text = strings.ReplaceAll(text, "–", "-") // Also remove en-dashes
	return text
}
