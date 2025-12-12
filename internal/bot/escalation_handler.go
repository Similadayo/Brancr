package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/security"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ReplySender interface for sending replies to customers
type ReplySender interface {
	SendReply(ctx context.Context, tenantID uint, platform, threadID, customerID, reply string) error
}

// EscalationHandler manages escalated messages from the MessageHandler
type EscalationHandler struct {
	DB              *gorm.DB
	Logger          *zap.Logger
	Manager         *TenantBotManager
	MessageHandler  ReplySender                      // Set after initialization via SetMessageHandler
	PaymentSecurity *security.PaymentSecurityHandler // Security handler for payment escalations
}

// SetMessageHandler sets the MessageHandler for sending replies
func (h *EscalationHandler) SetMessageHandler(handler ReplySender) {
	h.MessageHandler = handler
}

// PendingEscalation tracks escalations waiting for tenant approval
type PendingEscalation struct {
	InteractionID  uint
	TenantID       uint
	CustomerID     uint
	Platform       string
	Message        string
	SuggestedReply string
	ChatID         int64 // Telegram chat ID where escalation was sent
}

var pendingEscalations = make(map[uint]*PendingEscalation) // interactionID → escalation

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

	// Update incoming interaction
	now := time.Now()
	interaction.ResponseType = "manual"
	interaction.ResponseStatus = "sent"
	interaction.FinalReply = reply
	interaction.EscalatedTo = esc.ChatID
	interaction.EscalatedAt = &now
	h.DB.Save(&interaction)

	// Create outgoing interaction record (so it appears in conversation history)
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
	if err := h.DB.Create(&outgoingInteraction).Error; err != nil {
		h.Logger.Warn("failed to create outgoing interaction for escalation reply", zap.Error(err))
		// Continue - not critical, but log it
	}

	// Ensure order/payment records are created when tenant approves via Telegram
	h.ensureOrderAndPayment(ctx, &interaction)

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

// ensureOrderAndPayment creates an order and payment when approving via Telegram
func (h *EscalationHandler) ensureOrderAndPayment(ctx context.Context, interaction *storage.Interaction) {
	if interaction == nil {
		return
	}

	// Avoid duplicate orders in the last 30 minutes
	var recent storage.Order
	if err := h.DB.Where("tenant_id = ? AND customer_id = ? AND platform = ?", interaction.TenantID, interaction.CustomerID, interaction.Platform).
		Where("created_at > ?", time.Now().Add(-30*time.Minute)).
		Order("created_at DESC").
		First(&recent).Error; err == nil {
		h.ensurePaymentForOrder(interaction, &recent)
		return
	}

	// Load customer for metadata
	var customer storage.Customer
	if err := h.DB.First(&customer, interaction.CustomerID).Error; err != nil {
		h.Logger.Warn("ensureOrderAndPayment: customer not found", zap.Error(err), zap.Uint("customer_id", interaction.CustomerID))
		return
	}

	// Try to pull sales conversation to infer items/amount (if available)
	var salesConv storage.SalesConversation
	if interaction.ConversationID != 0 {
		_ = h.DB.Where("conversation_id = ?", interaction.ConversationID).Order("created_at DESC").First(&salesConv).Error
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
			zap.Uint("interaction_id", interaction.ID),
			zap.Uint("tenant_id", interaction.TenantID),
			zap.Uint("customer_id", interaction.CustomerID),
			zap.Uint("conversation_id", interaction.ConversationID),
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
			h.Logger.Warn("ensureOrderAndPayment: skipping auto-order creation (no amount inferred)",
				zap.Uint("interaction_id", interaction.ID),
				zap.Uint("tenant_id", interaction.TenantID),
				zap.Uint("customer_id", interaction.CustomerID),
				zap.String("platform", interaction.Platform),
				zap.Float64("price_discussed", salesConv.PriceDiscussed))
			return
		}
		items = []storage.OrderItem{{
			Name:       "Order",
			Quantity:   1,
			UnitPrice:  fallbackTotal,
			TotalPrice: fallbackTotal,
		}}
		total = fallbackTotal
		amountSource = "fallback.manual_item"
	}

	if logAmountDecisions {
		h.Logger.Info("tg approval: order amount decision (final)",
			zap.Uint("interaction_id", interaction.ID),
			zap.Uint("tenant_id", interaction.TenantID),
			zap.Uint("customer_id", interaction.CustomerID),
			zap.Uint("conversation_id", interaction.ConversationID),
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
	_ = h.DB.Preload("BusinessProfile").First(&tenant, interaction.TenantID).Error
	itemLabel := ""
	if len(items) > 0 {
		itemLabel = items[0].Name
	}
	businessName := tenant.BusinessProfile.Name
	if businessName == "" {
		businessName = tenant.Name
	}
	paymentRef := utils.GeneratePaymentReference(businessName, interaction.TenantID, itemLabel)

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

	if err := h.DB.Create(&orderRec).Error; err != nil {
		h.Logger.Warn("ensureOrderAndPayment: failed to create order", zap.Error(err))
		return
	}

	h.ensurePaymentForOrder(interaction, &orderRec)
}

// ensurePaymentForOrder creates a payment record if missing for the given order
func (h *EscalationHandler) ensurePaymentForOrder(interaction *storage.Interaction, orderRec *storage.Order) {
	if orderRec == nil {
		return
	}

	var existing storage.Payment
	if err := h.DB.Where("tenant_id = ? AND order_id = ?", orderRec.TenantID, orderRec.ID).First(&existing).Error; err == nil {
		return
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

	if err := h.DB.Create(&payment).Error; err != nil {
		h.Logger.Warn("ensureOrderAndPayment: failed to create payment", zap.Error(err))
	}
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
