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

	// Use customer-facing prompts (not tenant-facing)
	base := BuildCustomerBaseMessages(persona, biz)
	messages := append([]Message{}, base...)

	// Add conversation history
	messages = append(messages, conversationHistory...)

	// Add current message
	intro := BuildIntroTone(persona, biz)
	formatHint := getPlatformFormatting(platform)
	task := fmt.Sprintf(`%s

Task: Reply to the latest customer message on %s.

CRITICAL: Write like a real human customer service rep would - natural, empathetic, and genuinely helpful.
- Vary your sentence structure (mix short responses with longer explanations when needed)
- Use contractions and conversational language naturally
- Show genuine empathy - match their emotional state authentically
- Don't sound like a script, template, or corporate bot
- Write like you're texting a friend who happens to be a customer
- Match their energy - if they're casual, be casual; if they're serious, be appropriately serious
- Don't be overly polished or generic - sound like a real person

NIGERIAN ENGLISH AWARENESS: Many customers may use Nigerian English patterns. Always use "sir" or "ma" to show respect. Understand common phrases like "How far?", "Well done", "I am coming", and that "sorry" means empathy/acknowledgment, not just apology. Use proper greetings like "Good morning sir/ma". Remain professional and friendly in the Nigerian way - warm and respectful. You don't need to speak Pidgin - stay professional but understand their communication style.

Use the conversation history above as context.

Detected intent: %s
Formatting notes: %s

Customer message: %s`,
		intro,
		strings.ToUpper(platform),
		intent,
		formatHint,
		message,
	)

	messages = append(messages, Message{Role: "user", Content: task})

	reply, err := r.Client.Generate(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("failed to generate reply: %w", err)
	}

	return strings.TrimSpace(reply), nil
}

