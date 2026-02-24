package bot

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/receipt"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
)

// EscalationResolutionSession tracks when tenant is marking an escalation as fixed
type EscalationResolutionSession struct {
	InteractionID uuid.UUID
	TenantID      uuid.UUID
	CustomerID    uuid.UUID
	Platform      string
	StartedAt     time.Time
}

var escalationResolutionSessions = make(map[int64]*EscalationResolutionSession) // chatID → session

// HandleMarkAsFixed processes the "Mark as Fixed" action and sends the prompt message
func (h *EscalationHandler) HandleMarkAsFixed(ctx context.Context, tenantID uuid.UUID, interactionID uuid.UUID, chatID int64, bot *tgbotapi.BotAPI) error {
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
	// Use SessionManager if available via Manager
	var existingSession *EscalationResolutionSession
	var exists bool
	if h.Manager != nil && h.Manager.SessionManager != nil {
		existingSession, exists = h.Manager.SessionManager.GetEscalationResolutionSession(ctx, chatID)
	}
	if exists && existingSession != nil {
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

	// Save to database
	if h.Manager != nil && h.Manager.SessionManager != nil {
		if err := h.Manager.SessionManager.SaveEscalationResolutionSession(ctx, chatID, session); err != nil {
			h.Logger.Warn("failed to save escalation resolution session", zap.Error(err))
		}
	}

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
func (h *EscalationHandler) HandleResolutionMessage(ctx context.Context, tenantID uuid.UUID, chatID int64, resolutionMessage string) error {
	// Get session from database
	var session *EscalationResolutionSession
	var ok bool
	if h.Manager != nil && h.Manager.SessionManager != nil {
		session, ok = h.Manager.SessionManager.GetEscalationResolutionSession(ctx, chatID)
	}
	if !ok || session == nil {
		return fmt.Errorf("no active resolution session found")
	}

	// Verify session belongs to tenant
	if session.TenantID != tenantID {
		return fmt.Errorf("session does not belong to tenant")
	}

	// Load interaction
	var interaction storage.Interaction
	if err := h.DB.WithContext(ctx).First(&interaction, session.InteractionID).Error; err != nil {
		if h.Manager != nil && h.Manager.SessionManager != nil {
			h.Manager.SessionManager.DeleteEscalationResolutionSession(ctx, chatID)
		}
		return fmt.Errorf("interaction not found: %w", err)
	}

	// Load customer
	var customer storage.Customer
	if err := h.DB.WithContext(ctx).First(&customer, session.CustomerID).Error; err != nil {
		if h.Manager != nil && h.Manager.SessionManager != nil {
			h.Manager.SessionManager.DeleteEscalationResolutionSession(ctx, chatID)
		}
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
		if interaction.ConversationID != uuid.Nil {
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
				zap.Stringer("interaction_id", session.InteractionID))
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
	if interaction.ConversationID != uuid.Nil {
		var conversation storage.Conversation
		if err := h.DB.First(&conversation, interaction.ConversationID).Error; err == nil {
			conversation.Status = "resolved"
			h.DB.Save(&conversation)
		}
	}

	// Send payment receipt if this was a payment escalation.
	// Prefer event signal stored in metadata; keep legacy intent as fallback.
	if interaction.DetectedIntent == "payment" || ai.MetadataHasEvent(interaction.Metadata, ai.EventPaymentConfirmation) {
		if err := h.sendPaymentReceiptToCustomer(ctx, &interaction); err != nil {
			h.Logger.Error("failed to send payment receipt",
				zap.Error(err),
				zap.Stringer("interaction_id", interaction.ID))
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

	// Remove from pending escalations (now database-backed, handled in escalation_handler.go)
	// Delete session from database
	if h.Manager != nil && h.Manager.SessionManager != nil {
		h.Manager.SessionManager.DeleteEscalationResolutionSession(ctx, chatID)
	}

	h.Logger.Info("✅ Escalation resolved",
		zap.Stringer("interaction_id", session.InteractionID),
		zap.Stringer("tenant_id", tenantID),
		zap.Bool("message_sent", sendMessage))

	return nil
}

// GetUnresolvedEscalations returns all unresolved escalations for a tenant
func (h *EscalationHandler) GetUnresolvedEscalations(tenantID uuid.UUID) ([]storage.Interaction, error) {
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
				zap.Stringer("tenant_id", tenant.ID),
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

		// Format concise reminder message
		reminderMsg := fmt.Sprintf("🌅 *Daily Briefing*\n\n")

		// 1. Unresolved Escalations
		if len(escalations) > 0 {
			reminderMsg += fmt.Sprintf("🔔 You have *%d conversation(s)* waiting for you.\n", len(escalations))

			// Show oldest one as context
			oldest := escalations[0]
			age := time.Since(oldest.CreatedAt)
			ageStr := fmt.Sprintf("%.0fh ago", age.Hours())

			// Load customer name
			var customer storage.Customer
			custName := "Customer"
			if err := h.DB.First(&customer, oldest.CustomerID).Error; err == nil {
				if customer.DisplayName != "" {
					custName = customer.DisplayName
				}
			}

			previewLen := 30
			if len(oldest.Content) < previewLen {
				previewLen = len(oldest.Content)
			}
			reminderMsg += fmt.Sprintf("\nOldest: *%s* (%s)\n\"%s...\"\n", custName, ageStr, oldest.Content[:previewLen])
			reminderMsg += "\n👇 *Tap below to catch up:*"
		} else {
			reminderMsg += "✅ All caught up! No seeking escalations."
		}

		// Button to view/handle
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🚀 Handle Next", "escalations:next"),
				tgbotapi.NewInlineKeyboardButtonData("📋 View All", "escalations:list"),
			),
		)

		msg := tgbotapi.NewMessage(tenant.OwnerTelegramID, reminderMsg)
		msg.ParseMode = "Markdown"
		if len(escalations) > 0 {
			msg.ReplyMarkup = keyboard
		}

		if _, err := bot.Send(msg); err != nil {
			h.Logger.Error("failed to send daily reminder",
				zap.Stringer("tenant_id", tenant.ID),
				zap.Error(err))
		} else {
			h.Logger.Info("✅ Sent daily reminder summary",
				zap.Stringer("tenant_id", tenant.ID),
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
			zap.Stringer("tenant_id", interaction.TenantID),
			zap.Stringer("customer_id", interaction.CustomerID))
		return nil // Not an error, just no payment to receipt
	}

	publicURL := os.Getenv("PUBLIC_URL")
	if publicURL == "" {
		publicURL = "https://api.brancr.com"
	}

	// Ensure portal token exists for link fallback
	if payment.PortalToken == "" {
		payment.PortalToken = utils.GeneratePortalToken()
		if err := h.DB.Save(&payment).Error; err != nil {
			h.Logger.Warn("failed to save portal token", zap.Error(err), zap.Stringer("payment_id", payment.ID))
		}
	}

	// Ensure we have a receipt URL (generate if missing)
	if payment.ReceiptURL == "" || payment.ReceiptID == "" {
		cloudinaryURL := os.Getenv("CLOUDINARY_URL")
		receiptService := receipt.NewReceiptService(h.DB, h.Logger, cloudinaryURL, publicURL, nil)
		if generated, err := receiptService.GenerateReceipt(ctx, payment.ID, payment.TenantID); err == nil {
			payment = *generated
		} else {
			h.Logger.Warn("failed to generate receipt", zap.Error(err), zap.Stringer("payment_id", payment.ID))
		}
	}

	linkLine := ""
	receiptLink := ""
	if strings.HasPrefix(payment.ReceiptURL, "https://") || strings.HasPrefix(payment.ReceiptURL, "http://") {
		receiptLink = payment.ReceiptURL
	} else if payment.PortalToken != "" {
		receiptLink = fmt.Sprintf("%s/portal/receipt?token=%s", publicURL, payment.PortalToken)
	} else if payment.PortalURL != "" {
		receiptLink = payment.PortalURL
	}
	if receiptLink != "" {
		linkLine = fmt.Sprintf("\n🔗 Receipt: %s", receiptLink)
	}

	// Generate receipt message
	receiptMessage := fmt.Sprintf(`✅ *PAYMENT CONFIRMED*

📝 Receipt #: %s
💰 Amount: %s %.2f
✓ Status: Payment Verified%s

Thank you for your payment! Your transaction has been confirmed and processed successfully.

If you have any questions, feel free to reach out.`,
		payment.PaymentReference,
		payment.Currency,
		payment.Amount,
		linkLine)

	// Load customer for platform ID
	var customer storage.Customer
	if err := h.DB.First(&customer, interaction.CustomerID).Error; err != nil {
		return fmt.Errorf("customer not found: %w", err)
	}

	// Send receipt via MessageHandler
	if h.MessageHandler != nil {
		// Default to customer platform ID; for IG/FB prefer thread ID
		threadID := customer.PlatformID
		if interaction.ConversationID != uuid.Nil {
			var conversation storage.Conversation
			if err := h.DB.First(&conversation, interaction.ConversationID).Error; err == nil {
				if conversation.PlatformThreadID != "" {
					threadID = conversation.PlatformThreadID
				}
			}
		}
		if threadID == "" {
			threadID = customer.PlatformID
		}

		if err := h.MessageHandler.SendReply(ctx, interaction.TenantID, interaction.Platform, threadID, customer.PlatformID, receiptMessage); err != nil {
			return fmt.Errorf("failed to send receipt: %w", err)
		}

		h.Logger.Info("payment receipt sent",
			zap.Stringer("payment_id", payment.ID),
			zap.String("receipt_id", payment.ReceiptID),
			zap.String("platform", interaction.Platform))
	}

	return nil
}
