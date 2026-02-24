package tenantapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/storage/repositories"
	"github.com/similadayo/brancr/internal/webhooks"
	"go.uber.org/zap"
)

type notificationSettingsPayload struct {
	EmailNotificationsEnabled bool       `json:"email_notifications_enabled"`
	EmailOrderStatusChanges   bool       `json:"email_order_status_changes"`
	EmailPaymentConfirmations bool       `json:"email_payment_confirmations"`
	EmailReceiptGenerated     bool       `json:"email_receipt_generated"`
	WebhookEnabled            bool       `json:"webhook_enabled"`
	WebhookURL                string     `json:"webhook_url"`
	WebhookEvents             []string   `json:"webhook_events"`
	WebhookSecret             string     `json:"webhook_secret"`
	LastTestAt                *time.Time `json:"last_test_at,omitempty"`
	LastTestStatus            string     `json:"last_test_status"`
}

// getNotificationSettings returns tenant notification settings.
func (h *Handler) getNotificationSettings(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	repo := repositories.NewNotificationSettingsRepository(h.DB)
	settings, err := repo.GetOrCreateDefault(r.Context(), claims.TenantID)
	if err != nil {
		h.Logger.Error("failed to load notification settings", zap.Error(err))
		http.Error(w, "failed to load notification settings", http.StatusInternalServerError)
		return
	}

	var events []string
	if settings.WebhookEvents != "" {
		_ = json.Unmarshal([]byte(settings.WebhookEvents), &events)
	}
	if events == nil {
		events = []string{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"settings": notificationSettingsPayload{
			EmailNotificationsEnabled: settings.EmailNotificationsEnabled,
			EmailOrderStatusChanges:   settings.EmailOrderStatusChanges,
			EmailPaymentConfirmations: settings.EmailPaymentConfirmations,
			EmailReceiptGenerated:     settings.EmailReceiptGenerated,
			WebhookEnabled:            settings.WebhookEnabled,
			WebhookURL:                settings.WebhookURL,
			WebhookEvents:             events,
			WebhookSecret:             settings.WebhookSecret,
			LastTestAt:                settings.LastTestAt,
			LastTestStatus:            settings.LastTestStatus,
		},
	})
}

// updateNotificationSettings updates tenant notification settings.
func (h *Handler) updateNotificationSettings(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Settings *notificationSettingsPayload `json:"settings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Settings == nil {
		http.Error(w, "settings is required", http.StatusBadRequest)
		return
	}

	if req.Settings.WebhookEnabled {
		if req.Settings.WebhookURL == "" {
			http.Error(w, "webhook_url is required when webhook_enabled is true", http.StatusBadRequest)
			return
		}
		u, err := url.Parse(req.Settings.WebhookURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			http.Error(w, "invalid webhook_url", http.StatusBadRequest)
			return
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			http.Error(w, "webhook_url must be http or https", http.StatusBadRequest)
			return
		}
	}

	eventsJSON, err := json.Marshal(req.Settings.WebhookEvents)
	if err != nil {
		http.Error(w, "invalid webhook_events", http.StatusBadRequest)
		return
	}

	repo := repositories.NewNotificationSettingsRepository(h.DB)
	settings, err := repo.GetOrCreateDefault(r.Context(), claims.TenantID)
	if err != nil {
		h.Logger.Error("failed to load notification settings", zap.Error(err))
		http.Error(w, "failed to load notification settings", http.StatusInternalServerError)
		return
	}

	settings.EmailNotificationsEnabled = req.Settings.EmailNotificationsEnabled
	settings.EmailOrderStatusChanges = req.Settings.EmailOrderStatusChanges
	settings.EmailPaymentConfirmations = req.Settings.EmailPaymentConfirmations
	settings.EmailReceiptGenerated = req.Settings.EmailReceiptGenerated
	settings.WebhookEnabled = req.Settings.WebhookEnabled
	settings.WebhookURL = req.Settings.WebhookURL
	settings.WebhookEvents = string(eventsJSON)
	settings.WebhookSecret = req.Settings.WebhookSecret

	if err := repo.Update(r.Context(), settings); err != nil {
		h.Logger.Error("failed to update notification settings", zap.Error(err))
		http.Error(w, "failed to update notification settings", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
	})
}

// testNotificationSettings tests delivery for configured email/webhook settings.
func (h *Handler) testNotificationSettings(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	repo := repositories.NewNotificationSettingsRepository(h.DB)
	settings, err := repo.GetOrCreateDefault(r.Context(), claims.TenantID)
	if err != nil {
		h.Logger.Error("failed to load notification settings", zap.Error(err))
		http.Error(w, "failed to load notification settings", http.StatusInternalServerError)
		return
	}

	// Load tenant for email destination
	var tenant storage.Tenant
	if err := h.DB.WithContext(r.Context()).First(&tenant, claims.TenantID).Error; err != nil {
		h.Logger.Error("failed to load tenant", zap.Error(err))
		http.Error(w, "failed to load tenant", http.StatusInternalServerError)
		return
	}

	emailOK := true
	webhookOK := true

	// Test email
	if settings.EmailNotificationsEnabled && settings.EmailOrderStatusChanges {
		if h.EmailService == nil {
			emailOK = false
		} else if tenant.Email == "" {
			emailOK = false
		} else {
			if err := h.EmailService.SendOrderStatusEmail(tenant.Email, tenant.OwnerName, "TEST-NOTIFICATION", "test", ""); err != nil {
				h.Logger.Warn("failed to send test email", zap.Error(err))
				emailOK = false
			}
		}
	}

	// Test webhook
	if settings.WebhookEnabled {
		wh := webhooks.NewWebhookService(h.DB, h.Logger)
		if err := wh.SendWebhook(r.Context(), claims.TenantID, "notifications.test", map[string]interface{}{
			"event":     "notifications.test",
			"tenant_id": claims.TenantID,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}); err != nil {
			h.Logger.Warn("failed to send test webhook", zap.Error(err))
			webhookOK = false
		}
	}

	now := time.Now()
	settings.LastTestAt = &now
	if emailOK && webhookOK {
		settings.LastTestStatus = "success"
	} else {
		settings.LastTestStatus = "failed"
	}
	_ = repo.Update(r.Context(), settings)

	writeJSON(w, http.StatusOK, map[string]any{
		"success":         emailOK && webhookOK,
		"email_success":   emailOK,
		"webhook_success": webhookOK,
	})
}
