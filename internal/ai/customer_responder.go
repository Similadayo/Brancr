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

	// Load conversation history
	conversationHistory := r.Memory.GetRecentCustomerConversationWithTimeLimit(ctx, tenantID, customerID, 30) // Last 30 days

	// Use customer-facing prompts (not tenant-facing)
	base := BuildCustomerBaseMessages(persona, biz)
	messages := append([]Message{}, base...)

	// Add conversation history
	messages = append(messages, conversationHistory...)

	// Build business context (menu items and FAQs)
	var businessContext string
	if itemsContext != "" {
		// Use new industry-aware context
		businessContext = r.buildBusinessContextWithItems(itemsContext, faqs, intent)
	} else {
		// Use old BusinessMenuItem context
		businessContext = r.buildBusinessContext(menuItems, faqs, intent)
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
	
	// Add sales context if available
	salesGuidance := ""
	if salesContext != "" {
		salesGuidance = fmt.Sprintf("\n\nSALES CONVERSATION CONTEXT:\n%s\n\nUse this context to guide your sales approach. Adapt your response based on the sales stage and commitment level.", salesContext)
	}
	
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

%s%s

Detected intent: %s
Formatting notes: %s

Customer message: %s`,
		intro,
		strings.ToUpper(platform),
		businessContext,
		salesGuidance,
		intent,
		formatHint,
		message,
	)

	messages = append(messages, Message{Role: "user", Content: task})

	reply, err := r.Client.Generate(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("failed to generate reply: %w", err)
	}

	reply = strings.TrimSpace(reply)

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
						// ReceiptService for portal token generation
						receiptService := receipt.NewReceiptService(r.DB, r.Logger)
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

// buildBusinessContextWithItems creates context string from industry-aware items and FAQs
func (r *CustomerResponder) buildBusinessContextWithItems(itemsContext string, faqs []storage.BusinessFAQ, intent string) string {
	var context strings.Builder

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
		return fmt.Sprintf("BUSINESS INFORMATION:\n%s\nUse this information to answer customer questions accurately. If they ask about products/services, reference the items listed. If they ask questions similar to the FAQs, use those answers as a guide (but rephrase naturally).", result)
	}
	return ""
}

// buildBusinessContext creates context string from menu items and FAQs based on intent (legacy)
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

