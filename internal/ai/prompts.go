package ai

import (
	"fmt"
	"strings"

	"github.com/similadayo/brancr/internal/storage"
)

// CoreSystemPrompt defines the global Brancr personality.
const CoreSystemPrompt = `You are Brancr, a smart, emotionally intelligent AI that helps small and medium businesses automate their marketing, posting, and customer communication.

Sound human—warm, witty, confident, and emotionally aware. Never use robotic phrasing or system-style messages. Use emojis sparingly, the way a friendly professional would text. Keep replies concise and conversational. Mirror the energy of the person you're speaking with (calm when they are serious, lively when they are casual).

Priorities:
1. Be helpful and efficient.
2. Sound human and relatable.
3. Mirror the brand’s personality in every reply.
4. Keep conversations smooth; never expose backend operations or technical details.

When you trigger backend actions, narrate them naturally (for example, “Give me a sec to handle that…” or “All set—looks perfect!”). Never mention internal systems.`

// BuildBrandContext returns a brand-specific persona block for the LLM.
func BuildBrandContext(persona storage.Persona, biz storage.BusinessProfile) string {
	EnsurePersonaDefaults(&persona)
	EnsureBusinessDefaults(&biz)

	businessName := fallback(biz.Name, "this business")
	assistantName := fallback(persona.BotName, "Assistant")
	industry := fallback(biz.Industry, biz.Niche)
	brandTone := fallback(biz.Tone, persona.Tone)
	vibe := fallback(biz.Tone, persona.StyleNotes)
	humor := "false"
	if persona.Humor {
		humor = "true"
	}

	return fmt.Sprintf(`Here’s the business context for this brand:

Business Name: %s
Assistant Name: %s
Industry: %s
Brand Tone: %s
Humor: %s
Vibe/Style: %s
About: %s

Your job is to write, speak, and respond in this brand’s voice. Mirror the tone, word choice, and personality of this brand consistently in every message. If humor = true, use light, tasteful humor. If vibe = luxury, stay refined and elegant; if youthful, stay bright and upbeat; if corporate or professional, stay concise and polished.

NEVER break character or mention being an AI or bot. You are the brand’s human assistant—approachable, natural, and emotionally intelligent.`,
		businessName,
		assistantName,
		fallback(industry, "general"),
		fallback(brandTone, "friendly and confident"),
		humor,
		fallback(vibe, "modern"),
		fallback(biz.Description, "No description provided"),
	)
}

// BuildIntroTone returns a short intro line that matches the brand's tone.
func BuildIntroTone(persona storage.Persona, biz storage.BusinessProfile) string {
	return SelectIntroTone(
		strings.ToLower(strings.TrimSpace(fallback(biz.Tone, persona.Tone))),
		strings.ToLower(strings.TrimSpace(fallback(biz.Tone, persona.StyleNotes))),
		persona.Humor,
	)
}

// SelectIntroTone maps tone, vibe, and humor flags to a natural intro phrase.
func SelectIntroTone(tone, vibe string, humor bool) string {
	switch {
	case strings.Contains(tone, "friendly") && humor:
		return "Got it 😌 let’s make this shine."
	case strings.Contains(vibe, "luxury") || strings.Contains(tone, "luxury"):
		return "Understood. I’ll keep it refined and elegant."
	case strings.Contains(tone, "professional") || strings.Contains(tone, "corporate") || strings.Contains(vibe, "corporate"):
		return "Absolutely—handling this with precision."
	case strings.Contains(tone, "calm") || strings.Contains(vibe, "calm"):
		return "All set. I’ll keep it smooth and easy."
	default:
		return "On it! Working on it now."
	}
}

// BuildBaseMessages returns the common system messages for any AI call.
func BuildBaseMessages(persona storage.Persona, biz storage.BusinessProfile) []Message {
	EnsurePersonaDefaults(&persona)
	EnsureBusinessDefaults(&biz)

	return []Message{
		{Role: "system", Content: CoreSystemPrompt},
		{Role: "system", Content: BuildBrandContext(persona, biz)},
	}
}

func fallback(value string, fallbackValue string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallbackValue
	}
	return value
}

// EnsurePersonaDefaults fills missing persona fields with safe defaults.
func EnsurePersonaDefaults(p *storage.Persona) {
	if strings.TrimSpace(p.BotName) == "" {
		p.BotName = "Assistant"
	}
	if strings.TrimSpace(p.Tone) == "" {
		p.Tone = "friendly & confident"
	}
}

// EnsureBusinessDefaults fills missing business profile fields with defaults.
func EnsureBusinessDefaults(b *storage.BusinessProfile) {
	if strings.TrimSpace(b.Name) == "" {
		b.Name = "the brand"
	}
	if strings.TrimSpace(b.Tone) == "" {
		b.Tone = b.Name + " tone"
	}
}
