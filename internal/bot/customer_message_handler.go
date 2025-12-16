package bot

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// parseCustomerMessageInstruction parses messages like:
// "Tell Similoluwa Ilesanmi on Facebook that payment received"
// "Send message to John on Instagram saying hello"
// "Fred, please greet @zoovie_boy on instagram"
// "[BotName], message @username on facebook saying hello"
// Returns customer name/username, platform, message content, and whether it's a username (starts with @)
func parseCustomerMessageInstruction(text string) (customerName, platform, message string, isUsername bool, valid bool) {
	text = strings.TrimSpace(text)

	// Remove bot name prefix (e.g., "Fred, ", "Fred, please", etc.)
	// This handles patterns like "Fred, please greet @zoovie_boy on instagram"
	botNamePattern := regexp.MustCompile(`(?i)^([a-z0-9_]+)[,\s]+(?:please\s+)?(.+)$`)
	if matches := botNamePattern.FindStringSubmatch(text); len(matches) == 3 {
		text = strings.TrimSpace(matches[2]) // Use the part after bot name
	}

	// Check if customer name starts with @ (username/handle)
	isUsername = false
	if strings.HasPrefix(text, "@") || strings.Contains(text, " @") {
		isUsername = true
	}

	// Pattern 1: "Tell [name/@username] on [platform] that [message]"
	pattern1 := regexp.MustCompile(`(?i)^tell\s+(.+?)\s+on\s+(facebook|instagram|whatsapp|messenger)\s+that\s+(.+)$`)
	matches := pattern1.FindStringSubmatch(text)
	if len(matches) == 4 {
		customerName := strings.TrimSpace(matches[1])
		platform := strings.ToLower(strings.TrimSpace(matches[2]))
		if platform == "messenger" {
			platform = "facebook"
		}
		isUser := strings.HasPrefix(customerName, "@")
		if isUser {
			customerName = strings.TrimPrefix(customerName, "@")
		}
		return customerName, platform, strings.TrimSpace(matches[3]), isUser, true
	}

	// Pattern 2: "Send message to [name/@username] on [platform] saying [message]"
	pattern2 := regexp.MustCompile(`(?i)^send\s+(?:message\s+)?to\s+(.+?)\s+on\s+(facebook|instagram|whatsapp|messenger)\s+(?:saying|that|message|msg)\s+(.+)$`)
	matches = pattern2.FindStringSubmatch(text)
	if len(matches) == 4 {
		customerName := strings.TrimSpace(matches[1])
		platform := strings.ToLower(strings.TrimSpace(matches[2]))
		if platform == "messenger" {
			platform = "facebook"
		}
		isUser := strings.HasPrefix(customerName, "@")
		if isUser {
			customerName = strings.TrimPrefix(customerName, "@")
		}
		return customerName, platform, strings.TrimSpace(matches[3]), isUser, true
	}

	// Pattern 3: "[name/@username] on [platform]: [message]" (simpler format)
	pattern3 := regexp.MustCompile(`(?i)^(.+?)\s+on\s+(facebook|instagram|whatsapp|messenger):\s+(.+)$`)
	matches = pattern3.FindStringSubmatch(text)
	if len(matches) == 4 {
		customerName := strings.TrimSpace(matches[1])
		platform := strings.ToLower(strings.TrimSpace(matches[2]))
		if platform == "messenger" {
			platform = "facebook"
		}
		isUser := strings.HasPrefix(customerName, "@")
		if isUser {
			customerName = strings.TrimPrefix(customerName, "@")
		}
		return customerName, platform, strings.TrimSpace(matches[3]), isUser, true
	}

	// Pattern 4: "[BotName], [action] [name/@username] on [platform] [message]"
	// Example: "Fred, please greet @zoovie_boy on instagram"
	pattern4 := regexp.MustCompile(`(?i)^(?:please\s+)?(?:greet|message|tell|contact)\s+(.+?)\s+on\s+(facebook|instagram|whatsapp|messenger)(?:\s+(?:saying|that|message|msg|with)\s+(.+))?$`)
	matches = pattern4.FindStringSubmatch(text)
	if len(matches) >= 3 {
		customerName := strings.TrimSpace(matches[1])
		platform := strings.ToLower(strings.TrimSpace(matches[2]))
		if platform == "messenger" {
			platform = "facebook"
		}

		isUser := strings.HasPrefix(customerName, "@")
		if isUser {
			customerName = strings.TrimPrefix(customerName, "@")
		}

		// If message not provided, use default greeting
		message := ""
		if len(matches) >= 4 && strings.TrimSpace(matches[3]) != "" {
			message = strings.TrimSpace(matches[3])
		} else {
			// Default greeting message
			message = "Hello! How can I help you today?"
		}

		return customerName, platform, message, isUser, true
	}

	return "", "", "", false, false
}

// handleCustomerMessageInstruction processes tenant instructions to send messages to customers
func (h *TenantWebhookHandler) handleCustomerMessageInstruction(
	ctx context.Context,
	bot *tgbotapi.BotAPI,
	tenantID uint,
	chatID int64,
	text string,
) bool {
	customerName, platform, messageContent, isUsername, valid := parseCustomerMessageInstruction(text)
	if !valid {
		return false // Not a customer message instruction
	}

	// Send processing message
	processingMsg, _ := bot.Send(tgbotapi.NewMessage(chatID, "Got it—give me a sec to handle that..."))

	go func() {
		var customer *storage.Customer
		var err error

		// Find or create customer
		if isUsername {
			// Searching by username/handle
			customer, err = h.findOrCreateCustomerByUsername(ctx, tenantID, platform, customerName, bot, chatID, processingMsg.MessageID)
		} else {
			// Searching by name
			customer, err = h.findCustomerByName(tenantID, platform, customerName, bot, chatID, processingMsg.MessageID)
		}

		if err != nil || customer == nil {
			// Error already sent to user in helper functions
			return
		}

		// Get or create conversation
		var conversation storage.Conversation
		threadID := customer.PlatformID // Default fallback (PSID)

		if err := h.Manager.DB.Where("tenant_id = ? AND customer_id = ? AND platform = ?",
			tenantID, customer.ID, platform).Order("last_message_at DESC").First(&conversation).Error; err == nil {
			if conversation.PlatformThreadID != "" {
				threadID = conversation.PlatformThreadID
			}
		} else {
			// Create new conversation for this customer
			conversation = storage.Conversation{
				TenantID:         tenantID,
				CustomerID:       customer.ID,
				Platform:         platform,
				PlatformThreadID: threadID,
				Status:           "active",
				LastMessageAt:    time.Now(),
			}
			if err := h.Manager.DB.Create(&conversation).Error; err != nil {
				h.Manager.Logger.Warn("failed to create conversation", zap.Error(err))
			}
		}

		// Send message using MessageHandler
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
			if isUsername {
				displayName = "@" + displayName
			}

			successMsg := fmt.Sprintf("✅ Done! Messaged %s on %s:\n\n\"%s\"", displayName, strings.Title(platform), messageContent)
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

// findCustomerByName finds an existing customer by name
func (h *TenantWebhookHandler) findCustomerByName(tenantID uint, platform, customerName string, bot *tgbotapi.BotAPI, chatID int64, processingMsgID int) (*storage.Customer, error) {
	var customers []storage.Customer
	searchPattern := "%" + customerName + "%"

	// Try exact match on DisplayName or Username first
	var exactCustomer storage.Customer
	if err := h.Manager.DB.Where("tenant_id = ? AND channel = ? AND (LOWER(display_name) = LOWER(?) OR LOWER(username) = LOWER(?))",
		tenantID, platform, customerName, customerName).First(&exactCustomer).Error; err == nil {
		return &exactCustomer, nil
	}

	// Try partial match
	query := h.Manager.DB.Where("tenant_id = ? AND channel = ? AND (LOWER(display_name) LIKE ? OR LOWER(username) LIKE ?)",
		tenantID, platform, searchPattern, searchPattern)

	if err := query.Limit(10).Find(&customers).Error; err != nil || len(customers) == 0 {
		if processingMsgID > 0 {
			bot.Request(tgbotapi.NewDeleteMessage(chatID, processingMsgID))
		}
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Couldn't find customer '%s' on %s.\n\nThey need to message you first before you can send them a message.", customerName, strings.Title(platform))))
		return nil, fmt.Errorf("customer not found")
	}

	// If multiple matches, use the most recent one
	if len(customers) > 1 {
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

	return &customers[0], nil
}

// findOrCreateCustomerByUsername finds or creates a customer by username/handle
// Note: For Instagram/Facebook, you can only message users who have messaged you first (have their PSID)
func (h *TenantWebhookHandler) findOrCreateCustomerByUsername(ctx context.Context, tenantID uint, platform, username string, bot *tgbotapi.BotAPI, chatID int64, processingMsgID int) (*storage.Customer, error) {
	// Clean username (remove @ if present)
	username = strings.TrimPrefix(strings.ToLower(username), "@")

	// Try to find existing customer by username (exact match)
	var customer storage.Customer
	if err := h.Manager.DB.Where("tenant_id = ? AND channel = ? AND LOWER(username) = ?",
		tenantID, platform, username).First(&customer).Error; err == nil {
		// Found existing customer - they've messaged before, we have their PSID
		return &customer, nil
	}

	// Try partial match on username
	var customers []storage.Customer
	if err := h.Manager.DB.Where("tenant_id = ? AND channel = ? AND LOWER(username) LIKE ?",
		tenantID, platform, "%"+username+"%").Limit(5).Find(&customers).Error; err == nil && len(customers) > 0 {
		// Found similar usernames
		if len(customers) == 1 {
			return &customers[0], nil
		}
		// Multiple matches - show them
		if processingMsgID > 0 {
			bot.Request(tgbotapi.NewDeleteMessage(chatID, processingMsgID))
		}
		msg := fmt.Sprintf("Found multiple users matching @%s:\n\n", username)
		for i, c := range customers {
			displayName := c.Username
			if c.DisplayName != "" {
				displayName = c.DisplayName + " (@" + c.Username + ")"
			}
			msg += fmt.Sprintf("%d. %s\n", i+1, displayName)
		}
		msg += "\nPlease use their exact username or full name."
		bot.Send(tgbotapi.NewMessage(chatID, msg))
		return nil, fmt.Errorf("multiple matches found")
	}

	// Customer not found - explain limitation
	if processingMsgID > 0 {
		bot.Request(tgbotapi.NewDeleteMessage(chatID, processingMsgID))
	}

	msg := fmt.Sprintf("❌ Couldn't find user @%s on %s.\n\n", username, strings.Title(platform))
	msg += "📝 *How it works:*\n"
	msg += "To message someone on Instagram or Facebook, they need to message you first. Once they message you:\n\n"
	msg += "✅ I'll save their username and contact info\n"
	msg += "✅ You can then message them anytime by name or username\n"
	msg += "✅ I'll automatically continue the conversation\n\n"
	msg += "💡 *Tip:* Check your existing conversations to see who you can message."

	bot.Send(tgbotapi.NewMessage(chatID, msg))
	return nil, fmt.Errorf("user not found - requires prior message")
}
