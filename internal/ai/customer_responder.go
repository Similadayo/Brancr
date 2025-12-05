package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/similadayo/brancr/internal/receipt"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// CustomerResponder generates AI replies for customer conversations
type CustomerResponder struct {
	Client                   *AIClient
	Memory                   *MemoryService
	DB                       *gorm.DB
	Logger                   *zap.Logger
	RetrievalService         interface { // Industry-aware retrieval service (optional)
		SearchItems(ctx context.Context, tenantID uint, query string, limit int) (interface{}, error)
		GetAllActive(ctx context.Context, tenantID uint, limit int) (interface{}, error)
		FormatForAI(items interface{}) string
	}
	SalesConversationManager *SalesConversationManager      // Sales conversation tracking
	PaymentInstructionGen    *PaymentInstructionGenerator   // Payment instruction generator
	OrderService             interface { // Order service for creating orders
		CreateOrderFromSalesConversation(ctx context.Context, tenantID, customerID, conversationID uint, salesConv *storage.SalesConversation, customerName, customerPhone, customerEmail, platform string) (*storage.Order, error)
	}
	CustomerExtractor        *CustomerInfoExtractor         // Customer info extraction
	IndustryService          interface { // Industry service (optional)
		GetTenantIndustry(ctx context.Context, tenantID uint) (interface{}, error)
		GetIndustrySettings(ctx context.Context, tenantID uint) (map[string]interface{}, error)
	}
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
// conversationID is optional - if provided, sales conversation tracking will be used
func (r *CustomerResponder) GenerateCustomerReply(ctx context.Context, tenantID uint, customerID uint, message string, intent string, platform string, conversationID ...uint) (string, error) {
	var convID uint
	if len(conversationID) > 0 {
		convID = conversationID[0]
	}
	return r.GenerateCustomerReplyWithSales(ctx, tenantID, customerID, message, intent, platform, convID)
}

// GenerateCustomerReplyWithSales generates an AI reply with sales conversation tracking
func (r *CustomerResponder) GenerateCustomerReplyWithSales(ctx context.Context, tenantID uint, customerID uint, message string, intent string, platform string, conversationID uint) (string, error) {
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

	// Load business items using industry-aware retrieval (if available)
	// Fallback to old BusinessMenuItem for backward compatibility
	var menuItems []storage.BusinessMenuItem
	var itemsContext string
	
	// Try to use industry-aware retrieval if available
	if r.RetrievalService != nil {
		// Extract query from message for search
		query := message
		if len(query) > 50 {
			query = query[:50] // Limit query length
		}
		
		items, err := r.RetrievalService.SearchItems(ctx, tenantID, query, 10)
		if err == nil && items != nil {
			itemsContext = r.RetrievalService.FormatForAI(items)
		}
		
		// If search returned empty, try getting all active items
		if itemsContext == "" {
			items, err := r.RetrievalService.GetAllActive(ctx, tenantID, 10)
			if err == nil && items != nil {
				itemsContext = r.RetrievalService.FormatForAI(items)
			}
		}
	}
	
	// Fallback to old BusinessMenuItem if retrieval service not available or returned no results
	if itemsContext == "" {
		r.DB.Where("tenant_id = ?", tenantID).Find(&menuItems)
	}

	// Load FAQs for question matching
	var faqs []storage.BusinessFAQ
	r.DB.Where("tenant_id = ?", tenantID).Limit(10).Find(&faqs)

	// Load conversation history - use conversationID if available for more precise history
	var conversationHistory []Message
	if conversationID > 0 {
		// Use conversation-specific history for better context
		conversationHistory = r.Memory.GetRecentCustomerConversationWithTimeLimitAndConversation(ctx, tenantID, customerID, conversationID, 30)
	} else {
		// Fallback to customer-wide history
		conversationHistory = r.Memory.GetRecentCustomerConversationWithTimeLimit(ctx, tenantID, customerID, 30) // Last 30 days
	}

	// Use customer-facing prompts (not tenant-facing)
	base := BuildCustomerBaseMessages(persona, biz)
	messages := append([]Message{}, base...)

	// Add conversation history
	messages = append(messages, conversationHistory...)

	// Build business context (menu items, FAQs, and knowledge base)
	var businessContext string
	if itemsContext != "" {
		// Use new industry-aware context
		businessContext = r.buildBusinessContextWithItems(itemsContext, faqs, biz.KnowledgeBase, intent)
	} else {
		// Use old BusinessMenuItem context
		businessContext = r.buildBusinessContext(menuItems, faqs, biz.KnowledgeBase, intent)
	}

	// Sales conversation tracking (if enabled)
	var salesContext string
	var isCommitted, isNegotiating bool
	if r.SalesConversationManager != nil && conversationID > 0 {
		salesConv, err := r.SalesConversationManager.GetOrCreateSalesConversation(ctx, tenantID, customerID, conversationID)
		if err == nil {
			// Detect commitment
			isCommitted, _ = r.SalesConversationManager.DetectCommitment(ctx, message, salesConv)
			if isCommitted {
				r.SalesConversationManager.UpdateCommitmentLevel(ctx, salesConv.ID, "committed")
				r.SalesConversationManager.UpdateStage(ctx, salesConv.ID, "payment_instructions")
			}

			// Detect negotiation
			isNegotiating, _, _ = r.SalesConversationManager.DetectNegotiation(ctx, message)
			if isNegotiating {
				r.SalesConversationManager.UpdateStage(ctx, salesConv.ID, "negotiation")
				r.SalesConversationManager.IncrementNegotiation(ctx, salesConv.ID)
			}

			// Build sales context
			salesContext, _ = r.SalesConversationManager.BuildIndustrySalesContext(ctx, tenantID, salesConv)
			r.SalesConversationManager.UpdateStage(ctx, salesConv.ID, r.determineSalesStage(intent, isCommitted, isNegotiating))
		}
	}

	// Add current message
	intro := BuildIntroTone(persona, biz)
	formatHint := getPlatformFormatting(platform)
	
	// Build brand context with business details (name, description, niche, tone)
	brandContext := BuildCustomerBrandContext(persona, biz)
	
	// Add sales context if available
	salesGuidance := ""
	if salesContext != "" {
		salesGuidance = fmt.Sprintf("\n\nSALES CONVERSATION CONTEXT:\n%s\n\nUse this context to guide your sales approach. Adapt your response based on the sales stage and commitment level.", salesContext)
	}
	
	// Template: #8 GenerateCustomerReply (customer-facing reply generation)
	// Enhanced with GPT-5-mini best practices for human-like responses
	task := fmt.Sprintf(`%s

%s

You are responding as a real customer service representative for %s on %s. Write like you're texting a friend who happens to be a customer - natural, warm, and genuinely helpful.

CONVERSATION CONTEXT:
- Previous messages: {{history}}
- Customer just said: "%s"
- What they're asking about: %s
- Platform: %s (WhatsApp messaging - keep it conversational)

%s%s

WRITING STYLE - Write Like a Real Human:
1. **Answer directly first** - Get to the point immediately, then add context if needed
2. **Vary your sentence length** - Mix short punchy sentences ("Got it!") with slightly longer ones when explaining
3. **Use natural transitions** - "So...", "Actually...", "Here's the thing...", "Let me help with that..."
4. **Show you're listening** - Reference what they said naturally ("I hear you", "I understand", "That makes sense")
5. **Match their energy** - If they're casual, be casual. If they're serious, be appropriately serious
6. **Use fragments when natural** - "Sounds good!", "On it!", "No worries!"
7. **Avoid corporate speak** - Don't say "I understand your concern" - say "I get it" or "That's frustrating"
8. **Be specific, not generic** - Instead of "We can help", say "I can check on that for you right now"

LENGTH & CONCISENESS:
- Simple questions (greetings, basic info): 1-2 sentences max
- Regular questions: 2-3 sentences max  
- Complex issues: 3-4 sentences max
- NEVER exceed 150 words total
- ONE message only - never split into multiple messages

NIGERIAN ENGLISH & CULTURAL AWARENESS:
- Understand Nigerian English patterns but respond in simple, clear English
- Use customer's name when available ("Good morning John" or "Hi Sarah")
- Use "sir/ma" respectfully when gender is clear from context
- Understand phrases: "How far?" = "How are you?", "Well done" = thanks/acknowledgment, "I am coming" = "I'm on my way"
- "Sorry" often means empathy/acknowledgment, not just apology
- NEVER use Pidgin words (sha, abi, na so, dey, etc.) - always use proper English
- Keep vocabulary simple and everyday - avoid complex words

EMOTIONAL INTELLIGENCE:
- If they seem frustrated: Acknowledge it genuinely ("I can see this is frustrating")
- If they're happy: Match their enthusiasm naturally
- If they're confused: Be patient and break it down simply
- Show you actually care, not just being polite

AVOID ROBOTIC PATTERNS:
- Don't start every message the same way
- Don't use phrases like "Thank you for contacting us", "I understand your concern", "Is there anything else I can help you with?"
- Don't sound like a template or script
- Don't be overly formal or polished
- Vary your responses - each conversation should feel unique

TASK: Reply to their message naturally, like a real person would. Address what they asked directly. Be helpful, warm, and concise.`,
		intro,
		brandContext,
		strings.ToUpper(platform),
		message,
		intent,
		formatHint,
		businessContext,
		salesGuidance,
	)

	messages = append(messages, Message{Role: "user", Content: task})

	reply, err := r.Client.Generate(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("failed to generate reply: %w", err)
	}

	reply = strings.TrimSpace(reply)
	
	// Enforce maximum length for WhatsApp (keep responses concise)
	// Split by sentences to avoid cutting mid-sentence
	maxLength := 500 // characters (roughly 100 words)
	originalLength := len(reply)
	if originalLength > maxLength {
		// Try to cut at a sentence boundary
		cutPoint := maxLength
		for i := maxLength; i > maxLength-100 && i > 0; i-- {
			if i < len(reply) && (reply[i] == '.' || reply[i] == '!' || reply[i] == '?') {
				cutPoint = i + 1
				break
			}
		}
		if cutPoint < len(reply) {
			reply = reply[:cutPoint]
			// Add ellipsis if we cut it
			if !strings.HasSuffix(reply, "...") {
				reply += "..."
			}
			if r.Logger != nil {
				r.Logger.Warn("AI reply was too long, truncated",
					zap.Int("original_length", originalLength),
					zap.Int("truncated_length", len(reply)),
					zap.Int("max_length", maxLength))
			}
		}
	}

	// If customer is committed and ready for payment, create order and append payment instructions
	if r.SalesConversationManager != nil && conversationID > 0 && r.PaymentInstructionGen != nil {
		salesConv, err := r.SalesConversationManager.GetOrCreateSalesConversation(ctx, tenantID, customerID, conversationID)
		if err == nil && salesConv.CommitmentLevel == "committed" && salesConv.Stage == "payment_instructions" {
			// Create order if not already created
			var order *storage.Order
			if r.OrderService != nil && r.CustomerExtractor != nil {
				// Extract customer info
				customerName, customerPhone, customerEmail, _ := r.CustomerExtractor.ExtractCustomerInfo(
					ctx, tenantID, customerID, conversationID, platform)

				// Create order
				order, err = r.OrderService.CreateOrderFromSalesConversation(
					ctx,
					tenantID,
					customerID,
					conversationID,
					salesConv,
					customerName,
					customerPhone,
					customerEmail,
					platform,
				)
				if err != nil {
					r.Logger.Error("failed to create order", zap.Error(err))
				} else {
					r.Logger.Info("order created from sales conversation",
						zap.Uint("order_id", order.ID),
						zap.String("order_number", order.OrderNumber))
				}
			}

			// Generate payment instructions
			amount := salesConv.PriceDiscussed
			if order != nil {
				amount = order.TotalAmount
			} else if amount == 0 {
				// Try to get amount from items discussed
				var items []map[string]interface{}
				if salesConv.ItemsDiscussed != "" {
					json.Unmarshal([]byte(salesConv.ItemsDiscussed), &items)
					if len(items) > 0 {
						if price, ok := items[0]["price"].(float64); ok {
							amount = price
						}
					}
				}
			}
			
			if amount > 0 {
				var orderNumber, paymentRef, portalURL string
				if order != nil {
					orderNumber = order.OrderNumber
					paymentRef = order.PaymentReference
					
					// Generate portal token and URL for the order
					if r.OrderService != nil {
						// We'll generate portal token via receipt service
						// For now, we'll create a placeholder - portal token will be generated when payment is confirmed
						// But we can generate it now for customer access
						publicURL := "https://api.brancr.com" // Default fallback - TODO: Get from config if available
						// ReceiptService for portal token generation doesn't need Cloudinary or EmailService
						receiptService := receipt.NewReceiptService(r.DB, r.Logger, "", publicURL, nil)
						token, url, err := receiptService.GeneratePortalTokenAndURL(ctx, order.ID, tenantID, publicURL)
						if err == nil {
							portalURL = url
							r.Logger.Info("portal token generated for order", 
								zap.Uint("order_id", order.ID),
								zap.String("token", token))
						}
					}
				}
				
				paymentInstructions, err := r.PaymentInstructionGen.GeneratePaymentInstructions(
					ctx, tenantID, amount, "NGN", orderNumber, paymentRef, portalURL)
				if err == nil && paymentInstructions != "" {
					reply += "\n\n" + paymentInstructions
				}
			}
		}
	}

	return reply, nil
}

// determineSalesStage determines the sales stage based on intent and conversation state
func (r *CustomerResponder) determineSalesStage(intent string, isCommitted, isNegotiating bool) string {
	if isCommitted {
		return "payment_instructions"
	}
	if isNegotiating {
		return "negotiation"
	}
	switch intent {
	case "order", "payment":
		return "commitment_confirmation"
	case "inquiry":
		return "product_inquiry"
	default:
		return "greeting"
	}
}

// trackItemsInSalesConversation tracks items mentioned in the conversation
func (r *CustomerResponder) trackItemsInSalesConversation(ctx context.Context, salesConv *storage.SalesConversation, itemsContext, message string) {
	// This is a simplified tracking - in production, you might want to use AI to extract
	// specific items and prices from the conversation
	
	// For now, we'll update price discussed if a number is mentioned
	// and let the order creation extract items from ItemsDiscussed JSON
	
	// If ItemsDiscussed is empty, we could try to parse itemsContext
	// But for now, we'll rely on the retrieval service to populate items
	// when they're discussed in the conversation
	
	// This is a placeholder for future enhancement
	// The actual item tracking should happen when AI mentions specific products
}

// buildBusinessContextWithItems creates context string from industry-aware items, FAQs, and knowledge base
func (r *CustomerResponder) buildBusinessContextWithItems(itemsContext string, faqs []storage.BusinessFAQ, knowledgeBase string, intent string) string {
	var context strings.Builder

	// Always include knowledge base if available (contains comprehensive business information)
	if strings.TrimSpace(knowledgeBase) != "" {
		context.WriteString("\nBUSINESS KNOWLEDGE BASE:\n")
		// Limit knowledge base to first 2000 characters to avoid token bloat
		kb := strings.TrimSpace(knowledgeBase)
		if len(kb) > 2000 {
			kb = kb[:2000] + "..."
		}
		context.WriteString(kb)
		context.WriteString("\n")
	}

	// For product/service inquiries, include items
	if intent == "inquiry" || intent == "order" {
		if itemsContext != "" {
			context.WriteString("\nAVAILABLE PRODUCTS/SERVICES:\n")
			context.WriteString(itemsContext)
			context.WriteString("\n")
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
		return fmt.Sprintf("BUSINESS INFORMATION:\n%s\nUse this information to answer customer questions accurately. Reference the knowledge base for comprehensive business details, products/services for pricing inquiries, and FAQs for common questions. Always rephrase naturally and conversationally.", result)
	}
	return ""
}

// buildBusinessContext creates context string from menu items, FAQs, and knowledge base based on intent (legacy)
func (r *CustomerResponder) buildBusinessContext(menuItems []storage.BusinessMenuItem, faqs []storage.BusinessFAQ, knowledgeBase string, intent string) string {
	var context strings.Builder

	// Always include knowledge base if available (contains comprehensive business information)
	if strings.TrimSpace(knowledgeBase) != "" {
		context.WriteString("\nBUSINESS KNOWLEDGE BASE:\n")
		// Limit knowledge base to first 2000 characters to avoid token bloat
		kb := strings.TrimSpace(knowledgeBase)
		if len(kb) > 2000 {
			kb = kb[:2000] + "..."
		}
		context.WriteString(kb)
		context.WriteString("\n")
	}

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
		return fmt.Sprintf("BUSINESS INFORMATION:\n%s\nUse this information to answer customer questions accurately. Reference the knowledge base for comprehensive business details, menu items for pricing inquiries, and FAQs for common questions. Always rephrase naturally and conversationally.", result)
	}
	return ""
}

