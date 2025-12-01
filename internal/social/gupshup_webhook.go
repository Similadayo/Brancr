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

// GupshupCallbackPayload represents the main structure of Gupshup webhook events
type GupshupCallbackPayload struct {
	Type    string                 `json:"type"`
	Event   string                 `json:"event"`
	App     string                 `json:"app"`     // App ID for Partner API
	AppID   string                 `json:"appId"`   // Alternative field name
	AppID2  string                 `json:"app_id"`  // Alternative field name
	Payload GupshupEventPayload    `json:"payload"` // Nested payload for structured events
	RawData map[string]interface{} `json:"-"`       // Raw payload for backward compatibility
}

// GupshupEventPayload contains the actual event data
type GupshupEventPayload struct {
	ID          string `json:"id"`
	Source      string `json:"source"`      // Customer phone number
	Destination string `json:"destination"` // Business phone number
	Channel     string `json:"channel"`
	Event       string `json:"event"` // Event type: message, delivered, read, etc.
	Cause       string `json:"cause,omitempty"`

	// Message fields
	Text      string `json:"text,omitempty"`
	Message   string `json:"message,omitempty"` // Alternative field name
	MessageID string `json:"messageId,omitempty"`
	ReplyID   string `json:"replyId,omitempty"`

	// Alternative field names (for flexibility)
	Mobile   string `json:"mobile,omitempty"`
	Sender   string `json:"sender,omitempty"`
	WaNumber string `json:"waNumber,omitempty"`
	To       string `json:"to,omitempty"`

	// Timestamp
	Timestamp interface{} `json:"timestamp,omitempty"` // Can be string or number
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
// CRITICAL: Must handle GET requests for URL validation (Gupshup validates callback URLs with GET)
// VERIFIED: Must acknowledge POST requests immediately (< 10 seconds, ideally < 1 second)
func (h *GupshupWebhookHandler) HandleGupshupWebhook(w http.ResponseWriter, r *http.Request) {
	// Handle GET/HEAD requests for URL validation
	// Gupshup validates callback URLs by making GET or HEAD requests
	// We MUST return 200 OK (no redirects, no errors) or Gupshup will reject the URL
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		h.handleWebhookValidation(w, r)
		return
	}

	// Handle POST requests for actual webhook events
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

// handleWebhookValidation handles GET/HEAD requests from Gupshup for callback URL validation
// CRITICAL: Must return 200 OK with no redirects - any other status will cause Gupshup to reject the URL
func (h *GupshupWebhookHandler) handleWebhookValidation(w http.ResponseWriter, r *http.Request) {
	h.Logger.Info("🔍 Gupshup callback URL validation request",
		zap.String("method", r.Method),
		zap.String("url", r.URL.String()),
		zap.String("remote_addr", r.RemoteAddr),
		zap.String("user_agent", r.Header.Get("User-Agent")),
		zap.String("origin", r.Header.Get("Origin")))

	// Return 200 OK immediately - this validates the callback URL for Gupshup
	// Must be 200 OK (not 301, 302, 403, 404, 500) or Gupshup will reject it
	// For HEAD requests, don't write body (only headers)
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)

	// Only write body for GET requests (HEAD requests don't expect a body)
	if r.Method == http.MethodGet {
		w.Write([]byte("OK")) // Simple "OK" response to confirm URL is valid
	}
}

// processWebhookAsync processes webhook events asynchronously
// CRITICAL: This runs in a goroutine after immediate 200 OK acknowledgment
func (h *GupshupWebhookHandler) processWebhookAsync(ctx context.Context, r *http.Request) {
	h.Logger.Info("📨 Gupshup Partner webhook event received (async processing)",
		zap.String("method", r.Method),
		zap.String("url", r.URL.String()),
		zap.String("content_type", r.Header.Get("Content-Type")))

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.Logger.Error("❌ Failed to read Gupshup webhook body", zap.Error(err))
		return
	}

	if len(body) == 0 {
		h.Logger.Warn("⚠️ Gupshup webhook body is empty")
		return
	}

	h.Logger.Info("📦 Gupshup webhook payload received",
		zap.Int("body_size", len(body)),
		zap.String("body_preview", truncateString(string(body), 200)))

	// Try to parse as structured payload first
	var callbackPayload GupshupCallbackPayload
	if err := json.Unmarshal(body, &callbackPayload); err != nil {
		// Fallback to generic map for backward compatibility
		h.Logger.Warn("⚠️ Failed to parse as structured payload, trying generic map",
			zap.Error(err))

		var rawPayload map[string]interface{}
		if err := json.Unmarshal(body, &rawPayload); err != nil {
			// Try form-encoded as last resort
			if err := r.ParseForm(); err == nil {
				rawPayload = make(map[string]interface{})
				for k, v := range r.Form {
					if len(v) > 0 {
						rawPayload[k] = v[0]
					}
				}
				callbackPayload.RawData = rawPayload
			} else {
				h.Logger.Error("❌ Failed to parse Gupshup webhook payload (all formats failed)",
					zap.Error(err),
					zap.String("body_preview", truncateString(string(body), 200)))
				return
			}
		} else {
			callbackPayload.RawData = rawPayload
		}
	} else {
		// Store raw data for backward compatibility
		var rawData map[string]interface{}
		if err := json.Unmarshal(body, &rawData); err == nil {
			callbackPayload.RawData = rawData
		}
	}

	// Extract appId (try multiple field names for compatibility)
	appID := callbackPayload.App
	if appID == "" {
		appID = callbackPayload.AppID
	}
	if appID == "" {
		appID = callbackPayload.AppID2
	}
	if appID == "" && callbackPayload.RawData != nil {
		if val, ok := callbackPayload.RawData["app"].(string); ok {
			appID = val
		} else if val, ok := callbackPayload.RawData["appId"].(string); ok {
			appID = val
		} else if val, ok := callbackPayload.RawData["app_id"].(string); ok {
			appID = val
		}
	}

	if appID == "" {
		h.Logger.Warn("⚠️ Webhook payload missing appId - cannot route to tenant",
			zap.String("payload_type", callbackPayload.Type),
			zap.String("payload_event", callbackPayload.Event))
		return
	}

	// Route by appId → WhatsAppApp → tenantID
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

	// Determine event type
	eventType := callbackPayload.Type
	if eventType == "" {
		eventType = callbackPayload.Event
	}
	if eventType == "" && callbackPayload.Payload.Event != "" {
		eventType = callbackPayload.Payload.Event
	}

	// Handle sandbox-start event (required by Gupshup)
	if eventType == "sandbox-start" {
		h.Logger.Info("✅ Received sandbox-start event", zap.String("app_id", appID))
		return
	}

	// Process event based on type
	switch eventType {
	case "message", "message-event", "incoming":
		h.processGupshupEvent(ctx, tenantID, appID, callbackPayload)
	case "report-event", "delivered", "read", "failed":
		h.processReportEvent(ctx, tenantID, appID, callbackPayload)
	case "account-event", "system-event", "status-event":
		// Handle system events like APP_LIVE status updates after onboarding
		h.processAccountEvent(ctx, tenantID, appID, callbackPayload)
	default:
		// Check if it's a message based on payload content
		if callbackPayload.Payload.Text != "" || callbackPayload.Payload.Message != "" ||
			callbackPayload.Payload.Source != "" {
			h.processGupshupEvent(ctx, tenantID, appID, callbackPayload)
		} else {
			h.Logger.Info("ℹ️ Unhandled Gupshup webhook event",
				zap.String("event_type", eventType),
				zap.String("app_id", appID),
				zap.Uint("tenant_id", tenantID))
		}
	}
}

// processGupshupEvent handles message events from Gupshup
func (h *GupshupWebhookHandler) processGupshupEvent(ctx context.Context, tenantID uint, appID string, payload GupshupCallbackPayload) {
	h.Logger.Info("💬 Processing Gupshup message event",
		zap.Uint("tenant_id", tenantID),
		zap.String("app_id", appID),
		zap.String("event_type", payload.Type))

	h.handleIncomingMessage(ctx, tenantID, appID, payload)
}

// processReportEvent handles status report events (delivered, read, failed, etc.)
func (h *GupshupWebhookHandler) processReportEvent(ctx context.Context, tenantID uint, appID string, payload GupshupCallbackPayload) {
	h.Logger.Info("📊 Processing Gupshup report event",
		zap.Uint("tenant_id", tenantID),
		zap.String("app_id", appID),
		zap.String("event_type", payload.Event),
		zap.String("message_id", payload.Payload.MessageID),
		zap.String("status", payload.Payload.Event))

	// Update message delivery status in database
	messageID := payload.Payload.MessageID
	if messageID == "" {
		// Try alternative field names
		if payload.RawData != nil {
			if id, ok := payload.RawData["messageId"].(string); ok && id != "" {
				messageID = id
			} else if id, ok := payload.RawData["message_id"].(string); ok && id != "" {
				messageID = id
			}
		}
	}

	if messageID == "" {
		h.Logger.Warn("⚠️ Report event missing message ID, cannot update status",
			zap.String("event_type", payload.Payload.Event))
		return
	}

	deliveryStatus := payload.Payload.Event
	if deliveryStatus == "" {
		deliveryStatus = payload.Event
	}
	if deliveryStatus == "" {
		deliveryStatus = "unknown"
	}

	// Find interaction by platform message ID and update metadata
	var interaction storage.Interaction
	if err := h.DB.Where("tenant_id = ? AND platform = ? AND platform_message_id = ?",
		tenantID, "whatsapp", messageID).First(&interaction).Error; err != nil {
		h.Logger.Debug("Interaction not found for delivery status update",
			zap.String("message_id", messageID),
			zap.String("status", deliveryStatus),
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

	metadata["delivery_status"] = strings.ToLower(deliveryStatus)
	metadata["delivery_status_updated_at"] = time.Now().UTC().Format(time.RFC3339)
	if payload.Payload.Cause != "" {
		metadata["delivery_failure_cause"] = payload.Payload.Cause
	}

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		h.Logger.Error("Failed to marshal metadata", zap.Error(err))
		return
	}

	interaction.Metadata = string(metadataJSON)
	if err := h.DB.Save(&interaction).Error; err != nil {
		h.Logger.Error("Failed to update interaction delivery status",
			zap.String("message_id", messageID),
			zap.String("status", deliveryStatus),
			zap.Error(err))
		return
	}

	h.Logger.Info("✅ Updated message delivery status",
		zap.Uint("tenant_id", tenantID),
		zap.String("message_id", messageID),
		zap.String("status", deliveryStatus),
		zap.Uint("interaction_id", interaction.ID))
}

// processAccountEvent handles account/system events from Gupshup (e.g., Go-Live events with WABA ID and phone number)
// According to Gupshup docs, subscribing to ACCOUNT mode triggers Go-Live Event after onboarding
// Go-Live Event payload contains: app name, app ID, phone number, and WABA ID
func (h *GupshupWebhookHandler) processAccountEvent(ctx context.Context, tenantID uint, appID string, payload GupshupCallbackPayload) {
	h.Logger.Info("🏢 Processing Gupshup account/system event (Go-Live Event)",
		zap.Uint("tenant_id", tenantID),
		zap.String("app_id", appID),
		zap.String("event_type", payload.Type),
		zap.String("event", payload.Event))

	// Handle APP_LIVE status event (app becomes live after onboarding)
	var waApp storage.WhatsAppApp
	if err := h.DB.Where("tenant_id = ? AND gupshup_app_id = ?", tenantID, appID).First(&waApp).Error; err != nil {
		h.Logger.Warn("⚠️ WhatsAppApp not found for account event",
			zap.String("app_id", appID),
			zap.Uint("tenant_id", tenantID),
			zap.Error(err))
		return
	}

	updated := false

	// Extract all possible data sources for Go-Live Event
	// According to docs, Go-Live Event contains: app name, app ID, phone number, WABA ID

	// First, try RawData (if payload was parsed as map)
	if payload.RawData != nil {
		h.Logger.Info("📦 Processing account event from RawData",
			zap.Any("raw_data_keys", getMapKeys(payload.RawData)))

		// Extract WABA ID (try multiple field name variations)
		if wabaID, ok := payload.RawData["wabaId"].(string); ok && wabaID != "" {
			if waApp.WABAID == "" || waApp.WABAID != wabaID {
				waApp.WABAID = wabaID
				updated = true
				h.Logger.Info("✅✅✅ WABA ID extracted from Go-Live event (wabaId)",
					zap.String("waba_id", wabaID))
			}
		}
		if waApp.WABAID == "" {
			if wabaID, ok := payload.RawData["waba_id"].(string); ok && wabaID != "" {
				waApp.WABAID = wabaID
				updated = true
				h.Logger.Info("✅✅✅ WABA ID extracted from Go-Live event (waba_id)",
					zap.String("waba_id", wabaID))
			}
		}
		if waApp.WABAID == "" {
			if wabaID, ok := payload.RawData["wabaID"].(string); ok && wabaID != "" {
				waApp.WABAID = wabaID
				updated = true
				h.Logger.Info("✅✅✅ WABA ID extracted from Go-Live event (wabaID)",
					zap.String("waba_id", wabaID))
			}
		}

		// Extract Phone Number (try multiple field name variations)
		if phone, ok := payload.RawData["phoneNumber"].(string); ok && phone != "" {
			if waApp.PhoneNumber == "" || waApp.PhoneNumber != phone {
				waApp.PhoneNumber = phone
				updated = true
				h.Logger.Info("✅✅✅ Phone number extracted from Go-Live event (phoneNumber)",
					zap.String("phone_number", phone))
			}
		}
		if waApp.PhoneNumber == "" {
			if phone, ok := payload.RawData["phone_number"].(string); ok && phone != "" {
				waApp.PhoneNumber = phone
				updated = true
				h.Logger.Info("✅✅✅ Phone number extracted from Go-Live event (phone_number)",
					zap.String("phone_number", phone))
			}
		}
		if waApp.PhoneNumber == "" {
			if phone, ok := payload.RawData["phone"].(string); ok && phone != "" {
				waApp.PhoneNumber = phone
				updated = true
				h.Logger.Info("✅✅✅ Phone number extracted from Go-Live event (phone)",
					zap.String("phone_number", phone))
			}
		}

		// Extract Business ID
		if businessID, ok := payload.RawData["businessId"].(string); ok && businessID != "" {
			if waApp.BusinessID == "" || waApp.BusinessID != businessID {
				waApp.BusinessID = businessID
				updated = true
			}
		}
		if waApp.BusinessID == "" {
			if businessID, ok := payload.RawData["business_id"].(string); ok && businessID != "" {
				waApp.BusinessID = businessID
				updated = true
			}
		}

		// Extract Status
		if status, ok := payload.RawData["status"].(string); ok && status != "" {
			if strings.ToLower(status) == "live" || strings.ToLower(status) == "active" || strings.ToLower(status) == "go_live" {
				if waApp.Status != "live" {
					waApp.Status = "live"
					updated = true
					h.Logger.Info("✅ App status updated to live from Go-Live event",
						zap.String("status", status))
				}
			}
		}
	}

	// Also check payload.Payload structure for nested data
	if payload.Payload.Event != "" {
		eventLower := strings.ToLower(payload.Payload.Event)
		if eventLower == "app_live" || eventLower == "live" || eventLower == "active" || eventLower == "go_live" {
			if waApp.Status != "live" {
				waApp.Status = "live"
				updated = true
				h.Logger.Info("✅ App status updated to live from payload event",
					zap.String("event", payload.Payload.Event))
			}
		}
	}

	// Set status to "live" if we have both WABA ID and phone number (Go-Live complete)
	if waApp.WABAID != "" && waApp.PhoneNumber != "" && waApp.Status != "live" {
		waApp.Status = "live"
		updated = true
		h.Logger.Info("✅✅✅ Go-Live complete! Setting status to 'live' (has WABA ID and phone number)")
	}

	if updated {
		if err := h.DB.Save(&waApp).Error; err != nil {
			h.Logger.Error("❌ Failed to update WhatsAppApp from Go-Live event",
				zap.String("app_id", appID),
				zap.Uint("tenant_id", tenantID),
				zap.Error(err))
		} else {
			h.Logger.Info("✅✅✅ WhatsAppApp updated successfully from Go-Live event",
				zap.String("app_id", appID),
				zap.String("status", waApp.Status),
				zap.String("waba_id", waApp.WABAID),
				zap.String("phone_number", waApp.PhoneNumber),
				zap.String("business_id", waApp.BusinessID))
		}
	} else {
		h.Logger.Debug("No updates needed for account event",
			zap.String("app_id", appID),
			zap.String("current_status", waApp.Status),
			zap.String("current_waba_id", waApp.WABAID),
			zap.String("current_phone", waApp.PhoneNumber))
	}
}

// handleIncomingMessage processes incoming WhatsApp messages from Gupshup Partner
// VERIFIED: Tenant is already resolved by appId, so we don't need phone number matching
// Supports both structured payload and raw map for backward compatibility
func (h *GupshupWebhookHandler) handleIncomingMessage(ctx context.Context, tenantID uint, appID string, payload interface{}) {
	// Convert payload to map for extraction
	var payloadMap map[string]interface{}

	switch p := payload.(type) {
	case GupshupCallbackPayload:
		// Use structured payload
		if p.RawData != nil {
			payloadMap = p.RawData
		} else {
			// Build map from structured fields
			payloadMap = make(map[string]interface{})
			payloadMap["source"] = p.Payload.Source
			payloadMap["destination"] = p.Payload.Destination
			payloadMap["text"] = p.Payload.Text
			payloadMap["message"] = p.Payload.Message
			payloadMap["messageId"] = p.Payload.MessageID
			payloadMap["timestamp"] = p.Payload.Timestamp

			// Add alternative field names
			if p.Payload.Mobile != "" {
				payloadMap["mobile"] = p.Payload.Mobile
			}
			if p.Payload.Sender != "" {
				payloadMap["sender"] = p.Payload.Sender
			}
		}
	case map[string]interface{}:
		payloadMap = p
	default:
		h.Logger.Error("❌ Invalid payload type for handleIncomingMessage",
			zap.String("type", fmt.Sprintf("%T", payload)))
		return
	}
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
	if val, ok := payloadMap["source"].(string); ok {
		msg.Source = val
	} else if val, ok := payloadMap["mobile"].(string); ok {
		msg.Source = val
		msg.Mobile = val
	} else if val, ok := payloadMap["sender"].(string); ok {
		msg.Source = val
	}

	// Extract destination (tenant's WhatsApp number)
	if val, ok := payloadMap["destination"].(string); ok {
		msg.Destination = val
	} else if val, ok := payloadMap["waNumber"].(string); ok {
		msg.Destination = val
	} else if val, ok := payloadMap["to"].(string); ok {
		msg.Destination = val
	}

	// Extract message text
	if val, ok := payloadMap["text"].(string); ok {
		msg.Text = val
	} else if val, ok := payloadMap["message"].(string); ok {
		msg.Text = val
		msg.Message = val
	}

	// Extract message ID
	if val, ok := payloadMap["messageId"].(string); ok {
		msg.MessageID = val
	} else if val, ok := payloadMap["replyId"].(string); ok {
		msg.MessageID = val
		msg.ReplyID = val
	} else if val, ok := payloadMap["id"].(string); ok {
		msg.MessageID = val
	}

	// Extract timestamp
	if val, ok := payloadMap["timestamp"].(float64); ok {
		msg.Timestamp = int64(val)
	} else if val, ok := payloadMap["timestamp"].(string); ok {
		if ts, err := strconv.ParseInt(val, 10, 64); err == nil {
			msg.Timestamp = ts
		}
	}

	if msg.Source == "" || msg.Text == "" {
		h.Logger.Warn("⚠️ Gupshup message payload missing required fields",
			zap.Any("payload", payloadMap),
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
