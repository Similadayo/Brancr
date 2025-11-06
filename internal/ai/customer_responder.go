package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// CustomerResponder generates AI replies for customer conversations
type CustomerResponder struct {
	Client *AIClient
	Memory *MemoryService
	DB     *gorm.DB
	Logger *zap.Logger
}

// NewCustomerResponder creates a new CustomerResponder
func NewCustomerResponder(client *AIClient, db *gorm.DB, logger *zap.Logger, memory *MemoryService) *CustomerResponder {
	return &CustomerResponder{
		Client: client,
		Memory: memory,
		DB:     db,
		Logger: logger,
	}
}

// GenerateCustomerReply generates an AI reply for a customer message
func (r *CustomerResponder) GenerateCustomerReply(ctx context.Context, tenantID uint, customerID uint, message string, intent string, platform string) (string, error) {
	// Load persona and business profile
	var persona storage.Persona
	if err := r.DB.First(&persona, "tenant_id = ?", tenantID).Error; err != nil {
		persona.BotName = "Assistant"
		persona.Tone = "friendly"
	}

	var biz storage.BusinessProfile
	if err := r.DB.First(&biz, "tenant_id = ?", tenantID).Error; err != nil {
		biz.Description = ""
		biz.Niche = ""
	}

	// Load conversation history
	conversationHistory := r.Memory.GetRecentCustomerConversationWithTimeLimit(ctx, tenantID, customerID, 30) // Last 30 days

	// Build context-aware prompt
	systemPrompt := fmt.Sprintf(`You're %s from %s.
Business: %s
Niche: %s
Tone: %s

Detected intent: %s

Reply naturally. Be brief. Help the customer.`,
		persona.BotName,
		biz.Name,
		biz.Description,
		biz.Niche,
		persona.Tone,
		intent,
	)

	// Add platform formatting
	platformFormat := getPlatformFormatting(platform)
	if platformFormat != "" {
		systemPrompt += "\n" + platformFormat
	}

	messages := []Message{
		{Role: "system", Content: systemPrompt},
	}

	// Add conversation history
	messages = append(messages, conversationHistory...)

	// Add current message
	messages = append(messages, Message{Role: "user", Content: message})

	reply, err := r.Client.Generate(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("failed to generate reply: %w", err)
	}

	return strings.TrimSpace(reply), nil
}

