package social

import (
	"context"
	"fmt"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// WhatsAppProvider interface for sending WhatsApp messages
// Allows abstraction between different providers (Gupshup Partner, Meta Direct, etc.)
type WhatsAppProvider interface {
	SendText(ctx context.Context, tenantID uint, to, text string) error
}

// GupshupWhatsAppProvider implements WhatsAppProvider using Gupshup Partner API
type GupshupWhatsAppProvider struct {
	DB            *gorm.DB
	Logger        *zap.Logger
	PartnerClient *GupshupPartnerClient
}

// NewGupshupWhatsAppProvider creates a new Gupshup WhatsApp provider
func NewGupshupWhatsAppProvider(db *gorm.DB, logger *zap.Logger, partnerClient *GupshupPartnerClient) *GupshupWhatsAppProvider {
	return &GupshupWhatsAppProvider{
		DB:            db,
		Logger:        logger,
		PartnerClient: partnerClient,
	}
}

// SendText sends a WhatsApp text message via Gupshup Partner API
func (p *GupshupWhatsAppProvider) SendText(ctx context.Context, tenantID uint, to, text string) error {
	// Get tenant's Gupshup Partner app
	var waApp storage.WhatsAppApp
	if err := p.DB.Where("tenant_id = ? AND status = ?", tenantID, "live").First(&waApp).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("no live WhatsApp app found for tenant %d", tenantID)
		}
		return fmt.Errorf("failed to query WhatsApp app: %w", err)
	}

	// Validate app is configured
	if waApp.GupshupAppID == "" {
		return fmt.Errorf("WhatsApp app missing Gupshup App ID for tenant %d", tenantID)
	}

	if waApp.PhoneNumber == "" {
		return fmt.Errorf("WhatsApp app missing phone number for tenant %d", tenantID)
	}

	// Decrypt token (if needed - verify if Partner API key is sufficient)
	// For now, we'll use Partner API key directly (not app token)
	// If app token is required, uncomment below:
	/*
	decryptedToken, err := utils.DecryptToken(waApp.GupshupAppToken)
	if err != nil {
		return fmt.Errorf("failed to decrypt app token: %w", err)
	}
	*/

	// Send message via Partner API
	if err := p.PartnerClient.SendMessage(ctx, waApp.GupshupAppID, waApp.PhoneNumber, to, text); err != nil {
		if p.Logger != nil {
			p.Logger.Error("Failed to send WhatsApp message via Gupshup Partner",
				zap.Uint("tenant_id", tenantID),
				zap.String("app_id", waApp.GupshupAppID),
				zap.String("to", to),
				zap.Error(err))
		}
		return fmt.Errorf("send message failed: %w", err)
	}

	if p.Logger != nil {
		p.Logger.Info("✅ WhatsApp message sent via Gupshup Partner",
			zap.Uint("tenant_id", tenantID),
			zap.String("app_id", waApp.GupshupAppID),
			zap.String("to", to))
	}

	return nil
}

