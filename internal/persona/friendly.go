package persona

import "strings"

func FriendlyFormatter(input string) string {
	// light warmth, optional emoji
	if hasEmoji(input) {
		return input
	}
	// Don't add if empty
	if input == "" {
		return input
	}
	return input + " 😊"
}

func hasEmoji(s string) bool {
	// Simple heuristic check for emojis - real impl would use unicode range
	for _, r := range s {
		if r > 0x1F600 && r < 0x1F64F {
			return true
		}
	}
	return strings.Contains(s, "😊") || strings.Contains(s, "🙏") || strings.Contains(s, "👋")
}
