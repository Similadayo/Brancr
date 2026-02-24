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

// PaymentWebhookService handles webhook notifications for payment events
type PaymentWebhookService struct {
	DB     *gorm.DB
	Logger *zap.Logger
	Client *http.Client
}

// NewPaymentWebhookService creates a new webhook service
func NewPaymentWebhookService(db *gorm.DB, logger *zap.Logger) *PaymentWebhookService {
	return &PaymentWebhookService{
		DB:     db,
		Logger: logger,
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// PaymentWebhookPayload is the payload sent to webhook endpoints
type PaymentWebhookPayload struct {
	Event     string    `json:"event"` // payment.created, payment.verified, payment.completed, payment.failed
	Timestamp time.Time `json:"timestamp"`
	Data      struct {
		PaymentID          uuid.UUID `json:"payment_id"`
		PaymentReference   string    `json:"payment_reference"`
		TenantID           uuid.UUID `json:"tenant_id"`
		CustomerID         uuid.UUID `json:"customer_id"`
		Amount             float64   `json:"amount"`
		Currency           string    `json:"currency"`
		Status             string    `json:"status"`
		VerificationStatus string    `json:"verification_status"`
		PaymentMethod      string    `json:"payment_method"`
		CreatedAt          string    `json:"created_at"`
		VerifiedAt         *string   `json:"verified_at,omitempty"`
	} `json:"data"`
}

// NotifyPaymentCreated sends webhook notification when payment is created (pending status)
func (s *PaymentWebhookService) NotifyPaymentCreated(ctx context.Context, payment *storage.Payment) error {
	return s.sendWebhook(ctx, "payment.created", payment)
}

// NotifyPaymentVerified sends webhook notification when payment is verified
func (s *PaymentWebhookService) NotifyPaymentVerified(ctx context.Context, payment *storage.Payment) error {
	return s.sendWebhook(ctx, "payment.verified", payment)
}

// NotifyPaymentCompleted sends webhook notification when payment is completed
func (s *PaymentWebhookService) NotifyPaymentCompleted(ctx context.Context, payment *storage.Payment) error {
	return s.sendWebhook(ctx, "payment.completed", payment)
}

// NotifyPaymentFailed sends webhook notification when payment fails
func (s *PaymentWebhookService) NotifyPaymentFailed(ctx context.Context, payment *storage.Payment) error {
	return s.sendWebhook(ctx, "payment.failed", payment)
}

// sendWebhook sends webhook notification to all configured endpoints for the tenant
func (s *PaymentWebhookService) sendWebhook(ctx context.Context, event string, payment *storage.Payment) error {
	// Prefer tenant notification settings model.
	var settings storage.TenantNotificationSettings
	if err := s.DB.WithContext(ctx).Where("tenant_id = ?", payment.TenantID).First(&settings).Error; err == nil {
		if !settings.WebhookEnabled || settings.WebhookURL == "" {
			return nil
		}
		if !eventAllowed(settings.WebhookEvents, event) {
			return nil
		}
		payload := s.buildPayload(event, payment)
		if err := s.sendToURL(ctx, settings.WebhookURL, payload, settings.WebhookSecret); err != nil {
			s.Logger.Error("failed to send webhook", zap.Error(err), zap.String("url", settings.WebhookURL), zap.String("event", event))
			return nil
		}
		return nil
	}

	// Fallback: legacy metadata webhook_urls.
	var tenant storage.Tenant
	if err := s.DB.First(&tenant, payment.TenantID).Error; err != nil {
		return fmt.Errorf("tenant not found: %w", err)
	}

	webhookURLs := s.getWebhookURLs(&tenant)
	if len(webhookURLs) == 0 {
		s.Logger.Debug("no webhook URLs configured for tenant", zap.Stringer("tenant_id", payment.TenantID))
		return nil
	}

	payload := s.buildPayload(event, payment)

	// Send to all configured webhook URLs
	for _, url := range webhookURLs {
		if err := s.sendToURL(ctx, url, payload, tenant.WebhookSecret); err != nil {
			s.Logger.Error("failed to send webhook",
				zap.Error(err),
				zap.String("url", url),
				zap.String("event", event))
			// Continue sending to other URLs even if one fails
		} else {
			s.Logger.Info("webhook sent successfully",
				zap.String("url", url),
				zap.String("event", event),
				zap.Stringer("payment_id", payment.ID))
		}
	}

	return nil
}

func (s *PaymentWebhookService) buildPayload(event string, payment *storage.Payment) *PaymentWebhookPayload {
	payload := &PaymentWebhookPayload{
		Event:     event,
		Timestamp: time.Now(),
	}
	payload.Data.PaymentID = payment.ID
	payload.Data.PaymentReference = payment.PaymentReference
	payload.Data.TenantID = payment.TenantID
	payload.Data.CustomerID = payment.CustomerID
	payload.Data.Amount = payment.Amount
	payload.Data.Currency = payment.Currency
	payload.Data.Status = payment.Status
	payload.Data.VerificationStatus = payment.VerificationStatus
	payload.Data.PaymentMethod = payment.PaymentMethod
	payload.Data.CreatedAt = payment.CreatedAt.Format(time.RFC3339)

	if payment.VerifiedAt != nil {
		verifiedAt := payment.VerifiedAt.Format(time.RFC3339)
		payload.Data.VerifiedAt = &verifiedAt
	}

	return payload
}

// sendToURL sends webhook payload to a specific URL
func (s *PaymentWebhookService) sendToURL(ctx context.Context, url string, payload *PaymentWebhookPayload, secret string) error {
	// Marshal payload to JSON
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Brancr-Webhooks/1.0")

	// Generate HMAC signature if secret is configured
	if secret != "" {
		signature := s.generateSignature(jsonData, secret)
		req.Header.Set("X-Brancr-Signature", signature)
	}

	// Send request
	resp, err := s.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned non-2xx status: %d", resp.StatusCode)
	}

	return nil
}

// generateSignature generates HMAC-SHA256 signature for webhook payload
func (s *PaymentWebhookService) generateSignature(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// getWebhookURLs extracts webhook URLs from tenant metadata
func (s *PaymentWebhookService) getWebhookURLs(tenant *storage.Tenant) []string {
	var webhookURLs []string

	// Try to parse from tenant metadata
	if tenant.Metadata != "" {
		var metadata map[string]interface{}
		if err := json.Unmarshal([]byte(tenant.Metadata), &metadata); err == nil {
			if urls, ok := metadata["webhook_urls"].([]interface{}); ok {
				for _, url := range urls {
					if urlStr, ok := url.(string); ok {
						webhookURLs = append(webhookURLs, urlStr)
					}
				}
			}
		}
	}

	return webhookURLs
}

// VerifyWebhookSignature verifies the HMAC signature of an incoming webhook
func VerifyWebhookSignature(payload []byte, signature string, secret string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}
