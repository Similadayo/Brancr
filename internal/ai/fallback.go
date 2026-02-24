package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// DynamicFallbackGenerator generates AI-powered fallback responses
// CRITICAL: NO hardcoded responses - everything must be AI-generated based on context
type DynamicFallbackGenerator struct {
	Client *AIClient
	DB     *gorm.DB
	Logger *zap.Logger
}

// NewDynamicFallbackGenerator creates a new dynamic fallback generator
func NewDynamicFallbackGenerator(client *AIClient, db *gorm.DB, logger *zap.Logger) *DynamicFallbackGenerator {
	return &DynamicFallbackGenerator{
		Client: client,
		DB:     db,
		Logger: logger,
	}
}

// GenerateFallbackResponse generates a dynamic, AI-powered response when the main AI fails
// This uses a simpler prompt but still generates unique, contextual responses
func (g *DynamicFallbackGenerator) GenerateFallbackResponse(
	ctx context.Context,
	tenantID uuid.UUID,
	userMsg string,
	isOwner bool,
) (string, error) {
	// Load business context
	var biz storage.BusinessProfile
	if err := g.DB.First(&biz, "tenant_id = ?", tenantID).Error; err != nil {
		biz.Name = "our business"
	}

	// Load persona for tone
	var persona storage.Persona
	if err := g.DB.First(&persona, "tenant_id = ?", tenantID).Error; err != nil {
		persona.Tone = "friendly"
		persona.BotName = "Assistant"
	}

	// Build a lightweight prompt
	var prompt string
	if isOwner {
		prompt = fmt.Sprintf(`You are %s, helping the owner of %s manage their business.
Tone: %s

The owner just said: "%s"

Respond naturally and helpfully in 1-2 sentences. Match their energy. Be genuine.`,
			persona.BotName,
			biz.Name,
			persona.Tone,
			userMsg)
	} else {
		prompt = fmt.Sprintf(`You are %s from %s helping a customer.
Tone: %s

The customer just said: "%s"

Respond naturally and helpfully in 1-2 sentences. Match their energy. Be genuine, never generic.`,
			persona.BotName,
			biz.Name,
			persona.Tone,
			userMsg)
	}

	messages := []Message{
		{Role: "user", Content: prompt},
	}

	reply, err := g.Client.Generate(ctx, messages)
	if err != nil {
		// If AI fails, return error - don't use hardcoded response
		return "", fmt.Errorf("fallback generation failed: %w", err)
	}

	return strings.TrimSpace(reply), nil
}

// SanitizePrompt removes potentially problematic content from prompts
func SanitizePrompt(prompt string) string {
	// Remove excessive special characters
	prompt = strings.ReplaceAll(prompt, "!!!", "!")
	prompt = strings.ReplaceAll(prompt, "???", "?")

	// Normalize whitespace
	prompt = strings.TrimSpace(prompt)

	return prompt
}

// GenerateSafePrompt creates a dynamic system prompt based on persona and business
// NO hardcoded templates - everything is personalized
func GenerateSafePrompt(persona storage.Persona, biz storage.BusinessProfile, isOwner bool) string {
	tone := persona.Tone
	if tone == "" {
		tone = "friendly and helpful"
	}

	if isOwner {
		return fmt.Sprintf(`You're %s, helping %s manage their social media and business.

Business: %s
Industry: %s
Tone: %s

You help with scheduling posts, connecting accounts, managing content, and general questions.

Talk like a real person - natural, %s. Don't sound like a robot. Match the owner's energy and communication style.`,
			persona.BotName, biz.Name, biz.Name, biz.Niche, tone, tone)
	} else {
		return fmt.Sprintf(`You're %s from %s.

Business: %s
Industry: %s
Tone: %s

Talk to customers like a real person. Be %s and genuine. Use natural language. Never use generic phrases like "How can I help you today?" - be specific and contextual.`,
			persona.BotName, biz.Name, biz.Name, biz.Niche, tone, tone)
	}
}
