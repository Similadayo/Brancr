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

// ContextualFallbackGenerator generates contextual fallback messages using AI
// CRITICAL: NO hardcoded responses - everything must be AI-generated
type ContextualFallbackGenerator struct {
	Client *AIClient
	DB     *gorm.DB
	Logger *zap.Logger
	Memory *MemoryService
}

// NewContextualFallbackGenerator creates a new contextual fallback generator
func NewContextualFallbackGenerator(client *AIClient, db *gorm.DB, logger *zap.Logger, memory *MemoryService) *ContextualFallbackGenerator {
	return &ContextualFallbackGenerator{
		Client: client,
		DB:     db,
		Logger: logger,
		Memory: memory,
	}
}

// GenerateErrorResponse generates a contextual error response for customers
// Uses AI to generate a natural, business-specific response
func (g *ContextualFallbackGenerator) GenerateErrorResponse(
	ctx context.Context,
	tenantID uuid.UUID,
	customerID uuid.UUID,
	originalMessage string,
	errorType string, // "processing_error", "system_error", "timeout", etc.
) (string, error) {
	// Load business context
	var biz storage.BusinessProfile
	if err := g.DB.First(&biz, "tenant_id = ?", tenantID).Error; err != nil {
		biz.Name = "our business"
	}

	// Load persona for tone
	var persona storage.Persona
	if err := g.DB.First(&persona, "tenant_id = ?", tenantID).Error; err != nil {
		persona.Tone = "friendly and warm"
	}

	// Get recent conversation history for context
	var recentHistory []string
	if g.Memory != nil {
		history := g.Memory.GetRecentCustomerConversationWithTimeLimit(ctx, tenantID, customerID, 1) // Last day
		for _, msg := range history {
			if msg.Role == "user" || msg.Role == "assistant" {
				if content, ok := msg.Content.(string); ok {
					recentHistory = append(recentHistory, content)
				}
			}
		}
		// Limit to last 2-3 turns
		if len(recentHistory) > 6 {
			recentHistory = recentHistory[len(recentHistory)-6:]
		}
	}

	// Build contextual error message prompt
	var errorContext string
	switch errorType {
	case "processing_error":
		errorContext = "The system had trouble processing your message"
	case "system_error":
		errorContext = "We're experiencing a temporary issue"
	case "timeout":
		errorContext = "The response is taking longer than expected"
	default:
		errorContext = "Something unexpected happened"
	}

	historyStr := ""
	if len(recentHistory) > 0 {
		historyStr = "\n\nRecent conversation:\n" + strings.Join(recentHistory, "\n")
	}

	// Build thin, focused prompt for error response
	prompt := fmt.Sprintf(`You are a customer service representative for %s.
Tone: %s

The customer just said: "%s"
%s

Your response should:
- Acknowledge their message briefly
- Explain that %s, but you're here to help
- Offer to help them with what they need
- Match the tone of %s
- Keep it to 1-2 sentences max
- Sound natural and warm, not robotic
- Be unique and specific to this situation - NEVER use generic phrases

DO NOT SAY:
- "Thank you for contacting us"
- "We apologize for the inconvenience"
- "How can I help you today?"
- Any other generic customer service phrases

Be genuine, specific, and human.`,
		biz.Name,
		persona.Tone,
		originalMessage,
		historyStr,
		errorContext,
		biz.Name)

	messages := []Message{
		{Role: "system", Content: fmt.Sprintf("You are a helpful, genuine representative for %s. Your tone is %s. Never use generic phrases.", biz.Name, persona.Tone)},
		{Role: "user", Content: prompt},
	}

	reply, err := g.Client.Generate(ctx, messages)
	if err != nil {
		g.Logger.Warn("failed to generate contextual error response", zap.Error(err))
		// Return error so caller can escalate instead of sending canned response
		return "", fmt.Errorf("AI generation failed: %w", err)
	}

	return strings.TrimSpace(reply), nil
}

// GenerateProcessingResponse generates a contextual "we're processing" message
func (g *ContextualFallbackGenerator) GenerateProcessingResponse(
	ctx context.Context,
	tenantID uuid.UUID,
	customerID uuid.UUID,
	originalMessage string,
) (string, error) {
	var biz storage.BusinessProfile
	if err := g.DB.First(&biz, "tenant_id = ?", tenantID).Error; err != nil {
		biz.Name = "our business"
	}

	var persona storage.Persona
	if err := g.DB.First(&persona, "tenant_id = ?", tenantID).Error; err != nil {
		persona.Tone = "friendly"
	}

	prompt := fmt.Sprintf(`You are a customer service representative for %s.
Tone: %s

The customer just said: "%s"

Respond briefly (1 sentence) acknowledging their message and letting them know you're working on it. 
Match the business tone. Be natural and genuine - never generic.`,
		biz.Name,
		persona.Tone,
		originalMessage)

	messages := []Message{
		{Role: "system", Content: fmt.Sprintf("You are a helpful representative for %s. Your tone is %s.", biz.Name, persona.Tone)},
		{Role: "user", Content: prompt},
	}

	reply, err := g.Client.Generate(ctx, messages)
	if err != nil {
		g.Logger.Warn("failed to generate processing response", zap.Error(err))
		// Return error so caller can handle appropriately
		return "", fmt.Errorf("AI generation failed: %w", err)
	}

	return strings.TrimSpace(reply), nil
}

// GenerateEscalationAcknowledgment generates a contextual message when escalating to human
func (g *ContextualFallbackGenerator) GenerateEscalationAcknowledgment(
	ctx context.Context,
	tenantID uuid.UUID,
	customerID uuid.UUID,
	originalMessage string,
) (string, error) {
	var biz storage.BusinessProfile
	if err := g.DB.First(&biz, "tenant_id = ?", tenantID).Error; err != nil {
		biz.Name = "our business"
	}

	var persona storage.Persona
	if err := g.DB.First(&persona, "tenant_id = ?", tenantID).Error; err != nil {
		persona.Tone = "friendly and professional"
	}

	prompt := fmt.Sprintf(`You are a customer service representative for %s.
Tone: %s

The customer just said: "%s"

Respond briefly (1-2 sentences) acknowledging their message and letting them know someone from the team will get back to them.
Match the business tone. Be natural and confident, not robotic. Never use generic phrases.`,
		biz.Name,
		persona.Tone,
		originalMessage)

	messages := []Message{
		{Role: "system", Content: fmt.Sprintf("You are a representative for %s. Your tone is %s. Be warm and confident.", biz.Name, persona.Tone)},
		{Role: "user", Content: prompt},
	}

	reply, err := g.Client.Generate(ctx, messages)
	if err != nil {
		g.Logger.Warn("failed to generate escalation acknowledgment", zap.Error(err))
		// Return error so caller can handle appropriately
		return "", fmt.Errorf("AI generation failed: %w", err)
	}

	return strings.TrimSpace(reply), nil
}
