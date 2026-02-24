package channel

import (
	"strings"
)

func AssertHumanSafe(text string) {
	if looksLikeJSON(text) {
		panic("HUMAN OUTPUT VIOLATION: JSON detected in output: " + text)
	}
}

func looksLikeJSON(text string) bool {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "{") && strings.HasSuffix(t, "}") {
		return true
	}
	if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
		return true
	}
	return false
}
