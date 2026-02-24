package ai

import "strings"

// Humanizer optimizes the final text for the specific platform.
// It touches ONLY MessageToUser. It CANNOT change Action or Data.
type Humanizer struct{}

func (h *Humanizer) Humanize(text string, platform string) string {
	out := text

	// Platform specific tweaks
	switch strings.ToLower(platform) {
	case "whatsapp":
		// Ensure bolding is markdown * *
		// Convert standard Markdown ** to * for WhatsApp
		out = strings.ReplaceAll(out, "**", "*")
	case "sms":
		// Strip markdown, keep short
		out = strings.ReplaceAll(out, "**", "")
		out = strings.ReplaceAll(out, "*", "")
	}

	return out
}
