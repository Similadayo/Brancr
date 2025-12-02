package social

import (
	"context"
	"encoding/json"
	"fmt"
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
	Client         *GupshupClient
}

// NewGupshupWebhookHandler creates a new Gupshup webhook handler
func NewGupshupWebhookHandler(db *gorm.DB, logger *zap.Logger, msgHandler *MessageHandler, client *GupshupClient) *GupshupWebhookHandler {
	return &GupshupWebhookHandler{
		DB:             db,
		Logger:         logger,
		MessageHandler: msgHandler,
		Client:         client,
	}
}

// HandleWebhook processes incoming webhook events from Gupshup
func (h *GupshupWebhookHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	// Handle GET requests (webhook verification)
	if r.Method == http.MethodGet {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
		return
	}

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.Logger.Error("failed to read webhook body", zap.Error(err))
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	// Acknowledge quickly
	w.WriteHeader(http.StatusOK)

	// Parse payload
	var envelope map[string]interface{}
	if err := json.Unmarshal(body, &envelope); err != nil {
		h.Logger.Error("failed to parse webhook JSON", zap.Error(err), zap.ByteString("body", body))
		return
	}

	// Process asynchronously
	go h.processWebhookEvent(context.Background(), envelope, body)
}

// HandleRedirect handles the redirect URL after onboarding completion
func (h *GupshupWebhookHandler) HandleRedirect(w http.ResponseWriter, r *http.Request) {
	// Extract query parameters
	appID := r.URL.Query().Get("appId")
	status := r.URL.Query().Get("status")
	phone := r.URL.Query().Get("phone")
	token := r.URL.Query().Get("token")
	wabaID := r.URL.Query().Get("wabaId")
	businessID := r.URL.Query().Get("businessId")
	phoneNumberID := r.URL.Query().Get("phoneNumberId")
	tenantIDStr := r.URL.Query().Get("tenant_id")

	if appID == "" {
		http.Error(w, "appId required", http.StatusBadRequest)
		return
	}

	tenantID, err := strconv.ParseUint(tenantIDStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}

	// Find or create WhatsAppApp record
	var app storage.WhatsAppApp
	if err := h.DB.Where("tenant_id = ? AND gupshup_app_id = ?", uint(tenantID), appID).First(&app).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// Create new record
			app = storage.WhatsAppApp{
				TenantID:        uint(tenantID),
				GupshupAppID:    appID,
				GupshupAppToken: token,
				PhoneNumber:     phone,
				WABAID:          wabaID,
				BusinessID:      businessID,
				PhoneNumberID:   phoneNumberID,
				Status:          status,
				Mode:            "gupshup_partner",
			}
			if err := h.DB.Create(&app).Error; err != nil {
				h.Logger.Error("failed to create WhatsAppApp", zap.Error(err))
				http.Error(w, "database error", http.StatusInternalServerError)
				return
			}
		} else {
			h.Logger.Error("failed to query WhatsAppApp", zap.Error(err))
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}
	} else {
		// Update existing record
		if token != "" {
			app.GupshupAppToken = token
		}
		if phone != "" {
			app.PhoneNumber = phone
		}
		if wabaID != "" {
			app.WABAID = wabaID
		}
		if businessID != "" {
			app.BusinessID = businessID
		}
		if phoneNumberID != "" {
			app.PhoneNumberID = phoneNumberID
		}
		if status != "" {
			app.Status = status
		}
		if err := h.DB.Save(&app).Error; err != nil {
			h.Logger.Error("failed to update WhatsAppApp", zap.Error(err))
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}
	}

	// Set up webhook subscription if app is live
	if status == "live" && app.SubscriptionID == "" {
		webhookURL := fmt.Sprintf("%s/api/webhooks/gupshup?app_id=%s&tenant_id=%d", h.Client.Config.PublicURL, appID, tenantID)
		subID, err := h.Client.SetSubscription(appID, webhookURL, []string{"message", "delivery_status", "read_status"})
		if err != nil {
			h.Logger.Warn("failed to set subscription", zap.Error(err))
		} else {
			app.SubscriptionID = subID
			app.SubscriptionActive = true
			app.WebhookURL = webhookURL
			h.DB.Save(&app)
		}
	}

	// Also create/update SocialAccount for compatibility
	var account storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ?", uint(tenantID), "whatsapp").First(&account).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			account = storage.SocialAccount{
				TenantID:   uint(tenantID),
				Platform:   "whatsapp",
				Mode:       "gupshup_partner",
				Username:   phone,
				PageID:    phoneNumberID,
				ExternalID: phoneNumberID,
				Connected:  status == "live",
			}
			if token != "" {
				encrypted, _ := encryptToken(token)
				account.AccessToken = encrypted
			}
			h.DB.Create(&account)
		}
	} else {
		account.Username = phone
		account.PageID = phoneNumberID
		account.ExternalID = phoneNumberID
		account.Connected = status == "live"
		if token != "" {
			encrypted, _ := encryptToken(token)
			account.AccessToken = encrypted
		}
		h.DB.Save(&account)
	}

	h.Logger.Info("✅ Gupshup onboarding redirect processed",
		zap.Uint("tenant_id", uint(tenantID)),
		zap.String("app_id", appID),
		zap.String("status", status),
		zap.String("phone", phone))

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Onboarding completed",
		"status":  status,
	})
}

// processWebhookEvent processes webhook events asynchronously
func (h *GupshupWebhookHandler) processWebhookEvent(ctx context.Context, envelope map[string]interface{}, rawBody []byte) {
	eventType, _ := envelope["type"].(string)
	appID, _ := envelope["appId"].(string)
	if appID == "" {
		appID, _ = envelope["app"].(string)
	}

	// Resolve tenant from appID
	tenantID, err := h.resolveTenantFromAppID(appID)
	if err != nil {
		h.Logger.Warn("failed to resolve tenant from appID",
			zap.String("app_id", appID),
			zap.String("event_type", eventType),
			zap.Error(err))
		return
	}

	// Save webhook event to database
	webhookEvent := storage.GupshupWebhookEvent{
		TenantID:  tenantID,
		AppID:     appID,
		EventType: eventType,
		Payload:   string(rawBody),
		ReceivedAt: time.Now(),
	}
	h.DB.Create(&webhookEvent)

	// Process based on event type
	switch eventType {
	case "message":
		h.handleIncomingMessage(ctx, tenantID, envelope)
	case "delivery_status", "message-event":
		h.handleDeliveryStatus(ctx, tenantID, envelope)
	case "read_status":
		h.handleReadStatus(ctx, tenantID, envelope)
	default:
		h.Logger.Info("unhandled webhook event type",
			zap.String("type", eventType),
			zap.Uint("tenant_id", tenantID))
	}
}

// resolveTenantFromAppID resolves tenant ID from Gupshup app ID
func (h *GupshupWebhookHandler) resolveTenantFromAppID(appID string) (uint, error) {
	var app storage.WhatsAppApp
	if err := h.DB.Where("gupshup_app_id = ?", appID).First(&app).Error; err != nil {
		return 0, fmt.Errorf("app not found: %w", err)
	}
	return app.TenantID, nil
}

// handleIncomingMessage processes incoming message events
func (h *GupshupWebhookHandler) handleIncomingMessage(ctx context.Context, tenantID uint, envelope map[string]interface{}) {
	payload, ok := envelope["payload"].(map[string]interface{})
	if !ok {
		h.Logger.Warn("invalid message payload structure")
		return
	}

	source, _ := payload["source"].(string)
	waID, _ := payload["id"].(string)
	messageData, _ := payload["message"].(map[string]interface{})
	messageType, _ := messageData["type"].(string)

	// Extract text content
	var content string
	if textData, ok := messageData["text"].(map[string]interface{}); ok {
		content, _ = textData["body"].(string)
	} else if textStr, ok := messageData["text"].(string); ok {
		content = textStr
	}

	// Extract timestamp
	timestamp := time.Now()
	if ts, ok := payload["timestamp"].(float64); ok {
		timestamp = time.Unix(int64(ts), 0)
	}

	// Use existing MessageHandler to process message
	msg := IncomingMessage{
		Platform:         "whatsapp",
		TenantID:         tenantID,
		CustomerID:       source,
		CustomerName:      "",
		CustomerUsername: "",
		MessageID:        waID,
		Content:          content,
		MessageType:      messageType,
		ThreadID:         source,
		Timestamp:        timestamp,
		Metadata: map[string]interface{}{
			"gupshup_app_id": envelope["appId"],
			"raw_payload":    payload,
		},
	}

	// Process through existing message handler (handles AI, intent detection, etc.)
	if err := h.MessageHandler.ProcessMessage(ctx, msg); err != nil {
		h.Logger.Error("failed to process incoming message",
			zap.Uint("tenant_id", tenantID),
			zap.String("source", source),
			zap.Error(err))
	}
}

// handleDeliveryStatus processes delivery status updates
func (h *GupshupWebhookHandler) handleDeliveryStatus(ctx context.Context, tenantID uint, envelope map[string]interface{}) {
	payload, ok := envelope["payload"].(map[string]interface{})
	if !ok {
		return
	}

	messageID, _ := payload["id"].(string)
	if messageID == "" {
		messageID, _ = payload["gsId"].(string)
	}
	status, _ := payload["status"].(string)

	// Find interaction by platform message ID
	var interaction storage.Interaction
	if err := h.DB.Where("tenant_id = ? AND platform_message_id = ?", tenantID, messageID).First(&interaction).Error; err == nil {
		// Update metadata with delivery status
		var metadata map[string]interface{}
		if interaction.Metadata != "" {
			if err := json.Unmarshal([]byte(interaction.Metadata), &metadata); err != nil {
				metadata = make(map[string]interface{})
			}
		} else {
			metadata = make(map[string]interface{})
		}

		metadata["delivery_status"] = status
		metadata["delivery_status_updated_at"] = time.Now().UTC().Format(time.RFC3339)

		metadataJSON, err := json.Marshal(metadata)
		if err == nil {
			interaction.Metadata = string(metadataJSON)
			h.DB.Save(&interaction)

			h.Logger.Info("✅ Updated delivery status",
				zap.Uint("tenant_id", tenantID),
				zap.String("message_id", messageID),
				zap.String("status", status))
		}
	}
}

// handleReadStatus processes read receipt updates
func (h *GupshupWebhookHandler) handleReadStatus(ctx context.Context, tenantID uint, envelope map[string]interface{}) {
	payload, ok := envelope["payload"].(map[string]interface{})
	if !ok {
		return
	}

	messageID, _ := payload["id"].(string)
	if messageID == "" {
		messageID, _ = payload["gsId"].(string)
	}

	// Find interaction and update read status
	var interaction storage.Interaction
	if err := h.DB.Where("tenant_id = ? AND platform_message_id = ?", tenantID, messageID).First(&interaction).Error; err == nil {
		// Update metadata with read status
		var metadata map[string]interface{}
		if interaction.Metadata != "" {
			if err := json.Unmarshal([]byte(interaction.Metadata), &metadata); err != nil {
				metadata = make(map[string]interface{})
			}
		} else {
			metadata = make(map[string]interface{})
		}

		metadata["read_status"] = "read"
		metadata["read_at"] = time.Now().UTC().Format(time.RFC3339)

		metadataJSON, err := json.Marshal(metadata)
		if err == nil {
			interaction.Metadata = string(metadataJSON)
			h.DB.Save(&interaction)
		}
	}
}

