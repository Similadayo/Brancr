package social

import "strings"

// extractNameFromMessageContent tries to extract a name from message content
// Looks for patterns like "Account name is X", "Name: X", "I am X", etc.
func extractNameFromMessageContent(message string) string {
	lower := strings.ToLower(message)

	// Patterns to look for:
	patterns := []struct {
		prefix string
	}{
		{"account name is"},
		{"account name:"},
		{"name is"},
		{"name:"},
		{"i am"},
		{"i'm"},
		{"this is"},
		{"from"},
		{"signed"},
	}

	for _, pattern := range patterns {
		if idx := strings.Index(lower, pattern.prefix); idx >= 0 {
			start := idx + len(pattern.prefix)
			remaining := strings.TrimSpace(message[start:])

			// Extract name (up to next comma, period, or newline, or next 50 chars)
			nameEnd := len(remaining)
			for i, char := range remaining {
				if i >= 50 || char == ',' || char == '.' || char == '\n' {
					nameEnd = i
					break
				}
			}

			name := strings.TrimSpace(remaining[:nameEnd])
			// Basic validation - should be at least 2 chars and not too long
			if len(name) > 2 && len(name) < 50 {
				return name
			}
		}
	}

	return ""
}
