package social

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// MetaWebhookHandler handles Meta webhooks (Instagram, Facebook, WhatsApp) with tenant resolution
type MetaWebhookHandler struct {
	DB             *gorm.DB
	Logger         *zap.Logger
	MessageHandler *MessageHandler
}

// NewMetaWebhookHandler creates a new Meta webhook handler
func NewMetaWebhookHandler(db *gorm.DB, logger *zap.Logger, msgHandler *MessageHandler) *MetaWebhookHandler {
	return &MetaWebhookHandler{
		DB:             db,
		Logger:         logger,
		MessageHandler: msgHandler,
	}
}

// HandleMetaWebhook processes Meta webhook events (single global endpoint for all tenants)
func (h *MetaWebhookHandler) HandleMetaWebhook(w http.ResponseWriter, r *http.Request) {
	// Webhook verification (GET request) - Meta sends this during webhook setup
	if r.Method == http.MethodGet {
		h.handleWebhookVerification(w, r)
		return
	}

	// Webhook event processing (POST request) - Meta sends events here after verification
	if r.Method == http.MethodPost {
		h.handleWebhookEvent(w, r)
		return
	}

	// Method not allowed for other methods
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// handleWebhookVerification handles Meta webhook verification (single global verification)
func (h *MetaWebhookHandler) handleWebhookVerification(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("hub.mode")
	token := r.URL.Query().Get("hub.verify_token")
	challenge := r.URL.Query().Get("hub.challenge")

	// Log all incoming parameters for debugging
	h.Logger.Info("🔍 Meta webhook verification request received",
		zap.String("method", r.Method),
		zap.String("url", r.URL.String()),
		zap.String("mode", mode),
		zap.String("token_received", token),
		zap.String("challenge", challenge))

	// Get verify token from environment variable
	expectedToken := strings.TrimSpace(os.Getenv("META_WEBHOOK_VERIFY_TOKEN"))
	if expectedToken == "" {
		expectedToken = strings.TrimSpace(os.Getenv("WEBHOOK_VERIFY_TOKEN"))
	}

	if expectedToken == "" {
		h.Logger.Error("❌ Webhook verification failed — META_WEBHOOK_VERIFY_TOKEN not set in environment")
		http.Error(w, "Server configuration error", http.StatusInternalServerError)
		return
	}

	// Meta requires: mode=subscribe AND verify_token matches exactly (case-sensitive)
	if mode != "subscribe" {
		h.Logger.Warn("❌ Webhook verification failed — invalid mode",
			zap.String("mode", mode),
			zap.String("expected", "subscribe"))
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if token == "" {
		h.Logger.Warn("❌ Webhook verification failed — no verify token provided")
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Verify token matches exactly (case-sensitive, no trimming)
	if token != expectedToken {
		h.Logger.Warn("❌ Webhook verification failed — token mismatch",
			zap.String("expected", expectedToken),
			zap.String("received", token),
			zap.Bool("case_sensitive_match", token == expectedToken),
			zap.Int("expected_length", len(expectedToken)),
			zap.Int("received_length", len(token)))
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if challenge == "" {
		h.Logger.Warn("❌ Webhook verification failed — no challenge provided")
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Meta requires: plain text response with ONLY the challenge string (no newlines, no extra content)
	// Content-Type must be text/plain, HTTP 200 OK
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)

	// Write ONLY the challenge string - no newlines, no extra characters
	n, err := fmt.Fprint(w, challenge)
	if err != nil {
		h.Logger.Error("❌ Failed to write challenge response", zap.Error(err))
		return
	}

	h.Logger.Info("✅ Meta webhook verified successfully",
		zap.String("challenge", challenge),
		zap.Int("bytes_written", n))
}

// handleWebhookEvent processes incoming webhook events and routes to correct tenant
func (h *MetaWebhookHandler) handleWebhookEvent(w http.ResponseWriter, r *http.Request) {
	h.Logger.Info("📨 Meta webhook event received (POST)",
		zap.String("method", r.Method),
		zap.String("url", r.URL.String()),
		zap.String("content_type", r.Header.Get("Content-Type")),
		zap.String("user_agent", r.Header.Get("User-Agent")))

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.Logger.Error("❌ Failed to read webhook body", zap.Error(err))
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	h.Logger.Info("📦 Webhook payload received",
		zap.Int("body_size", len(body)),
		zap.String("body_preview", string(body[:min(200, len(body))])))

	// Parse payload to determine platform and extract identifiers
	var basePayload struct {
		Object string          `json:"object"`
		Entry  json.RawMessage `json:"entry"`
	}

	if err := json.Unmarshal(body, &basePayload); err != nil {
		h.Logger.Error("❌ Failed to parse webhook payload", zap.Error(err), zap.String("raw_body", string(body)))
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	h.Logger.Info("🔍 Webhook payload parsed",
		zap.String("object_type", basePayload.Object))

	ctx := context.Background()

	// Route based on object type
	switch basePayload.Object {
	case "instagram":
		// Instagram messages use a different structure than Facebook
		h.Logger.Info("📱 Processing Instagram webhook", zap.String("platform", "instagram"))
		h.processInstagramWebhook(ctx, body)
	case "page":
		// Facebook messages
		h.Logger.Info("📱 Processing Facebook webhook", zap.String("platform", "page"))
		h.processFacebookWebhook(ctx, body)
	case "whatsapp_business_account":
		h.Logger.Info("📱 Processing WhatsApp webhook")
		h.processWhatsAppWebhook(ctx, body)
	case "permissions":
		// Permissions change event - ignore but log
		h.Logger.Info("📋 Permissions change event received (ignoring)", zap.String("object", "permissions"))
	default:
		h.Logger.Warn("⚠️ Unknown webhook object type",
			zap.String("object", basePayload.Object),
			zap.String("raw_payload", string(body)))
	}

	h.Logger.Info("✅ Webhook event processed successfully")
	w.WriteHeader(http.StatusOK)
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// processInstagramWebhook processes Instagram webhook events
// Instagram messaging uses the same structure as Facebook but with object="instagram"
func (h *MetaWebhookHandler) processInstagramWebhook(ctx context.Context, body []byte) {
	h.Logger.Info("📱 Parsing Instagram webhook payload")

	// Log full payload for debugging
	var rawPayload map[string]interface{}
	if err := json.Unmarshal(body, &rawPayload); err == nil {
		payloadJSON, _ := json.MarshalIndent(rawPayload, "", "  ")
		h.Logger.Info("📋 Full Instagram webhook payload",
			zap.String("payload", string(payloadJSON)))
	}

	var payload struct {
		Object string `json:"object"`
		Entry  []struct {
			ID        string `json:"id"` // This is the instagram_business_account.id
			Time      int64  `json:"time"`
			Messaging []struct {
				Sender struct {
					ID string `json:"id"`
				} `json:"sender"`
				Recipient struct {
					ID string `json:"id"` // This is the instagram_business_account.id
				} `json:"recipient"`
				Timestamp int64 `json:"timestamp"`
				Message   struct {
					MID  string `json:"mid"`
					Text string `json:"text"`
				} `json:"message"`
			} `json:"messaging"`
		} `json:"entry"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		h.Logger.Error("❌ Failed to decode Instagram webhook payload",
			zap.Error(err),
			zap.String("raw_body", string(body)))
		return
	}

	h.Logger.Info("📋 Instagram webhook decoded",
		zap.String("platform", "instagram"),
		zap.Int("entries_count", len(payload.Entry)))

	if len(payload.Entry) == 0 {
		h.Logger.Warn("⚠️ Instagram webhook has no entries")
		return
	}

	for i, entry := range payload.Entry {
		h.Logger.Info("📬 Processing Instagram webhook entry",
			zap.Int("entry_index", i),
			zap.String("entry_id", entry.ID),
			zap.Int("messaging_count", len(entry.Messaging)))

		if len(entry.Messaging) == 0 {
			h.Logger.Warn("⚠️ Instagram entry has no messaging events",
				zap.String("entry_id", entry.ID))
			continue
		}

		// Resolve tenant from entry ID (instagram_business_account.id)
		tenantID, err := h.resolveTenantFromExternalID(entry.ID, "instagram")
		if err != nil {
			h.Logger.Warn("⚠️ Could not resolve tenant from Instagram webhook",
				zap.String("platform", "instagram"),
				zap.String("external_id", entry.ID),
				zap.Error(err))
			continue
		}

		h.Logger.Info("✅ Tenant resolved for Instagram",
			zap.Uint("tenant_id", tenantID),
			zap.String("external_id", entry.ID),
			zap.String("platform", "instagram"))

		// Process each message
		for j, msg := range entry.Messaging {
			h.Logger.Info("💬 Processing Instagram message",
				zap.Int("message_index", j),
				zap.Uint("tenant_id", tenantID),
				zap.String("sender_id", msg.Sender.ID),
				zap.String("recipient_id", msg.Recipient.ID),
				zap.String("message_id", msg.Message.MID),
				zap.String("message_preview", truncateString(msg.Message.Text, 100)),
				zap.Int64("timestamp", msg.Timestamp))

			incomingMsg := IncomingMessage{
				Platform:         "instagram",
				TenantID:         tenantID,
				CustomerID:       msg.Sender.ID,
				CustomerName:     "",
				CustomerUsername: "",
				MessageID:        msg.Message.MID,
				Content:          msg.Message.Text,
				MessageType:      "text",
				ThreadID:         msg.Sender.ID,
				Timestamp:        time.Unix(msg.Timestamp/1000, 0),
				Metadata:         map[string]interface{}{"entry_id": entry.ID},
			}

			h.Logger.Info("🚀 Sending Instagram message to MessageHandler",
				zap.Uint("tenant_id", tenantID),
				zap.String("customer_id", msg.Sender.ID))

			go h.MessageHandler.ProcessMessage(ctx, incomingMsg)
		}
	}
}

// processFacebookWebhook processes Facebook webhook events
func (h *MetaWebhookHandler) processFacebookWebhook(ctx context.Context, body []byte) {
	h.Logger.Info("📱 Parsing Facebook webhook payload")

	var payload struct {
		Object string `json:"object"`
		Entry  []struct {
			ID        string `json:"id"` // This is the page_id
			Time      int64  `json:"time"`
			Messaging []struct {
				Sender struct {
					ID string `json:"id"`
				} `json:"sender"`
				Recipient struct {
					ID string `json:"id"` // This is the page_id
				} `json:"recipient"`
				Timestamp int64 `json:"timestamp"`
				Message   struct {
					MID  string `json:"mid"`
					Text string `json:"text"`
				} `json:"message"`
			} `json:"messaging"`
		} `json:"entry"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		h.Logger.Error("❌ Failed to decode Facebook webhook payload", zap.Error(err))
		return
	}

	h.Logger.Info("📋 Facebook webhook decoded",
		zap.String("platform", "facebook"),
		zap.Int("entries_count", len(payload.Entry)))

	for i, entry := range payload.Entry {
		h.Logger.Info("📬 Processing Facebook webhook entry",
			zap.Int("entry_index", i),
			zap.String("entry_id", entry.ID),
			zap.Int("messages_count", len(entry.Messaging)))

		// Resolve tenant from entry ID (page_id)
		tenantID, err := h.resolveTenantFromExternalID(entry.ID, "facebook")
		if err != nil {
			h.Logger.Warn("⚠️ Could not resolve tenant from Facebook webhook",
				zap.String("platform", "facebook"),
				zap.String("external_id", entry.ID),
				zap.Error(err))
			continue
		}

		h.Logger.Info("✅ Tenant resolved for Facebook",
			zap.Uint("tenant_id", tenantID),
			zap.String("external_id", entry.ID),
			zap.String("platform", "facebook"))

		// Process each message
		for j, msg := range entry.Messaging {
			h.Logger.Info("💬 Processing Facebook message",
				zap.Int("message_index", j),
				zap.Uint("tenant_id", tenantID),
				zap.String("sender_id", msg.Sender.ID),
				zap.String("message_id", msg.Message.MID),
				zap.String("message_preview", truncateString(msg.Message.Text, 100)))

			incomingMsg := IncomingMessage{
				Platform:         "facebook",
				TenantID:         tenantID,
				CustomerID:       msg.Sender.ID,
				CustomerName:     "",
				CustomerUsername: "",
				MessageID:        msg.Message.MID,
				Content:          msg.Message.Text,
				MessageType:      "text",
				ThreadID:         msg.Sender.ID,
				Timestamp:        time.Unix(msg.Timestamp/1000, 0),
				Metadata:         map[string]interface{}{"entry_id": entry.ID},
			}

			go h.MessageHandler.ProcessMessage(ctx, incomingMsg)
		}
	}
}

// processWhatsAppWebhook processes WhatsApp Business API webhook events
func (h *MetaWebhookHandler) processWhatsAppWebhook(ctx context.Context, body []byte) {
	var payload struct {
		Object string `json:"object"`
		Entry  []struct {
			ID      string `json:"id"`
			Changes []struct {
				Value struct {
					MessagingProduct string `json:"messaging_product"`
					Metadata         struct {
						DisplayPhoneNumber string `json:"display_phone_number"`
						PhoneNumberID      string `json:"phone_number_id"` // This is used for tenant resolution
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

	if err := json.Unmarshal(body, &payload); err != nil {
		h.Logger.Error("❌ Failed to decode WhatsApp webhook", zap.Error(err))
		return
	}

	h.Logger.Info("📋 WhatsApp webhook decoded",
		zap.Int("entries_count", len(payload.Entry)))

	for i, entry := range payload.Entry {
		h.Logger.Info("📬 Processing WhatsApp entry",
			zap.Int("entry_index", i),
			zap.String("entry_id", entry.ID),
			zap.Int("changes_count", len(entry.Changes)))

		for j, change := range entry.Changes {
			phoneNumberID := change.Value.Metadata.PhoneNumberID

			h.Logger.Info("📞 Processing WhatsApp change",
				zap.Int("change_index", j),
				zap.String("phone_number_id", phoneNumberID),
				zap.String("display_phone_number", change.Value.Metadata.DisplayPhoneNumber),
				zap.Int("messages_count", len(change.Value.Messages)))

			// Resolve tenant from phone_number_id
			tenantID, err := h.resolveTenantFromExternalID(phoneNumberID, "whatsapp")
			if err != nil {
				h.Logger.Warn("⚠️ Could not resolve tenant from WhatsApp webhook",
					zap.String("phone_number_id", phoneNumberID),
					zap.Error(err))
				continue
			}

			h.Logger.Info("✅ Tenant resolved for WhatsApp",
				zap.Uint("tenant_id", tenantID),
				zap.String("phone_number_id", phoneNumberID))

			// Process each message
			for k, msg := range change.Value.Messages {
				h.Logger.Info("💬 Processing WhatsApp message",
					zap.Int("message_index", k),
					zap.Uint("tenant_id", tenantID),
					zap.String("from", msg.From),
					zap.String("message_id", msg.ID),
					zap.String("message_type", msg.Type),
					zap.String("message_preview", truncateString(msg.Text.Body, 100)))

				timestamp, _ := strconv.ParseInt(msg.Timestamp, 10, 64)

				incomingMsg := IncomingMessage{
					Platform:         "whatsapp",
					TenantID:         tenantID,
					CustomerID:       msg.From,
					CustomerName:     "",
					CustomerUsername: "",
					MessageID:        msg.ID,
					Content:          msg.Text.Body,
					MessageType:      msg.Type,
					ThreadID:         msg.From,
					Timestamp:        time.Unix(timestamp, 0),
					Metadata: map[string]interface{}{
						"phone_number_id": phoneNumberID,
					},
				}

				go h.MessageHandler.ProcessMessage(ctx, incomingMsg)
			}
		}
	}
}

// resolveTenantFromExternalID finds the tenant ID by matching external ID (page_id, instagram_business_account.id, phone_number_id)
func (h *MetaWebhookHandler) resolveTenantFromExternalID(externalID, platform string) (uint, error) {
	var account storage.SocialAccount

	h.Logger.Debug("🔍 Resolving tenant",
		zap.String("external_id", externalID),
		zap.String("platform", platform))

	// Try ExternalID first (most reliable)
	if err := h.DB.Where("external_id = ? AND platform = ? AND connected = ?", externalID, platform, true).First(&account).Error; err == nil {
		h.Logger.Info("✅ Tenant found via ExternalID",
			zap.Uint("tenant_id", account.TenantID),
			zap.String("external_id", externalID))
		return account.TenantID, nil
	}

	// Fallback to PageID for backward compatibility
	if err := h.DB.Where("page_id = ? AND platform = ? AND connected = ?", externalID, platform, true).First(&account).Error; err == nil {
		// Update ExternalID for future lookups
		account.ExternalID = externalID
		h.DB.Save(&account)
		h.Logger.Info("✅ Tenant found via PageID (updated ExternalID)",
			zap.Uint("tenant_id", account.TenantID),
			zap.String("external_id", externalID))
		return account.TenantID, nil
	}

	h.Logger.Warn("⚠️ No tenant found",
		zap.String("external_id", externalID),
		zap.String("platform", platform))
	return 0, fmt.Errorf("no tenant found for external_id=%s platform=%s", externalID, platform)
}

// truncateString truncates a string to a maximum length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
