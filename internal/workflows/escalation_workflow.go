package workflows

import (
	"context"
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// MessageSender interface for sending replies to customers
type MessageSender interface {
	SendWhatsAppReply(ctx context.Context, tenantID uint, customerPlatformID, message string) error
}

// EscalationWorkflow manages escalated customer messages requiring tenant attention
type EscalationWorkflow struct {
	DB            *gorm.DB
	Logger        *zap.Logger
	BotManager    interface{}   // TenantBotManager interface
	MessageSender MessageSender // Interface for sending messages to customers
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
	CustomerName   string
	Platform       string
	Message        string
	SuggestedReply string
	Intent         string
	Tone           string
	Confidence     float64
	Priority       EscalationPriority
	CreatedAt      time.Time
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
	// Load tenant
	var tenant storage.Tenant
	if err := w.DB.First(&tenant, notification.TenantID).Error; err != nil {
		return fmt.Errorf("tenant not found: %w", err)
	}

	// Format escalation message - stored in notification for later use
	_ = w.formatEscalationMessage(notification)

	// Create inline keyboard - stored for later use
	_ = w.createEscalationKeyboard(notification.InteractionID, notification.Priority)

	// Send via Telegram bot
	// This would require access to the bot instance
	// For now, we'll log the notification structure

	w.Logger.Info("escalation notification created",
		zap.Uint("tenant_id", notification.TenantID),
		zap.Uint("interaction_id", notification.InteractionID),
		zap.String("priority", string(notification.Priority)),
		zap.String("intent", notification.Intent),
	)

	// TODO: Actually send Telegram message
	// bot.Send(tgbotapi.NewMessage(tenant.OwnerTelegramID, message).WithReplyMarkup(keyboard))

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

	// If this was a payment escalation, send receipt/invoice
	if interaction.DetectedIntent == "payment" {
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
	if err := w.DB.First(&customer, interaction.CustomerID).Error; err != nil {
		return fmt.Errorf("customer not found: %w", err)
	}

	// Send receipt via MessageSender
	if w.MessageSender != nil && interaction.Platform == "whatsapp" {
		if err := w.MessageSender.SendWhatsAppReply(ctx, interaction.TenantID, customer.PlatformID, receiptMessage); err != nil {
			return fmt.Errorf("failed to send receipt: %w", err)
		}

		// Update payment with receipt info
		payment.ReceiptID = fmt.Sprintf("RCP-%s", payment.PaymentReference)
		w.DB.Save(&payment)

		w.Logger.Info("payment receipt sent",
			zap.Uint("payment_id", payment.ID),
			zap.String("receipt_id", payment.ReceiptID))
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

	// Actually send reply via social media API
	if w.MessageSender != nil && interaction.Platform == "whatsapp" {
		if err := w.MessageSender.SendWhatsAppReply(ctx, interaction.TenantID, customer.PlatformID, reply); err != nil {
			w.Logger.Error("failed to send reply to customer",
				zap.Error(err),
				zap.Uint("interaction_id", interaction.ID),
				zap.String("customer_platform_id", customer.PlatformID))
			return fmt.Errorf("failed to send reply: %w", err)
		}
	} else {
		w.Logger.Warn("message sender not configured or unsupported platform",
			zap.String("platform", interaction.Platform))
	}

	// Update interaction
	interaction.ResponseType = "manual"
	interaction.ResponseStatus = "sent"
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
	outgoingInteraction := storage.Interaction{
		TenantID:       interaction.TenantID,
		CustomerID:     interaction.CustomerID,
		ConversationID: interaction.ConversationID,
		Platform:       interaction.Platform,
		Direction:      "outgoing",
		MessageType:    "text",
		Content:        reply,
		ResponseType:   "manual",
		ResponseStatus: "sent",
		FinalReply:     reply,
		Metadata:       "{}", // Empty JSON object - required for JSONB field
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
	// Generate unique payment reference
	paymentRef := fmt.Sprintf("PAY-%d-%d-%s", interaction.TenantID, interaction.CustomerID, time.Now().Format("20060102150405"))

	// Check if payment already exists for this interaction
	var existingPayment storage.Payment
	err := w.DB.Where("tenant_id = ? AND customer_id = ? AND status = ?", 
		interaction.TenantID, interaction.CustomerID, "pending").
		Order("created_at DESC").
		First(&existingPayment).Error

	if err == nil {
		// Update existing payment
		existingPayment.Status = "verified"
		existingPayment.VerificationStatus = "tenant_verified"
		existingPayment.ClaimedByCustomer = true
		now := time.Now()
		existingPayment.ClaimedAt = &now
		existingPayment.VerifiedAt = &now
		existingPayment.CompletedAt = &now
		
		if err := w.DB.Save(&existingPayment).Error; err != nil {
			return fmt.Errorf("failed to update payment: %w", err)
		}
		
		w.Logger.Info("payment updated",
			zap.Uint("payment_id", existingPayment.ID),
			zap.String("payment_ref", existingPayment.PaymentReference))
		return nil
	}

	// Create new payment record
	payment := storage.Payment{
		TenantID:           interaction.TenantID,
		CustomerID:         interaction.CustomerID,
		PaymentReference:   paymentRef,
		Amount:             0, // TODO: Extract from order if linked
		Currency:           "NGN",
		PaymentMethod:      "bank_transfer", // Default, could be extracted from message
		Status:             "verified",
		VerificationStatus: "tenant_verified",
		ClaimedByCustomer:  true,
		Metadata:           fmt.Sprintf(`{"interaction_id":%d,"customer_message":"%s"}`, interaction.ID, interaction.Content),
	}
	
	now := time.Now()
	payment.ClaimedAt = &now
	payment.VerifiedAt = &now
	payment.CompletedAt = &now

	if err := w.DB.Create(&payment).Error; err != nil {
		return fmt.Errorf("failed to create payment: %w", err)
	}

	w.Logger.Info("payment record created",
		zap.Uint("payment_id", payment.ID),
		zap.String("payment_ref", payment.PaymentReference),
		zap.Uint("tenant_id", interaction.TenantID))

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
