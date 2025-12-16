package social

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
		ContactID string `json:"contactId"` // Contact identifier (phone number or contact ID)
		ChannelID string `json:"channelId"` // WhatsApp channel ID
		MessageID string `json:"messageId"` // Unique message ID
		Text      string `json:"text"`      // Message text content
		From      string `json:"from"`      // Sender phone number
		Timestamp int64  `json:"timestamp"` // Unix timestamp
		Contact   *struct {
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
	// CRITICAL: Must match tenant exactly - no fallback for security
	tenantID, err := h.resolveTenantFromRespondIOChannel(msg.ChannelID, msg.ContactID)
	if err != nil {
		h.Logger.Error("❌ SECURITY: Rejecting Respond.io message - tenant resolution failed",
			zap.String("channel_id", msg.ChannelID),
			zap.String("contact_id", msg.ContactID),
			zap.String("message_id", msg.MessageID),
			zap.Error(err),
			zap.String("action", "Message rejected to prevent routing to wrong tenant"))
		// DO NOT process message if tenant cannot be determined
		// This prevents messages from being routed to wrong tenant
		return
	}

	customerName := ""
	if msg.Contact != nil {
		customerName = msg.Contact.Name
	}

	incomingMsg := IncomingMessage{
		Platform:           "whatsapp",
		TenantID:           tenantID,
		CustomerID:         msg.ContactID, // Use contact ID as customer ID
		CustomerName:       customerName,
		CustomerUsername:   msg.From, // Phone number
		CustomerProfilePic: "",
		MessageID:          msg.MessageID,
		Content:            msg.Text,
		MessageType:        "text",
		ThreadID:           msg.ContactID,
		Timestamp:          time.Unix(msg.Timestamp/1000, 0), // Convert milliseconds to seconds
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

	// Update message delivery status in database
	if status.MessageID == "" {
		h.Logger.Warn("⚠️ Delivery status update missing message ID")
		return
	}

	// Find interaction by platform message ID and update metadata
	var interaction storage.Interaction
	if err := h.DB.Where("platform = ? AND platform_message_id = ?", "whatsapp", status.MessageID).First(&interaction).Error; err != nil {
		h.Logger.Debug("Interaction not found for Respond.io delivery status update",
			zap.String("message_id", status.MessageID),
			zap.String("status", status.Status),
			zap.Error(err))
		return
	}

	// Update metadata with delivery status
	var metadata map[string]interface{}
	if interaction.Metadata != "" {
		if err := json.Unmarshal([]byte(interaction.Metadata), &metadata); err != nil {
			metadata = make(map[string]interface{})
		}
	} else {
		metadata = make(map[string]interface{})
	}

	metadata["delivery_status"] = strings.ToLower(status.Status)
	metadata["delivery_status_updated_at"] = time.Now().UTC().Format(time.RFC3339)
	if status.Error != "" {
		metadata["delivery_error"] = status.Error
	}

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		h.Logger.Error("Failed to marshal metadata for Respond.io status update", zap.Error(err))
		return
	}

	interaction.Metadata = string(metadataJSON)
	if err := h.DB.Save(&interaction).Error; err != nil {
		h.Logger.Error("Failed to update interaction delivery status for Respond.io",
			zap.String("message_id", status.MessageID),
			zap.String("status", status.Status),
			zap.Error(err))
		return
	}

	h.Logger.Info("✅ Updated Respond.io message delivery status",
		zap.String("message_id", status.MessageID),
		zap.String("status", status.Status),
		zap.Uint("interaction_id", interaction.ID),
		zap.Uint("tenant_id", interaction.TenantID))
}

// resolveTenantFromRespondIOChannel resolves tenant ID from Respond.io channel or contact
// SECURITY: Must match tenant exactly - no fallback to prevent routing to wrong tenant
func (h *RespondIOWebhookHandler) resolveTenantFromRespondIOChannel(channelID, contactID string) (uint, error) {
	// Strategy 1: Match by channel ID (most reliable)
	// Channel ID is stored in ExternalID when tenant connects
	var account storage.SocialAccount
	if channelID != "" {
		if err := h.DB.Where("platform = ? AND connected = ? AND external_id = ?", "whatsapp", true, channelID).First(&account).Error; err == nil {
			h.Logger.Info("✅ Tenant resolved by channel ID",
				zap.Uint("tenant_id", account.TenantID),
				zap.String("channel_id", channelID))
			return account.TenantID, nil
		}
		// Also try page_id as fallback (some tenants might store channel ID there)
		if err := h.DB.Where("platform = ? AND connected = ? AND page_id = ?", "whatsapp", true, channelID).First(&account).Error; err == nil {
			h.Logger.Info("✅ Tenant resolved by channel ID in page_id",
				zap.Uint("tenant_id", account.TenantID),
				zap.String("channel_id", channelID))
			return account.TenantID, nil
		}
	}

	// Strategy 2: Match by contact ID (phone number)
	// Phone number is stored in PageID when tenant connects
	if contactID != "" {
		// Normalize phone number - try with and without +
		phoneNumberWithPlus := contactID
		phoneNumberWithoutPlus := strings.TrimPrefix(contactID, "+")

		// Try exact match with +
		if err := h.DB.Where("platform = ? AND connected = ? AND page_id = ?", "whatsapp", true, phoneNumberWithPlus).First(&account).Error; err == nil {
			h.Logger.Info("✅ Tenant resolved by phone number (with +)",
				zap.Uint("tenant_id", account.TenantID),
				zap.String("phone_number", phoneNumberWithPlus))
			return account.TenantID, nil
		}

		// Try exact match without +
		if err := h.DB.Where("platform = ? AND connected = ? AND page_id = ?", "whatsapp", true, phoneNumberWithoutPlus).First(&account).Error; err == nil {
			h.Logger.Info("✅ Tenant resolved by phone number (without +)",
				zap.Uint("tenant_id", account.TenantID),
				zap.String("phone_number", phoneNumberWithoutPlus))
			return account.TenantID, nil
		}
	}

	// NO FALLBACK: Reject message if tenant cannot be determined
	// This prevents messages from being routed to wrong tenant (security critical)
	h.Logger.Error("❌ SECURITY: Could not resolve tenant for Respond.io webhook",
		zap.String("channel_id", channelID),
		zap.String("contact_id", contactID),
		zap.String("reason", "No matching tenant found - rejecting message to prevent data leak"),
		zap.String("action_required", "Ensure tenant's SocialAccount has correct ExternalID (channel ID: "+channelID+") or PageID (phone number: "+contactID+")"))

	return 0, fmt.Errorf("could not resolve tenant for Respond.io channel %s / contact %s - message rejected for security", channelID, contactID)
}
