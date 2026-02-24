package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/receipt"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// ============================================================
// ESCALATION V3 - Payment-Focused & Order Status
// ============================================================
// Design Principles:
// 1. Regular escalations: Even more bite-sized (3 lines max)
// 2. Payment escalations: Special format with Confirm/Not Found buttons
// 3. Auto-notify customer on payment confirmation
// 4. Natural language order status updates
// ============================================================

// Payment indicators moved to escalation_types.go

// SendEscalationV3 sends a simplified escalation to the owner
// For regular messages: Ultra-short format
// For specific types: Tailored format with specific actions
func (h *EscalationHandler) SendEscalationV3(ctx context.Context, tenantID uuid.UUID, interactionID uuid.UUID, customerName, platform, message, suggestedReply string, hasMedia bool) error {
	// Load tenant
	var tenant storage.Tenant
	if err := h.DB.WithContext(ctx).First(&tenant, tenantID).Error; err != nil {
		return fmt.Errorf("tenant not found: %w", err)
	}

	// Get bot
	bot, ok := h.Manager.GetBot(tenantID)
	if !ok {
		return fmt.Errorf("bot not found for tenant")
	}

	// Load interaction for context
	var interaction storage.Interaction
	if err := h.DB.WithContext(ctx).First(&interaction, interactionID).Error; err != nil {
		return fmt.Errorf("interaction not found: %w", err)
	}

	// Detect escalation type
	escType := DetectEscalationType(message, hasMedia)

	// If it's a payment escalation, try to find the linked Order ID immediately
	// This prevents guessing later during approval
	var linkedOrderID *uuid.UUID
	if escType == TypePayment {
		var order storage.Order
		// Look for an order created within 2 hours BEFORE the interaction
		// This strictly links the "I paid" message to a recent pending order
		orderWindowStart := interaction.CreatedAt.Add(-2 * time.Hour)
		if err := h.DB.WithContext(ctx).Where("tenant_id = ? AND customer_id = ? AND status = ?",
			tenantID, interaction.CustomerID, "pending").
			Where("created_at > ? AND created_at <= ?", orderWindowStart, interaction.CreatedAt.Add(5*time.Minute)).
			Order("created_at DESC").First(&order).Error; err == nil {
			linkedOrderID = &order.ID
			h.Logger.Info("🔗 Linked escalation to order", zap.Stringer("order_id", order.ID))
		}
	}

	// Format based on type
	var escalationMsg string
	var keyboard tgbotapi.InlineKeyboardMarkup

	switch escType {
	case TypePayment:
		escalationMsg, keyboard = h.formatPaymentEscalationV3(customerName, platform, message, interactionID, hasMedia)
	case TypeComplaint:
		escalationMsg, keyboard = h.formatComplaintEscalationV3(customerName, platform, message, interactionID)
	case TypeBooking:
		escalationMsg, keyboard = h.formatBookingEscalationV3(customerName, platform, message, interactionID)
	default:
		escalationMsg, keyboard = h.formatRegularEscalationV3(customerName, platform, message, suggestedReply, interactionID)
	}

	msg := tgbotapi.NewMessage(tenant.OwnerTelegramID, escalationMsg)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard

	if _, err := bot.Send(msg); err != nil {
		return fmt.Errorf("failed to send escalation: %w", err)
	}

	// Store pending escalation with type and linked OrderID
	pendingEscalation := storage.PendingEscalation{
		InteractionID:    interactionID,
		TenantID:         tenantID,
		CustomerID:       interaction.CustomerID,
		Platform:         platform,
		Message:          message,
		SuggestedReply:   suggestedReply,
		ChatID:           tenant.OwnerTelegramID,
		EscalationType:   string(escType),
		ResolutionStatus: storage.ResolutionPending,
		Intent:           string(escType), // Keep intent for backward compatibility
		Reason:           "escalation",
		OrderID:          linkedOrderID, // Save the specific Order ID
	}

	h.DB.WithContext(ctx).Where("interaction_id = ?", interactionID).
		FirstOrCreate(&pendingEscalation, storage.PendingEscalation{InteractionID: interactionID})

	h.Logger.Info("📤 V3 Escalation sent",
		zap.Stringer("tenant_id", tenantID),
		zap.String("type", string(escType)))

	// Checkpoint Trigger: Escalation (V3)
	if h.CheckpointService != nil {
		if err := h.CheckpointService.TriggerCheckpoint(context.Background(), tenantID, "escalation", interactionID); err != nil {
			h.Logger.Warn("failed to trigger escalation checkpoint (v3)", zap.Error(err))
		}
	}

	return nil
}

// formatRegularEscalationV3 creates the ultra-short regular escalation
func (h *EscalationHandler) formatRegularEscalationV3(customerName, platform, message, suggestedReply string, interactionID uuid.UUID) (string, tgbotapi.InlineKeyboardMarkup) {
	// V3 Format: Just 3 lines
	// 💬 IG | John
	// "Is the blue one available?"
	platformEmoji := getPlatformEmoji(platform)

	// Truncate message if too long
	displayMsg := message
	if len(displayMsg) > 100 {
		displayMsg = displayMsg[:97] + "..."
	}

	escalationMsg := fmt.Sprintf(
		"💬 %s | %s\n\"%s\"",
		platformEmoji,
		escapeMarkdown(customerName),
		escapeMarkdown(displayMsg),
	)

	// Only show suggested reply if it's short
	if len(suggestedReply) > 0 && len(suggestedReply) < 80 {
		escalationMsg += fmt.Sprintf("\n→ _%s_", escapeMarkdown(suggestedReply))
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Send AI Reply", fmt.Sprintf("v3esc:send:%s", interactionID)),
			tgbotapi.NewInlineKeyboardButtonData("Reply", fmt.Sprintf("v3esc:reply:%s", interactionID)),
		),
	)

	return escalationMsg, keyboard
}

// formatPaymentEscalationV3 creates the special payment confirmation format
func (h *EscalationHandler) formatPaymentEscalationV3(customerName, platform, message string, interactionID uuid.UUID, hasMedia bool) (string, tgbotapi.InlineKeyboardMarkup) {
	// V3 Payment Format:
	// 💰 PAYMENT | IG
	// 👤 Adaeze
	// "I just paid, here's my receipt"
	// 📎 [has attachment]

	platformEmoji := getPlatformEmoji(platform)

	displayMsg := message
	if len(displayMsg) > 80 {
		displayMsg = displayMsg[:77] + "..."
	}

	escalationMsg := fmt.Sprintf(
		"💰 PAYMENT | %s\n👤 %s\n\"%s\"",
		platformEmoji,
		escapeMarkdown(customerName),
		escapeMarkdown(displayMsg),
	)

	if hasMedia {
		escalationMsg += "\n📎 _has attachment_"
	}

	// Payment-specific buttons
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Confirm", fmt.Sprintf("v3pay:confirm:%s", interactionID)),
			tgbotapi.NewInlineKeyboardButtonData("❌ Not Found", fmt.Sprintf("v3pay:notfound:%s", interactionID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💬 Ask More", fmt.Sprintf("v3pay:ask:%s", interactionID)),
		),
	)

	return escalationMsg, keyboard
}

// formatComplaintEscalationV3 creates complaint escalation format
func (h *EscalationHandler) formatComplaintEscalationV3(customerName, platform, message string, interactionID uuid.UUID) (string, tgbotapi.InlineKeyboardMarkup) {
	platformEmoji := getPlatformEmoji(platform)
	displayMsg := truncateMessage(message, 100)

	escalationMsg := fmt.Sprintf(
		"⚠️ COMPLAINT | %s\n👤 %s\n\"%s\"",
		platformEmoji,
		escapeMarkdown(customerName),
		escapeMarkdown(displayMsg),
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Resolved", fmt.Sprintf("v3comp:resolve:%s", interactionID)),
			tgbotapi.NewInlineKeyboardButtonData("💬 Reply", fmt.Sprintf("v3comp:reply:%s", interactionID)),
		),
	)

	return escalationMsg, keyboard
}

// formatBookingEscalationV3 creates booking escalation format
func (h *EscalationHandler) formatBookingEscalationV3(customerName, platform, message string, interactionID uuid.UUID) (string, tgbotapi.InlineKeyboardMarkup) {
	platformEmoji := getPlatformEmoji(platform)
	displayMsg := truncateMessage(message, 100)

	escalationMsg := fmt.Sprintf(
		"📅 BOOKING | %s\n👤 %s\n\"%s\"",
		platformEmoji,
		escapeMarkdown(customerName),
		escapeMarkdown(displayMsg),
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Confirm", fmt.Sprintf("v3book:confirm:%s", interactionID)),
			tgbotapi.NewInlineKeyboardButtonData("📅 Propose", fmt.Sprintf("v3book:propose:%s", interactionID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Unavailable", fmt.Sprintf("v3book:decline:%s", interactionID)),
		),
	)

	return escalationMsg, keyboard
}

func truncateMessage(msg string, limit int) string {
	if len(msg) <= limit {
		return msg
	}
	return msg[:limit-3] + "..."
}

// ============================================================
// PAYMENT CONFIRMATION HANDLERS
// ============================================================

// HandlePaymentConfirmV3 handles payment confirmation from owner
func (m *TenantBotManager) HandlePaymentConfirmV3(bot *tgbotapi.BotAPI, chatID int64, interactionID uuid.UUID) {
	ctx := context.Background()

	// Get interaction and customer
	var interaction storage.Interaction
	if err := m.DB.Preload("Customer").First(&interaction, interactionID).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Could not find that conversation."))
		return
	}

	// Update order status to "confirmed" if order exists
	var order storage.Order
	hasOrder := false
	now := time.Now() // Define `now` here so it's in scope for Payment creation
	if err := m.DB.Where("tenant_id = ? AND customer_id = ? AND status IN ?",
		interaction.TenantID, interaction.CustomerID, []string{"pending", "processing"}).
		Order("created_at DESC").First(&order).Error; err == nil {
		hasOrder = true
		order.Status = "confirmed"
		order.ConfirmedAt = &now
		m.DB.Save(&order)
	}

	// Parse order items from JSON string
	var orderItems []storage.OrderItem
	if hasOrder && order.Items != "" {
		_ = json.Unmarshal([]byte(order.Items), &orderItems)
	}

	// Generate real receipt and portal URL
	var portalURL, receiptURL string
	publicURL := "https://api.brancr.com" // Default

	if hasOrder {
		receiptService := receipt.NewReceiptService(m.DB, m.Logger, "", publicURL, nil)
		token, url, err := receiptService.GeneratePortalTokenAndURL(ctx, order.ID, interaction.TenantID, publicURL)
		if err == nil {
			portalURL = url
			receiptURL = url
			m.Logger.Info("portal token generated", zap.String("token", token))

			// Update the payment record created/found by receipt service to be CONFIRMED
			// GeneratePortalTokenAndURL ensures a payment record exists linked to this order
			m.DB.Model(&storage.Payment{}).
				Where("order_id = ? AND tenant_id = ?", order.ID, interaction.TenantID).
				Updates(map[string]interface{}{
					"status":       "confirmed",
					"completed_at": now,
					"amount":       order.TotalAmount, // Ensure amount matches order
				})
		} else {
			// Log the error if link generation fails
			m.Logger.Error("failed to generate receipt link", zap.Error(err))
		}
	}

	// Build order details string
	orderDetails := ""
	if hasOrder {
		orderDetails = fmt.Sprintf("\n\n📦 Order #%s", order.OrderNumber)
		if len(orderItems) > 0 {
			orderDetails += "\n"
			for _, item := range orderItems {
				orderDetails += fmt.Sprintf("• %dx %s - ₦%.0f\n", item.Quantity, item.Name, item.TotalPrice)
			}
		}
		orderDetails += fmt.Sprintf("💰 Total: ₦%.0f", order.TotalAmount)
	}

	// Send confirmation to owner with order details
	ownerMsg := fmt.Sprintf("✅ Payment confirmed!%s", orderDetails)
	if receiptURL != "" {
		ownerMsg += fmt.Sprintf("\n\n📄 Receipt: %s", receiptURL)
	}
	bot.Send(tgbotapi.NewMessage(chatID, ownerMsg))

	// Send notification to customer with order details and receipt link
	customerMsg := "✅ Payment confirmed! Thank you 🎉\n\n📦 Status: Processing"
	if hasOrder && len(orderItems) > 0 {
		customerMsg += "\n\n🧾 Your Order:"
		for _, item := range orderItems {
			customerMsg += fmt.Sprintf("\n• %dx %s", item.Quantity, item.Name)
		}
		customerMsg += fmt.Sprintf("\n💰 Total: ₦%.0f", order.TotalAmount)
	}
	if portalURL != "" {
		customerMsg += fmt.Sprintf("\n\n📄 View Receipt & Track Order:\n%s", portalURL)
	}
	customerMsg += "\n\nWe'll message you when it ships!"
	m.sendToCustomer(ctx, interaction.TenantID, interaction.CustomerID, interaction.Platform, customerMsg)

	// Clear pending escalation
	m.DB.Where("interaction_id = ?", interactionID).Delete(&storage.PendingEscalation{})

	m.Logger.Info("💰 Payment confirmed via V3",
		zap.Stringer("interaction_id", interactionID),
		zap.Bool("has_order", hasOrder))
}

// HandlePaymentNotFoundV3 handles when payment is not found
func (m *TenantBotManager) HandlePaymentNotFoundV3(bot *tgbotapi.BotAPI, chatID int64, interactionID uuid.UUID) {
	// Show quick options for asking more info
	msg := tgbotapi.NewMessage(chatID, "What do you want to ask?")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Bank name?", fmt.Sprintf("v3pay:askbank:%s", interactionID)),
			tgbotapi.NewInlineKeyboardButtonData("Reference #?", fmt.Sprintf("v3pay:askref:%s", interactionID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Amount sent?", fmt.Sprintf("v3pay:askamount:%s", interactionID)),
			tgbotapi.NewInlineKeyboardButtonData("Custom", fmt.Sprintf("v3pay:custom:%s", interactionID)),
		),
	)
	bot.Send(msg)
}

// HandlePaymentAskV3 sends a follow-up question to customer
func (m *TenantBotManager) HandlePaymentAskV3(bot *tgbotapi.BotAPI, chatID int64, interactionID uuid.UUID, askType string) {
	ctx := context.Background()

	var interaction storage.Interaction
	if err := m.DB.First(&interaction, interactionID).Error; err != nil {
		return
	}

	var customerMsg string
	switch askType {
	case "bank":
		customerMsg = "Thanks for sending! Could you let us know which bank you transferred from?"
	case "ref":
		customerMsg = "Thanks! Could you share the transaction reference number so we can locate your payment?"
	case "amount":
		customerMsg = "Thanks! What amount did you transfer? This will help us locate it faster."
	default:
		// Custom - prompt owner to type message
		bot.Send(tgbotapi.NewMessage(chatID, "Type your message to the customer:"))
		// Store session for custom reply
		m.SessionManager.SetGenericSession(ctx, fmt.Sprintf("payask:%d", chatID), interactionID.String(), 10*time.Minute)
		return
	}

	// Send to customer
	m.sendToCustomer(ctx, interaction.TenantID, interaction.CustomerID, interaction.Platform, customerMsg)
	bot.Send(tgbotapi.NewMessage(chatID, "✅ Sent to customer."))
}

// ============================================================
// ORDER STATUS COMMANDS
// ============================================================

// HandleOrderStatusCommand processes natural language order status updates
// Examples: "order 1234 shipped", "order 1234 delivered"
func (m *TenantBotManager) HandleOrderStatusCommand(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, text string) bool {
	ctx := context.Background()
	lower := strings.ToLower(text)

	// Pattern: "order <id> <status>"
	orderPattern := regexp.MustCompile(`order\s+#?(\d+)\s+(shipped|delivered|cancelled|processing|paid)`)
	matches := orderPattern.FindStringSubmatch(lower)

	if len(matches) < 3 {
		return false
	}

	orderRef := matches[1]
	newStatus := matches[2]

	// Find order
	var order storage.Order
	if err := m.DB.Where("tenant_id = ? AND (id::text LIKE ? OR reference LIKE ?)",
		tenantID, "%"+orderRef+"%", "%"+orderRef+"%").
		Order("created_at DESC").First(&order).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Order #%s not found.", orderRef)))
		return true
	}

	// Update order status
	order.Status = newStatus
	if err := m.DB.Save(&order).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Failed to update order."))
		return true
	}

	// Notify customer
	var customerMsg string
	switch newStatus {
	case "shipped":
		customerMsg = fmt.Sprintf("📦 Great news! Your order #%s has shipped!\n\nWe'll reach out if you have any questions. Thank you! 🙏", orderRef)
	case "delivered":
		customerMsg = fmt.Sprintf("✅ Your order #%s has been delivered!\n\nWe hope you love it. Thank you for shopping with us! 🎉", orderRef)
	case "cancelled":
		customerMsg = fmt.Sprintf("❌ Your order #%s has been cancelled.\n\nIf you have questions, please reach out.", orderRef)
	default:
		customerMsg = fmt.Sprintf("📦 Order #%s update: %s", orderRef, newStatus)
	}

	// Find customer for order
	var customer storage.Customer
	if err := m.DB.First(&customer, order.CustomerID).Error; err == nil {
		// Determine platform from customer
		platform := "telegram" // default
		if customer.Channel != "" {
			platform = customer.Channel
		}
		m.sendToCustomer(ctx, tenantID, order.CustomerID, platform, customerMsg)
	}

	// Confirm to owner
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Order #%s → %s\nCustomer notified.", orderRef, newStatus)))

	m.Logger.Info("📦 Order status updated via V3",
		zap.Stringer("order_id", order.ID),
		zap.String("new_status", newStatus))

	return true
}

// HandleOrdersToday shows today's orders
func (m *TenantBotManager) HandleOrdersToday(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64) {
	today := time.Now().Truncate(24 * time.Hour)
	tomorrow := today.Add(24 * time.Hour)

	var orders []storage.Order
	m.DB.Where("tenant_id = ? AND created_at BETWEEN ? AND ?", tenantID, today, tomorrow).
		Order("created_at DESC").
		Limit(10).
		Find(&orders)

	if len(orders) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "📦 No orders today yet."))
		return
	}

	var lines []string
	for _, o := range orders {
		lines = append(lines, fmt.Sprintf("• #%s - %s (₦%.0f)", o.ID.String()[:8], o.Status, o.TotalAmount))
	}

	msg := fmt.Sprintf("📦 Today's Orders:\n%s", strings.Join(lines, "\n"))
	bot.Send(tgbotapi.NewMessage(chatID, msg))
}

// ============================================================
// HELPER FUNCTIONS
// ============================================================

// sendToCustomer sends a message to a customer via their platform
func (m *TenantBotManager) sendToCustomer(ctx context.Context, tenantID uuid.UUID, customerID uuid.UUID, platform, message string) error {
	// Get customer's platform ID
	var customer storage.Customer
	if err := m.DB.First(&customer, customerID).Error; err != nil {
		return err
	}

	// Use the EscalationHandler's MessageHandler if available
	if m.EscalationHandler != nil && m.EscalationHandler.MessageHandler != nil {
		return m.EscalationHandler.MessageHandler.SendReply(ctx, tenantID, platform, "", customer.PlatformID, message)
	}

	m.Logger.Warn("Cannot send to customer - no message handler configured")
	return nil
}

// ============================================================
// V3 CALLBACK ROUTER
// ============================================================

// HandleEscalationV3Callback routes V3 escalation callbacks
func (m *TenantBotManager) HandleEscalationV3Callback(bot *tgbotapi.BotAPI, chatID int64, data string) {
	// Parse: v3esc:action:id or v3pay:action:id
	parts := strings.SplitN(data, ":", 3)
	if len(parts) < 3 {
		return
	}

	action := parts[1]
	interactionIDStr := parts[2]
	interactionID, err := uuid.Parse(interactionIDStr)
	if err != nil {
		return
	}

	switch parts[0] {
	case "v3esc":
		// Regular escalation callbacks
		switch action {
		case "send":
			// Send the AI-suggested reply
			m.sendAISuggestedReply(bot, chatID, interactionID)
		case "reply":
			// Prompt for custom reply
			bot.Send(tgbotapi.NewMessage(chatID, "Type your reply:"))
			ctx := context.Background()
			m.SessionManager.SetGenericSession(ctx, fmt.Sprintf("customreply:%d", chatID), interactionIDStr, 10*time.Minute)
		}
	case "v3pay":
		// Payment-specific callbacks
		switch action {
		case "confirm":
			m.HandlePaymentConfirmV3(bot, chatID, interactionID)
		case "notfound":
			m.HandlePaymentNotFoundV3(bot, chatID, interactionID)
		case "ask":
			// Generic ask
			m.HandlePaymentAskV3(bot, chatID, interactionID, "")
		case "askbank":
			m.HandlePaymentAskV3(bot, chatID, interactionID, "bank")
		case "askref":
			m.HandlePaymentAskV3(bot, chatID, interactionID, "ref")
		case "askamount":
			m.HandlePaymentAskV3(bot, chatID, interactionID, "amount")
		case "custom":
			m.HandlePaymentAskV3(bot, chatID, interactionID, "custom")
		}
	case "v3comp":
		// Complaint callbacks
		switch action {
		case "resolve":
			m.HandleComplaintResolveV3(bot, chatID, interactionID)
		case "reply":
			// Prompt for custom reply
			bot.Send(tgbotapi.NewMessage(chatID, "Type your reply:"))
			ctx := context.Background()
			m.SessionManager.SetGenericSession(ctx, fmt.Sprintf("customreply:%d", chatID), interactionIDStr, 10*time.Minute)
		}
	case "v3book":
		// Booking callbacks
		switch action {
		case "confirm":
			m.HandleBookingConfirmV3(bot, chatID, interactionID)
		case "propose":
			bot.Send(tgbotapi.NewMessage(chatID, "Type proposed time (e.g. 'Monday at 2pm'):"))
			ctx := context.Background()
			m.SessionManager.SetGenericSession(ctx, fmt.Sprintf("bookpropose:%d", chatID), interactionIDStr, 10*time.Minute)
		case "decline":
			m.HandleBookingDeclineV3(bot, chatID, interactionID)
		}
	}
}

// sendAISuggestedReply sends the pre-generated AI reply
func (m *TenantBotManager) sendAISuggestedReply(bot *tgbotapi.BotAPI, chatID int64, interactionID uuid.UUID) {
	ctx := context.Background()

	var pending storage.PendingEscalation
	if err := m.DB.Where("interaction_id = ?", interactionID).First(&pending).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Escalation expired."))
		return
	}

	// Get customer info
	var customer storage.Customer
	m.DB.First(&customer, pending.CustomerID)

	// Send reply to customer
	if m.EscalationHandler != nil && m.EscalationHandler.MessageHandler != nil {
		err := m.EscalationHandler.MessageHandler.SendReply(ctx, pending.TenantID, pending.Platform, "", customer.PlatformID, pending.SuggestedReply)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Failed to send reply."))
			return
		}
	}

	// Confirm and cleanup
	bot.Send(tgbotapi.NewMessage(chatID, "✅ Sent!"))
	m.DB.Where("interaction_id = ?", interactionID).Delete(&storage.PendingEscalation{})
}

// HandleComplaintResolveV3 marks complaint as resolved
func (m *TenantBotManager) HandleComplaintResolveV3(bot *tgbotapi.BotAPI, chatID int64, interactionID uuid.UUID) {
	// Optimistic UI update
	bot.Send(tgbotapi.NewMessage(chatID, "✅ Complaint marked as resolved."))
	m.DB.Where("interaction_id = ?", interactionID).Delete(&storage.PendingEscalation{})
}

// HandleBookingConfirmV3 confirms a booking
func (m *TenantBotManager) HandleBookingConfirmV3(bot *tgbotapi.BotAPI, chatID int64, interactionID uuid.UUID) {
	ctx := context.Background()
	var pending storage.PendingEscalation
	if err := m.DB.Where("interaction_id = ?", interactionID).First(&pending).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Expiration expired or not found."))
		return
	}

	// Notify customer
	m.sendToCustomer(ctx, pending.TenantID, pending.CustomerID, pending.Platform, "✅ Your appointment is confirmed! We look forward to seeing you.")

	bot.Send(tgbotapi.NewMessage(chatID, "✅ Booking confirmed & customer notified."))
	m.DB.Where("interaction_id = ?", interactionID).Delete(&storage.PendingEscalation{})
}

// HandleBookingDeclineV3 declines a booking
func (m *TenantBotManager) HandleBookingDeclineV3(bot *tgbotapi.BotAPI, chatID int64, interactionID uuid.UUID) {
	ctx := context.Background()
	var pending storage.PendingEscalation
	if err := m.DB.Where("interaction_id = ?", interactionID).First(&pending).Error; err != nil {
		return
	}

	// Notify customer
	m.sendToCustomer(ctx, pending.TenantID, pending.CustomerID, pending.Platform, "We're sorry, but we aren't available at that time. Could we try another time?")

	bot.Send(tgbotapi.NewMessage(chatID, "❌ Booking declined & customer notified."))
	m.DB.Where("interaction_id = ?", interactionID).Delete(&storage.PendingEscalation{})
}
