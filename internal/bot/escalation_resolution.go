package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// EscalationResolutionSession tracks when tenant is marking an escalation as fixed
type EscalationResolutionSession struct {
	InteractionID uint
	TenantID      uint
	CustomerID    uint
	Platform      string
	StartedAt     time.Time
}

var escalationResolutionSessions = make(map[int64]*EscalationResolutionSession) // chatID → session

// HandleMarkAsFixed processes the "Mark as Fixed" action and sends the prompt message
func (h *EscalationHandler) HandleMarkAsFixed(ctx context.Context, tenantID uint, interactionID uint, chatID int64, bot *tgbotapi.BotAPI) error {
	// Load interaction
	var interaction storage.Interaction
	if err := h.DB.First(&interaction, interactionID).Error; err != nil {
		return fmt.Errorf("interaction not found: %w", err)
	}

	// Verify it belongs to this tenant
	if interaction.TenantID != tenantID {
		return fmt.Errorf("interaction does not belong to tenant")
	}

	// Check if already resolved
	if interaction.ResponseStatus == "resolved" {
		if bot != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "✅ This escalation is already marked as resolved."))
		}
		return nil
	}

	// Check if session already exists (prevent duplicate prompts)
	if existingSession, exists := escalationResolutionSessions[chatID]; exists {
		// If it's the same interaction, session already exists - don't send prompt again
		if existingSession.InteractionID == interactionID {
			return nil // Session already active, no need to send prompt again
		}
		// Different interaction - replace session and send new prompt
	}

	// Store session for resolution message
	session := &EscalationResolutionSession{
		InteractionID: interactionID,
		TenantID:      tenantID,
		CustomerID:    interaction.CustomerID,
		Platform:      interaction.Platform,
		StartedAt:     time.Now(),
	}
	escalationResolutionSessions[chatID] = session

	// Load customer for context
	var customer storage.Customer
	customerName := "the customer"
	if err := h.DB.First(&customer, interaction.CustomerID).Error; err == nil {
		customerName = customer.DisplayName
		if customerName == "" {
			customerName = customer.Username
		}
		if customerName == "" {
			customerName = fmt.Sprintf("Customer (%s)", customer.PlatformID)
		}
	}

	// Send prompt message (only once, when session is created)
	// Get bot from Manager if not provided
	if bot == nil && h.Manager != nil {
		var ok bool
		bot, ok = h.Manager.GetBot(tenantID)
		if !ok {
			bot = nil
		}
	}

	if bot != nil {
		promptMsg := "✅ Great! Type a message to inform the customer that the issue is resolved.\n\nExample: 'Issue resolved! Your account has been activated.'\n\nOr type 'skip' to mark as fixed without sending a message."
		bot.Send(tgbotapi.NewMessage(chatID, promptMsg))
	}

	return nil
}

// HandleResolutionMessage processes the resolution message from tenant and sends it to customer
func (h *EscalationHandler) HandleResolutionMessage(ctx context.Context, tenantID uint, chatID int64, resolutionMessage string) error {
	// Get session
	session, ok := escalationResolutionSessions[chatID]
	if !ok {
		return fmt.Errorf("no active resolution session found")
	}

	// Verify session belongs to tenant
	if session.TenantID != tenantID {
		return fmt.Errorf("session does not belong to tenant")
	}

	// Load interaction
	var interaction storage.Interaction
	if err := h.DB.First(&interaction, session.InteractionID).Error; err != nil {
		delete(escalationResolutionSessions, chatID)
		return fmt.Errorf("interaction not found: %w", err)
	}

	// Load customer
	var customer storage.Customer
	if err := h.DB.First(&customer, session.CustomerID).Error; err != nil {
		delete(escalationResolutionSessions, chatID)
		return fmt.Errorf("customer not found: %w", err)
	}

	// Skip sending message if tenant said "skip"
	resolutionMessage = strings.TrimSpace(resolutionMessage)
	sendMessage := !strings.EqualFold(resolutionMessage, "skip") && resolutionMessage != ""

	// Send message to customer if provided
	if sendMessage && h.MessageHandler != nil {
		// Get conversation thread ID
		var conversation storage.Conversation
		threadID := customer.PlatformID // Default
		if interaction.ConversationID > 0 {
			if err := h.DB.First(&conversation, interaction.ConversationID).Error; err == nil {
				threadID = conversation.PlatformThreadID
			}
		}
		if threadID == "" {
			threadID = customer.PlatformID
		}

		// Send resolution message to customer
		if err := h.MessageHandler.SendReply(ctx, tenantID, session.Platform, threadID, customer.PlatformID, resolutionMessage); err != nil {
			h.Logger.Error("failed to send resolution message to customer",
				zap.Error(err),
				zap.Uint("interaction_id", session.InteractionID))
			// Continue anyway - still mark as resolved
		} else {
			// Log the resolution message as an outgoing interaction
			resolutionInteraction := storage.Interaction{
				TenantID:       tenantID,
				CustomerID:     customer.ID,
				ConversationID: interaction.ConversationID,
				Platform:       session.Platform,
				Direction:      "outgoing",
				MessageType:    "text",
				Content:        resolutionMessage,
				ResponseType:   "manual",
				ResponseStatus: "sent",
				FinalReply:     resolutionMessage,
				DetectedIntent: "resolution",
				DetectedTone:   "positive",
				Confidence:     1.0,
			}
			h.DB.Create(&resolutionInteraction)
		}
	}

	// Mark interaction as resolved
	now := time.Now()
	interaction.ResponseStatus = "resolved"
	if interaction.EscalatedAt == nil {
		interaction.EscalatedAt = &now
	}
	h.DB.Save(&interaction)

	// Mark conversation as resolved if it exists
	if interaction.ConversationID > 0 {
		var conversation storage.Conversation
		if err := h.DB.First(&conversation, interaction.ConversationID).Error; err == nil {
			conversation.Status = "resolved"
			h.DB.Save(&conversation)
		}
	}

	// Send payment receipt if this was a payment escalation
	if interaction.DetectedIntent == "payment" {
		if err := h.sendPaymentReceiptToCustomer(ctx, &interaction); err != nil {
			h.Logger.Error("failed to send payment receipt",
				zap.Error(err),
				zap.Uint("interaction_id", interaction.ID))
			// Don't fail - just log the error
		}
	}

	// Get tenant's bot for confirmation
	bot, _ := h.Manager.GetBot(tenantID)
	if bot != nil {
		customerName := customer.DisplayName
		if customerName == "" {
			customerName = customer.Username
		}
		if customerName == "" {
			customerName = fmt.Sprintf("Customer (%s)", customer.PlatformID)
		}

		confirmation := fmt.Sprintf("✅ Escalation marked as resolved!")
		if sendMessage {
			confirmation += fmt.Sprintf("\n\n📤 Resolution message sent to *%s* on %s:\n%s", customerName, strings.Title(session.Platform), resolutionMessage)
		} else {
			confirmation += fmt.Sprintf("\n\nNo message sent to customer (skipped).")
		}

		msg := tgbotapi.NewMessage(chatID, confirmation)
		msg.ParseMode = "Markdown"
		bot.Send(msg)
	}

	// Remove from pending escalations
	delete(pendingEscalations, session.InteractionID)
	delete(escalationResolutionSessions, chatID)

	h.Logger.Info("✅ Escalation resolved",
		zap.Uint("interaction_id", session.InteractionID),
		zap.Uint("tenant_id", tenantID),
		zap.Bool("message_sent", sendMessage))

	return nil
}

// GetUnresolvedEscalations returns all unresolved escalations for a tenant
func (h *EscalationHandler) GetUnresolvedEscalations(tenantID uint) ([]storage.Interaction, error) {
	var interactions []storage.Interaction

	// Find all escalated interactions that are not resolved
	// ResponseType = "escalated" or "manual" and ResponseStatus != "resolved"
	err := h.DB.Where("tenant_id = ? AND ((response_type = ? OR response_type = ?) AND response_status != ? AND response_status != ?)",
		tenantID, "escalated", "manual", "resolved", "rejected").
		Order("created_at ASC").Find(&interactions).Error

	return interactions, err
}

// SendDailyReminders sends daily reminders for unresolved escalations
func (h *EscalationHandler) SendDailyReminders(ctx context.Context) error {
	// Get all tenants
	var tenants []storage.Tenant
	if err := h.DB.Find(&tenants).Error; err != nil {
		return fmt.Errorf("failed to load tenants: %w", err)
	}

	for _, tenant := range tenants {
		if tenant.OwnerTelegramID == 0 || tenant.TelegramBotToken == "" {
			continue // Skip tenants without Telegram setup
		}

		// Get unresolved escalations
		escalations, err := h.GetUnresolvedEscalations(tenant.ID)
		if err != nil {
			h.Logger.Error("failed to get unresolved escalations",
				zap.Uint("tenant_id", tenant.ID),
				zap.Error(err))
			continue
		}

		if len(escalations) == 0 {
			continue // No unresolved escalations
		}

		// Get tenant's bot
		bot, ok := h.Manager.GetBot(tenant.ID)
		if !ok {
			continue // Bot not initialized
		}

		// Format reminder message
		reminderMsg := fmt.Sprintf("📋 *Daily Escalation Reminder*\n\n")
		reminderMsg += fmt.Sprintf("You have *%d* unresolved escalation(s):\n\n", len(escalations))

		// Show up to 5 oldest escalations
		maxShow := 5
		if len(escalations) < maxShow {
			maxShow = len(escalations)
		}

		for i := 0; i < maxShow; i++ {
			esc := escalations[i]

			// Load customer
			var customer storage.Customer
			customerName := "Customer"
			if err := h.DB.First(&customer, esc.CustomerID).Error; err == nil {
				customerName = customer.DisplayName
				if customerName == "" {
					customerName = customer.Username
				}
			}

			// Calculate age
			age := time.Since(esc.CreatedAt)
			ageStr := "just now"
			if age.Hours() >= 24 {
				ageStr = fmt.Sprintf("%.0f days ago", age.Hours()/24)
			} else if age.Hours() >= 1 {
				ageStr = fmt.Sprintf("%.0f hours ago", age.Hours())
			} else {
				ageStr = fmt.Sprintf("%.0f minutes ago", age.Minutes())
			}

			reminderMsg += fmt.Sprintf("• *%s* on %s (%s)\n", customerName, strings.Title(esc.Platform), ageStr)
			contentPreview := esc.Content
			if len(contentPreview) > 60 {
				contentPreview = contentPreview[:57] + "..."
			}
			reminderMsg += fmt.Sprintf("  \"%s\"\n", contentPreview)
			reminderMsg += fmt.Sprintf("  ID: %d\n\n", esc.ID)
		}

		if len(escalations) > maxShow {
			reminderMsg += fmt.Sprintf("... and %d more. Use /escalations to see all.\n\n", len(escalations)-maxShow)
		}

		reminderMsg += "💡 Tip: Use the escalation buttons or type 'approve <id>' to respond."

		msg := tgbotapi.NewMessage(tenant.OwnerTelegramID, reminderMsg)
		msg.ParseMode = "Markdown"
		if _, err := bot.Send(msg); err != nil {
			h.Logger.Error("failed to send daily reminder",
				zap.Uint("tenant_id", tenant.ID),
				zap.Error(err))
		} else {
			h.Logger.Info("✅ Sent daily reminder",
				zap.Uint("tenant_id", tenant.ID),
				zap.Int("escalation_count", len(escalations)))
		}
	}

	return nil
}

// sendPaymentReceiptToCustomer sends payment receipt via WhatsApp when escalation is resolved
func (h *EscalationHandler) sendPaymentReceiptToCustomer(ctx context.Context, interaction *storage.Interaction) error {
	// Find the payment record
	var payment storage.Payment
	err := h.DB.Where("tenant_id = ? AND customer_id = ? AND status IN ?",
		interaction.TenantID, interaction.CustomerID, []string{"verified", "confirmed"}).
		Order("created_at DESC").
		First(&payment).Error

	if err != nil {
		h.Logger.Warn("no payment record found for receipt",
			zap.Uint("tenant_id", interaction.TenantID),
			zap.Uint("customer_id", interaction.CustomerID))
		return nil // Not an error, just no payment to receipt
	}

	// Generate receipt message
	receiptMessage := fmt.Sprintf(`✅ *PAYMENT CONFIRMED*

📝 Receipt #: %s
💰 Amount: %s %.2f
✓ Status: Payment Verified

Thank you for your payment! Your transaction has been confirmed and processed successfully.

If you have any questions, feel free to reach out.`,
		payment.PaymentReference,
		payment.Currency,
		payment.Amount)

	// Load customer for platform ID
	var customer storage.Customer
	if err := h.DB.First(&customer, interaction.CustomerID).Error; err != nil {
		return fmt.Errorf("customer not found: %w", err)
	}

	// Send receipt via MessageHandler
	if h.MessageHandler != nil && interaction.Platform == "whatsapp" {
		// Get conversation thread ID
		var conversation storage.Conversation
		threadID := customer.PlatformID // Default
		if interaction.ConversationID > 0 {
			if err := h.DB.First(&conversation, interaction.ConversationID).Error; err == nil {
				threadID = conversation.PlatformThreadID
			}
		}
		if threadID == "" {
			threadID = customer.PlatformID
		}

		if err := h.MessageHandler.SendReply(ctx, interaction.TenantID, interaction.Platform, threadID, customer.PlatformID, receiptMessage); err != nil {
			return fmt.Errorf("failed to send receipt: %w", err)
		}

		// Update payment with receipt info
		payment.ReceiptID = fmt.Sprintf("RCP-%s", payment.PaymentReference)
		h.DB.Save(&payment)

		h.Logger.Info("payment receipt sent",
			zap.Uint("payment_id", payment.ID),
			zap.String("receipt_id", payment.ReceiptID))
	}

	return nil
}
