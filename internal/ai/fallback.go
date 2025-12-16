package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/similadayo/brancr/internal/storage"
)

// GenerateFallbackResponse generates a simple, helpful response when AI fails
// Template: #21 GenerateFallbackResponse (safe fallback response)
func GenerateFallbackResponse(ctx context.Context, tenantID uint, userMsg string, isOwner bool) string {
	lower := strings.ToLower(strings.TrimSpace(userMsg))

	// Simple keyword-based fallback responses - human-like and casual
	if isOwner {
		// Owner-specific fallbacks
		if strings.Contains(lower, "schedule") || strings.Contains(lower, "post") {
			return "Sure! Just upload your photos or videos first, then tell me when you want to post them and I'll take care of it."
		}
		if strings.Contains(lower, "connect") {
			return "Yep! You can connect accounts by saying things like 'connect instagram' or 'connect facebook' - I'll walk you through it."
		}
		if strings.Contains(lower, "help") || strings.Contains(lower, "what can you") {
			return "I can help you schedule posts, connect your social accounts, check your stats, and manage your content. Just ask me anything!"
		}
		if strings.Contains(lower, "hello") || strings.Contains(lower, "hi") || strings.Contains(lower, "hey") {
			return "Hey! What's up? How can I help you today?"
		}

		// Generic owner response
		return "I'm here to help! Feel free to ask me about scheduling posts, connecting accounts, or anything else you need."
	} else {
		// Customer-specific fallbacks
		if strings.Contains(lower, "hello") || strings.Contains(lower, "hi") || strings.Contains(lower, "hey") {
			return "Hey there! How can I help you today?"
		}
		if strings.Contains(lower, "thank") {
			return "Of course! Happy to help. Anything else I can do for you?"
		}
		if strings.Contains(lower, "bye") || strings.Contains(lower, "goodbye") {
			return "Thanks for reaching out! Have a great day!"
		}

		// Generic customer response
		return "Thanks for messaging! I'm here to help. What can I do for you?"
	}
}

// SanitizePrompt removes potentially problematic content from prompts
func SanitizePrompt(prompt string) string {
	// Remove any content that might trigger false positives
	// This is a simple sanitization - can be enhanced

	// Remove excessive special characters
	prompt = strings.ReplaceAll(prompt, "!!!", "!")
	prompt = strings.ReplaceAll(prompt, "???", "?")

	// Normalize whitespace
	prompt = strings.TrimSpace(prompt)

	return prompt
}

// GenerateSafePrompt creates a safer version of the system prompt
// Template: #22 GenerateSafePrompt (policy-compliant prompt conversion)
func GenerateSafePrompt(persona storage.Persona, biz storage.BusinessProfile, isOwner bool) string {
	if isOwner {
		return fmt.Sprintf(`You're %s, helping %s manage their social media.

Business: %s
Niche: %s
Tone: %s

You help with scheduling posts, connecting accounts, managing content, and general questions.

Talk like a real person - casual, friendly, and helpful. Use natural language and contractions. Don't sound like a robot.`, persona.BotName, biz.Name, biz.Name, biz.Niche, persona.Tone)
	} else {
		return fmt.Sprintf(`You're %s from %s.

Business: %s
Niche: %s
Tone: %s

Talk to customers like a real person. Be genuine, friendly, and helpful. Use natural language, contractions, and sound human.`, persona.BotName, biz.Name, biz.Name, biz.Niche, persona.Tone)
	}
}
