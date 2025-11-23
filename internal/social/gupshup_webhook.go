package social

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
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

// HandleGupshupWebhook processes Gupshup webhook events
// Gupshup sends webhooks for incoming messages, delivery status, etc.
func (h *GupshupWebhookHandler) HandleGupshupWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	h.Logger.Info("📨 Gupshup webhook event received",
		zap.String("method", r.Method),
		zap.String("url", r.URL.String()),
		zap.String("content_type", r.Header.Get("Content-Type")))

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.Logger.Error("❌ Failed to read Gupshup webhook body", zap.Error(err))
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	h.Logger.Info("📦 Gupshup webhook payload received",
		zap.Int("body_size", len(body)),
		zap.String("body_preview", truncateString(string(body), 200)))

	// Gupshup sends webhooks in different formats depending on the event
	// Common formats: JSON or form-encoded
	var payload map[string]interface{}

	// Try JSON first
	if err := json.Unmarshal(body, &payload); err != nil {
		// Try form-encoded
		if err := r.ParseForm(); err == nil {
			payload = make(map[string]interface{})
			for k, v := range r.Form {
				if len(v) > 0 {
					payload[k] = v[0]
				}
			}
		} else {
			h.Logger.Error("❌ Failed to parse Gupshup webhook payload", zap.Error(err))
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
	}

	h.Logger.Info("🔍 Gupshup webhook event parsed",
		zap.Any("payload_keys", getMapKeys(payload)))

	ctx := context.Background()

	// Gupshup webhook events can have different structures
	// Common fields: "event", "type", "message", "payload"
	eventType := ""
	if val, ok := payload["event"].(string); ok {
		eventType = val
	} else if val, ok := payload["type"].(string); ok {
		eventType = val
	}

	switch eventType {
	case "message", "sent", "delivered", "read", "failed":
		h.handleIncomingMessage(ctx, payload)
	default:
		// Try to detect message events by checking for common message fields
		if _, hasText := payload["text"]; hasText {
			h.handleIncomingMessage(ctx, payload)
		} else if _, hasMessage := payload["message"]; hasMessage {
			h.handleIncomingMessage(ctx, payload)
		} else {
			h.Logger.Info("ℹ️ Unhandled Gupshup webhook event",
				zap.String("event_type", eventType),
				zap.Any("payload", payload))
		}
	}

	// Always return 200 OK to acknowledge receipt
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// handleIncomingMessage processes incoming WhatsApp messages from Gupshup
func (h *GupshupWebhookHandler) handleIncomingMessage(ctx context.Context, payload map[string]interface{}) {
	// Gupshup webhook format varies, so we need to handle multiple formats
	// Common fields: source, destination, message, text, timestamp, messageId

	var msg struct {
		Source      string // Phone number that sent the message
		Destination string // Your WhatsApp number (source number)
		Text        string // Message text
		MessageID   string // Unique message ID
		Timestamp   int64  // Unix timestamp
		EventType   string // "message", "sent", etc.
	}

	// Extract fields from payload
	if val, ok := payload["source"].(string); ok {
		msg.Source = val
	} else if val, ok := payload["sender"].(string); ok {
		msg.Source = val
	}

	if val, ok := payload["destination"].(string); ok {
		msg.Destination = val
	} else if val, ok := payload["to"].(string); ok {
		msg.Destination = val
	}

	if val, ok := payload["text"].(string); ok {
		msg.Text = val
	} else if val, ok := payload["message"].(string); ok {
		msg.Text = val
	}

	if val, ok := payload["messageId"].(string); ok {
		msg.MessageID = val
	} else if val, ok := payload["id"].(string); ok {
		msg.MessageID = val
	}

	if val, ok := payload["timestamp"].(float64); ok {
		msg.Timestamp = int64(val)
	} else if val, ok := payload["timestamp"].(string); ok {
		if ts, err := strconv.ParseInt(val, 10, 64); err == nil {
			msg.Timestamp = ts
		}
	}

	if msg.Text == "" && msg.Source == "" {
		h.Logger.Warn("⚠️ Gupshup message payload missing required fields", zap.Any("payload", payload))
		return
	}

	h.Logger.Info("💬 Processing Gupshup incoming message",
		zap.String("message_id", msg.MessageID),
		zap.String("source", msg.Source),
		zap.String("destination", msg.Destination),
		zap.String("text_preview", truncateString(msg.Text, 100)))

	// Find tenant by destination (source number) or phone number
	tenantID, err := h.resolveTenantFromGupshupNumber(msg.Destination, msg.Source)
	if err != nil {
		h.Logger.Warn("⚠️ Could not resolve tenant for Gupshup message",
			zap.String("destination", msg.Destination),
			zap.String("source", msg.Source),
			zap.Error(err))
		return
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
		Timestamp:        time.Unix(msg.Timestamp/1000, 0), // Convert milliseconds to seconds
		Metadata: map[string]interface{}{
			"destination": msg.Destination,
		},
	}

	h.Logger.Info("🚀 Sending Gupshup message to MessageHandler",
		zap.Uint("tenant_id", tenantID),
		zap.String("customer_id", msg.Source))

	go h.MessageHandler.ProcessMessage(ctx, incomingMsg)
}

// resolveTenantFromGupshupNumber resolves tenant ID from Gupshup phone numbers
func (h *GupshupWebhookHandler) resolveTenantFromGupshupNumber(destination, source string) (uint, error) {
	// Try to find tenant by destination (your WhatsApp number stored in source_number config)
	// For now, we'll match against any WhatsApp account
	// TODO: Store Gupshup source numbers in database and match them

	var account storage.SocialAccount

	// Try to match by phone number patterns
	phoneNumbers := []string{destination, source}
	for _, phone := range phoneNumbers {
		if phone == "" {
			continue
		}
		// Normalize phone number (remove +, spaces, etc.)
		normalized := strings.TrimPrefix(phone, "+")
		normalized = strings.ReplaceAll(normalized, " ", "")
		normalized = strings.ReplaceAll(normalized, "-", "")

		// Try to match by phone number stored somewhere
		// This is simplified - you may need to store Gupshup source numbers differently
		if err := h.DB.Where("platform = ? AND (external_id LIKE ? OR page_id LIKE ?)", "whatsapp", "%"+normalized+"%", "%"+normalized+"%").First(&account).Error; err == nil {
			return account.TenantID, nil
		}
	}

	// Fallback: Find first active WhatsApp account
	var fallbackAccount storage.SocialAccount
	if err := h.DB.Where("platform = ? AND connected = ?", "whatsapp", true).First(&fallbackAccount).Error; err == nil {
		h.Logger.Info("⚠️ Using fallback tenant resolution for Gupshup",
			zap.Uint("tenant_id", fallbackAccount.TenantID),
			zap.String("destination", destination))
		return fallbackAccount.TenantID, nil
	}

	return 0, fmt.Errorf("could not resolve tenant for Gupshup destination %s / source %s", destination, source)
}

// getMapKeys returns all keys from a map (helper function)
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

