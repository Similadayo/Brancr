package social

import (
	"context"
)

// ReplySender provides an interface for sending replies that EscalationHandler can use
type ReplySender interface {
	SendReply(ctx context.Context, tenantID uint, platform, threadID, customerID, reply string) error
}

// SendReply sends a reply using MessageHandler - implements ReplySender interface
func (h *MessageHandler) SendReply(ctx context.Context, tenantID uint, platform, threadID, customerID, reply string) error {
	// Create IncomingMessage-like structure for sending
	msg := IncomingMessage{
		TenantID:          tenantID,
		Platform:          platform,
		CustomerID:        customerID,
		CustomerProfilePic: "",
		ThreadID:          threadID,
	}
	
	// Use existing sendReply method
	return h.sendReply(ctx, msg, reply)
}

