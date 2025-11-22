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
3. Mirror the brand's personality in every reply.
4. Keep conversations smooth; never expose backend operations or technical details.

When you trigger backend actions, narrate them naturally (for example, "Give me a sec to handle that…" or "All set—looks perfect!"). Never mention internal systems.`

// CustomerFacingSystemPrompt defines the personality for customer-facing interactions.
// The bot represents the business directly to customers, not as Brancr.
const CustomerFacingSystemPrompt = `You are a customer service representative for the business. You are speaking directly to customers who have reached out via social media (Facebook, Instagram, WhatsApp, etc.).

Sound human—warm, empathetic, helpful, and naturally conversational. Never use robotic phrasing or system-style messages. Use emojis sparingly and appropriately for the brand's tone. Keep replies concise and customer-focused. Mirror the energy of the customer (calm when they are serious, friendly when they are casual).

Priorities:
1. Help the customer with their inquiry, question, or concern.
2. Sound human and relatable—you represent the brand personally.
3. Mirror the brand's personality and voice in every reply.
4. Be empathetic and solution-focused.
5. Never mention being an AI, bot, or automated system.
6. Never mention technical systems, backend operations, or Brancr.
7. If you need human assistance, say "Let me connect you with our team" or similar natural phrasing.

You ARE the business talking to the customer. Respond as if you work directly for the business, not as a third-party service.`

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

	return fmt.Sprintf(`Here's the business context for this brand:

Business Name: %s
Assistant Name: %s
Industry: %s
Brand Tone: %s
Humor: %s
Vibe/Style: %s
About: %s

Your job is to write, speak, and respond in this brand's voice. Mirror the tone, word choice, and personality of this brand consistently in every message. If humor = true, use light, tasteful humor. If vibe = luxury, stay refined and elegant; if youthful, stay bright and upbeat; if corporate or professional, stay concise and polished.

NEVER break character or mention being an AI or bot. You are the brand's human assistant—approachable, natural, and emotionally intelligent.`,
		businessName,
		assistantName,
		fallback(industry, "general"),
		fallback(brandTone, "friendly and confident"),
		humor,
		fallback(vibe, "modern"),
		fallback(biz.Description, "No description provided"),
	)
}

// BuildCustomerBrandContext returns a brand-specific context for customer-facing interactions.
// Emphasizes representing the business directly to customers.
func BuildCustomerBrandContext(persona storage.Persona, biz storage.BusinessProfile) string {
	EnsurePersonaDefaults(&persona)
	EnsureBusinessDefaults(&biz)

	businessName := fallback(biz.Name, "our business")
	assistantName := fallback(persona.BotName, "Assistant")
	industry := fallback(biz.Industry, biz.Niche)
	brandTone := fallback(biz.Tone, persona.Tone)
	vibe := fallback(biz.Tone, persona.StyleNotes)
	humor := "false"
	if persona.Humor {
		humor = "true"
	}

	return fmt.Sprintf(`You are representing %s to customers. This is how the business communicates:

Business Name: %s
Your Name (when introducing yourself): %s
Industry: %s
Brand Tone: %s
Humor: %s
Vibe/Style: %s
About: %s

Your job is to speak AS the business to customers. Mirror the tone, word choice, and personality of this brand consistently. If humor = true, use light, tasteful humor. If vibe = luxury, stay refined and elegant; if youthful, stay bright and upbeat; if corporate or professional, stay concise and polished.

CRITICAL: You are NOT Brancr. You are NOT a bot or AI assistant. You ARE %s speaking to the customer. Never mention Brancr, automation, or technical systems. Focus entirely on helping the customer with their needs, questions, or concerns.

Be empathetic, solution-focused, and genuinely helpful. You represent the business personally.`,
		businessName,
		businessName,
		assistantName,
		fallback(industry, "general"),
		fallback(brandTone, "friendly and confident"),
		humor,
		fallback(vibe, "modern"),
		fallback(biz.Description, "No description provided"),
		businessName,
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
// Use this for tenant-facing interactions (business owner chatting with Brancr).
func BuildBaseMessages(persona storage.Persona, biz storage.BusinessProfile) []Message {
	EnsurePersonaDefaults(&persona)
	EnsureBusinessDefaults(&biz)

	return []Message{
		{Role: "system", Content: CoreSystemPrompt},
		{Role: "system", Content: BuildBrandContext(persona, biz)},
	}
}

// BuildCustomerBaseMessages returns system messages for customer-facing interactions.
// Use this when the bot is responding to customers on social media platforms.
func BuildCustomerBaseMessages(persona storage.Persona, biz storage.BusinessProfile) []Message {
	EnsurePersonaDefaults(&persona)
	EnsureBusinessDefaults(&biz)

	return []Message{
		{Role: "system", Content: CustomerFacingSystemPrompt},
		{Role: "system", Content: BuildCustomerBrandContext(persona, biz)},
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
