package social

import (
	"context"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// sendTypingIndicator simulates a "typing..." indicator to make AI responses feel faster
// This is a placeholder - actual implementation would depend on platform API support
func (h *MessageHandler) sendTypingIndicator(ctx context.Context, msg IncomingMessage) {
	// Note: Most platforms don't have direct typing indicator APIs we can call
	// This is mainly for logging/analytics purposes
	// For platforms that support it (like Telegram), we'd need platform-specific implementations

	h.Logger.Info("💬 Simulating typing indicator",
		zap.String("platform", msg.Platform),
		zap.Uint("tenant_id", msg.TenantID),
		zap.String("customer_id", msg.CustomerID))

	// For now, we'll rely on the natural delay of AI generation to simulate typing
	// In the future, if platforms support typing indicators via webhook/API, we can implement them here
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
			zap.Uint("customer_id", customer.ID))
		// Don't fail escalation if clarity message fails
	}

	h.Logger.Info("📢 Sent escalation clarity message to customer",
		zap.Uint("tenant_id", msg.TenantID),
		zap.Uint("customer_id", customer.ID),
		zap.String("platform", msg.Platform))
}
