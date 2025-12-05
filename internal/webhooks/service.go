package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// WebhookService handles webhook delivery
type WebhookService struct {
	DB     *gorm.DB
	Logger *zap.Logger
	Client *http.Client
}

// NewWebhookService creates a new webhook service
func NewWebhookService(db *gorm.DB, logger *zap.Logger) *WebhookService {
	return &WebhookService{
		DB:     db,
		Logger: logger,
		Client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// SendWebhook sends a webhook to tenant's configured webhook URL
func (s *WebhookService) SendWebhook(ctx context.Context, tenantID uint, event string, payload map[string]interface{}) error {
	// Get tenant webhook URL from settings
	var tenant storage.Tenant
	if err := s.DB.Where("id = ?", tenantID).First(&tenant).Error; err != nil {
		return fmt.Errorf("tenant not found: %w", err)
	}

	// Parse webhook URL from tenant settings (stored in IndustrySettings JSONB)
	// For now, we'll check if there's a webhook_url field
	// In production, you might want to add a dedicated WebhookURL field to Tenant model
	webhookURL := "" // TODO: Extract from tenant.IndustrySettings or add WebhookURL field

	if webhookURL == "" {
		s.Logger.Debug("webhook URL not configured for tenant", zap.Uint("tenant_id", tenantID))
		return nil // Not an error - webhooks are optional
	}

	// Prepare webhook payload
	webhookPayload := map[string]interface{}{
		"event":     event,
		"tenant_id": tenantID,
		"data":      payload,
		"timestamp": time.Now().Unix(),
	}

	jsonData, err := json.Marshal(webhookPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	// Send webhook
	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Brancr-Webhook/1.0")

	resp, err := s.Client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned error status: %d", resp.StatusCode)
	}

	s.Logger.Info("webhook sent successfully",
		zap.Uint("tenant_id", tenantID),
		zap.String("event", event),
		zap.String("url", webhookURL))

	return nil
}

