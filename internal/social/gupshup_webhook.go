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
	// Log all incoming parameters for debugging
	h.Logger.Info("🔔 Gupshup redirect received",
		zap.String("method", r.Method),
		zap.String("url", r.URL.String()),
		zap.String("query", r.URL.RawQuery))

	// Extract query parameters
	appID := r.URL.Query().Get("appId")
	status := r.URL.Query().Get("status")
	phone := r.URL.Query().Get("phone")
	token := r.URL.Query().Get("token")
	wabaID := r.URL.Query().Get("wabaId")
	businessID := r.URL.Query().Get("businessId")
	phoneNumberID := r.URL.Query().Get("phoneNumberId")
	tenantIDStr := r.URL.Query().Get("tenant_id")

	h.Logger.Info("📋 Redirect parameters extracted",
		zap.String("app_id", appID),
		zap.String("status", status),
		zap.String("phone", phone),
		zap.String("waba_id", wabaID),
		zap.String("tenant_id", tenantIDStr))

	if appID == "" {
		h.Logger.Error("❌ Missing appId in redirect", zap.String("url", r.URL.String()))
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

	// Set up webhook subscription when onboarding completes
	// Try to set subscription if status indicates success OR if we have app details
	// Status can be: "live", "connected", "success", or empty (check if we have phone number)
	shouldSetSubscription := false
	if status == "live" || status == "connected" || status == "success" {
		shouldSetSubscription = true
	} else if status == "" && (phone != "" || appID != "") {
		// If status is empty but we have phone/appID, onboarding might have completed
		// Try to set subscription anyway (Gupshup will validate)
		shouldSetSubscription = true
	}

	if shouldSetSubscription && app.SubscriptionID == "" {
		// Webhook URL for incoming messages
		webhookURL := fmt.Sprintf("%s/api/webhooks/gupshup", h.Client.Config.PublicURL)

		h.Logger.Info("🔔 Setting up webhook subscription",
			zap.String("app_id", appID),
			zap.String("status", status),
			zap.String("webhook_url", webhookURL))

		// Subscribe to required events: message, message-event, status
		subID, err := h.Client.SetSubscription(appID, webhookURL, []string{"message", "message-event", "status"})
		if err != nil {
			h.Logger.Error("❌ Failed to set subscription",
				zap.String("app_id", appID),
				zap.String("status", status),
				zap.String("webhook_url", webhookURL),
				zap.Error(err))
			// Don't fail the redirect - log error but continue
		} else {
			app.SubscriptionID = subID
			app.SubscriptionActive = true
			app.WebhookURL = webhookURL
			if status == "" || status == "pending_onboarding" {
				app.Status = "connected" // Mark as connected after subscription
			}
			if err := h.DB.Save(&app).Error; err != nil {
				h.Logger.Error("failed to save subscription details", zap.Error(err))
			} else {
				h.Logger.Info("✅ Webhook subscription created successfully",
					zap.String("app_id", appID),
					zap.String("subscription_id", subID),
					zap.String("webhook_url", webhookURL),
					zap.String("status", app.Status))
			}
		}
	} else if app.SubscriptionID != "" {
		h.Logger.Info("ℹ️  Subscription already exists",
			zap.String("app_id", appID),
			zap.String("subscription_id", app.SubscriptionID))
	}

	// Also create/update SocialAccount for compatibility with existing connection status checks
	// Mark as connected if we have phone number and subscription is active
	isConnected := (phone != "" && app.SubscriptionActive) ||
		(status == "live" || status == "connected" || status == "success")

	var account storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ?", uint(tenantID), "whatsapp").First(&account).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			account = storage.SocialAccount{
				TenantID:   uint(tenantID),
				Platform:   "whatsapp",
				Mode:       "gupshup_partner",
				Username:   phone,
				PageID:     phoneNumberID,
				ExternalID: phoneNumberID,
				Connected:  isConnected,
			}
			if token != "" {
				encrypted, _ := encryptToken(token)
				account.AccessToken = encrypted
			}
			if err := h.DB.Create(&account).Error; err != nil {
				h.Logger.Warn("failed to create SocialAccount", zap.Error(err))
			} else {
				h.Logger.Info("✅ SocialAccount created for compatibility",
					zap.Uint("tenant_id", uint(tenantID)),
					zap.Bool("connected", isConnected),
					zap.String("phone", phone))
			}
		}
	} else {
		// Update existing SocialAccount
		account.Username = phone
		account.PageID = phoneNumberID
		account.ExternalID = phoneNumberID
		account.Connected = isConnected
		if token != "" {
			encrypted, _ := encryptToken(token)
			account.AccessToken = encrypted
		}
		if err := h.DB.Save(&account).Error; err != nil {
			h.Logger.Warn("failed to update SocialAccount", zap.Error(err))
		} else {
			h.Logger.Info("✅ SocialAccount updated",
				zap.Uint("tenant_id", uint(tenantID)),
				zap.Bool("connected", isConnected),
				zap.String("phone", phone))
		}
	}

	h.Logger.Info("✅ Gupshup onboarding redirect processed",
		zap.Uint("tenant_id", uint(tenantID)),
		zap.String("app_id", appID),
		zap.String("status", status),
		zap.String("phone", phone),
		zap.String("subscription_id", app.SubscriptionID),
		zap.Bool("subscription_active", app.SubscriptionActive))

	// Redirect to frontend success page
	frontendSuccessURL := fmt.Sprintf("%s/settings/integrations?whatsapp_connected=true&provider=gupshup&app_id=%s&status=%s",
		h.Client.Config.FrontendURL, appID, status)

	// Return success response with redirect info
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":          true,
		"message":          "WhatsApp connected successfully",
		"status":           status,
		"app_id":           appID,
		"phone":            phone,
		"subscription_id":  app.SubscriptionID,
		"subscription_set": app.SubscriptionID != "",
		"redirect_url":     frontendSuccessURL,
	})

	// Also redirect browser if this is a GET request (user clicking the link)
	if r.Method == http.MethodGet {
		http.Redirect(w, r, frontendSuccessURL, http.StatusFound)
	}
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
		TenantID:   tenantID,
		AppID:      appID,
		EventType:  eventType,
		Payload:    string(rawBody),
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
	if waID == "" {
		waID, _ = payload["gsId"].(string) // Try alternative field name
	}

	messageData, _ := payload["message"].(map[string]interface{})
	messageType, _ := messageData["type"].(string)

	// Extract sender name if available
	customerName := ""
	if sender, ok := payload["sender"].(map[string]interface{}); ok {
		if name, ok := sender["name"].(string); ok {
			customerName = name
		}
	}

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
	} else if ts, ok := payload["timestamp"].(string); ok {
		// Try parsing as string timestamp
		if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
			timestamp = parsed
		}
	}

	// Use existing MessageHandler to process message
	// This will automatically:
	// 1. Find or create customer
	// 2. Find or create conversation
	// 3. Save interaction to DB
	// 4. Send to AI for intent detection
	// 5. Generate and send reply
	msg := IncomingMessage{
		Platform:         "whatsapp",
		TenantID:         tenantID,
		CustomerID:       source, // Phone number in E.164 format
		CustomerName:     customerName,
		CustomerUsername: "",
		MessageID:        waID,
		Content:          content,
		MessageType:      messageType,
		ThreadID:         source, // Use phone number as thread ID
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
