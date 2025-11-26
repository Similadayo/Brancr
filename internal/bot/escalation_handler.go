package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/security"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ReplySender interface for sending replies to customers
type ReplySender interface {
	SendReply(ctx context.Context, tenantID uint, platform, threadID, customerID, reply string) error
}

// EscalationHandler manages escalated messages from the MessageHandler
type EscalationHandler struct {
	DB             *gorm.DB
	Logger         *zap.Logger
	Manager        *TenantBotManager
	MessageHandler ReplySender // Set after initialization via SetMessageHandler
	PaymentSecurity *security.PaymentSecurityHandler // Security handler for payment escalations
}

// SetMessageHandler sets the MessageHandler for sending replies
func (h *EscalationHandler) SetMessageHandler(handler ReplySender) {
	h.MessageHandler = handler
}

// PendingEscalation tracks escalations waiting for tenant approval
type PendingEscalation struct {
	InteractionID uint
	TenantID      uint
	CustomerID    uint
	Platform      string
	Message       string
	SuggestedReply string
	ChatID        int64 // Telegram chat ID where escalation was sent
}

var pendingEscalations = make(map[uint]*PendingEscalation) // interactionID → escalation

// SendEscalation sends an escalated message to tenant's Telegram control center
func (h *EscalationHandler) SendEscalation(ctx context.Context, tenantID uint, interactionID uint, customerName, platform, message, suggestedReply string, intent string, tone string, confidence float64) error {
	// Load tenant
	var tenant storage.Tenant
	if err := h.DB.First(&tenant, tenantID).Error; err != nil {
		return fmt.Errorf("tenant not found: %w", err)
	}

	// Get tenant's bot - try to initialize if not found
	bot, ok := h.Manager.GetBot(tenantID)
	if !ok {
		// Try to initialize the bot if it doesn't exist
		if tenant.TelegramBotToken != "" {
			h.Logger.Info("🤖 Bot not found for tenant, attempting to initialize",
				zap.Uint("tenant_id", tenantID),
				zap.String("tenant_name", tenant.Name))
			
			if err := h.Manager.startTenantBot(&tenant); err != nil {
				h.Logger.Error("❌ Failed to initialize bot for tenant",
					zap.Uint("tenant_id", tenantID),
					zap.Error(err))
				return fmt.Errorf("bot not found for tenant %d and failed to initialize: %w. Please ensure the tenant has a valid Telegram bot token set", tenantID, err)
			}
			
			// Try to get the bot again
			bot, ok = h.Manager.GetBot(tenantID)
			if !ok {
				return fmt.Errorf("bot not found for tenant %d after initialization attempt. Please ensure the tenant has a valid Telegram bot token and is active", tenantID)
			}
		} else {
			h.Logger.Warn("⚠️ Bot not found for tenant - no Telegram bot token configured",
				zap.Uint("tenant_id", tenantID),
				zap.String("tenant_name", tenant.Name),
				zap.String("tenant_status", tenant.Status))
			return fmt.Errorf("bot not found for tenant %d: tenant has no Telegram bot token configured. Please set up a Telegram bot for this tenant", tenantID)
		}
	}

	// Load interaction to get customer ID
	var interaction storage.Interaction
	if err := h.DB.First(&interaction, interactionID).Error; err != nil {
		return fmt.Errorf("interaction not found: %w", err)
	}
	
	// Load customer to get proper name (with fallback)
	var customer storage.Customer
	if err := h.DB.First(&customer, interaction.CustomerID).Error; err == nil {
		if customerName == "" {
			customerName = customer.DisplayName
		}
		if customerName == "" {
			customerName = customer.Username
		}
		// Try to extract name from message content if still empty
		if customerName == "" {
			customerName = extractCustomerNameFromMessage(message)
		}
		// Last resort: use a shortened platform ID
		if customerName == "" {
			shortID := customer.PlatformID
			if len(shortID) > 15 {
				shortID = shortID[:12] + "..."
			}
			customerName = fmt.Sprintf("Customer (%s)", shortID)
		}
	}

	// Format escalation alert title based on intent
	alertTitle := strings.Title(intent)
	if intent == "payment" {
		alertTitle = "💰 Payment Alert"
	} else if intent == "refund" {
		alertTitle = "💸 Refund Alert"
	} else if intent == "complaint" {
		alertTitle = "⚠️ Complaint Alert"
	} else {
		alertTitle = "🚨 " + alertTitle + " Alert"
	}
	
	// 🔒 SECURITY: For payment escalations, encrypt message content and add security measures
	isPaymentIntent := intent == "payment" || intent == "refund"
	if isPaymentIntent && h.PaymentSecurity != nil {
		// Validate payment intent
		if !h.PaymentSecurity.ValidatePaymentIntent(message) {
			h.Logger.Warn("⚠️ Payment intent flagged but validation failed",
				zap.Uint("interaction_id", interactionID),
				zap.String("message_preview", h.PaymentSecurity.MaskForLogging(message)[:min(50, len(message))]))
		}
		
		// Encrypt payment message content for secure storage
		encryptedMessage, err := h.PaymentSecurity.EncryptPaymentMessage(message)
		if err != nil {
			h.Logger.Error("🔒 Failed to encrypt payment message",
				zap.Uint("interaction_id", interactionID),
				zap.Error(err))
		} else {
			// Store encrypted version in interaction
			interaction.EncryptedContent = encryptedMessage
			interaction.IsEncrypted = true
			interaction.SecurityLevel = "payment"
			h.DB.Save(&interaction)
		}
		
		// Log masked version for security audit
		maskedMessage := h.PaymentSecurity.MaskForLogging(message)
		h.Logger.Info("🔒 Payment escalation created (message encrypted)",
			zap.Uint("tenant_id", tenantID),
			zap.Uint("interaction_id", interactionID),
			zap.String("masked_message", maskedMessage))
	}
	
	// Format escalation message (use masked version for payment in Telegram)
	displayMessage := message
	if isPaymentIntent && h.PaymentSecurity != nil {
		// Show partially masked version in Telegram for security
		displayMessage = h.PaymentSecurity.MaskForLogging(message)
	}
	
	escalationMsg := fmt.Sprintf(
		"%s\n\n"+
			"👤 Customer: %s\n"+
			"📱 Platform: %s\n"+
			"💬 Message: %s\n\n"+
			"🎯 Intent: %s (%.0f%% confidence)\n"+
			"😊 Tone: %s\n\n"+
			"💡 Suggested reply:\n%s",
		alertTitle,
		customerName,
		strings.Title(platform),
		displayMessage,
		intent,
		confidence*100,
		tone,
		suggestedReply,
	)
	
	// Add special security warning for financial matters
	if isPaymentIntent {
		escalationMsg += "\n\n🔒 *SECURITY ALERT - Financial Transaction*\n"
		escalationMsg += "⚠️ *VERIFY ALL DETAILS BEFORE PROCESSING*\n"
		escalationMsg += "📝 Full message encrypted and stored securely.\n"
		escalationMsg += "🔐 Access logged for audit compliance."
	}

	// Create keyboard with quick action buttons
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Approve & Send", fmt.Sprintf("escalate:approve:%d", interactionID)),
			tgbotapi.NewInlineKeyboardButtonData("✏️ Edit Reply", fmt.Sprintf("escalate:edit:%d", interactionID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Mark as Fixed", fmt.Sprintf("escalate:fixed:%d", interactionID)),
			tgbotapi.NewInlineKeyboardButtonData("❌ Ignore", fmt.Sprintf("escalate:ignore:%d", interactionID)),
		),
	)

	msg := tgbotapi.NewMessage(tenant.OwnerTelegramID, escalationMsg)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	
	if _, err := bot.Send(msg); err != nil {
		return fmt.Errorf("failed to send escalation: %w", err)
	}
	
	// 🔒 SECURITY: Log payment escalation access for audit
	if isPaymentIntent && h.PaymentSecurity != nil {
		h.PaymentSecurity.LogPaymentAccess(context.Background(), tenantID, interactionID, "viewed", tenant.OwnerTelegramID, true, "")
	}

	// Store pending escalation
	pendingEscalations[interactionID] = &PendingEscalation{
		InteractionID:  interactionID,
		TenantID:       tenantID,
		CustomerID:     interaction.CustomerID,
		Platform:       platform,
		Message:        message,
		SuggestedReply: suggestedReply,
		ChatID:         tenant.OwnerTelegramID,
	}

	h.Logger.Info("📤 Escalation sent to tenant",
		zap.Uint("tenant_id", tenantID),
		zap.Uint("interaction_id", interactionID),
		zap.String("intent", intent),
	)

	return nil
}

// HandleEscalationResponse processes tenant's response to escalation
func (h *EscalationHandler) HandleEscalationResponse(ctx context.Context, tenantID uint, chatID int64, message string) error {
	// Check if this is an approval
	lower := strings.ToLower(strings.TrimSpace(message))
	
	// Parse "approve <interaction_id>" or just "approve" for most recent
	if strings.HasPrefix(lower, "approve") {
		var interactionID uint
		parts := strings.Fields(lower)
		if len(parts) > 1 {
			id, err := strconv.ParseUint(parts[1], 10, 32)
			if err == nil {
				interactionID = uint(id)
			}
		}
		
		// If no ID specified, find most recent pending escalation
		if interactionID == 0 {
			for id, esc := range pendingEscalations {
				if esc.TenantID == tenantID && esc.ChatID == chatID {
					interactionID = id
					break
				}
			}
		}
		
		if interactionID == 0 {
			// Try to find most recent escalation from database
			var interaction storage.Interaction
			if err := h.DB.Where("tenant_id = ? AND response_type = ? AND response_status = ?", tenantID, "escalated", "pending").
				Order("created_at DESC").First(&interaction).Error; err != nil {
				return fmt.Errorf("no pending escalation found")
			}
			interactionID = interaction.ID
			h.Logger.Info("📋 Found escalation from database", zap.Uint("interaction_id", interactionID))
		}
		
		esc, ok := pendingEscalations[interactionID]
		if !ok {
			// Escalation not in memory - rebuild from database
			h.Logger.Info("📋 Escalation not in memory, loading from database", zap.Uint("interaction_id", interactionID))
			var err error
			esc, err = h.loadEscalationFromDB(ctx, interactionID, tenantID, chatID)
			if err != nil {
				h.Logger.Error("failed to load escalation from database", zap.Error(err), zap.Uint("interaction_id", interactionID))
				return fmt.Errorf("escalation not found: %w", err)
			}
			// Store in memory for future use
			pendingEscalations[interactionID] = esc
		}
		
		// Send approved reply
		return h.sendApprovedReply(ctx, esc, esc.SuggestedReply)
	}
	
	// Check if message starts with interaction ID (for custom replies)
	// Format: "<interaction_id> <custom message>"
	parts := strings.Fields(message)
	if len(parts) >= 2 {
		if id, err := strconv.ParseUint(parts[0], 10, 32); err == nil {
			interactionID := uint(id)
			esc, ok := pendingEscalations[interactionID]
			if ok && esc.TenantID == tenantID {
				customReply := strings.Join(parts[1:], " ")
				return h.sendApprovedReply(ctx, esc, customReply)
			}
		}
	}
	
	// Otherwise, treat as custom reply to most recent escalation
	for _, esc := range pendingEscalations {
		if esc.TenantID == tenantID && esc.ChatID == chatID {
			return h.sendApprovedReply(ctx, esc, message)
		}
	}
	
	return fmt.Errorf("no matching escalation found")
}

// sendApprovedReply sends the approved reply to the customer
func (h *EscalationHandler) sendApprovedReply(ctx context.Context, esc *PendingEscalation, reply string) error {
	// Load interaction
	var interaction storage.Interaction
	if err := h.DB.First(&interaction, esc.InteractionID).Error; err != nil {
		return fmt.Errorf("interaction not found: %w", err)
	}
	
	// Load customer
	var customer storage.Customer
	if err := h.DB.First(&customer, esc.CustomerID).Error; err != nil {
		return fmt.Errorf("customer not found: %w", err)
	}
	
	// Load conversation to get thread ID
	var conversation storage.Conversation
	threadID := customer.PlatformID // Default to customer ID
	if interaction.ConversationID > 0 {
		if err := h.DB.First(&conversation, interaction.ConversationID).Error; err == nil {
			threadID = conversation.PlatformThreadID
		}
	}
	if threadID == "" {
		threadID = customer.PlatformID
	}
	
	// Get social account for the platform
	var acc storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ? AND connected = ?", 
		esc.TenantID, esc.Platform, true).First(&acc).Error; err != nil {
		h.Logger.Error("failed to find connected account for reply",
			zap.Uint("tenant_id", esc.TenantID),
			zap.String("platform", esc.Platform),
			zap.Error(err))
		return fmt.Errorf("no connected %s account found", esc.Platform)
	}
	
	// Use MessageHandler to send reply if available
	var sendErr error
	if h.MessageHandler != nil {
		sendErr = h.MessageHandler.SendReply(ctx, esc.TenantID, esc.Platform, threadID, customer.PlatformID, reply)
	} else {
		// Fallback: return error asking user to reconnect
		sendErr = fmt.Errorf("message handler not configured - please contact support")
		h.Logger.Error("message handler not set in escalation handler",
			zap.Uint("tenant_id", esc.TenantID),
			zap.String("platform", esc.Platform))
	}
	
	if sendErr != nil {
		h.Logger.Error("failed to send reply to customer",
			zap.Error(sendErr),
			zap.String("platform", esc.Platform),
			zap.Uint("customer_id", customer.ID))
		return fmt.Errorf("failed to send reply: %w", sendErr)
	}
	
	// Update interaction
	now := time.Now()
	interaction.ResponseType = "manual"
	interaction.ResponseStatus = "sent"
	interaction.FinalReply = reply
	interaction.EscalatedTo = esc.ChatID
	interaction.EscalatedAt = &now
	h.DB.Save(&interaction)
	
	// Remove from pending
	delete(pendingEscalations, esc.InteractionID)
	
	// Notify tenant with customer name fallback
	bot, _ := h.Manager.GetBot(esc.TenantID)
	if bot != nil {
		customerName := customer.DisplayName
		if customerName == "" {
			customerName = customer.Username
		}
		if customerName == "" {
			customerName = fmt.Sprintf("Customer (%s)", customer.PlatformID)
		}
		
		confirmation := fmt.Sprintf("✅ Reply sent to *%s* on %s:\n\n%s", customerName, strings.Title(esc.Platform), reply)
		
		// Add a note that we'll notify if customer responds
		followUpNote := fmt.Sprintf("\n\n💬 If %s responds, you'll be notified here.", customerName)
		confirmation += followUpNote
		
		msg := tgbotapi.NewMessage(esc.ChatID, confirmation)
		msg.ParseMode = "Markdown"
		bot.Send(msg)
	}
	
	h.Logger.Info("✅ Approved reply sent",
		zap.Uint("interaction_id", esc.InteractionID),
		zap.Uint("tenant_id", esc.TenantID),
		zap.String("platform", esc.Platform),
	)
	
	return nil
}

// loadEscalationFromDB rebuilds a PendingEscalation from the database when it's not in memory
func (h *EscalationHandler) loadEscalationFromDB(ctx context.Context, interactionID, tenantID uint, chatID int64) (*PendingEscalation, error) {
	// Load interaction
	var interaction storage.Interaction
	if err := h.DB.First(&interaction, interactionID).Error; err != nil {
		return nil, fmt.Errorf("interaction not found: %w", err)
	}
	
	// Verify it belongs to this tenant and is escalated
	if interaction.TenantID != tenantID {
		return nil, fmt.Errorf("interaction does not belong to tenant")
	}
	
	if interaction.ResponseType != "escalated" {
		return nil, fmt.Errorf("interaction is not escalated (type: %s)", interaction.ResponseType)
	}
	
	// Load customer
	var customer storage.Customer
	if err := h.DB.First(&customer, interaction.CustomerID).Error; err != nil {
		return nil, fmt.Errorf("customer not found: %w", err)
	}
	
	// Build escalation object
	esc := &PendingEscalation{
		InteractionID:  interaction.ID,
		TenantID:       tenantID,
		CustomerID:     customer.ID,
		Platform:       interaction.Platform,
		Message:        interaction.Content,
		SuggestedReply: interaction.SuggestedReply,
		ChatID:         chatID,
	}
	
	h.Logger.Info("✅ Loaded escalation from database",
		zap.Uint("interaction_id", interactionID),
		zap.String("platform", interaction.Platform))
	
	return esc, nil
}

// extractCustomerNameFromMessage tries to extract a name from message content
func extractCustomerNameFromMessage(message string) string {
	lower := strings.ToLower(message)
	
	patterns := []string{
		"account name is",
		"account name:",
		"name is",
		"name:",
		"i am",
		"i'm",
		"this is",
	}
	
	for _, prefix := range patterns {
		if idx := strings.Index(lower, prefix); idx >= 0 {
			start := idx + len(prefix)
			remaining := strings.TrimSpace(message[start:])
			
			nameEnd := len(remaining)
			for i, char := range remaining {
				if i >= 50 || char == ',' || char == '.' || char == '\n' {
					nameEnd = i
					break
				}
			}
			
			name := strings.TrimSpace(remaining[:nameEnd])
			if len(name) > 2 && len(name) < 50 {
				return name
			}
		}
	}
	
	return ""
}

// NotifyAftermath sends a notification to tenant when customer responds after escalated conversation
func (h *EscalationHandler) NotifyAftermath(ctx context.Context, tenantID uint, customerID uint, platform, customerMessage, originalMessage, tenantReply string) error {
	// Load tenant
	var tenant storage.Tenant
	if err := h.DB.First(&tenant, tenantID).Error; err != nil {
		return fmt.Errorf("tenant not found: %w", err)
	}
	
	// Load customer
	var customer storage.Customer
	if err := h.DB.First(&customer, customerID).Error; err != nil {
		return fmt.Errorf("customer not found: %w", err)
	}
	
	// Get tenant's bot
	bot, ok := h.Manager.GetBot(tenantID)
	if !ok {
		return fmt.Errorf("bot not found for tenant %d", tenantID)
	}
	
	customerName := customer.DisplayName
	if customerName == "" {
		customerName = customer.Username
	}
	if customerName == "" {
		customerName = fmt.Sprintf("Customer (%s)", customer.PlatformID)
	}
	
	aftermathMsg := fmt.Sprintf(
		"💬 *Customer Follow-up*\n\n"+
			"👤 Customer: %s\n"+
			"📱 Platform: %s\n"+
			"💬 New message: %s\n\n"+
			"📝 Previous context:\n"+
			"Original: %s\n"+
			"Your reply: %s",
		customerName,
		strings.Title(platform),
		customerMessage,
		originalMessage,
		tenantReply,
	)
	
	msg := tgbotapi.NewMessage(tenant.OwnerTelegramID, aftermathMsg)
	msg.ParseMode = "Markdown"
	
	if _, err := bot.Send(msg); err != nil {
		return fmt.Errorf("failed to send aftermath notification: %w", err)
	}
	
	h.Logger.Info("📬 Sent aftermath notification",
		zap.Uint("tenant_id", tenantID),
		zap.Uint("customer_id", customerID))
	
	return nil
}

