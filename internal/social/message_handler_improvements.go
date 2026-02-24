package social

import (
	"context"
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// sendTypingIndicator sends a "typing..." status to the customer's platform
// This makes the AI feel more responsive and human-like
func (h *MessageHandler) sendTypingIndicator(ctx context.Context, msg IncomingMessage) {
	// Don't block processing if typing indicator fails
	defer func() {
		if r := recover(); r != nil {
			h.Logger.Error("panic in sendTypingIndicator", zap.Any("reason", r))
		}
	}()

	h.Logger.Info("💬 simulating typing indicator",
		zap.String("platform", msg.Platform),
		zap.Stringer("tenant_id", msg.TenantID))

	switch msg.Platform {
	case "telegram":
		h.sendTelegramTyping(msg)
	case "whatsapp":
		// WhatsApp typing indicators (Gupshup/Respond.io)
		// Currently generic implementation, can be expanded based on specific provider capabilities
	case "instagram", "facebook":
		// Meta Graph API typing indicators
		// Requires 'sender_action' endpoint support
	}
}

// sendTelegramTyping sends a typing action to Telegram
func (h *MessageHandler) sendTelegramTyping(msg IncomingMessage) {
	if h.BotGetter == nil {
		return
	}

	botInterface, ok := h.BotGetter.GetBot(msg.TenantID)
	if !ok {
		return
	}

	bot, ok := botInterface.(*tgbotapi.BotAPI)
	if !ok {
		return
	}

	// Parse Chat ID (CustomerID in generic struct is usually the stringified ChatID for Telegram)
	var chatID int64
	_, err := fmt.Sscanf(msg.CustomerID, "%d", &chatID)
	if err != nil {
		h.Logger.Debug("failed to parse telegram chat ID for typing indicator", zap.String("customer_id", msg.CustomerID))
		return
	}

	// Create typing action
	action := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)

	// Send action (fire and forget)
	if _, err := bot.Request(action); err != nil {
		// Log at debug level to avoid noise
		h.Logger.Debug("failed to send telegram typing action", zap.Error(err))
	}
}

// sendEscalationClarityMessage sends a message to customer when their message is escalated
func (h *MessageHandler) sendEscalationClarityMessage(ctx context.Context, msg IncomingMessage, customer *storage.Customer) {
	// Send a friendly message to let customer know we're looking into their message
	clarityMsg := "We're looking into this and will get back to you shortly. Thanks for your patience! 🙏"

	// Send via the same platform the customer used
	if err := h.sendReply(ctx, msg, clarityMsg); err != nil {
		h.Logger.Warn("failed to send escalation clarity message",
			zap.Error(err),
			zap.String("platform", msg.Platform),
			zap.Stringer("customer_id", customer.ID))
		// Don't fail escalation if clarity message fails
	}

	h.Logger.Info("📢 Sent escalation clarity message to customer",
		zap.Stringer("tenant_id", msg.TenantID),
		zap.Stringer("customer_id", customer.ID),
		zap.String("platform", msg.Platform))
}
