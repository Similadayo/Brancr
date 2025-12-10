package tenantapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// =======================
// Webhook Management
// =======================

// listWebhooks lists all configured webhook URLs for the tenant
func (h *Handler) listWebhooks(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var tenant storage.Tenant
	if err := h.DB.First(&tenant, claims.TenantID).Error; err != nil {
		http.Error(w, "tenant not found", http.StatusNotFound)
		return
	}

	// Parse webhook URLs from metadata
	var webhookURLs []string
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

	writeJSON(w, http.StatusOK, map[string]any{
		"webhook_urls":     webhookURLs,
		"webhook_secret":   tenant.WebhookSecret != "",
		"supported_events": []string{"payment.created", "payment.verified", "payment.completed", "payment.failed"},
	})
}

// addWebhook adds a new webhook URL for the tenant
func (h *Handler) addWebhook(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		URL string `json:"url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.URL == "" {
		http.Error(w, "url is required", http.StatusBadRequest)
		return
	}

	var tenant storage.Tenant
	if err := h.DB.First(&tenant, claims.TenantID).Error; err != nil {
		http.Error(w, "tenant not found", http.StatusNotFound)
		return
	}

	// Parse existing metadata
	var metadata map[string]interface{}
	if tenant.Metadata != "" {
		if err := json.Unmarshal([]byte(tenant.Metadata), &metadata); err != nil {
			metadata = make(map[string]interface{})
		}
	} else {
		metadata = make(map[string]interface{})
	}

	// Get existing webhook URLs
	var webhookURLs []string
	if urls, ok := metadata["webhook_urls"].([]interface{}); ok {
		for _, url := range urls {
			if urlStr, ok := url.(string); ok {
				webhookURLs = append(webhookURLs, urlStr)
			}
		}
	}

	// Check if URL already exists
	for _, url := range webhookURLs {
		if url == req.URL {
			http.Error(w, "webhook URL already exists", http.StatusConflict)
			return
		}
	}

	// Add new URL
	webhookURLs = append(webhookURLs, req.URL)
	metadata["webhook_urls"] = webhookURLs

	// Update tenant metadata
	metadataJSON, _ := json.Marshal(metadata)
	tenant.Metadata = string(metadataJSON)

	// Generate webhook secret if not exists
	if tenant.WebhookSecret == "" {
		secret := make([]byte, 32)
		rand.Read(secret)
		tenant.WebhookSecret = hex.EncodeToString(secret)
	}

	if err := h.DB.Save(&tenant).Error; err != nil {
		h.Logger.Error("failed to save webhook URL", zap.Error(err))
		http.Error(w, "failed to save webhook", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":        true,
		"message":        "webhook URL added successfully",
		"webhook_url":    req.URL,
		"webhook_secret": tenant.WebhookSecret,
	})
}

// removeWebhook removes a webhook URL for the tenant
func (h *Handler) removeWebhook(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		URL string `json:"url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.URL == "" {
		http.Error(w, "url is required", http.StatusBadRequest)
		return
	}

	var tenant storage.Tenant
	if err := h.DB.First(&tenant, claims.TenantID).Error; err != nil {
		http.Error(w, "tenant not found", http.StatusNotFound)
		return
	}

	// Parse existing metadata
	var metadata map[string]interface{}
	if tenant.Metadata != "" {
		if err := json.Unmarshal([]byte(tenant.Metadata), &metadata); err != nil {
			http.Error(w, "invalid metadata", http.StatusInternalServerError)
			return
		}
	} else {
		http.Error(w, "no webhooks configured", http.StatusNotFound)
		return
	}

	// Get existing webhook URLs
	var webhookURLs []string
	if urls, ok := metadata["webhook_urls"].([]interface{}); ok {
		for _, url := range urls {
			if urlStr, ok := url.(string); ok && urlStr != req.URL {
				webhookURLs = append(webhookURLs, urlStr)
			}
		}
	}

	// Update metadata
	metadata["webhook_urls"] = webhookURLs
	metadataJSON, _ := json.Marshal(metadata)
	tenant.Metadata = string(metadataJSON)

	if err := h.DB.Save(&tenant).Error; err != nil {
		h.Logger.Error("failed to remove webhook URL", zap.Error(err))
		http.Error(w, "failed to remove webhook", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "webhook URL removed successfully",
	})
}

// regenerateWebhookSecret regenerates the webhook secret for the tenant
func (h *Handler) regenerateWebhookSecret(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var tenant storage.Tenant
	if err := h.DB.First(&tenant, claims.TenantID).Error; err != nil {
		http.Error(w, "tenant not found", http.StatusNotFound)
		return
	}

	// Generate new secret
	secret := make([]byte, 32)
	rand.Read(secret)
	tenant.WebhookSecret = hex.EncodeToString(secret)

	if err := h.DB.Save(&tenant).Error; err != nil {
		h.Logger.Error("failed to regenerate webhook secret", zap.Error(err))
		http.Error(w, "failed to regenerate secret", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":        true,
		"message":        "webhook secret regenerated",
		"webhook_secret": tenant.WebhookSecret,
	})
}
