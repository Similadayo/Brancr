package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/order"
	"github.com/similadayo/brancr/internal/receipt"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// MessageSender interface for sending replies to customers
type MessageSender interface {
	SendWhatsAppReply(ctx context.Context, tenantID uint, customerPlatformID, message string) error
	SendReply(ctx context.Context, tenantID uint, platform, threadID, customerPlatformID, message string) error
}

// EscalationWorkflow manages escalated customer messages requiring tenant attention
type EscalationWorkflow struct {
	DB            *gorm.DB
	Logger        *zap.Logger
	BotManager    interface{}   // TenantBotManager interface
	MessageSender MessageSender // Interface for sending messages to customers
}

// getBot attempts to fetch a Telegram bot for the tenant via BotManager.
func (w *EscalationWorkflow) getBot(tenantID uint) (*tgbotapi.BotAPI, bool) {
	if w.BotManager == nil {
		return nil, false
	}

	manager, ok := w.BotManager.(interface {
		GetBot(id uint) (*tgbotapi.BotAPI, bool)
	})
	if !ok {
		return nil, false
	}

	return manager.GetBot(tenantID)
}

// EscalationPriority determines the urgency of an escalation
type EscalationPriority string

const (
	EscalationPriorityLow      EscalationPriority = "low"
	EscalationPriorityNormal   EscalationPriority = "normal"
	EscalationPriorityHigh     EscalationPriority = "high"
	EscalationPriorityUrgent   EscalationPriority = "urgent"
	EscalationPriorityCritical EscalationPriority = "critical"
)

// EscalationAction represents possible actions on an escalation
type EscalationAction string

const (
	EscalationActionApprove EscalationAction = "approve" // Approve and send suggested reply
	EscalationActionEdit    EscalationAction = "edit"    // Edit suggested reply before sending
	EscalationActionCustom  EscalationAction = "custom"  // Write custom reply
	EscalationActionIgnore  EscalationAction = "ignore"  // Ignore for now
	EscalationActionResolve EscalationAction = "resolve" // Mark as resolved without reply
	EscalationActionAssign  EscalationAction = "assign"  // Assign to team member
)

// EscalationNotification formats and sends escalation notifications
type EscalationNotification struct {
	InteractionID  uint
	TenantID       uint
	CustomerID     uint
	ConversationID uint
	CustomerName   string
	Platform       string
	Message        string
	SuggestedReply string
	Intent         string
	Tone           string
	Confidence     float64
	Priority       EscalationPriority
	CreatedAt      time.Time
	PaymentMode    string
}

// CreateEscalation creates a new escalation from an interaction
func (w *EscalationWorkflow) CreateEscalation(ctx context.Context, interactionID uint, priority EscalationPriority) error {
	// Load interaction
	var interaction storage.Interaction
	if err := w.DB.First(&interaction, interactionID).Error; err != nil {
		return fmt.Errorf("interaction not found: %w", err)
	}

	// Verify it's in the right state for escalation
	if interaction.ResponseType != "pending" && interaction.ResponseType != "escalated" {
		return fmt.Errorf("interaction not eligible for escalation (type: %s)", interaction.ResponseType)
	}

	// Load customer
	var customer storage.Customer
	if err := w.DB.First(&customer, interaction.CustomerID).Error; err != nil {
		return fmt.Errorf("customer not found: %w", err)
	}

	// Load conversation
	var conversation storage.Conversation
	if interaction.ConversationID > 0 {
		if err := w.DB.First(&conversation, interaction.ConversationID).Error; err != nil {
			w.Logger.Warn("conversation not found for escalation", zap.Uint("conversation_id", interaction.ConversationID))
		}
	}

	// Determine customer name
	customerName := customer.DisplayName
	if customerName == "" {
		customerName = customer.Username
	}
	if customerName == "" {
		customerName = fmt.Sprintf("Customer (%s)", customer.PlatformID[:min(10, len(customer.PlatformID))])
	}

	// Determine priority based on intent if not specified
	if priority == "" {
		priority = w.determinePriority(interaction.DetectedIntent, interaction.DetectedTone, interaction.Confidence)
	}

	// Update interaction status
	interaction.ResponseType = "escalated"
	interaction.ResponseStatus = "pending"
	if err := w.DB.Save(&interaction).Error; err != nil {
		return fmt.Errorf("failed to update interaction: %w", err)
	}

	// Create notification
	notification := &EscalationNotification{
		InteractionID:  interaction.ID,
		TenantID:       interaction.TenantID,
		CustomerID:     customer.ID,
		ConversationID: interaction.ConversationID,
		CustomerName:   customerName,
		Platform:       interaction.Platform,
		Message:        interaction.Content,
		SuggestedReply: interaction.SuggestedReply,
		Intent:         interaction.DetectedIntent,
		Tone:           interaction.DetectedTone,
		Confidence:     interaction.Confidence,
		Priority:       priority,
		CreatedAt:      time.Now(),
	}

	// Send notification to tenant
	return w.sendEscalationNotification(ctx, notification)
}

// sendEscalationNotification sends escalation to tenant's Telegram control center
func (w *EscalationWorkflow) sendEscalationNotification(ctx context.Context, notification *EscalationNotification) error {
	// Load tenant for chat target
	var tenant storage.Tenant
	if err := w.DB.First(&tenant, notification.TenantID).Error; err != nil {
		return fmt.Errorf("tenant not found: %w", err)
	}

	if tenant.OwnerTelegramID == 0 {
		w.Logger.Warn("skipping escalation notification - no owner telegram id",
			zap.Uint("tenant_id", notification.TenantID),
			zap.Uint("interaction_id", notification.InteractionID))
		return nil
	}

	bot, ok := w.getBot(notification.TenantID)
	if !ok || bot == nil {
		w.Logger.Warn("skipping escalation notification - bot not available",
			zap.Uint("tenant_id", notification.TenantID),
			zap.Uint("interaction_id", notification.InteractionID))
		return nil
	}

	messageText := w.formatEscalationMessage(notification)
	keyboard := w.createEscalationKeyboard(notification.InteractionID, notification.Priority)
	msg := tgbotapi.NewMessage(tenant.OwnerTelegramID, messageText)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard

	if _, err := bot.Send(msg); err != nil {
		w.Logger.Warn("failed to send escalation notification",
			zap.Error(err),
			zap.Uint("tenant_id", notification.TenantID),
			zap.Uint("interaction_id", notification.InteractionID))
		return nil // soft-fail so escalation flow continues
	}

	w.Logger.Info("escalation notification sent",
		zap.Uint("tenant_id", notification.TenantID),
		zap.Uint("interaction_id", notification.InteractionID),
		zap.String("platform", notification.Platform))

	return nil
}

// formatEscalationMessage formats the escalation message for Telegram
func (w *EscalationWorkflow) formatEscalationMessage(n *EscalationNotification) string {
	var msg strings.Builder

	// Priority indicator
	priorityEmoji := w.getPriorityEmoji(n.Priority)
	msg.WriteString(fmt.Sprintf("%s *%s Escalation*\n\n", priorityEmoji, strings.Title(string(n.Priority))))

	// Customer info
	msg.WriteString(fmt.Sprintf("👤 *Customer:* %s\n", n.CustomerName))
	msg.WriteString(fmt.Sprintf("📱 *Platform:* %s\n", strings.Title(n.Platform)))
	msg.WriteString(fmt.Sprintf("⏰ *Time:* %s\n\n", n.CreatedAt.Format("Jan 2, 3:04 PM")))

	// Customer message
	msg.WriteString(fmt.Sprintf("💬 *Customer Message:*\n%s\n\n", n.Message))

	// AI Analysis
	msg.WriteString(fmt.Sprintf("🎯 *Detected Intent:* %s (%.0f%% confidence)\n", strings.Title(n.Intent), n.Confidence*100))
	msg.WriteString(fmt.Sprintf("😊 *Tone:* %s\n\n", strings.Title(n.Tone)))

	// Suggested reply
	msg.WriteString(fmt.Sprintf("💡 *Suggested Reply:*\n%s\n\n", n.SuggestedReply))

	// Priority warning for urgent escalations
	if n.Priority == EscalationPriorityUrgent || n.Priority == EscalationPriorityCritical {
		msg.WriteString(fmt.Sprintf("⚠️ *%s Priority* - Action required\n\n", strings.ToUpper(string(n.Priority))))
	}

	msg.WriteString("Use the buttons below to respond or manage this escalation.")

	return msg.String()
}

// createEscalationKeyboard creates inline keyboard for escalation actions
func (w *EscalationWorkflow) createEscalationKeyboard(interactionID uint, priority EscalationPriority) tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{}

	// Primary actions row
	primaryRow := []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("✅ Approve & Send", fmt.Sprintf("escalation:approve:%d", interactionID)),
		tgbotapi.NewInlineKeyboardButtonData("✏️ Edit Reply", fmt.Sprintf("escalation:edit:%d", interactionID)),
	}
	rows = append(rows, primaryRow)

	// Secondary actions row
	secondaryRow := []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("💬 Custom Reply", fmt.Sprintf("escalation:custom:%d", interactionID)),
		tgbotapi.NewInlineKeyboardButtonData("✅ Mark Resolved", fmt.Sprintf("escalation:resolve:%d", interactionID)),
	}
	rows = append(rows, secondaryRow)

	// Tertiary actions row
	tertiaryRow := []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("⏸️ Ignore", fmt.Sprintf("escalation:ignore:%d", interactionID)),
		tgbotapi.NewInlineKeyboardButtonData("📊 View Context", fmt.Sprintf("escalation:context:%d", interactionID)),
	}
	rows = append(rows, tertiaryRow)

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// HandleEscalationAction processes tenant's action on an escalation
func (w *EscalationWorkflow) HandleEscalationAction(ctx context.Context, tenantID uint, interactionID uint, action EscalationAction, params map[string]interface{}) error {
	// Load interaction
	var interaction storage.Interaction
	if err := w.DB.First(&interaction, interactionID).Error; err != nil {
		return fmt.Errorf("interaction not found: %w", err)
	}

	if interaction.TenantID != tenantID {
		return fmt.Errorf("unauthorized: interaction belongs to different tenant")
	}

	// Verify escalation status
	if interaction.ResponseType != "escalated" || interaction.ResponseStatus != "pending" {
		return fmt.Errorf("interaction not in pending escalation state")
	}

	switch action {
	case EscalationActionApprove:
		return w.approveEscalation(ctx, &interaction)
	case EscalationActionEdit:
		return w.editEscalation(ctx, &interaction, params)
	case EscalationActionCustom:
		return w.customReplyEscalation(ctx, &interaction, params)
	case EscalationActionIgnore:
		return w.ignoreEscalation(ctx, &interaction)
	case EscalationActionResolve:
		return w.resolveEscalation(ctx, &interaction)
	default:
		return fmt.Errorf("unknown escalation action: %s", action)
	}
}

// approveEscalation approves and sends the suggested reply
func (w *EscalationWorkflow) approveEscalation(ctx context.Context, interaction *storage.Interaction) error {
	if interaction.SuggestedReply == "" {
		return fmt.Errorf("no suggested reply to approve")
	}

	reply := interaction.SuggestedReply

	// Attempt to create an order record when approving an escalation that looks like an order/payment
	if err := w.createOrderIfPossible(ctx, interaction); err != nil {
		w.Logger.Warn("failed to create order on escalation approval", zap.Error(err))
	}

	return w.sendReplyToCustomer(ctx, interaction, reply)
}

// editEscalation allows tenant to edit the suggested reply
func (w *EscalationWorkflow) editEscalation(ctx context.Context, interaction *storage.Interaction, params map[string]interface{}) error {
	editedReply, ok := params["reply"].(string)
	if !ok || editedReply == "" {
		return fmt.Errorf("edited reply not provided")
	}

	return w.sendReplyToCustomer(ctx, interaction, editedReply)
}

// customReplyEscalation sends a custom reply from tenant
func (w *EscalationWorkflow) customReplyEscalation(ctx context.Context, interaction *storage.Interaction, params map[string]interface{}) error {
	customReply, ok := params["reply"].(string)
	if !ok || customReply == "" {
		return fmt.Errorf("custom reply not provided")
	}

	// If this is an order/payment related escalation, create order if possible
	if err := w.createOrderIfPossible(ctx, interaction); err != nil {
		w.Logger.Warn("failed to create order on custom reply", zap.Error(err))
	}

	return w.sendReplyToCustomer(ctx, interaction, customReply)
}

// ignoreEscalation ignores the escalation (keeps it in pending state but doesn't send reply)
func (w *EscalationWorkflow) ignoreEscalation(ctx context.Context, interaction *storage.Interaction) error {
	// Mark as ignored but keep in pending state
	interaction.ResponseStatus = "ignored"
	now := time.Now()
	interaction.EscalatedAt = &now

	if err := w.DB.Save(interaction).Error; err != nil {
		return fmt.Errorf("failed to update interaction: %w", err)
	}

	w.Logger.Info("escalation ignored",
		zap.Uint("interaction_id", interaction.ID),
		zap.Uint("tenant_id", interaction.TenantID),
	)

	return nil
}

// resolveEscalation marks escalation as resolved and sends receipt if it's a payment
func (w *EscalationWorkflow) resolveEscalation(ctx context.Context, interaction *storage.Interaction) error {
	interaction.ResponseType = "manual"
	interaction.ResponseStatus = "resolved"
	now := time.Now()
	interaction.EscalatedAt = &now

	// Update conversation status if exists
	if interaction.ConversationID > 0 {
		var conversation storage.Conversation
		if err := w.DB.First(&conversation, interaction.ConversationID).Error; err == nil {
			conversation.Status = "resolved"
			w.DB.Save(&conversation)
		}
	}

	if err := w.DB.Save(interaction).Error; err != nil {
		return fmt.Errorf("failed to update interaction: %w", err)
	}

	// If this was a payment escalation, send receipt/invoice and ensure order exists
	if interaction.DetectedIntent == "payment" {
		if err := w.createOrderIfPossible(ctx, interaction); err != nil {
			w.Logger.Warn("failed to ensure order on payment resolution", zap.Error(err))
		}
		if err := w.sendPaymentReceipt(ctx, interaction); err != nil {
			w.Logger.Error("failed to send payment receipt",
				zap.Error(err),
				zap.Uint("interaction_id", interaction.ID))
			// Don't fail the whole operation
		}
	}

	w.Logger.Info("escalation resolved",
		zap.Uint("interaction_id", interaction.ID),
		zap.Uint("tenant_id", interaction.TenantID),
	)

	return nil
}

// sendPaymentReceipt sends payment confirmation receipt to customer
func (w *EscalationWorkflow) sendPaymentReceipt(ctx context.Context, interaction *storage.Interaction) error {
	// Find the payment record
	var payment storage.Payment
	err := w.DB.Where("tenant_id = ? AND customer_id = ? AND status IN ?",
		interaction.TenantID, interaction.CustomerID, []string{"verified", "confirmed"}).
		Order("created_at DESC").
		First(&payment).Error

	if err != nil {
		w.Logger.Warn("no payment record found for receipt",
			zap.Uint("tenant_id", interaction.TenantID),
			zap.Uint("customer_id", interaction.CustomerID))
		return nil // Not an error, just no payment to receipt
	}

	publicURL := os.Getenv("PUBLIC_URL")
	if publicURL == "" {
		publicURL = "https://api.brancr.com"
	}

	// Ensure we have a receipt URL (generate if missing)
	if payment.ReceiptURL == "" || payment.ReceiptID == "" {
		cloudinaryURL := os.Getenv("CLOUDINARY_URL")
		receiptService := receipt.NewReceiptService(w.DB, w.Logger, cloudinaryURL, publicURL, nil)
		if generated, err := receiptService.GenerateReceipt(ctx, payment.ID, payment.TenantID); err == nil {
			payment = *generated
		} else {
			w.Logger.Warn("failed to generate receipt url", zap.Error(err), zap.Uint("payment_id", payment.ID))
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
	if err := w.DB.First(&customer, interaction.CustomerID).Error; err != nil {
		return fmt.Errorf("customer not found: %w", err)
	}

	// Use conversation thread ID if available (needed for IG/FB replies)
	threadID := customer.PlatformID
	if interaction.ConversationID > 0 {
		var conversation storage.Conversation
		if err := w.DB.First(&conversation, interaction.ConversationID).Error; err == nil {
			if conversation.PlatformThreadID != "" {
				threadID = conversation.PlatformThreadID
			}
		}
	}

	// Send receipt via MessageSender
	if w.MessageSender != nil {
		switch interaction.Platform {
		case "whatsapp":
			if err := w.MessageSender.SendWhatsAppReply(ctx, interaction.TenantID, customer.PlatformID, receiptMessage); err != nil {
				return fmt.Errorf("failed to send receipt: %w", err)
			}
		case "instagram", "facebook", "telegram":
			if err := w.MessageSender.SendReply(ctx, interaction.TenantID, interaction.Platform, threadID, customer.PlatformID, receiptMessage); err != nil {
				return fmt.Errorf("failed to send receipt: %w", err)
			}
		default:
			w.Logger.Warn("receipt sending not implemented for platform",
				zap.String("platform", interaction.Platform),
				zap.Uint("interaction_id", interaction.ID))
		}

		// Update payment with receipt info for supported platforms
		if interaction.Platform == "whatsapp" || interaction.Platform == "instagram" || interaction.Platform == "facebook" || interaction.Platform == "telegram" {
			payment.ReceiptID = fmt.Sprintf("RCP-%s", payment.PaymentReference)
			w.DB.Save(&payment)

			w.Logger.Info("payment receipt sent",
				zap.Uint("payment_id", payment.ID),
				zap.String("receipt_id", payment.ReceiptID),
				zap.String("platform", interaction.Platform))
		}
	}

	return nil
}

// sendReplyToCustomer sends the reply to customer and updates interaction
func (w *EscalationWorkflow) sendReplyToCustomer(ctx context.Context, interaction *storage.Interaction, reply string) error {
	// Validate reply is not empty
	if reply == "" {
		return fmt.Errorf("cannot send empty reply")
	}

	// Load conversation for thread ID
	var conversation storage.Conversation
	if interaction.ConversationID > 0 {
		_ = w.DB.First(&conversation, interaction.ConversationID).Error
	}

	// Load customer for platform ID
	var customer storage.Customer
	if err := w.DB.First(&customer, interaction.CustomerID).Error; err != nil {
		return fmt.Errorf("customer not found: %w", err)
	}

	// Prefer conversation thread ID; fallback to customer platform ID
	threadID := conversation.PlatformThreadID
	if threadID == "" {
		threadID = customer.PlatformID
	}

	// Actually send reply via social media API; soft-fail so we still record the outgoing interaction
	sendErr := error(nil)
	if w.MessageSender != nil {
		switch interaction.Platform {
		case "whatsapp":
			if err := w.MessageSender.SendWhatsAppReply(ctx, interaction.TenantID, customer.PlatformID, reply); err != nil {
				sendErr = err
				w.Logger.Warn("failed to send reply to customer",
					zap.Error(err),
					zap.Uint("interaction_id", interaction.ID),
					zap.String("customer_platform_id", customer.PlatformID))
			}
		case "instagram", "facebook":
			if err := w.MessageSender.SendReply(ctx, interaction.TenantID, interaction.Platform, threadID, customer.PlatformID, reply); err != nil {
				sendErr = err
				w.Logger.Warn("failed to send reply to customer",
					zap.Error(err),
					zap.Uint("interaction_id", interaction.ID),
					zap.String("customer_platform_id", customer.PlatformID),
					zap.String("thread_id", threadID))
			}
		default:
			w.Logger.Warn("message sender not configured or unsupported platform",
				zap.String("platform", interaction.Platform))
		}
	} else {
		w.Logger.Warn("message sender not configured",
			zap.String("platform", interaction.Platform))
	}

	// Update interaction
	interaction.ResponseType = "manual"
	if sendErr != nil {
		interaction.ResponseStatus = "delivery_failed"
	} else {
		interaction.ResponseStatus = "sent"
	}
	interaction.FinalReply = reply
	now := time.Now()
	interaction.EscalatedAt = &now

	if err := w.DB.Save(interaction).Error; err != nil {
		return fmt.Errorf("failed to update interaction: %w", err)
	}

	// Update conversation
	if interaction.ConversationID > 0 {
		conversation.Status = "waiting"
		conversation.LastMessageAt = time.Now()
		w.DB.Save(&conversation)
	}

	// Create outgoing interaction record
	metadata := "{}"
	if sendErr != nil {
		metadataBytes, _ := json.Marshal(map[string]interface{}{
			"delivery_failed": true,
			"error":           sendErr.Error(),
		})
		metadata = string(metadataBytes)
	}

	outgoingInteraction := storage.Interaction{
		TenantID:       interaction.TenantID,
		CustomerID:     interaction.CustomerID,
		ConversationID: interaction.ConversationID,
		Platform:       interaction.Platform,
		Direction:      "outgoing",
		MessageType:    "text",
		Content:        reply,
		ResponseType:   "manual",
		ResponseStatus: interaction.ResponseStatus,
		FinalReply:     reply,
		Metadata:       metadata,
		CreatedAt:      time.Now(),
	}

	if err := w.DB.Create(&outgoingInteraction).Error; err != nil {
		w.Logger.Warn("failed to create outgoing interaction", zap.Error(err))
		return fmt.Errorf("failed to create outgoing interaction: %w", err)
	}

	// If this was a payment confirmation, create/update payment record
	if interaction.DetectedIntent == "payment" && strings.Contains(strings.ToLower(interaction.Content), "paid") {
		if err := w.handlePaymentConfirmation(ctx, interaction); err != nil {
			w.Logger.Error("failed to handle payment confirmation",
				zap.Error(err),
				zap.Uint("interaction_id", interaction.ID))
			// Don't fail the whole operation, just log it
		}
	}

	w.Logger.Info("reply sent to customer",
		zap.Uint("interaction_id", interaction.ID),
		zap.Uint("tenant_id", interaction.TenantID),
		zap.Uint("customer_id", interaction.CustomerID),
	)

	return nil
}

// handlePaymentConfirmation creates/updates payment record when tenant approves payment
func (w *EscalationWorkflow) handlePaymentConfirmation(ctx context.Context, interaction *storage.Interaction) error {
	if interaction == nil {
		return nil
	}

	// Prefer attaching payment confirmation to an unambiguous recent pending order.
	var pending []storage.Order
	if err := w.DB.
		Where("tenant_id = ? AND customer_id = ? AND platform = ? AND status = ? AND total_amount > 0", interaction.TenantID, interaction.CustomerID, interaction.Platform, "pending").
		Where("created_at > ?", time.Now().Add(-7*24*time.Hour)).
		Order("created_at DESC").
		Limit(2).
		Find(&pending).Error; err == nil {
		if len(pending) == 1 {
			now := time.Now()
			var payment storage.Payment
			if err := w.DB.Where("tenant_id = ? AND order_id = ?", pending[0].TenantID, pending[0].ID).First(&payment).Error; err == nil {
				payment.Status = "verified"
				payment.VerificationStatus = "tenant_verified"
				payment.ClaimedByCustomer = true
				payment.ClaimedAt = &now
				payment.VerifiedAt = &now
				payment.CompletedAt = &now
				if payment.Amount <= 0 {
					payment.Amount = pending[0].TotalAmount
				}
				if payment.Currency == "" {
					payment.Currency = pending[0].Currency
					if payment.Currency == "" {
						payment.Currency = "NGN"
					}
				}
				if payment.PortalToken == "" {
					payment.PortalToken = utils.GeneratePortalToken()
				}
				if payment.ReceiptID == "" {
					payment.ReceiptID = utils.GenerateReceiptID()
				}
				_ = w.DB.Save(&payment).Error
			} else {
				payment = storage.Payment{
					TenantID:           pending[0].TenantID,
					OrderID:            pending[0].ID,
					CustomerID:         pending[0].CustomerID,
					PaymentReference:   pending[0].PaymentReference,
					Amount:             pending[0].TotalAmount,
					Currency:           pending[0].Currency,
					PaymentMethod:      "bank_transfer",
					Status:             "verified",
					VerificationStatus: "tenant_verified",
					ClaimedByCustomer:  true,
					ClaimedAt:          &now,
					VerifiedAt:         &now,
					CompletedAt:        &now,
					Metadata:           fmt.Sprintf(`{"interaction_id":%d}`, interaction.ID),
					CreatedAt:          now,
				}
				if payment.Currency == "" {
					payment.Currency = "NGN"
				}
				if payment.PortalToken == "" {
					payment.PortalToken = utils.GeneratePortalToken()
				}
				if payment.ReceiptID == "" {
					payment.ReceiptID = utils.GenerateReceiptID()
				}
				_ = w.DB.Create(&payment).Error
			}

			_ = w.DB.Model(&storage.Order{}).
				Where("id = ?", pending[0].ID).
				Updates(map[string]interface{}{"status": "completed", "completed_at": &now}).Error

			w.Logger.Info("payment confirmation attached to pending order",
				zap.Uint("interaction_id", interaction.ID),
				zap.Uint("order_id", pending[0].ID),
				zap.Uint("tenant_id", interaction.TenantID),
				zap.Uint("customer_id", interaction.CustomerID))
			return nil
		}
		if len(pending) > 1 {
			w.Logger.Warn("payment confirmation ambiguous: multiple pending orders; skipping auto-link",
				zap.Uint("interaction_id", interaction.ID),
				zap.Uint("tenant_id", interaction.TenantID),
				zap.Uint("customer_id", interaction.CustomerID),
				zap.String("platform", interaction.Platform),
				zap.Uint("most_recent_order_id", pending[0].ID))
			return nil
		}
	}

	// Fallback: if there's already a pending payment for this customer, verify it.
	var existingPayment storage.Payment
	if err := w.DB.Where("tenant_id = ? AND customer_id = ? AND status = ? AND amount > 0",
		interaction.TenantID, interaction.CustomerID, "pending").
		Order("created_at DESC").
		First(&existingPayment).Error; err == nil {
		now := time.Now()
		existingPayment.Status = "verified"
		existingPayment.VerificationStatus = "tenant_verified"
		existingPayment.ClaimedByCustomer = true
		existingPayment.ClaimedAt = &now
		existingPayment.VerifiedAt = &now
		existingPayment.CompletedAt = &now
		if existingPayment.PortalToken == "" {
			existingPayment.PortalToken = utils.GeneratePortalToken()
		}
		if existingPayment.ReceiptID == "" {
			existingPayment.ReceiptID = utils.GenerateReceiptID()
		}
		_ = w.DB.Save(&existingPayment).Error
		return nil
	}

	// No safe amount/order to attach to; do not create a zero-amount payment.
	w.Logger.Warn("payment confirmation had no attachable order/amount; skipping payment creation",
		zap.Uint("interaction_id", interaction.ID),
		zap.Uint("tenant_id", interaction.TenantID),
		zap.Uint("customer_id", interaction.CustomerID),
		zap.String("platform", interaction.Platform))
	return nil
}

// GetPendingEscalations retrieves all pending escalations for a tenant
func (w *EscalationWorkflow) GetPendingEscalations(ctx context.Context, tenantID uint, priority EscalationPriority, limit int) ([]storage.Interaction, error) {
	query := w.DB.Where("tenant_id = ? AND response_type = ? AND response_status = ?", tenantID, "escalated", "pending")

	var interactions []storage.Interaction
	if err := query.Order("created_at DESC").Limit(limit).Find(&interactions).Error; err != nil {
		return nil, fmt.Errorf("failed to load escalations: %w", err)
	}

	// Filter by priority if specified
	if priority != "" {
		filtered := []storage.Interaction{}
		for _, interaction := range interactions {
			interactionPriority := w.determinePriority(interaction.DetectedIntent, interaction.DetectedTone, interaction.Confidence)
			if interactionPriority == priority {
				filtered = append(filtered, interaction)
			}
		}
		return filtered, nil
	}

	return interactions, nil
}

// determinePriority determines escalation priority based on intent, tone, and confidence
func (w *EscalationWorkflow) determinePriority(intent string, tone string, confidence float64) EscalationPriority {
	// Critical: payment/refund with high confidence
	if (intent == "payment" || intent == "refund") && confidence > 0.8 {
		return EscalationPriorityCritical
	}

	// Urgent: complaint with negative tone
	if intent == "complaint" && (tone == "negative" || tone == "urgent") {
		return EscalationPriorityUrgent
	}

	// High: refund, delivery issues
	if intent == "refund" || intent == "delivery_issue" {
		return EscalationPriorityHigh
	}

	// Normal: inquiry, order questions
	if intent == "inquiry" || intent == "order" {
		return EscalationPriorityNormal
	}

	// Low: casual messages
	if intent == "casual" {
		return EscalationPriorityLow
	}

	// Default to normal
	return EscalationPriorityNormal
}

// getPriorityEmoji returns emoji for priority level
func (w *EscalationWorkflow) getPriorityEmoji(priority EscalationPriority) string {
	switch priority {
	case EscalationPriorityCritical:
		return "🚨"
	case EscalationPriorityUrgent:
		return "⚠️"
	case EscalationPriorityHigh:
		return "🔴"
	case EscalationPriorityNormal:
		return "🟡"
	case EscalationPriorityLow:
		return "🟢"
	default:
		return "📌"
	}
}

// inferPaymentMode derives a coarse payment mode from the customer message
func inferPaymentMode(message string) string {
	m := strings.ToLower(message)
	switch {
	case strings.Contains(m, "pay on delivery") || strings.Contains(m, "cash on delivery") || strings.Contains(m, "pod"):
		return "pay_on_delivery"
	case strings.Contains(m, "bank transfer") || strings.Contains(m, "transfer") || strings.Contains(m, "send account") || strings.Contains(m, "account number"):
		return "bank_transfer"
	default:
		return "unknown"
	}
}

// inferOrderCue checks for quick order intent cues in free text
func inferOrderCue(message string) bool {
	m := strings.ToLower(message)
	return strings.Contains(m, "order") || strings.Contains(m, "i'll take") || strings.Contains(m, "i want one") || strings.Contains(m, "add one")
}

// createOrderIfPossible tries to build an order from the sales conversation when escalations are approved/resolved
func (w *EscalationWorkflow) createOrderIfPossible(ctx context.Context, interaction *storage.Interaction) error {
	if interaction == nil {
		return nil
	}

	var salesConv storage.SalesConversation
	if interaction.ConversationID != 0 {
		_ = w.DB.Where("conversation_id = ?", interaction.ConversationID).Order("created_at DESC").First(&salesConv).Error
	}
	if salesConv.ID == 0 {
		// Fallback: some interactions don't have ConversationID populated.
		if err := w.DB.Where("tenant_id = ? AND customer_id = ?", interaction.TenantID, interaction.CustomerID).
			Order("updated_at DESC").
			First(&salesConv).Error; err != nil {
			return nil
		}
	}

	// Extract draft order if present
	var draft ai.OrderDraft
	if salesConv.PendingOrderDraft != "" {
		_ = json.Unmarshal([]byte(salesConv.PendingOrderDraft), &draft)
		w.Logger.Info("order draft parsed",
			zap.Uint("interaction_id", interaction.ID),
			zap.Uint("conversation_id", interaction.ConversationID),
			zap.Int("draft_item_count", len(draft.Items)),
			zap.Float64("draft_total", draft.TotalAmount),
			zap.String("draft_currency", draft.Currency),
			zap.String("draft_payment_mode", draft.PaymentMode))
	}

	items := draft.Items
	currency := draft.Currency
	if currency == "" {
		currency = "NGN"
	}
	total := draft.TotalAmount
	amountSource := ""
	if total > 0 {
		amountSource = "draft.total_amount"
	}
	if total <= 0 && len(items) > 0 {
		for _, it := range items {
			total += it.TotalPrice
		}
		if total > 0 {
			amountSource = "draft.items_sum"
		}
	}
	paymentMode := draft.PaymentMode

	logAmountDecisions := os.Getenv("BRANCR_LOG_AMOUNT_DECISIONS") == "1"
	amountFromContent := 0.0
	amountFromReplies := 0.0
	fallbackExtractTotal := 0.0
	fallbackExtractItemCount := 0
	if logAmountDecisions {
		amountFromContent = utils.ExtractLikelyAmountNGN(interaction.Content)
		amountFromReplies = utils.ExtractLikelyAmountNGN(interaction.FinalReply + " " + interaction.SuggestedReply)
		w.Logger.Info("order amount decision (initial)",
			zap.Uint("interaction_id", interaction.ID),
			zap.Uint("conversation_id", interaction.ConversationID),
			zap.Uint("tenant_id", interaction.TenantID),
			zap.Uint("customer_id", interaction.CustomerID),
			zap.String("platform", interaction.Platform),
			zap.Int("draft_item_count", len(draft.Items)),
			zap.Float64("draft_total_amount", draft.TotalAmount),
			zap.String("draft_currency", draft.Currency),
			zap.Float64("draft_items_sum_total", total),
			zap.Float64("salesconv_price_discussed", salesConv.PriceDiscussed),
			zap.Float64("amount_from_content", amountFromContent),
			zap.Float64("amount_from_replies", amountFromReplies),
			zap.String("current_amount_source", amountSource))
	}

	// Fallback to extracting from sales conversation if draft missing
	if len(items) == 0 || total <= 0 {
		os := order.NewOrderService(w.DB, w.Logger)
		fallbackItems, fallbackTotal, err := os.ExtractOrderItems(&salesConv)
		if err == nil {
			items = fallbackItems
			total = fallbackTotal
			amountSource = "sales_conversation.extract_order_items"
			fallbackExtractTotal = fallbackTotal
			fallbackExtractItemCount = len(fallbackItems)
			if currency == "" {
				currency = "NGN"
			}
			w.Logger.Info("order payload extracted from sales conversation",
				zap.Uint("interaction_id", interaction.ID),
				zap.Uint("conversation_id", interaction.ConversationID),
				zap.Int("item_count", len(items)),
				zap.Float64("total", total),
				zap.String("currency", currency))
		} else {
			w.Logger.Warn("order payload: failed to extract from sales conversation",
				zap.Uint("interaction_id", interaction.ID),
				zap.Uint("conversation_id", interaction.ConversationID),
				zap.Error(err))
		}
	}

	// Final fallback: create a single-line manual item to ensure order persistence
	if len(items) == 0 || total <= 0 {
		fallbackTotal := salesConv.PriceDiscussed
		if fallbackTotal <= 0 {
			fallbackTotal = utils.ExtractLikelyAmountNGN(interaction.Content)
		}
		if fallbackTotal <= 0 {
			fallbackTotal = utils.ExtractLikelyAmountNGN(interaction.FinalReply + " " + interaction.SuggestedReply)
		}
		if fallbackTotal <= 0 {
			// If we can't infer an amount from the current message, try to reuse a single recent pending order.
			// This is common on payment-confirmation messages like "I've paid" which contain no amount.
			var pending []storage.Order
			if err := w.DB.
				Where("tenant_id = ? AND customer_id = ? AND platform = ? AND status = ? AND total_amount > 0", interaction.TenantID, interaction.CustomerID, interaction.Platform, "pending").
				Where("created_at > ?", time.Now().Add(-7*24*time.Hour)).
				Order("created_at DESC").
				Limit(2).
				Find(&pending).Error; err == nil {
				if len(pending) == 1 {
					var existing storage.Payment
					if err := w.DB.Where("tenant_id = ? AND order_id = ?", pending[0].TenantID, pending[0].ID).First(&existing).Error; err != nil {
						now := time.Now()
						p := storage.Payment{
							TenantID:           pending[0].TenantID,
							OrderID:            pending[0].ID,
							CustomerID:         pending[0].CustomerID,
							PaymentReference:   pending[0].PaymentReference,
							Amount:             pending[0].TotalAmount,
							Currency:           pending[0].Currency,
							PaymentMethod:      "unknown",
							Status:             "pending",
							VerificationStatus: "unverified",
							Metadata:           "{}",
							CreatedAt:          now,
						}
						if p.Currency == "" {
							p.Currency = "NGN"
						}
						if p.PortalToken == "" {
							p.PortalToken = utils.GeneratePortalToken()
						}
						if p.ReceiptID == "" {
							p.ReceiptID = utils.GenerateReceiptID()
						}
						_ = w.DB.Create(&p).Error
					}
					w.Logger.Info("createOrderIfPossible: reused existing pending order (no amount inferred)",
						zap.Uint("interaction_id", interaction.ID),
						zap.Uint("tenant_id", interaction.TenantID),
						zap.Uint("customer_id", interaction.CustomerID),
						zap.Uint("order_id", pending[0].ID),
						zap.String("platform", interaction.Platform),
						zap.Float64("order_total", pending[0].TotalAmount))
					return nil
				}
				if len(pending) > 1 {
					w.Logger.Warn("createOrderIfPossible: multiple pending orders found; skipping auto-link",
						zap.Uint("interaction_id", interaction.ID),
						zap.Uint("tenant_id", interaction.TenantID),
						zap.Uint("customer_id", interaction.CustomerID),
						zap.String("platform", interaction.Platform),
						zap.Uint("most_recent_order_id", pending[0].ID))
				}
			}

			w.Logger.Warn("order payload missing details; skipping auto-order creation (no amount inferred)",
				zap.Uint("interaction_id", interaction.ID),
				zap.Uint("conversation_id", interaction.ConversationID),
				zap.Uint("tenant_id", interaction.TenantID),
				zap.Uint("customer_id", interaction.CustomerID),
				zap.String("platform", interaction.Platform),
				zap.Float64("price_discussed", salesConv.PriceDiscussed))
			return nil
		}
		amountSource = "fallback.manual_item"
		items = []storage.OrderItem{
			{
				Name:       "Order",
				Quantity:   1,
				UnitPrice:  fallbackTotal,
				TotalPrice: fallbackTotal,
			},
		}
		total = fallbackTotal
		if currency == "" {
			currency = "NGN"
		}
		w.Logger.Warn("order payload missing details; using fallback pricing",
			zap.Uint("interaction_id", interaction.ID),
			zap.Uint("conversation_id", interaction.ConversationID),
			zap.Float64("fallback_total", fallbackTotal),
			zap.Float64("price_discussed", salesConv.PriceDiscussed))
	}

	if logAmountDecisions {
		w.Logger.Info("order amount decision (final)",
			zap.Uint("interaction_id", interaction.ID),
			zap.Uint("conversation_id", interaction.ConversationID),
			zap.Uint("tenant_id", interaction.TenantID),
			zap.Uint("customer_id", interaction.CustomerID),
			zap.String("platform", interaction.Platform),
			zap.String("currency", currency),
			zap.Int("final_item_count", len(items)),
			zap.Float64("final_total", total),
			zap.String("amount_source", amountSource),
			zap.Float64("salesconv_price_discussed", salesConv.PriceDiscussed),
			zap.Float64("extract_order_items_total", fallbackExtractTotal),
			zap.Int("extract_order_items_count", fallbackExtractItemCount),
			zap.Float64("amount_from_content", amountFromContent),
			zap.Float64("amount_from_replies", amountFromReplies))
	}

	// Prevent duplicate orders created recently for the same customer on the same platform
	var recent storage.Order
	if err := w.DB.Where("tenant_id = ? AND customer_id = ? AND platform = ?", interaction.TenantID, interaction.CustomerID, interaction.Platform).
		Where("created_at > ?", time.Now().Add(-10*time.Minute)).
		Order("created_at DESC").First(&recent).Error; err == nil {
		return nil
	}

	// Load customer for metadata
	var customer storage.Customer
	if err := w.DB.First(&customer, interaction.CustomerID).Error; err != nil {
		w.Logger.Warn("customer not found when creating order", zap.Uint("customer_id", interaction.CustomerID))
	}
	customerName := customer.DisplayName
	if customerName == "" {
		customerName = customer.Username
	}
	if customerName == "" {
		customerName = fmt.Sprintf("Customer-%d", interaction.CustomerID)
	}

	os := order.NewOrderService(w.DB, w.Logger)
	orderNumber, err := os.GenerateOrderNumber(ctx, interaction.TenantID)
	if err != nil {
		return err
	}

	var tenant storage.Tenant
	_ = w.DB.Preload("BusinessProfile").First(&tenant, interaction.TenantID).Error
	itemLabel := ""
	if len(items) > 0 {
		itemLabel = items[0].Name
	}
	businessName := tenant.BusinessProfile.Name
	if businessName == "" {
		businessName = tenant.Name
	}
	paymentRef := utils.GeneratePaymentReference(businessName, interaction.TenantID, itemLabel)

	itemsJSON, _ := json.Marshal(items)
	orderStatus := "pending"
	var confirmedAt *time.Time
	var completedAt *time.Time
	if paymentMode == "paid" {
		orderStatus = "completed"
		now := time.Now()
		confirmedAt = &now
		completedAt = &now
	}

	orderRec := storage.Order{
		TenantID:         interaction.TenantID,
		CustomerID:       interaction.CustomerID,
		OrderNumber:      orderNumber,
		PaymentReference: paymentRef,
		Status:           orderStatus,
		TotalAmount:      total,
		Currency:         currency,
		Items:            string(itemsJSON),
		CustomerName:     customerName,
		CustomerPhone:    customer.PlatformID,
		Platform:         interaction.Platform,
		ConfirmedAt:      confirmedAt,
		CompletedAt:      completedAt,
	}

	if err := w.DB.Create(&orderRec).Error; err != nil {
		return err
	}

	now := time.Now()
	paymentStatus := "pending"
	verificationStatus := "tenant_verified"
	method := "cash_on_delivery"
	claimed := false
	var verifiedAt *time.Time

	if paymentMode == "paid" {
		paymentStatus = "verified"
		method = "bank_transfer"
		claimed = true
		verifiedAt = &now
		completedAt = &now
	}

	payment := storage.Payment{
		TenantID:           interaction.TenantID,
		OrderID:            orderRec.ID,
		CustomerID:         interaction.CustomerID,
		PaymentReference:   orderRec.PaymentReference,
		Amount:             total,
		Currency:           currency,
		PaymentMethod:      method,
		Status:             paymentStatus,
		VerificationStatus: verificationStatus,
		ClaimedByCustomer:  claimed,
		ClaimedAt:          &now,
		VerifiedAt:         verifiedAt,
		CompletedAt:        completedAt,
		Metadata:           "{}",
	}

	// Prevent unique constraint collisions on portal_token by generating one if empty.
	if payment.PortalToken == "" {
		payment.PortalToken = utils.GeneratePortalToken()
	}
	// Ensure receipt id is unique per payment
	if payment.ReceiptID == "" {
		payment.ReceiptID = utils.GenerateReceiptID()
	}

	if err := w.DB.Create(&payment).Error; err != nil {
		w.Logger.Warn("failed to auto-create payment for order",
			zap.Error(err),
			zap.Uint("order_id", orderRec.ID),
			zap.String("payment_reference", orderRec.PaymentReference),
			zap.Float64("amount", total),
			zap.String("currency", currency))
	}

	w.Logger.Info("order created from escalation approval",
		zap.Uint("order_id", orderRec.ID),
		zap.String("order_number", orderRec.OrderNumber),
		zap.Uint("tenant_id", interaction.TenantID),
		zap.Uint("customer_id", interaction.CustomerID))

	return nil
}

// GetEscalationStats returns statistics about escalations for a tenant
func (w *EscalationWorkflow) GetEscalationStats(ctx context.Context, tenantID uint, startDate, endDate time.Time) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Total escalations (all time, or within date range if specified)
	var totalCount int64
	totalQuery := w.DB.Model(&storage.Interaction{}).
		Where("tenant_id = ? AND response_type = ?", tenantID, "escalated")
	if !startDate.IsZero() && !endDate.IsZero() {
		totalQuery = totalQuery.Where("created_at BETWEEN ? AND ?", startDate, endDate)
	}
	totalQuery.Count(&totalCount)
	stats["total"] = totalCount

	// Pending escalations (all time - not filtered by date)
	var pendingCount int64
	w.DB.Model(&storage.Interaction{}).
		Where("tenant_id = ? AND response_type = ? AND response_status = ?", tenantID, "escalated", "pending").
		Count(&pendingCount)
	stats["pending"] = pendingCount

	// Resolved escalations (within date range if specified, otherwise all time)
	var resolvedCount int64
	resolvedQuery := w.DB.Model(&storage.Interaction{}).
		Where("tenant_id = ? AND response_type = ? AND response_status = ?", tenantID, "escalated", "resolved")
	if !startDate.IsZero() && !endDate.IsZero() {
		resolvedQuery = resolvedQuery.Where("created_at BETWEEN ? AND ?", startDate, endDate)
	}
	resolvedQuery.Count(&resolvedCount)
	stats["resolved"] = resolvedCount

	// Average response time - calculate from escalated_at to when response was sent
	var avgResponseTime string = "N/A"
	var interactions []storage.Interaction
	avgQuery := w.DB.Model(&storage.Interaction{}).
		Where("tenant_id = ? AND response_type = ? AND response_status IN ? AND escalated_at IS NOT NULL",
			tenantID, "escalated", []string{"sent", "resolved"})
	if !startDate.IsZero() && !endDate.IsZero() {
		avgQuery = avgQuery.Where("created_at BETWEEN ? AND ?", startDate, endDate)
	}
	if err := avgQuery.Find(&interactions).Error; err == nil && len(interactions) > 0 {
		var totalDuration time.Duration
		validCount := 0
		for _, inter := range interactions {
			if inter.EscalatedAt != nil {
				// Find when response was sent (check for outgoing interaction)
				var outgoing storage.Interaction
				if err := w.DB.Where("conversation_id = ? AND direction = ? AND created_at > ?",
					inter.ConversationID, "outgoing", *inter.EscalatedAt).
					Order("created_at ASC").
					First(&outgoing).Error; err == nil {
					duration := outgoing.CreatedAt.Sub(*inter.EscalatedAt)
					if duration > 0 {
						totalDuration += duration
						validCount++
					}
				}
			}
		}
		if validCount > 0 {
			avgDuration := totalDuration / time.Duration(validCount)
			if avgDuration < time.Hour {
				avgResponseTime = fmt.Sprintf("%.0f min", avgDuration.Minutes())
			} else {
				avgResponseTime = fmt.Sprintf("%.1f hrs", avgDuration.Hours())
			}
		}
	}
	stats["avg_response_time"] = avgResponseTime

	return stats, nil
}
