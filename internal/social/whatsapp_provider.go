package social

import (
	"github.com/google/uuid"
	"context"
)

// WhatsAppProvider interface for sending WhatsApp messages
// Allows abstraction between different providers (Meta Direct, etc.)
type WhatsAppProvider interface {
	SendText(ctx context.Context, tenantID uuid.UUID, to, text string) error
}
