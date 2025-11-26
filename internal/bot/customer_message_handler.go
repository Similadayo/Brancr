package bot

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// parseCustomerMessageInstruction parses messages like:
// "Tell Similoluwa Ilesanmi on Facebook that payment received"
// "Send message to John on Instagram saying hello"
// Returns customer name, platform, and message content
func parseCustomerMessageInstruction(text string) (customerName, platform, message string, valid bool) {
	text = strings.TrimSpace(text)
	
	// Pattern 1: "Tell [name] on [platform] that [message]"
	pattern1 := regexp.MustCompile(`(?i)^tell\s+(.+?)\s+on\s+(facebook|instagram|whatsapp|messenger)\s+that\s+(.+)$`)
	matches := pattern1.FindStringSubmatch(text)
	if len(matches) == 4 {
		return strings.TrimSpace(matches[1]), strings.ToLower(strings.TrimSpace(matches[2])), strings.TrimSpace(matches[3]), true
	}
	
	// Pattern 2: "Send message to [name] on [platform] saying [message]"
	pattern2 := regexp.MustCompile(`(?i)^send\s+(?:message\s+)?to\s+(.+?)\s+on\s+(facebook|instagram|whatsapp|messenger)\s+(?:saying|that|message|msg)\s+(.+)$`)
	matches = pattern2.FindStringSubmatch(text)
	if len(matches) == 4 {
		platform := strings.ToLower(strings.TrimSpace(matches[2]))
		// Normalize "messenger" to "facebook"
		if platform == "messenger" {
			platform = "facebook"
		}
		return strings.TrimSpace(matches[1]), platform, strings.TrimSpace(matches[3]), true
	}
	
	// Pattern 3: "[name] on [platform]: [message]" (simpler format)
	pattern3 := regexp.MustCompile(`(?i)^(.+?)\s+on\s+(facebook|instagram|whatsapp|messenger):\s+(.+)$`)
	matches = pattern3.FindStringSubmatch(text)
	if len(matches) == 4 {
		platform := strings.ToLower(strings.TrimSpace(matches[2]))
		if platform == "messenger" {
			platform = "facebook"
		}
		return strings.TrimSpace(matches[1]), platform, strings.TrimSpace(matches[3]), true
	}
	
	return "", "", "", false
}

// handleCustomerMessageInstruction processes tenant instructions to send messages to customers
func (h *TenantWebhookHandler) handleCustomerMessageInstruction(
	ctx context.Context,
	bot *tgbotapi.BotAPI,
	tenantID uint,
	chatID int64,
	text string,
) bool {
	customerName, platform, messageContent, valid := parseCustomerMessageInstruction(text)
	if !valid {
		return false // Not a customer message instruction
	}
	
	// Send processing message
	processingMsg, _ := bot.Send(tgbotapi.NewMessage(chatID, "Got it—give me a sec to handle that..."))
	
	go func() {
		// Find customer by name (try DisplayName, Username, or partial match)
		var customers []storage.Customer
		searchPattern := "%" + customerName + "%"
		
		// Try exact match on DisplayName first
		var exactCustomer storage.Customer
		if err := h.Manager.DB.Where("tenant_id = ? AND channel = ? AND (LOWER(display_name) = LOWER(?) OR LOWER(username) = LOWER(?))", 
			tenantID, platform, customerName, customerName).First(&exactCustomer).Error; err == nil {
			customers = []storage.Customer{exactCustomer}
		} else {
			// Try partial match
			query := h.Manager.DB.Where("tenant_id = ? AND channel = ? AND (LOWER(display_name) LIKE ? OR LOWER(username) LIKE ?)", 
				tenantID, platform, searchPattern, searchPattern)
			
			// Limit to 10 to avoid too many results
			if err := query.Limit(10).Find(&customers).Error; err != nil || len(customers) == 0 {
				// Delete processing message
				if processingMsg.MessageID > 0 {
					bot.Request(tgbotapi.NewDeleteMessage(chatID, processingMsg.MessageID))
				}
				bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Couldn't find customer '%s' on %s. Make sure they've messaged you before.", customerName, strings.Title(platform))))
				return
			}
		}
		
		// If multiple matches, use the most recent one
		if len(customers) > 1 {
			// Sort by LastMessageAt descending to get most recent
			for i := 0; i < len(customers)-1; i++ {
				if customers[i].LastMessageAt.Before(customers[i+1].LastMessageAt) {
					customers[i], customers[i+1] = customers[i+1], customers[i]
				}
			}
			h.Manager.Logger.Info("Multiple customers matched, using most recent",
				zap.Uint("tenant_id", tenantID),
				zap.String("customer_name", customerName),
				zap.Int("matches", len(customers)))
		}
		
		customer := customers[0]
		
		// Get conversation thread ID
		var conversation storage.Conversation
		threadID := customer.PlatformID // Default fallback
		
		if err := h.Manager.DB.Where("tenant_id = ? AND customer_id = ? AND platform = ?", 
			tenantID, customer.ID, platform).Order("last_message_at DESC").First(&conversation).Error; err == nil {
			if conversation.PlatformThreadID != "" {
				threadID = conversation.PlatformThreadID
			}
		}
		
		// Send message using MessageHandler (through EscalationHandler)
		if h.Manager.EscalationHandler != nil && h.Manager.EscalationHandler.MessageHandler != nil {
			err := h.Manager.EscalationHandler.MessageHandler.SendReply(ctx, tenantID, platform, threadID, customer.PlatformID, messageContent)
			
			// Delete processing message
			if processingMsg.MessageID > 0 {
				bot.Request(tgbotapi.NewDeleteMessage(chatID, processingMsg.MessageID))
			}
			
			if err != nil {
				h.Manager.Logger.Error("failed to send message to customer",
					zap.Uint("tenant_id", tenantID),
					zap.Uint("customer_id", customer.ID),
					zap.String("platform", platform),
					zap.Error(err))
				
				errorMsg := fmt.Sprintf("❌ Couldn't send message to %s on %s: %v", customerName, strings.Title(platform), err)
				bot.Send(tgbotapi.NewMessage(chatID, errorMsg))
				return
			}
			
			// Success message
			displayName := customer.DisplayName
			if displayName == "" {
				displayName = customer.Username
			}
			if displayName == "" {
				displayName = customerName
			}
			
			successMsg := fmt.Sprintf("Done! 👍 Messaged %s on %s: \"%s\"", displayName, strings.Title(platform), messageContent)
			bot.Send(tgbotapi.NewMessage(chatID, successMsg))
			
			// Log as manual interaction
			interaction := storage.Interaction{
				TenantID:       tenantID,
				CustomerID:     customer.ID,
				ConversationID: conversation.ID,
				Platform:       platform,
				Direction:      "outgoing",
				MessageType:    "text",
				Content:        messageContent,
				ResponseType:   "manual",
				ResponseStatus: "sent",
				FinalReply:     messageContent,
			}
			h.Manager.DB.Create(&interaction)
			
			h.Manager.Logger.Info("✅ Sent manual message to customer",
				zap.Uint("tenant_id", tenantID),
				zap.Uint("customer_id", customer.ID),
				zap.String("platform", platform))
		} else {
			// Delete processing message
			if processingMsg.MessageID > 0 {
				bot.Request(tgbotapi.NewDeleteMessage(chatID, processingMsg.MessageID))
			}
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Message handler not available. Please try again later."))
		}
	}()
	
	return true // Instruction handled
}

