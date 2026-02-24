package persona

import "strings"

func ProfessionalFormatter(input string) string {
	// neutral, clean, no emojis
	return stripEmojis(input)
}

func stripEmojis(s string) string {
	// Simple strip for demo - removes common ones
	// Real implementation would use regex or unicode
	r := strings.NewReplacer(
		"😊", "", "🙏", "", "👋", "", "👍", "",
		"🔥", "", "✨", "", "🚀", "",
	)
	return strings.TrimSpace(r.Replace(s))
}
