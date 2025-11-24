package social

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// GupshupWebhookHandler handles incoming webhooks from Gupshup
type GupshupWebhookHandler struct {
	DB             *gorm.DB
	Logger         *zap.Logger
	MessageHandler *MessageHandler
}

// NewGupshupWebhookHandler creates a new Gupshup webhook handler
func NewGupshupWebhookHandler(db *gorm.DB, logger *zap.Logger, msgHandler *MessageHandler) *GupshupWebhookHandler {
	return &GupshupWebhookHandler{
		DB:             db,
		Logger:         logger,
		MessageHandler: msgHandler,
	}
}

// HandleGupshupWebhook processes Gupshup Partner webhook events
// VERIFIED: Must acknowledge immediately (< 10 seconds, ideally < 1 second)
func (h *GupshupWebhookHandler) HandleGupshupWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// CRITICAL: Acknowledge immediately (within 10 seconds, ideally < 1 second)
	// Return 200 OK with empty body BEFORE processing
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("")) // Empty response body required

	// Process asynchronously after acknowledgment
	go h.processWebhookAsync(r.Context(), r)
}

// processWebhookAsync processes webhook events asynchronously
func (h *GupshupWebhookHandler) processWebhookAsync(ctx context.Context, r *http.Request) {
	h.Logger.Info("📨 Gupshup Partner webhook event received",
		zap.String("method", r.Method),
		zap.String("url", r.URL.String()),
		zap.String("content_type", r.Header.Get("Content-Type")))

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.Logger.Error("❌ Failed to read Gupshup webhook body", zap.Error(err))
		return
	}

	h.Logger.Info("📦 Gupshup webhook payload received",
		zap.Int("body_size", len(body)),
		zap.String("body_preview", truncateString(string(body), 200)))

	// Parse payload
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		// Try form-encoded as fallback
		if err := r.ParseForm(); err == nil {
			payload = make(map[string]interface{})
			for k, v := range r.Form {
				if len(v) > 0 {
					payload[k] = v[0]
				}
			}
		} else {
			h.Logger.Error("❌ Failed to parse Gupshup webhook payload", zap.Error(err))
			return
		}
	}

	h.Logger.Info("🔍 Gupshup webhook event parsed",
		zap.Any("payload_keys", getMapKeys(payload)))

	// VERIFIED: Extract appId from payload (Gupshup always sends this for Partner apps)
	appID := ""
	if val, ok := payload["app"].(string); ok {
		appID = val
	} else if val, ok := payload["appId"].(string); ok {
		appID = val
	} else if val, ok := payload["app_id"].(string); ok {
		appID = val
	}

	if appID == "" {
		h.Logger.Warn("⚠️ Webhook payload missing appId - cannot route to tenant",
			zap.Any("payload", payload))
		return
	}

	// VERIFIED: Route by appId → WhatsAppApp → tenantID
	var waApp storage.WhatsAppApp
	if err := h.DB.Where("gupshup_app_id = ?", appID).First(&waApp).Error; err != nil {
		h.Logger.Warn("⚠️ Unknown appId in webhook",
			zap.String("app_id", appID),
			zap.Error(err))
		return
	}

	tenantID := waApp.TenantID

	h.Logger.Info("✅ Resolved tenant from appId",
		zap.String("app_id", appID),
		zap.Uint("tenant_id", tenantID))

	// Process event based on type
	eventType := ""
	if val, ok := payload["event"].(string); ok {
		eventType = val
	} else if val, ok := payload["type"].(string); ok {
		eventType = val
	}

	// Handle sandbox-start event (required by Gupshup)
	if eventType == "sandbox-start" {
		h.Logger.Info("✅ Received sandbox-start event", zap.String("app_id", appID))
		return
	}

	// Process incoming messages
	if eventType == "message" || eventType == "incoming" {
		h.handleIncomingMessage(ctx, tenantID, appID, payload)
	} else if _, hasText := payload["text"]; hasText {
		h.handleIncomingMessage(ctx, tenantID, appID, payload)
	} else if _, hasMessage := payload["message"]; hasMessage {
		h.handleIncomingMessage(ctx, tenantID, appID, payload)
	} else {
		h.Logger.Info("ℹ️ Unhandled Gupshup webhook event",
			zap.String("event_type", eventType),
			zap.String("app_id", appID),
			zap.Uint("tenant_id", tenantID))
	}
}

// handleIncomingMessage processes incoming WhatsApp messages from Gupshup Partner
// VERIFIED: Tenant is already resolved by appId, so we don't need phone number matching
func (h *GupshupWebhookHandler) handleIncomingMessage(ctx context.Context, tenantID uint, appID string, payload map[string]interface{}) {
	// Extract message fields from payload
	var msg struct {
		Source      string // Phone number that sent the message
		Mobile      string // Customer phone number (alternative field name)
		Destination string // Your WhatsApp number
		Text        string // Message text
		Message     string // Alternative field name
		MessageID   string // Unique message ID
		ReplyID     string // Alternative field name
		Timestamp   int64  // Unix timestamp
	}

	// Extract source/mobile (customer phone number)
	if val, ok := payload["source"].(string); ok {
		msg.Source = val
	} else if val, ok := payload["mobile"].(string); ok {
		msg.Source = val
		msg.Mobile = val
	} else if val, ok := payload["sender"].(string); ok {
		msg.Source = val
	}

	// Extract destination (tenant's WhatsApp number)
	if val, ok := payload["destination"].(string); ok {
		msg.Destination = val
	} else if val, ok := payload["waNumber"].(string); ok {
		msg.Destination = val
	} else if val, ok := payload["to"].(string); ok {
		msg.Destination = val
	}

	// Extract message text
	if val, ok := payload["text"].(string); ok {
		msg.Text = val
	} else if val, ok := payload["message"].(string); ok {
		msg.Text = val
		msg.Message = val
	}

	// Extract message ID
	if val, ok := payload["messageId"].(string); ok {
		msg.MessageID = val
	} else if val, ok := payload["replyId"].(string); ok {
		msg.MessageID = val
		msg.ReplyID = val
	} else if val, ok := payload["id"].(string); ok {
		msg.MessageID = val
	}

	// Extract timestamp
	if val, ok := payload["timestamp"].(float64); ok {
		msg.Timestamp = int64(val)
	} else if val, ok := payload["timestamp"].(string); ok {
		if ts, err := strconv.ParseInt(val, 10, 64); err == nil {
			msg.Timestamp = ts
		}
	}

	if msg.Source == "" || msg.Text == "" {
		h.Logger.Warn("⚠️ Gupshup message payload missing required fields",
			zap.Any("payload", payload),
			zap.String("app_id", appID))
		return
	}

	h.Logger.Info("💬 Processing Gupshup incoming message",
		zap.Uint("tenant_id", tenantID),
		zap.String("app_id", appID),
		zap.String("message_id", msg.MessageID),
		zap.String("source", msg.Source),
		zap.String("text_preview", truncateString(msg.Text, 100)))

	// Create timestamp (default to now if not provided)
	timestamp := time.Now()
	if msg.Timestamp > 0 {
		// Convert milliseconds to seconds if needed
		if msg.Timestamp > 1e12 {
			timestamp = time.Unix(msg.Timestamp/1000, 0)
		} else {
			timestamp = time.Unix(msg.Timestamp, 0)
		}
	}

	incomingMsg := IncomingMessage{
		Platform:         "whatsapp",
		TenantID:         tenantID,
		CustomerID:       msg.Source, // Phone number of sender
		CustomerName:     "",         // Gupshup may not provide name
		CustomerUsername: msg.Source, // Use phone number as username
		MessageID:        msg.MessageID,
		Content:          msg.Text,
		MessageType:      "text",
		ThreadID:         msg.Source,
		Timestamp:        timestamp,
		Metadata: map[string]interface{}{
			"app_id":      appID,
			"destination": msg.Destination,
			"raw_payload": payload,
		},
	}

	h.Logger.Info("🚀 Sending Gupshup message to MessageHandler",
		zap.Uint("tenant_id", tenantID),
		zap.String("customer_id", msg.Source))

	// Process message (already in goroutine, so no need for another go statement)
	if err := h.MessageHandler.ProcessMessage(ctx, incomingMsg); err != nil {
		h.Logger.Error("Failed to process incoming WhatsApp message",
			zap.Uint("tenant_id", tenantID),
			zap.String("customer_id", msg.Source),
			zap.Error(err))
	}
}

// NOTE: resolveTenantFromGupshupNumber is no longer used for Partner apps
// Tenant routing is now done by appId in processWebhookAsync above
// This function is kept for backward compatibility but should not be called for Partner apps

// getMapKeys returns all keys from a map (helper function)
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

