package v3

import (
	"strings"
)

// UserStyle represents the detected linguistic style of the user.
type UserStyle string

const (
	StyleCasual  UserStyle = "CASUAL"
	StyleFormal  UserStyle = "FORMAL"
	StyleNeutral UserStyle = "NEUTRAL"
)

// AnalyzeUserStyle detects if the user is acting casual, formal, or neutral.
func AnalyzeUserStyle(message string) UserStyle {
	msg := strings.TrimSpace(message)
	if msg == "" {
		return StyleNeutral
	}

	// Heuristics for Casualness
	casualMarkers := []string{"u", "ur", "lol", "yooo", "thx", "gonna", "wanna", "cool", "!", "haha", "nah", "yep"}
	isLowercase := msg == strings.ToLower(msg) && len(msg) > 5

	// Count casual markers
	markerCount := 0
	lowerMsg := strings.ToLower(msg)
	for _, m := range casualMarkers {
		if strings.Contains(lowerMsg, " "+m+" ") || strings.HasPrefix(lowerMsg, m+" ") || strings.HasSuffix(lowerMsg, " "+m) || lowerMsg == m {
			markerCount++
		}
	}

	if markerCount >= 1 || (isLowercase && !strings.ContainsAny(msg, ".")) {
		return StyleCasual
	}

	// Heuristics for Formalness
	formalMarkers := []string{"dear", "sincerely", "regards", "please", "could you", "would you", "kindly"}
	for _, m := range formalMarkers {
		if strings.Contains(lowerMsg, m) {
			return StyleFormal
		}
	}

	return StyleNeutral
}
