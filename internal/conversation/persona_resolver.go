package conversation

import (
	"github.com/similadayo/brancr/internal/memory"
)

// ResolvePersona determines the appropriate voice based on platform and history.
// It is purely deterministic.
func ResolvePersona(platform Platform, mem memory.Summary) Persona {
	// Base Defaults
	p := Persona{
		FormalityLevel:  3,
		EmojiDensity:    0,
		SentenceLength:  "medium",
		CulturalContext: "GLOBAL",
	}

	switch platform {
	case PlatformWhatsApp:
		p.FormalityLevel = 2              // Casual but polite
		p.EmojiDensity = 1                // Sparse emojis allowed
		p.CulturalContext = "NG_WHATSAPP" // Assumption for this market
	case PlatformInstagram:
		p.FormalityLevel = 1 // Very casual
		p.EmojiDensity = 2   // Moderate emojis
	case PlatformSMS:
		p.FormalityLevel = 4 // Brief, formal
		p.SentenceLength = "short"
	case PlatformWeb:
		p.FormalityLevel = 3 // Standard
	}

	// Memory Overrides (Advisory)
	// Example: If user is "VIP", maybe be more formal? Or if "GenZ", more casual?
	// For Phase 10, we keep it simple. Memory mainly informs tone logic not implemented yet.

	return p
}
