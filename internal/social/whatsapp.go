package social

import (
	"context"
	"fmt"

	"github.com/similadayo/brancr/internal/storage"
)

type WhatsAppConnector struct{}

func (w *WhatsAppConnector) Name() string { return "whatsapp" }

func (w *WhatsAppConnector) PostMedia(ctx context.Context, acc *storage.SocialAccount, asset *storage.MediaAsset, caption string) error {
	// Example: POST /v17.0/{phone_number_id}/messages
	fmt.Printf("[WhatsApp] Sending media via Business ID %s to chats... Caption=%s\n", acc.PageID, caption)
	return nil
}

func (w *WhatsAppConnector) HealthCheck(ctx context.Context, acc *storage.SocialAccount) error {
	// Simple ping placeholder
	return nil
}

func init() {
	Connectors["whatsapp"] = &WhatsAppConnector{}
}
