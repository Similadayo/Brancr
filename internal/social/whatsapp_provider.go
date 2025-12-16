package social

import (
	"context"
)

// WhatsAppProvider interface for sending WhatsApp messages
// Allows abstraction between different providers (Meta Direct, etc.)
type WhatsAppProvider interface {
	SendText(ctx context.Context, tenantID uint, to, text string) error
}
