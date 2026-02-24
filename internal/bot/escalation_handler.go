package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/checkpoint"
	"github.com/similadayo/brancr/internal/receipt"
	"github.com/similadayo/brancr/internal/security"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// escapeMarkdown escapes special Markdown characters for Telegram Markdown mode
// Special characters in Telegram Markdown: * _ ` [ ] ( )
func escapeMarkdown(text string) string {
	if text == "" {
		return text
	}
	// Escape each special character
	escaped := strings.ReplaceAll(text, "\\", "\\\\") // Escape backslashes first
	escaped = strings.ReplaceAll(escaped, "*", "\\*")
	escaped = strings.ReplaceAll(escaped, "_", "\\_")
	escaped = strings.ReplaceAll(escaped, "`", "\\`")
	escaped = strings.ReplaceAll(escaped, "[", "\\[")
	escaped = strings.ReplaceAll(escaped, "]", "\\]")
	escaped = strings.ReplaceAll(escaped, "(", "\\(")
	escaped = strings.ReplaceAll(escaped, ")", "\\)")
	return escaped
}

// getPlatformEmoji returns a compact platform indicator with emoji
func getPlatformEmoji(platform string) string {
	switch strings.ToLower(platform) {
	case "instagram":
		return "(IG)"
	case "facebook":
		return "(FB)"
	case "whatsapp":
		return "(WA)"
	case "telegram":
		return "(TG)"
	case "tiktok":
		return "(TT)"
	case "youtube":
		return "(YT)"
	default:
		return ""
	}
}

// ReplySender interface for sending replies to customers
type ReplySender interface {
	SendReply(ctx context.Context, tenantID uuid.UUID, platform, threadID, customerID, reply string) error
}

// EscalationHandler manages escalated messages from the MessageHandler
type EscalationHandler struct {
	DB                *gorm.DB
	Logger            *zap.Logger
	Manager           *TenantBotManager
	MessageHandler    ReplySender                      // Set after initialization via SetMessageHandler
	PaymentSecurity   *security.PaymentSecurityHandler // Security handler for payment escalations
	BehaviorService   *EscalationBehaviorService       // Service for checking escalation rules
	CheckpointService *checkpoint.Service              // Experience Checkpoint Service
	ReceiptService    *receipt.ReceiptService          // Service for generating receipts
}

// SetMessageHandler sets the MessageHandler for sending replies
func (h *EscalationHandler) SetMessageHandler(handler ReplySender) {
	h.MessageHandler = handler
}

// Note: PendingEscalation is now defined in internal/storage/models.go
// This allows persistence across restarts and multi-instance support

// paymentModeFromText is a lightweight detector for payment hints
func paymentModeFromText(text string) string {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "cash on delivery") || strings.Contains(lower, "pay on delivery") || strings.Contains(lower, "pod"):
		return "cash_on_delivery"
	case strings.Contains(lower, "i have paid") || strings.Contains(lower, "i've paid") || strings.Contains(lower, "paid") || strings.Contains(lower, "payment made") || strings.Contains(lower, "sent payment") || strings.Contains(lower, "transferred"):
		return "bank_transfer"
	default:
		return "unknown"
	}
}

// SendEscalation sends an escalated message to tenant's Telegram control center
func (h *EscalationHandler) SendEscalation(ctx context.Context, tenantID uuid.UUID, interactionID uuid.UUID, customerName, platform, message, suggestedReply string, intent string, tone string, confidence float64, reason string) error {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Load tenant
	var tenant storage.Tenant
	if err := h.DB.WithContext(ctx).First(&tenant, tenantID).Error; err != nil {
		return fmt.Errorf("tenant not found: %w", err)
	}

	// Defensive: Do NOT treat alerts (informational) as escalations.
	// If the reason string looks like an alert (contains 'alert' or ends with '_alert'), skip creating an escalation.
	if strings.Contains(strings.ToLower(reason), "alert") || strings.HasSuffix(strings.ToLower(reason), "_alert") {
		h.Logger.Info("⚠️ Skipping escalation because reason looks like an alert", zap.String("reason", reason))
		return nil
	}

	// ✅ Check Escalation Behavior Rules
	if h.BehaviorService != nil {
		if !h.BehaviorService.ShouldEscalate(ctx, tenantID, reason, intent) {
			h.Logger.Info("🛑 Escalation suppressed by plan settings",
				zap.Stringer("tenant_id", tenantID),
				zap.String("behavior", h.BehaviorService.GetEscalationBehavior(ctx, tenantID)),
				zap.String("reason", reason),
				zap.String("intent", intent))
			return nil
		}
	}

	// Get tenant's bot - try to initialize if not found
	bot, ok := h.Manager.GetBot(tenantID)
	if !ok {
		// Try to initialize the bot if it doesn't exist
		if tenant.TelegramBotToken != "" {
			h.Logger.Info("🤖 Bot not found for tenant, attempting to initialize",
				zap.Stringer("tenant_id", tenantID),
				zap.String("tenant_name", tenant.Name))

			if err := h.Manager.startTenantBot(&tenant); err != nil {
				h.Logger.Error("❌ Failed to initialize bot for tenant",
					zap.Stringer("tenant_id", tenantID),
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
				zap.Stringer("tenant_id", tenantID),
				zap.String("tenant_name", tenant.Name),
				zap.String("tenant_status", tenant.Status))
			return fmt.Errorf("bot not found for tenant %d: tenant has no Telegram bot token configured. Please set up a Telegram bot for this tenant", tenantID)
		}
	}

	// 🔒 SECURITY: For payment escalations, encrypt message content and add security measures
	isPaymentIntent := intent == "payment" || intent == "refund"
	if isPaymentIntent && h.PaymentSecurity != nil {
		// Validate payment intent
		if !h.PaymentSecurity.ValidatePaymentIntent(message) {
			h.Logger.Warn("⚠️ Payment intent flagged but validation failed",
				zap.Stringer("interaction_id", interactionID),
				zap.String("message_preview", h.PaymentSecurity.MaskForLogging(message)[:min(50, len(message))]))
		}

		// Encrypt payment message content for secure storage
		encryptedMessage, err := h.PaymentSecurity.EncryptPaymentMessage(message)
		if err != nil {
			h.Logger.Error("🔒 Failed to encrypt payment message",
				zap.Stringer("interaction_id", interactionID),
				zap.Error(err))
		} else {
			// Store encrypted version in interaction (within transaction context)
			// Load interaction to get customer ID
			var interaction storage.Interaction
			if err := h.DB.WithContext(ctx).First(&interaction, interactionID).Error; err != nil {
				return fmt.Errorf("interaction not found: %w", err)
			}
			interaction.EncryptedContent = encryptedMessage
			interaction.IsEncrypted = true
			interaction.SecurityLevel = "payment"
			if err := h.DB.WithContext(ctx).Save(&interaction).Error; err != nil {
				h.Logger.Warn("failed to save encrypted interaction", zap.Error(err))
			}
		}

		// Log masked version for security audit
		maskedMessage := h.PaymentSecurity.MaskForLogging(message)
		h.Logger.Info("🔒 Payment escalation created (message encrypted)",
			zap.Stringer("tenant_id", tenantID),
			zap.Stringer("interaction_id", interactionID),
			zap.String("masked_message", maskedMessage))
	}

	// Format escalation message (use masked version for payment in Telegram)
	displayMessage := message
	if isPaymentIntent && h.PaymentSecurity != nil {
		// Show partially masked version in Telegram for security
		displayMessage = h.PaymentSecurity.MaskForLogging(message)
	}

	// Load interaction to get customer ID
	var interaction storage.Interaction
	if err := h.DB.WithContext(ctx).First(&interaction, interactionID).Error; err != nil {
		return fmt.Errorf("interaction not found: %w", err)
	}

	// Load customer to get proper name (with fallback)
	var customer storage.Customer
	if err := h.DB.WithContext(ctx).First(&customer, interaction.CustomerID).Error; err == nil {
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

	// Format escalation message - V2 UX: Simple, warm, action-oriented
	// Focus: Who said what, suggested reply, and 2 action buttons
	escalatedCustomerName := escapeMarkdown(customerName)
	escalatedPlatform := getPlatformEmoji(platform)
	escalatedMessage := escapeMarkdown(displayMessage)
	escalatedSuggestedReply := escapeMarkdown(suggestedReply)

	// V2 Format: Clean and conversational
	// "💬 John (IG):
	// "Can I get a discount?"
	//
	// You could say:
	// "Hey! Yes I can do 10% off..."
	escalationMsg := fmt.Sprintf(
		"💬 *%s* %s:\n\"%s\"\n\n"+
			"You could say:\n\"%s\"",
		escalatedCustomerName,
		escalatedPlatform,
		escalatedMessage,
		escalatedSuggestedReply,
	)

	// Add security note only for payment (very brief)
	// if isPaymentIntent {
	// 	escalationMsg += "\n\n🔒 _Payment mentioned_"
	// }

	// V2 Keyboard: Payment-specific vs. general escalation buttons
	var keyboard tgbotapi.InlineKeyboardMarkup
	if isPaymentIntent {
		// Payment escalation: Approve (resolves + sends receipt), Edit Reply (with confirm/stall)
		keyboard = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Approve", fmt.Sprintf("escalate:payment_approve:%s", interactionID)),
				tgbotapi.NewInlineKeyboardButtonData("✏️ Edit Reply", fmt.Sprintf("escalate:payment_edit:%s", interactionID)),
			),
		)
	} else {
		// General escalation: Send this, Reply myself
		keyboard = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Send this", fmt.Sprintf("escalate:approve:%s", interactionID)),
				tgbotapi.NewInlineKeyboardButtonData("Reply myself", fmt.Sprintf("escalate:edit:%s", interactionID)),
			),
		)
	}

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

	// Store pending escalation in database (replaces in-memory map)
	pendingEscalation := storage.PendingEscalation{
		InteractionID:  interactionID,
		TenantID:       tenantID,
		CustomerID:     interaction.CustomerID,
		Platform:       platform,
		Message:        message,
		SuggestedReply: suggestedReply,
		ChatID:         tenant.OwnerTelegramID,
		Intent:         intent,
		Tone:           tone,
		Confidence:     confidence,
		Reason:         reason,
	}

	// Use FirstOrCreate to handle duplicates gracefully
	if err := h.DB.WithContext(ctx).Where("interaction_id = ?", interactionID).
		FirstOrCreate(&pendingEscalation, storage.PendingEscalation{InteractionID: interactionID}).Error; err != nil {
		h.Logger.Error("failed to store pending escalation", zap.Error(err), zap.Stringer("interaction_id", interactionID))
		// Continue - escalation was sent, just logging failed
	}

	h.Logger.Info("📤 Escalation sent to tenant",
		zap.Stringer("tenant_id", tenantID),
		zap.Stringer("interaction_id", interactionID),
		zap.String("intent", intent),
	)

	// Checkpoint Trigger: Escalation
	if h.CheckpointService != nil {
		if err := h.CheckpointService.TriggerCheckpoint(context.Background(), tenantID, "escalation", interactionID); err != nil {
			h.Logger.Warn("failed to trigger escalation checkpoint", zap.Error(err))
		}
	}

	return nil
}

// HandleEscalationResponse processes tenant's response to escalation
func (h *EscalationHandler) HandleEscalationResponse(ctx context.Context, tenantID uuid.UUID, chatID int64, message string) error {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Check if this is an approval
	lower := strings.ToLower(strings.TrimSpace(message))

	// Parse "approve <interaction_id>" or just "approve" for most recent
	if strings.HasPrefix(lower, "approve") {
		var interactionID uuid.UUID
		parts := strings.Fields(lower)
		if len(parts) > 1 {
			id, err := uuid.Parse(parts[1])
			if err == nil {
				interactionID = id
			}
		}

		// If no ID specified, find most recent pending escalation from database
		if interactionID == uuid.Nil {
			var pendingEsc storage.PendingEscalation
			if err := h.DB.WithContext(ctx).Where("tenant_id = ? AND chat_id = ?", tenantID, chatID).
				Order("created_at DESC").First(&pendingEsc).Error; err != nil {
				// Fallback: try to find from interaction table
				var interaction storage.Interaction
				if err := h.DB.WithContext(ctx).Where("tenant_id = ? AND response_type = ? AND response_status = ?", tenantID, "escalated", "pending").
					Order("created_at DESC").First(&interaction).Error; err != nil {
					return fmt.Errorf("no pending escalation found")
				}
				interactionID = interaction.ID
				h.Logger.Info("📋 Found escalation from interaction table", zap.Stringer("interaction_id", interactionID))
			} else {
				interactionID = pendingEsc.InteractionID
				h.Logger.Info("📋 Found escalation from pending_escalations table", zap.Stringer("interaction_id", interactionID))
			}
		}

		// Load escalation from database
		var esc storage.PendingEscalation
		if err := h.DB.WithContext(ctx).Where("interaction_id = ? AND tenant_id = ?", interactionID, tenantID).First(&esc).Error; err != nil {
			// Fallback: try to rebuild from interaction
			escPtr, err := h.loadEscalationFromDB(ctx, interactionID, tenantID, chatID)
			if err != nil {
				h.Logger.Error("failed to load escalation from database", zap.Error(err), zap.Stringer("interaction_id", interactionID))
				return fmt.Errorf("escalation not found: %w", err)
			}
			// Convert to storage.PendingEscalation
			esc = storage.PendingEscalation{
				InteractionID:  escPtr.InteractionID,
				TenantID:       escPtr.TenantID,
				CustomerID:     escPtr.CustomerID,
				Platform:       escPtr.Platform,
				Message:        escPtr.Message,
				SuggestedReply: escPtr.SuggestedReply,
				ChatID:         escPtr.ChatID,
			}
		}

		// Send approved reply
		return h.sendApprovedReply(ctx, &esc, esc.SuggestedReply)
	}

	// Check if message starts with interaction ID (for custom replies)
	// Format: "<interaction_id> <custom message>"
	parts := strings.Fields(message)
	if len(parts) >= 2 {
		if id, err := uuid.Parse(parts[0]); err == nil {
			interactionID := id
			var esc storage.PendingEscalation
			if err := h.DB.WithContext(ctx).Where("interaction_id = ? AND tenant_id = ?", interactionID, tenantID).First(&esc).Error; err == nil {
				customReply := strings.Join(parts[1:], " ")
				return h.sendApprovedReply(ctx, &esc, customReply)
			}
		}
	}

	// Otherwise, treat as custom reply to most recent escalation
	var esc storage.PendingEscalation
	if err := h.DB.WithContext(ctx).Where("tenant_id = ? AND chat_id = ?", tenantID, chatID).
		Order("created_at DESC").First(&esc).Error; err == nil {
		return h.sendApprovedReply(ctx, &esc, message)
	}

	return fmt.Errorf("no matching escalation found")
}

// sendApprovedReply sends the approved reply to the customer
func (h *EscalationHandler) sendApprovedReply(ctx context.Context, esc *storage.PendingEscalation, reply string) error {
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
	if interaction.ConversationID != uuid.Nil {
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
			zap.Stringer("tenant_id", esc.TenantID),
			zap.String("platform", esc.Platform),
			zap.Error(err))
		return fmt.Errorf("no connected %s account found", esc.Platform)
	}

	// Use MessageHandler to send reply if available
	// Note: SendReply now creates an outgoing interaction automatically
	// We'll create another one with escalation metadata (EscalatedTo, EscalatedAt) for better tracking
	var sendErr error
	if h.MessageHandler != nil {
		sendErr = h.MessageHandler.SendReply(ctx, esc.TenantID, esc.Platform, threadID, customer.PlatformID, reply)
	} else {
		// Fallback: return error asking user to reconnect
		sendErr = fmt.Errorf("message handler not configured - please contact support")
		h.Logger.Error("message handler not set in escalation handler",
			zap.Stringer("tenant_id", esc.TenantID),
			zap.String("platform", esc.Platform))
	}

	if sendErr != nil {
		h.Logger.Error("failed to send reply to customer",
			zap.Error(sendErr),
			zap.String("platform", esc.Platform),
			zap.Stringer("customer_id", customer.ID))
		return fmt.Errorf("failed to send reply: %w", sendErr)
	}

	// Use transaction to ensure atomicity of interaction updates and order/payment creation
	now := time.Now()
	err := h.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Update incoming interaction
		interaction.ResponseType = "manual"
		interaction.ResponseStatus = "sent"
		interaction.FinalReply = reply
		interaction.EscalatedTo = esc.ChatID
		interaction.EscalatedAt = &now
		if err := tx.Save(&interaction).Error; err != nil {
			return fmt.Errorf("failed to update interaction: %w", err)
		}

		// Update the outgoing interaction created by SendReply with escalation metadata
		// Find the most recent outgoing interaction for this conversation
		var outgoingInteraction storage.Interaction
		if err := tx.Where("tenant_id = ? AND customer_id = ? AND conversation_id = ? AND direction = ? AND platform = ? AND content = ?",
			esc.TenantID, esc.CustomerID, interaction.ConversationID, "outgoing", esc.Platform, reply).
			Order("created_at DESC").First(&outgoingInteraction).Error; err == nil {
			// Update with escalation metadata
			outgoingInteraction.EscalatedTo = esc.ChatID
			outgoingInteraction.EscalatedAt = &now
			if err := tx.Save(&outgoingInteraction).Error; err != nil {
				h.Logger.Warn("failed to update outgoing interaction with escalation metadata", zap.Error(err))
			}
		} else {
			// Fallback: Create outgoing interaction if SendReply didn't create one (shouldn't happen)
			outgoingInteraction := storage.Interaction{
				TenantID:       esc.TenantID,
				CustomerID:     esc.CustomerID,
				ConversationID: interaction.ConversationID,
				Platform:       esc.Platform,
				Direction:      "outgoing",
				MessageType:    "text",
				Content:        reply,
				ResponseType:   "manual",
				ResponseStatus: "sent",
				FinalReply:     reply,
				EscalatedTo:    esc.ChatID,
				EscalatedAt:    &now,
				Metadata:       "{}",
			}
			if err := tx.Create(&outgoingInteraction).Error; err != nil {
				h.Logger.Warn("failed to create outgoing interaction for escalation reply", zap.Error(err))
			}
		}

		// Ensure order/payment records are created when tenant approves via Telegram
		// Pass transaction context to ensureOrderAndPayment
		if err := h.ensureOrderAndPaymentWithTx(ctx, tx, &interaction); err != nil {
			h.Logger.Warn("failed to ensure order and payment", zap.Error(err))
			// Don't fail transaction - order/payment can be created later
		}

		return nil
	})
	if err != nil {
		h.Logger.Error("transaction failed in sendApprovedReply", zap.Error(err))
		return fmt.Errorf("failed to process approved reply: %w", err)
	}

	// Feedback loop: record tenant edit when they modify the suggested reply.
	_ = ai.RecordTenantEditFeedback(ctx, h.DB, interaction.ID, reply)

	// Remove from pending escalations table
	if err := h.DB.WithContext(ctx).Where("interaction_id = ?", esc.InteractionID).Delete(&storage.PendingEscalation{}).Error; err != nil {
		h.Logger.Warn("failed to delete pending escalation", zap.Error(err), zap.Stringer("interaction_id", esc.InteractionID))
		// Continue - not critical
	}

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

		// Escape user content to prevent Markdown parsing errors
		escapedCustomerName := escapeMarkdown(customerName)
		escapedPlatform := escapeMarkdown(strings.Title(esc.Platform))
		escapedReply := escapeMarkdown(reply)

		confirmation := fmt.Sprintf("✅ Reply sent to *%s* on %s:\n\n%s", escapedCustomerName, escapedPlatform, escapedReply)

		// Add a note that we'll notify if customer responds
		followUpNote := fmt.Sprintf("\n\n💬 If %s responds, you'll be notified here.", escapedCustomerName)
		confirmation += followUpNote

		msg := tgbotapi.NewMessage(esc.ChatID, confirmation)
		msg.ParseMode = "Markdown"
		bot.Send(msg)
	}

	h.Logger.Info("✅ Approved reply sent",
		zap.Stringer("interaction_id", esc.InteractionID),
		zap.Stringer("tenant_id", esc.TenantID),
		zap.String("platform", esc.Platform),
	)

	// 🆕 RECEIPT GENERATION: If this was a payment confirmation, send a receipt
	if esc.Intent == "payment" {
		// Load tenant to check industry for terminology
		var tenant storage.Tenant
		termLabel := "Order"
		if err := h.DB.WithContext(ctx).Preload("BusinessProfile").First(&tenant, esc.TenantID).Error; err == nil {
			industry := strings.ToLower(tenant.BusinessProfile.Industry)
			// Check for service-based industries
			if strings.Contains(industry, "service") ||
				strings.Contains(industry, "consulting") ||
				strings.Contains(industry, "salon") ||
				strings.Contains(industry, "clinic") ||
				strings.Contains(industry, "repair") ||
				strings.Contains(industry, "logistics") {
				termLabel = "Service"
			}
		}

		// Find the payment record
		var paymentRec storage.Payment
		if err := h.DB.WithContext(ctx).Where("tenant_id = ? AND customer_id = ?", esc.TenantID, esc.CustomerID).Order("created_at DESC").First(&paymentRec).Error; err == nil {

			// Use ReceiptService to generate official receipt (and upload PDF)
			// This will also populate ReceiptURL in the payment record
			var receiptURL string
			publicURL := os.Getenv("PUBLIC_URL")
			if strings.TrimSpace(publicURL) == "" {
				publicURL = "https://api.brancr.com"
			}
			if h.ReceiptService != nil {
				if updatedPayment, err := h.ReceiptService.GenerateReceipt(ctx, paymentRec.ID, esc.TenantID); err == nil {
					paymentRec = *updatedPayment
					receiptURL = updatedPayment.ReceiptURL
				} else {
					h.Logger.Error("failed to generate official receipt", zap.Error(err))
				}
			}

			// Always ensure a portal token exists so we can send a downloadable link even if PDF generation isn't available
			if paymentRec.PortalToken == "" {
				paymentRec.PortalToken = utils.GeneratePortalToken()
				_ = h.DB.WithContext(ctx).Save(&paymentRec).Error
			}
			if h.ReceiptService != nil {
				_, _, _ = h.ReceiptService.GeneratePortalTokenAndURL(ctx, paymentRec.OrderID, esc.TenantID, publicURL)
				_ = h.DB.WithContext(ctx).Where("id = ?", paymentRec.ID).First(&paymentRec).Error
			}

			receiptLink := ""
			if strings.HasPrefix(receiptURL, "https://") || strings.HasPrefix(receiptURL, "http://") {
				receiptLink = receiptURL
			} else if paymentRec.PortalToken != "" {
				receiptLink = fmt.Sprintf("%s/portal/receipt?token=%s", strings.TrimRight(publicURL, "/"), paymentRec.PortalToken)
			} else if paymentRec.PortalURL != "" {
				receiptLink = paymentRec.PortalURL
			}

			// Construct message with link if available
			linkText := ""
			if receiptLink != "" {
				linkText = fmt.Sprintf("\n📄 [Download Receipt](%s)", receiptLink)
			}

			receiptMsg := fmt.Sprintf("🧾 *Payment Receipt*\n\n%s: %s\nAmount: %s%.2f\nDate: %s\nStatus: %s%s\n\nThank you for your business!",
				termLabel, paymentRec.PaymentReference, paymentRec.Currency, paymentRec.Amount, paymentRec.CreatedAt.Format("Jan 02, 2006"), "Paid", linkText)

			// Send receipt as a separate bubble
			if err := h.SendReceiptToCustomer(ctx, esc.TenantID, &interaction, receiptMsg); err != nil {
				h.Logger.Warn("failed to send auto-receipt", zap.Error(err))
			}
		}
	}

	return nil
}

// SendReceiptToCustomer sends a payment confirmation receipt directly to the customer
// This bypasses the suggested reply and sends a proper receipt message
func (h *EscalationHandler) SendReceiptToCustomer(ctx context.Context, tenantID uuid.UUID, interaction *storage.Interaction, receiptMessage string) error {
	// Load customer
	var customer storage.Customer
	if err := h.DB.First(&customer, interaction.CustomerID).Error; err != nil {
		return fmt.Errorf("customer not found: %w", err)
	}

	// Load conversation to get thread ID
	var conversation storage.Conversation
	threadID := customer.PlatformID // Default to customer ID
	if interaction.ConversationID != uuid.Nil {
		if err := h.DB.First(&conversation, interaction.ConversationID).Error; err == nil {
			threadID = conversation.PlatformThreadID
		}
	}
	if threadID == "" {
		threadID = customer.PlatformID
	}

	// Use MessageHandler to send reply
	if h.MessageHandler == nil {
		return fmt.Errorf("message handler not configured - please contact support")
	}

	// Send the receipt message
	if err := h.MessageHandler.SendReply(ctx, tenantID, interaction.Platform, threadID, customer.PlatformID, receiptMessage); err != nil {
		h.Logger.Error("failed to send receipt to customer",
			zap.Error(err),
			zap.String("platform", interaction.Platform),
			zap.Stringer("customer_id", customer.ID))
		return fmt.Errorf("failed to send receipt: %w", err)
	}

	h.Logger.Info("💳 Receipt sent to customer",
		zap.Stringer("interaction_id", interaction.ID),
		zap.Stringer("tenant_id", tenantID),
		zap.String("platform", interaction.Platform),
	)

	return nil
}

// ensureOrderAndPayment creates an order and payment when approving via Telegram
// This is a wrapper that starts its own transaction
func (h *EscalationHandler) ensureOrderAndPayment(ctx context.Context, interaction *storage.Interaction) {
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		return h.ensureOrderAndPaymentWithTx(ctx, tx, interaction)
	}); err != nil {
		h.Logger.Warn("ensureOrderAndPayment: transaction failed", zap.Error(err))
	}
}

// ensureOrderAndPaymentWithTx creates an order and payment within an existing transaction
func (h *EscalationHandler) ensureOrderAndPaymentWithTx(ctx context.Context, tx *gorm.DB, interaction *storage.Interaction) error {
	if interaction == nil {
		return nil
	}

	// Avoid duplicate orders in the last 30 minutes
	var recent storage.Order
	if err := tx.Where("tenant_id = ? AND customer_id = ? AND platform = ?", interaction.TenantID, interaction.CustomerID, interaction.Platform).
		Where("created_at > ?", time.Now().Add(-30*time.Minute)).
		Order("created_at DESC").
		First(&recent).Error; err == nil {
		return h.ensurePaymentForOrderWithTx(ctx, tx, interaction, &recent)
	}

	// Load customer for metadata
	var customer storage.Customer
	if err := tx.First(&customer, interaction.CustomerID).Error; err != nil {
		h.Logger.Warn("ensureOrderAndPayment: customer not found", zap.Error(err), zap.Stringer("customer_id", interaction.CustomerID))
		return nil // Don't fail transaction - order can be created manually
	}

	// Try to pull sales conversation to infer items/amount (if available)
	var salesConv storage.SalesConversation
	if interaction.ConversationID != uuid.Nil {
		_ = tx.Where("conversation_id = ?", interaction.ConversationID).Order("created_at DESC").First(&salesConv).Error
	}
	if salesConv.ID == uuid.Nil {
		// Fallback: interactions sometimes lack ConversationID; use latest sales conversation for this tenant/customer.
		_ = tx.Where("tenant_id = ? AND customer_id = ?", interaction.TenantID, interaction.CustomerID).
			Order("updated_at DESC").
			First(&salesConv).Error
	}

	items := []storage.OrderItem{}
	total := 0.0
	currency := "NGN"
	amountSource := ""
	logAmountDecisions := os.Getenv("BRANCR_LOG_AMOUNT_DECISIONS") == "1"
	amountFromContent := 0.0
	amountFromReplies := 0.0
	if logAmountDecisions {
		amountFromContent = utils.ExtractLikelyAmountNGN(interaction.Content)
		amountFromReplies = utils.ExtractLikelyAmountNGN(interaction.FinalReply + " " + interaction.SuggestedReply)
		h.Logger.Info("tg approval: order amount decision (start)",
			zap.Stringer("interaction_id", interaction.ID),
			zap.Stringer("tenant_id", interaction.TenantID),
			zap.Stringer("customer_id", interaction.CustomerID),
			zap.Stringer("conversation_id", interaction.ConversationID),
			zap.String("platform", interaction.Platform),
			zap.Float64("salesconv_price_discussed", salesConv.PriceDiscussed),
			zap.Float64("amount_from_content", amountFromContent),
			zap.Float64("amount_from_replies", amountFromReplies))
	}

	if salesConv.PendingOrderDraft != "" {
		var draft struct {
			Items       []storage.OrderItem `json:"items"`
			TotalAmount float64             `json:"total_amount"`
			Currency    string              `json:"currency"`
		}
		if err := json.Unmarshal([]byte(salesConv.PendingOrderDraft), &draft); err == nil {
			items = draft.Items
			total = draft.TotalAmount
			if total > 0 {
				amountSource = "draft.total_amount"
			}
			if draft.Currency != "" {
				currency = draft.Currency
			}
		}
	}

	if len(items) > 0 && total == 0 {
		for _, it := range items {
			total += it.TotalPrice
		}
		if total > 0 {
			amountSource = "draft.items_sum"
		}
	}

	if len(items) == 0 && salesConv.PriceDiscussed > 0 {
		items = []storage.OrderItem{{
			Name:       "Order",
			Quantity:   1,
			UnitPrice:  salesConv.PriceDiscussed,
			TotalPrice: salesConv.PriceDiscussed,
		}}
		total = salesConv.PriceDiscussed
		amountSource = "salesconv.price_discussed"
	}

	if len(items) == 0 {
		fallbackTotal := salesConv.PriceDiscussed
		if fallbackTotal <= 0 {
			fallbackTotal = utils.ExtractLikelyAmountNGN(interaction.Content)
		}
		if fallbackTotal <= 0 {
			fallbackTotal = utils.ExtractLikelyAmountNGN(interaction.FinalReply + " " + interaction.SuggestedReply)
		}
		if fallbackTotal <= 0 {
			expectedAmount, expectedCurrency, expectedSource := h.inferExpectedAmount(ctx, interaction)
			if expectedAmount > 0 {
				fallbackTotal = expectedAmount
				currency = expectedCurrency
				amountSource = expectedSource
			}
		}
		if fallbackTotal <= 0 {
			// If we can't infer an amount from the current message, try to reuse a single recent pending order.
			// This is common on payment-confirmation messages like "I've paid" which contain no amount.
			var pending []storage.Order
			if err := tx.
				Where("tenant_id = ? AND customer_id = ? AND platform = ? AND status = ? AND total_amount > 0", interaction.TenantID, interaction.CustomerID, interaction.Platform, "pending").
				Where("created_at > ?", time.Now().Add(-7*24*time.Hour)).
				Order("created_at DESC").
				Limit(10).
				Find(&pending).Error; err == nil {
				if len(pending) == 1 {
					h.Logger.Info("ensureOrderAndPayment: reusing existing pending order (no amount in message)",
						zap.Stringer("interaction_id", interaction.ID),
						zap.Stringer("tenant_id", interaction.TenantID),
						zap.Stringer("customer_id", interaction.CustomerID),
						zap.Stringer("order_id", pending[0].ID),
						zap.String("platform", interaction.Platform),
						zap.Float64("order_total", pending[0].TotalAmount))
					return h.ensurePaymentForOrderWithTx(ctx, tx, interaction, &pending[0])
				}
				if len(pending) > 1 {
					expectedAmount, _, expectedSource := h.inferExpectedAmount(ctx, interaction)
					if expectedAmount > 0 {
						matches := make([]storage.Order, 0, len(pending))
						for _, o := range pending {
							if math.Abs(o.TotalAmount-expectedAmount) < 0.01 {
								matches = append(matches, o)
							}
						}
						if len(matches) == 1 {
							h.Logger.Info("ensureOrderAndPayment: matched pending order via expected amount",
								zap.Stringer("interaction_id", interaction.ID),
								zap.Stringer("tenant_id", interaction.TenantID),
								zap.Stringer("customer_id", interaction.CustomerID),
								zap.String("platform", interaction.Platform),
								zap.Float64("expected_amount", expectedAmount),
								zap.String("expected_source", expectedSource),
								zap.Stringer("order_id", matches[0].ID))
							return h.ensurePaymentForOrderWithTx(ctx, tx, interaction, &matches[0])
						}
						h.Logger.Warn("ensureOrderAndPayment: multiple pending orders match expected amount; skipping auto-link",
							zap.Stringer("interaction_id", interaction.ID),
							zap.Stringer("tenant_id", interaction.TenantID),
							zap.Stringer("customer_id", interaction.CustomerID),
							zap.String("platform", interaction.Platform),
							zap.Float64("expected_amount", expectedAmount),
							zap.String("expected_source", expectedSource),
							zap.Int("pending_count", len(pending)),
							zap.Int("match_count", len(matches)))
					} else {
						h.Logger.Warn("ensureOrderAndPayment: multiple pending orders found; skipping auto-link",
							zap.Stringer("interaction_id", interaction.ID),
							zap.Stringer("tenant_id", interaction.TenantID),
							zap.Stringer("customer_id", interaction.CustomerID),
							zap.String("platform", interaction.Platform),
							zap.Stringer("most_recent_order_id", pending[0].ID))
					}
				}
			}
			h.Logger.Warn("ensureOrderAndPayment: skipping auto-order creation (no amount inferred)",
				zap.Stringer("interaction_id", interaction.ID),
				zap.Stringer("tenant_id", interaction.TenantID),
				zap.Stringer("customer_id", interaction.CustomerID),
				zap.Stringer("conversation_id", interaction.ConversationID),
				zap.String("platform", interaction.Platform),
				zap.Float64("price_discussed", salesConv.PriceDiscussed))
			return nil // Don't fail transaction - order can be created manually
		}
		items = []storage.OrderItem{{
			Name:       "Order",
			Quantity:   1,
			UnitPrice:  fallbackTotal,
			TotalPrice: fallbackTotal,
		}}
		total = fallbackTotal
		if amountSource == "" {
			amountSource = "fallback.manual_item"
		}
	}

	if logAmountDecisions {
		h.Logger.Info("tg approval: order amount decision (final)",
			zap.Stringer("interaction_id", interaction.ID),
			zap.Stringer("tenant_id", interaction.TenantID),
			zap.Stringer("customer_id", interaction.CustomerID),
			zap.Stringer("conversation_id", interaction.ConversationID),
			zap.String("platform", interaction.Platform),
			zap.String("currency", currency),
			zap.Int("final_item_count", len(items)),
			zap.Float64("final_total", total),
			zap.String("amount_source", amountSource),
			zap.Float64("salesconv_price_discussed", salesConv.PriceDiscussed),
			zap.Float64("amount_from_content", amountFromContent),
			zap.Float64("amount_from_replies", amountFromReplies))
	}

	itemsJSON, _ := json.Marshal(items)
	orderNumber := fmt.Sprintf("ORD-TG-%d", time.Now().Unix())

	var tenant storage.Tenant
	if err := tx.Preload("BusinessProfile").First(&tenant, interaction.TenantID).Error; err != nil {
		h.Logger.Warn("ensureOrderAndPayment: tenant not found", zap.Error(err))
		return nil // Don't fail transaction
	}
	itemLabel := ""
	if len(items) > 0 {
		itemLabel = items[0].Name
	}
	businessName := tenant.BusinessProfile.Name
	if businessName == "" {
		businessName = tenant.Name
	}

	// Generate unique payment reference with retry on conflict
	paymentRef := utils.GeneratePaymentReference(businessName, interaction.TenantID, itemLabel)
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		var existingOrder storage.Order
		if err := tx.Where("payment_reference = ?", paymentRef).First(&existingOrder).Error; err != nil {
			// Payment reference is unique, proceed
			break
		}
		// Conflict - generate new one
		paymentRef = utils.GeneratePaymentReference(businessName, interaction.TenantID, itemLabel)
		if i == maxRetries-1 {
			h.Logger.Error("ensureOrderAndPayment: failed to generate unique payment reference after retries")
			return fmt.Errorf("failed to generate unique payment reference")
		}
	}

	customerName := customer.DisplayName
	handle := customer.Username
	if customerName == "" {
		customerName = handle
	}
	if customerName == "" {
		customerName = fmt.Sprintf("Customer-%d", customer.ID)
	}

	customerPhone := customer.PlatformID
	switch strings.ToLower(interaction.Platform) {
	case "whatsapp":
		// keep phone as the chat number
		if customerName == "" {
			customerName = customer.Username
		}
	case "instagram", "facebook":
		if handle == "" {
			handle = customer.PlatformID
		}
		customerPhone = handle // store handle for visibility in dashboard
	default:
		// leave defaults
	}
	orderRec := storage.Order{
		TenantID:         interaction.TenantID,
		CustomerID:       interaction.CustomerID,
		OrderNumber:      orderNumber,
		PaymentReference: paymentRef,
		Status:           "pending",
		TotalAmount:      total,
		Currency:         currency,
		Items:            string(itemsJSON),
		CustomerName:     customerName,
		CustomerPhone:    customerPhone,
		Platform:         interaction.Platform,
	}

	if err := tx.Create(&orderRec).Error; err != nil {
		// Check if it's a unique constraint violation
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			h.Logger.Warn("ensureOrderAndPayment: order with same payment reference already exists", zap.Error(err))
			return nil // Don't fail - order might already exist
		}
		h.Logger.Warn("ensureOrderAndPayment: failed to create order", zap.Error(err))
		return fmt.Errorf("failed to create order: %w", err)
	}

	return h.ensurePaymentForOrderWithTx(ctx, tx, interaction, &orderRec)
}

func (h *EscalationHandler) inferExpectedAmount(ctx context.Context, interaction *storage.Interaction) (amount float64, currency string, source string) {
	if interaction == nil {
		return 0, "", ""
	}

	// Check context cancellation
	select {
	case <-ctx.Done():
		return 0, "", ""
	default:
	}

	if amt, cur, ok := ai.ExpectedAmountFromMetadataJSON(interaction.Metadata); ok {
		return amt, cur, "interaction.metadata.expected_amount"
	}

	// Prefer recent outgoing messages in the same conversation.
	if interaction.ConversationID != uuid.Nil {
		var outgoing []storage.Interaction
		_ = h.DB.WithContext(ctx).
			Where("conversation_id = ? AND direction = ?", interaction.ConversationID, "outgoing").
			Order("created_at DESC").
			Limit(10).
			Find(&outgoing).Error
		for _, out := range outgoing {
			if amt, cur, ok := ai.ExpectedAmountFromMetadataJSON(out.Metadata); ok {
				return amt, cur, "conversation.outgoing.metadata.expected_amount"
			}
			content := out.Content
			if out.FinalReply != "" {
				content += " " + out.FinalReply
			}
			if amt := utils.ExtractLikelyAmountNGN(content); amt > 0 {
				return amt, "NGN", "conversation.outgoing.text.amount"
			}
		}
	}

	// Fallback: recent outgoing for this tenant/customer/platform.
	{
		var outgoing []storage.Interaction
		_ = h.DB.WithContext(ctx).
			Where("tenant_id = ? AND customer_id = ? AND platform = ? AND direction = ?", interaction.TenantID, interaction.CustomerID, interaction.Platform, "outgoing").
			Order("created_at DESC").
			Limit(10).
			Find(&outgoing).Error
		for _, out := range outgoing {
			if amt, cur, ok := ai.ExpectedAmountFromMetadataJSON(out.Metadata); ok {
				return amt, cur, "customer.outgoing.metadata.expected_amount"
			}
			content := out.Content
			if out.FinalReply != "" {
				content += " " + out.FinalReply
			}
			if amt := utils.ExtractLikelyAmountNGN(content); amt > 0 {
				return amt, "NGN", "customer.outgoing.text.amount"
			}
		}
	}

	return 0, "", ""
}

// ensurePaymentForOrder creates a payment record if missing for the given order
// This is a wrapper that starts its own transaction
func (h *EscalationHandler) ensurePaymentForOrder(interaction *storage.Interaction, orderRec *storage.Order) {
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		return h.ensurePaymentForOrderWithTx(context.Background(), tx, interaction, orderRec)
	}); err != nil {
		h.Logger.Warn("ensurePaymentForOrder: transaction failed", zap.Error(err))
	}
}

// ensurePaymentForOrderWithTx creates a payment record within an existing transaction
func (h *EscalationHandler) ensurePaymentForOrderWithTx(ctx context.Context, tx *gorm.DB, interaction *storage.Interaction, orderRec *storage.Order) error {
	if orderRec == nil {
		return nil
	}

	var existing storage.Payment
	if err := tx.Where("tenant_id = ? AND order_id = ?", orderRec.TenantID, orderRec.ID).First(&existing).Error; err == nil {
		return nil // Payment already exists
	}

	mode := paymentModeFromText(interaction.Content + " " + interaction.FinalReply)
	status := "pending"
	verification := "tenant_verified"
	now := time.Now()
	if mode != "cash_on_delivery" {
		status = "verified"
	}

	payment := storage.Payment{
		TenantID:           orderRec.TenantID,
		OrderID:            orderRec.ID,
		CustomerID:         orderRec.CustomerID,
		PaymentReference:   orderRec.PaymentReference,
		Amount:             orderRec.TotalAmount,
		Currency:           orderRec.Currency,
		PaymentMethod:      mode,
		Status:             status,
		VerificationStatus: verification,
		ClaimedByCustomer:  true,
		ClaimedAt:          &now,
		Metadata:           "{}",
	}

	// Generate a portal token to avoid duplicate key on idx_payments_portal_token.
	if payment.PortalToken == "" {
		payment.PortalToken = utils.GeneratePortalToken()
	}
	// Generate a unique receipt id to avoid duplicate key on idx_payments_receipt_id.
	if payment.ReceiptID == "" {
		payment.ReceiptID = utils.GenerateReceiptID()
	}

	if status == "verified" {
		payment.VerifiedAt = &now
	}

	// Generate unique portal token and receipt ID with retry on conflict
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		if payment.PortalToken == "" {
			payment.PortalToken = utils.GeneratePortalToken()
		}
		if payment.ReceiptID == "" {
			payment.ReceiptID = utils.GenerateReceiptID()
		}

		// Check for conflicts
		var conflictPayment storage.Payment
		if err := tx.Where("portal_token = ? OR receipt_id = ?", payment.PortalToken, payment.ReceiptID).First(&conflictPayment).Error; err != nil {
			// No conflict, proceed with creation
			if err := tx.Create(&payment).Error; err != nil {
				if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
					// Retry with new tokens
					payment.PortalToken = ""
					payment.ReceiptID = ""
					continue
				}
				return fmt.Errorf("failed to create payment: %w", err)
			}

			// 🆕 UPDATE ORDER STATUS
			// If payment is verified, mark order as paid to prevent AI loops
			if payment.Status == "verified" || payment.Status == "confirmed" {
				if err := tx.Model(orderRec).Update("status", "paid").Error; err != nil {
					h.Logger.Warn("ensurePaymentForOrder: failed to update order status to paid", zap.Error(err))
					// Non-critical, continue
				} else {
					h.Logger.Info("✅ Order marked as PAID", zap.String("order_number", orderRec.OrderNumber))
				}
			}

			return nil // Success
		}
		// Conflict found, retry with new tokens
		payment.PortalToken = ""
		payment.ReceiptID = ""
	}

	return fmt.Errorf("failed to generate unique payment tokens after %d retries", maxRetries)
}

// loadEscalationFromDB rebuilds a PendingEscalation from the database when it's not in pending_escalations table
func (h *EscalationHandler) loadEscalationFromDB(ctx context.Context, interactionID, tenantID uuid.UUID, chatID int64) (*storage.PendingEscalation, error) {
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
	esc := &storage.PendingEscalation{
		InteractionID:  interaction.ID,
		TenantID:       tenantID,
		CustomerID:     customer.ID,
		Platform:       interaction.Platform,
		Message:        interaction.Content,
		SuggestedReply: interaction.SuggestedReply,
		ChatID:         chatID,
	}

	h.Logger.Info("✅ Loaded escalation from database",
		zap.Stringer("interaction_id", interactionID),
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
func (h *EscalationHandler) NotifyAftermath(ctx context.Context, tenantID uuid.UUID, customerID uuid.UUID, platform, customerMessage, originalMessage, tenantReply string) error {
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

	// Escape all user-generated content to prevent Markdown entity parsing errors
	escapedCustomerName := escapeMarkdown(customerName)
	escapedPlatform := escapeMarkdown(strings.Title(platform))
	escapedCustomerMessage := escapeMarkdown(customerMessage)
	escapedOriginalMessage := escapeMarkdown(originalMessage)
	escapedTenantReply := escapeMarkdown(tenantReply)

	aftermathMsg := fmt.Sprintf(
		"💬 *Customer Follow-up*\n\n"+
			"👤 Customer: %s\n"+
			"📱 Platform: %s\n"+
			"💬 New message: %s\n\n"+
			"📝 Previous context:\n"+
			"Original: %s\n"+
			"Your reply: %s",
		escapedCustomerName,
		escapedPlatform,
		escapedCustomerMessage,
		escapedOriginalMessage,
		escapedTenantReply,
	)

	msg := tgbotapi.NewMessage(tenant.OwnerTelegramID, aftermathMsg)
	msg.ParseMode = "Markdown"

	if _, err := bot.Send(msg); err != nil {
		return fmt.Errorf("failed to send aftermath notification: %w", err)
	}

	h.Logger.Info("📬 Sent aftermath notification",
		zap.Stringer("tenant_id", tenantID),
		zap.Stringer("customer_id", customerID))

	return nil
}
