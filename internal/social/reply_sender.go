package social

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// ReplySender provides an interface for sending replies that EscalationHandler can use
type ReplySender interface {
	SendReply(ctx context.Context, tenantID uuid.UUID, platform, threadID, customerID, reply string) error
}

// SendReply sends a reply using MessageHandler - implements ReplySender interface
// This method creates an outgoing interaction so the message appears in the dashboard
func (h *MessageHandler) SendReply(ctx context.Context, tenantID uuid.UUID, platform, threadID, customerID, reply string) error {
	// Create IncomingMessage-like structure for sending
	msg := IncomingMessage{
		TenantID:           tenantID,
		Platform:           platform,
		CustomerID:         customerID,
		CustomerProfilePic: "",
		ThreadID:           threadID,
	}

	// Get customer to find customer UUID
	// customerID parameter is the external platform ID (e.g., WhatsApp phone number)
	var customer storage.Customer
	err := h.DB.Where("tenant_id = ? AND platform_id = ? AND channel = ?", tenantID, customerID, platform).First(&customer).Error
	if err != nil {
		// If customer not found, still try to send message
		h.Logger.Warn("customer not found for SendReply, sending message anyway",
			zap.Stringer("tenant_id", tenantID),
			zap.String("platform", platform),
			zap.String("platform_id", customerID),
			zap.Error(err))
		// Still try to send the message
		return h.sendReply(ctx, msg, reply)
	}

	// Get or find conversation
	var conversationID uuid.UUID
	var conversation storage.Conversation
	if err := h.DB.Where("tenant_id = ? AND customer_id = ? AND platform = ?", tenantID, customer.ID, platform).
		Order("last_message_at DESC").First(&conversation).Error; err == nil {
		conversationID = conversation.ID
	}

	// HUMAN-LIKE PACING: Add delay before sending to avoid instant "bot" feel
	var policy storage.TenantPolicy
	if err := h.DB.Where("tenant_id = ?", tenantID).First(&policy).Error; err == nil {
		if policy.ReplyDelayMs > 0 {
			delay := time.Duration(policy.ReplyDelayMs) * time.Millisecond
			// Cap delay at 3 seconds to prevent excessive wait
			if delay > 3*time.Second {
				delay = 3 * time.Second
			}
			time.Sleep(delay)
		}
	}

	// Send the message
	if err := h.sendReply(ctx, msg, reply); err != nil {
		return err
	}

	// Create outgoing interaction so it appears in dashboard
	// Note: Escalation handlers may also create interactions, but this ensures all SendReply calls create one
	outgoingInteraction := storage.Interaction{
		TenantID:       tenantID,
		CustomerID:     customer.ID,
		ConversationID: conversationID,
		Platform:       platform,
		Direction:      "outgoing",
		MessageType:    "text",
		Content:        reply,
		ResponseType:   "manual", // Manual reply (from tenant via Telegram or dashboard)
		ResponseStatus: "sent",
		FinalReply:     reply,
		Metadata:       "{}",
	}

	if err := h.DB.Create(&outgoingInteraction).Error; err != nil {
		h.Logger.Warn("failed to create outgoing interaction for SendReply",
			zap.Error(err),
			zap.Stringer("tenant_id", tenantID),
			zap.String("platform", platform))
		// Continue - message was sent successfully
	} else {
		h.Logger.Debug("✅ Created outgoing interaction for SendReply",
			zap.Stringer("interaction_id", outgoingInteraction.ID),
			zap.Stringer("conversation_id", conversationID),
			zap.Stringer("tenant_id", tenantID))

		// Update conversation's last message time
		if conversationID != uuid.Nil {
			conversation.LastMessageAt = time.Now()
			conversation.Status = "active"
			if err := h.DB.Save(&conversation).Error; err != nil {
				h.Logger.Warn("failed to update conversation after SendReply", zap.Error(err))
			}
		}
	}

	return nil
}
