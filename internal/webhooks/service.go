package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
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
func (s *WebhookService) SendWebhook(ctx context.Context, tenantID uuid.UUID, event string, payload map[string]interface{}) error {
	var settings storage.TenantNotificationSettings
	if err := s.DB.WithContext(ctx).Where("tenant_id = ?", tenantID).First(&settings).Error; err != nil {
		// If settings are missing, treat webhooks as not configured.
		s.Logger.Debug("notification settings not found for tenant", zap.Stringer("tenant_id", tenantID))
		return nil
	}

	if !settings.WebhookEnabled || settings.WebhookURL == "" {
		s.Logger.Debug("webhooks disabled or missing url", zap.Stringer("tenant_id", tenantID))
		return nil
	}

	if !eventAllowed(settings.WebhookEvents, event) {
		s.Logger.Debug("webhook event not enabled", zap.Stringer("tenant_id", tenantID), zap.String("event", event))
		return nil
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
	req, err := http.NewRequestWithContext(ctx, "POST", settings.WebhookURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Brancr-Webhook/1.0")
	if settings.WebhookSecret != "" {
		req.Header.Set("X-Brancr-Signature", generateSignature(jsonData, settings.WebhookSecret))
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned error status: %d", resp.StatusCode)
	}

	s.Logger.Info("webhook sent successfully",
		zap.Stringer("tenant_id", tenantID),
		zap.String("event", event),
		zap.String("url", settings.WebhookURL))

	return nil
}

func eventAllowed(eventsJSON string, event string) bool {
	if eventsJSON == "" {
		return true
	}
	var events []string
	if err := json.Unmarshal([]byte(eventsJSON), &events); err != nil {
		return true
	}
	if len(events) == 0 {
		return true
	}
	for _, e := range events {
		if e == event {
			return true
		}
	}
	return false
}

func generateSignature(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
