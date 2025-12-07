//go:build legacy

package social

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// WebhookHandler handles incoming webhooks from social platforms
// Deprecated: This handler is kept for potential non-Meta webhook integrations.
type WebhookHandler struct {
	DB             *gorm.DB
	Logger         *zap.Logger
	MessageHandler *MessageHandler
}

// NewWebhookHandler creates a new webhook handler
func NewWebhookHandler(db *gorm.DB, logger *zap.Logger, msgHandler *MessageHandler) *WebhookHandler {
	return &WebhookHandler{
		DB:             db,
		Logger:         logger,
		MessageHandler: msgHandler,
	}
}

// HandleInstagramWebhook processes Instagram webhook events
func (h *WebhookHandler) HandleInstagramWebhook(w http.ResponseWriter, r *http.Request, tenantID uint) {
	// Instagram webhook verification (Meta webhook verification)
	if r.Method == "GET" {
		mode := r.URL.Query().Get("hub.mode")
		token := r.URL.Query().Get("hub.verify_token")
		challenge := r.URL.Query().Get("hub.challenge")

		// Load social account to get stored verify token
		var account storage.SocialAccount
		if err := h.DB.Where("tenant_id = ? AND platform = ?", tenantID, "instagram").First(&account).Error; err != nil {
			h.Logger.Error("Instagram account not found for webhook verification", zap.Uint("tenant_id", tenantID))
			http.Error(w, "Account not found", http.StatusNotFound)
			return
		}

		// Meta requires: mode=subscribe AND verify_token matches
		// Get verify token from environment variable (set by system admin)
		expectedToken := os.Getenv("META_WEBHOOK_VERIFY_TOKEN")
		if expectedToken == "" {
			expectedToken = os.Getenv("WEBHOOK_VERIFY_TOKEN") // Fallback
		}

		if mode == "subscribe" && token != "" {
			// Verify token matches (if env var is set) or accept any token (for initial setup)
			if expectedToken != "" && token != expectedToken {
				h.Logger.Warn("Webhook verification failed — token mismatch", zap.Uint("tenant_id", tenantID))
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			// Save the token for future reference
			if account.WebhookVerifyToken == "" {
				account.WebhookVerifyToken = token
				h.DB.Save(&account)
				h.Logger.Info("Saved webhook verify token for Instagram", zap.Uint("tenant_id", tenantID))
			}

			// Meta requires: plain text response with only the challenge string
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, challenge)
			h.Logger.Info("✅ Webhook verified", zap.String("platform", "instagram"), zap.Uint("tenant_id", tenantID))
			return
		}

		h.Logger.Warn("Instagram webhook verification failed",
			zap.Uint("tenant_id", tenantID),
			zap.String("mode", mode),
			zap.Bool("token_matches", account.WebhookVerifyToken != "" && account.WebhookVerifyToken == token))
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Process webhook event
	var payload struct {
		Object string `json:"object"`
		Entry  []struct {
			ID        string `json:"id"`
			Time      int64  `json:"time"`
			Messaging []struct {
				Sender struct {
					ID string `json:"id"`
				} `json:"sender"`
				Recipient struct {
					ID string `json:"id"`
				} `json:"recipient"`
				Timestamp int64 `json:"timestamp"`
				Message   struct {
					MID  string `json:"mid"`
					Text string `json:"text"`
				} `json:"message"`
			} `json:"messaging"`
		} `json:"entry"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.Logger.Error("failed to decode Instagram webhook", zap.Error(err))
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// Process each message
	ctx := context.Background()
	for _, entry := range payload.Entry {
		for _, msg := range entry.Messaging {
			incomingMsg := IncomingMessage{
				Platform:          "instagram",
				TenantID:          tenantID,
				CustomerID:        msg.Sender.ID,
				CustomerName:      "", // Will be fetched or set
				CustomerUsername:  "",
				CustomerProfilePic: "",
				MessageID:         msg.Message.MID,
				Content:           msg.Message.Text,
				MessageType:       "text",
				ThreadID:          msg.Sender.ID,
				Timestamp:         time.Unix(msg.Timestamp/1000, 0),
				Metadata:          map[string]interface{}{"entry_id": entry.ID},
			}

			go h.MessageHandler.ProcessMessage(ctx, incomingMsg)
		}
	}

	w.WriteHeader(http.StatusOK)
}

// HandleFacebookWebhook processes Facebook webhook events
func (h *WebhookHandler) HandleFacebookWebhook(w http.ResponseWriter, r *http.Request, tenantID uint) {
	// Facebook webhook verification (Meta webhook verification)
	if r.Method == "GET" {
		mode := r.URL.Query().Get("hub.mode")
		token := r.URL.Query().Get("hub.verify_token")
		challenge := r.URL.Query().Get("hub.challenge")

		// Load social account to get stored verify token
		var account storage.SocialAccount
		if err := h.DB.Where("tenant_id = ? AND platform = ?", tenantID, "facebook").First(&account).Error; err != nil {
			h.Logger.Error("Facebook account not found for webhook verification", zap.Uint("tenant_id", tenantID))
			http.Error(w, "Account not found", http.StatusNotFound)
			return
		}

		// Meta requires: mode=subscribe AND verify_token matches
		// Get verify token from environment variable (set by system admin)
		expectedToken := os.Getenv("META_WEBHOOK_VERIFY_TOKEN")
		if expectedToken == "" {
			expectedToken = os.Getenv("WEBHOOK_VERIFY_TOKEN") // Fallback
		}

		if mode == "subscribe" && token != "" {
			// Verify token matches (if env var is set) or accept any token (for initial setup)
			if expectedToken != "" && token != expectedToken {
				h.Logger.Warn("Webhook verification failed — token mismatch", zap.Uint("tenant_id", tenantID))
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			// Save the token for future reference
			if account.WebhookVerifyToken == "" {
				account.WebhookVerifyToken = token
				h.DB.Save(&account)
				h.Logger.Info("Saved webhook verify token for Facebook", zap.Uint("tenant_id", tenantID))
			}

			// Meta requires: plain text response with only the challenge string
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, challenge)
			h.Logger.Info("✅ Webhook verified", zap.String("platform", "facebook"), zap.Uint("tenant_id", tenantID))
			return
		}

		h.Logger.Warn("Facebook webhook verification failed", zap.Uint("tenant_id", tenantID))
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Process webhook event (same as Instagram)
	h.HandleInstagramWebhook(w, r, tenantID)
}

// HandleWhatsAppWebhook processes WhatsApp Business API webhooks
func (h *WebhookHandler) HandleWhatsAppWebhook(w http.ResponseWriter, r *http.Request, tenantID uint) {
	var payload struct {
		Object string `json:"object"`
		Entry  []struct {
			ID      string `json:"id"`
			Changes []struct {
				Value struct {
					MessagingProduct string `json:"messaging_product"`
					Metadata         struct {
						DisplayPhoneNumber string `json:"display_phone_number"`
						PhoneNumberID      string `json:"phone_number_id"`
					} `json:"metadata"`
					Messages []struct {
						From      string `json:"from"`
						ID        string `json:"id"`
						Timestamp string `json:"timestamp"`
						Type      string `json:"type"`
						Text      struct {
							Body string `json:"body"`
						} `json:"text"`
					} `json:"messages"`
				} `json:"value"`
			} `json:"changes"`
		} `json:"entry"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.Logger.Error("failed to decode WhatsApp webhook", zap.Error(err))
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			for _, msg := range change.Value.Messages {
				timestamp, _ := strconv.ParseInt(msg.Timestamp, 10, 64)

				incomingMsg := IncomingMessage{
					Platform:          "whatsapp",
					TenantID:          tenantID,
					CustomerID:        msg.From,
					CustomerName:      "",
					CustomerUsername:  "",
					CustomerProfilePic: "",
					MessageID:         msg.ID,
					Content:           msg.Text.Body,
					MessageType:       msg.Type,
					ThreadID:          msg.From,
					Timestamp:         time.Unix(timestamp, 0),
					Metadata: map[string]interface{}{
						"phone_number_id": change.Value.Metadata.PhoneNumberID,
					},
				}

				go h.MessageHandler.ProcessMessage(ctx, incomingMsg)
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}

// ExtractTenantIDFromRequest extracts tenant ID from request (could be from URL, header, or webhook payload)
func (h *WebhookHandler) ExtractTenantIDFromRequest(r *http.Request) (uint, error) {
	// Try URL parameter first
	if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
		id, err := strconv.ParseUint(tenantIDStr, 10, 32)
		if err == nil {
			return uint(id), nil
		}
	}

	// Try header
	if tenantIDStr := r.Header.Get("X-Tenant-ID"); tenantIDStr != "" {
		id, err := strconv.ParseUint(tenantIDStr, 10, 32)
		if err == nil {
			return uint(id), nil
		}
	}

	return 0, fmt.Errorf("tenant ID not found in request")
}
