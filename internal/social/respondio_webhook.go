package social

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// RespondIOWebhookHandler handles incoming webhooks from Respond.io
type RespondIOWebhookHandler struct {
	DB             *gorm.DB
	Logger         *zap.Logger
	MessageHandler *MessageHandler
}

// NewRespondIOWebhookHandler creates a new Respond.io webhook handler
func NewRespondIOWebhookHandler(db *gorm.DB, logger *zap.Logger, msgHandler *MessageHandler) *RespondIOWebhookHandler {
	return &RespondIOWebhookHandler{
		DB:             db,
		Logger:         logger,
		MessageHandler: msgHandler,
	}
}

// HandleRespondIOWebhook processes Respond.io webhook events
// Respond.io sends webhooks for incoming messages, delivery status, etc.
func (h *RespondIOWebhookHandler) HandleRespondIOWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	h.Logger.Info("📨 Respond.io webhook event received",
		zap.String("method", r.Method),
		zap.String("url", r.URL.String()),
		zap.String("content_type", r.Header.Get("Content-Type")))

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.Logger.Error("❌ Failed to read Respond.io webhook body", zap.Error(err))
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	h.Logger.Info("📦 Respond.io webhook payload received",
		zap.Int("body_size", len(body)),
		zap.String("body_preview", truncateString(string(body), 200)))

	// Parse Respond.io webhook payload
	// Respond.io webhook format: https://docs.respond.io/v2/webhooks
	var payload struct {
		Type      string          `json:"type"`      // "message", "delivery_status", etc.
		Data      json.RawMessage `json:"data"`      // Event-specific data
		Timestamp int64           `json:"timestamp"` // Unix timestamp
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		h.Logger.Error("❌ Failed to parse Respond.io webhook payload", zap.Error(err))
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	h.Logger.Info("🔍 Respond.io webhook event parsed",
		zap.String("event_type", payload.Type))

	ctx := context.Background()

	// Handle different event types
	switch payload.Type {
	case "message":
		h.handleIncomingMessage(ctx, payload.Data)
	case "delivery_status":
		h.handleDeliveryStatus(ctx, payload.Data)
	default:
		h.Logger.Info("ℹ️ Unhandled Respond.io webhook event type",
			zap.String("type", payload.Type))
	}

	// Always return 200 OK to acknowledge receipt
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// handleIncomingMessage processes incoming WhatsApp messages from Respond.io
func (h *RespondIOWebhookHandler) handleIncomingMessage(ctx context.Context, data json.RawMessage) {
	var msg struct {
		ContactID  string `json:"contactId"`  // Contact identifier (phone number or contact ID)
		ChannelID  string `json:"channelId"`  // WhatsApp channel ID
		MessageID  string `json:"messageId"`  // Unique message ID
		Text       string `json:"text"`       // Message text content
		From       string `json:"from"`       // Sender phone number
		Timestamp  int64  `json:"timestamp"`  // Unix timestamp
		Contact    *struct {
			Name string `json:"name"`
		} `json:"contact"`
		Channel *struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"channel"`
	}

	if err := json.Unmarshal(data, &msg); err != nil {
		h.Logger.Error("❌ Failed to parse Respond.io message data", zap.Error(err))
		return
	}

	h.Logger.Info("💬 Processing Respond.io incoming message",
		zap.String("message_id", msg.MessageID),
		zap.String("contact_id", msg.ContactID),
		zap.String("channel_id", msg.ChannelID),
		zap.String("from", msg.From),
		zap.String("text_preview", truncateString(msg.Text, 100)))

	// Find tenant by channel ID or contact identifier
	// For now, we'll need to match based on phone number or channel
	tenantID, err := h.resolveTenantFromRespondIOChannel(msg.ChannelID, msg.ContactID)
	if err != nil {
		h.Logger.Warn("⚠️ Could not resolve tenant for Respond.io message",
			zap.String("channel_id", msg.ChannelID),
			zap.String("contact_id", msg.ContactID),
			zap.Error(err))
		return
	}

	customerName := ""
	if msg.Contact != nil {
		customerName = msg.Contact.Name
	}

	incomingMsg := IncomingMessage{
		Platform:         "whatsapp",
		TenantID:         tenantID,
		CustomerID:       msg.ContactID, // Use contact ID as customer ID
		CustomerName:     customerName,
		CustomerUsername: msg.From, // Phone number
		MessageID:        msg.MessageID,
		Content:          msg.Text,
		MessageType:      "text",
		ThreadID:         msg.ContactID,
		Timestamp:        time.Unix(msg.Timestamp/1000, 0), // Convert milliseconds to seconds
		Metadata: map[string]interface{}{
			"channel_id": msg.ChannelID,
			"from":       msg.From,
		},
	}

	h.Logger.Info("🚀 Sending Respond.io message to MessageHandler",
		zap.Uint("tenant_id", tenantID),
		zap.String("customer_id", msg.ContactID))

	go h.MessageHandler.ProcessMessage(ctx, incomingMsg)
}

// handleDeliveryStatus processes message delivery status updates
func (h *RespondIOWebhookHandler) handleDeliveryStatus(ctx context.Context, data json.RawMessage) {
	var status struct {
		MessageID string `json:"messageId"`
		Status    string `json:"status"` // "sent", "delivered", "read", "failed"
		Error     string `json:"error,omitempty"`
	}

	if err := json.Unmarshal(data, &status); err != nil {
		h.Logger.Error("❌ Failed to parse Respond.io delivery status", zap.Error(err))
		return
	}

	h.Logger.Info("📬 Respond.io delivery status update",
		zap.String("message_id", status.MessageID),
		zap.String("status", status.Status))

	// TODO: Update message delivery status in database if needed
}

// resolveTenantFromRespondIOChannel resolves tenant ID from Respond.io channel or contact
// This matches the channel/contact to a tenant's WhatsApp account
func (h *RespondIOWebhookHandler) resolveTenantFromRespondIOChannel(channelID, contactID string) (uint, error) {
	// Try to find tenant by channel ID stored in ExternalID or PageID
	var account storage.SocialAccount
	if channelID != "" {
		if err := h.DB.Where("platform = ? AND (external_id = ? OR page_id = ?)", "whatsapp", channelID, channelID).First(&account).Error; err == nil {
			return account.TenantID, nil
		}
	}

	// Try to find by contact ID (phone number)
	if contactID != "" {
		// Remove + prefix if present
		phoneNumber := contactID
		if len(phoneNumber) > 0 && phoneNumber[0] == '+' {
			phoneNumber = phoneNumber[1:]
		}

		// Try to match by phone number stored somewhere
		// This is a simplified approach - you may need to store phone numbers differently
		if err := h.DB.Where("platform = ? AND (external_id LIKE ? OR page_id LIKE ?)", "whatsapp", "%"+phoneNumber+"%", "%"+phoneNumber+"%").First(&account).Error; err == nil {
			return account.TenantID, nil
		}
	}

	// Fallback: Find first active WhatsApp account (for single-tenant testing)
	var fallbackAccount storage.SocialAccount
	if err := h.DB.Where("platform = ? AND connected = ?", "whatsapp", true).First(&fallbackAccount).Error; err == nil {
		h.Logger.Info("⚠️ Using fallback tenant resolution for Respond.io",
			zap.Uint("tenant_id", fallbackAccount.TenantID),
			zap.String("channel_id", channelID))
		return fallbackAccount.TenantID, nil
	}

	return 0, fmt.Errorf("could not resolve tenant for Respond.io channel %s / contact %s", channelID, contactID)
}

