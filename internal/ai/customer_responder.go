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

	// Load business menu items for product inquiries
	var menuItems []storage.BusinessMenuItem
	r.DB.Where("tenant_id = ?", tenantID).Find(&menuItems)

	// Load FAQs for question matching
	var faqs []storage.BusinessFAQ
	r.DB.Where("tenant_id = ?", tenantID).Limit(10).Find(&faqs)

	// Load conversation history
	conversationHistory := r.Memory.GetRecentCustomerConversationWithTimeLimit(ctx, tenantID, customerID, 30) // Last 30 days

	// Use customer-facing prompts (not tenant-facing)
	base := BuildCustomerBaseMessages(persona, biz)
	messages := append([]Message{}, base...)

	// Add conversation history
	messages = append(messages, conversationHistory...)

	// Build business context (menu items and FAQs)
	businessContext := r.buildBusinessContext(menuItems, faqs, intent)

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

NIGERIAN ENGLISH AWARENESS: Many customers may use Nigerian English or Pidgin patterns. 

Style: Casual professional - friendly, approachable, use SIMPLE vocabulary (avoid complex words). Follow Nigerian conversation patterns - direct, warm, natural flow.

Address customers respectfully - use their name when available (e.g., "Good morning John") or "sir/ma" when you can determine gender. If gender is unclear, use their name directly or neutral friendly language. Understand common phrases like "How far?", "Well done", "I am coming", and that "sorry" means empathy/acknowledgment, not just apology. 

CRITICAL: 
- NEVER respond in Pidgin English
- Use SIMPLE, everyday words - keep it easy to understand
- Casual professional tone - friendly, not stiff
- Nigerian conversation pattern - direct, warm, natural
- Do NOT use Pidgin words like "sha", "abi", "na so", "dey", "go come", etc.
- Use customer's name when available - more personal and respectful
- If uncertain about gender, use name directly - it's okay to not use sir/ma

Use the conversation history above as context.

%s

Detected intent: %s
Formatting notes: %s

Customer message: %s`,
		intro,
		strings.ToUpper(platform),
		businessContext,
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

// buildBusinessContext creates context string from menu items and FAQs based on intent
func (r *CustomerResponder) buildBusinessContext(menuItems []storage.BusinessMenuItem, faqs []storage.BusinessFAQ, intent string) string {
	var context strings.Builder

	// For product/service inquiries, include menu items
	if intent == "inquiry" || intent == "order" {
		if len(menuItems) > 0 {
			context.WriteString("\nAVAILABLE PRODUCTS/SERVICES:\n")
			for i, item := range menuItems {
				if i >= 10 { // Limit to 10 items to avoid token bloat
					break
				}
				context.WriteString(fmt.Sprintf("- %s", item.Name))
				if item.Category != "" {
					context.WriteString(fmt.Sprintf(" (%s)", item.Category))
				}
				if item.Price != "" {
					context.WriteString(fmt.Sprintf(" - %s", item.Price))
				}
				if item.Description != "" {
					context.WriteString(fmt.Sprintf(": %s", item.Description))
				}
				context.WriteString("\n")
			}
		}
	}

	// For general inquiries, include relevant FAQs
	if intent == "inquiry" || intent == "casual" {
		if len(faqs) > 0 {
			context.WriteString("\nFREQUENTLY ASKED QUESTIONS:\n")
			for i, faq := range faqs {
				if i >= 5 { // Limit to 5 FAQs
					break
				}
				context.WriteString(fmt.Sprintf("Q: %s\nA: %s\n", faq.Question, faq.Answer))
			}
		}
	}

	result := context.String()
	if result != "" {
		return fmt.Sprintf("BUSINESS INFORMATION:\n%s\nUse this information to answer customer questions accurately. If they ask about products/services, reference the menu items. If they ask questions similar to the FAQs, use those answers as a guide (but rephrase naturally).", result)
	}
	return ""
}

