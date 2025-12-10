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

	"github.com/similadayo/brancr/internal/ai"
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
	MediaProcessor *MediaProcessor
}

// NewGupshupWebhookHandler creates a new Gupshup webhook handler
func NewGupshupWebhookHandler(db *gorm.DB, logger *zap.Logger, msgHandler *MessageHandler, client *GupshupClient) *GupshupWebhookHandler {
	// Initialize media processor if OpenAI and Cloudinary are configured
	var mediaProcessor *MediaProcessor
	openAIKey := os.Getenv("OPENAI_API_KEY")
	cloudURL := os.Getenv("CLOUDINARY_URL")
	if openAIKey != "" && cloudURL != "" {
		aiClient := ai.NewAIClient()
		var err error
		mediaProcessor, err = NewMediaProcessor(logger, aiClient, cloudURL)
		if err != nil {
			logger.Warn("failed to initialize media processor", zap.Error(err))
		} else {
			logger.Info("✅ Media processor initialized (audio transcription, image analysis, document extraction)")
		}
	} else {
		logger.Info("⚠️ Media processor not initialized (OpenAI or Cloudinary not configured)")
	}

	return &GupshupWebhookHandler{
		DB:             db,
		Logger:         logger,
		MessageHandler: msgHandler,
		Client:         client,
		MediaProcessor: mediaProcessor,
	}
}

// HandleWebhook processes incoming webhook events from Gupshup
// CRITICAL: This endpoint must be publicly reachable and respond quickly (2xx) to avoid Gupshup retries/drops
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

	// Log ALL incoming webhook requests (headers + body) for debugging
	// This helps verify Gupshup is hitting the endpoint and what events are being sent
	bodyPreview := func() string {
		if len(body) > 500 {
			return string(body[:500])
		}
		return string(body)
	}()

	h.Logger.Info("📨 Gupshup webhook received",
		zap.String("method", r.Method),
		zap.String("url", r.URL.String()),
		zap.String("content_type", r.Header.Get("Content-Type")),
		zap.String("user_agent", r.Header.Get("User-Agent")),
		zap.String("x_forwarded_for", r.Header.Get("X-Forwarded-For")),
		zap.String("x_real_ip", r.Header.Get("X-Real-IP")),
		zap.Int("body_size", len(body)),
		zap.String("body_preview", bodyPreview),
		zap.String("full_body", string(body))) // Log full body for debugging

	// Acknowledge quickly (within 2 seconds) to prevent Gupshup from retrying
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))

	// Parse payload
	var envelope map[string]interface{}
	if err := json.Unmarshal(body, &envelope); err != nil {
		h.Logger.Error("failed to parse webhook JSON", zap.Error(err), zap.ByteString("body", body))
		return
	}

	// Process asynchronously (after acknowledging)
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
	// CRITICAL: Don't rely on redirect URL params for WABA/phone metadata
	// Treat redirect as purely UX (to let user complete login) - not a guarantee of WABA success
	// Metadata will come from webhook events (account-event) or polling GetAppDetails
	var app storage.WhatsAppApp
	if err := h.DB.Where("tenant_id = ? AND gupshup_app_id = ?", uint(tenantID), appID).First(&app).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// Create new record with minimal info from redirect (just app ID and status)
			// Don't trust redirect params for WABA/phone - wait for webhook events
			app = storage.WhatsAppApp{
				TenantID:        uint(tenantID),
				GupshupAppID:    appID,
				GupshupAppToken: token,                // Token from redirect is OK to use
				Status:          "pending_onboarding", // Always start as pending, even if redirect says otherwise
				Mode:            "gupshup_partner",
			}
			// Only set phone/WABA if they're actually present (but don't rely on them)
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
			if err := h.DB.Create(&app).Error; err != nil {
				h.Logger.Error("failed to create WhatsAppApp", zap.Error(err))
				http.Error(w, "database error", http.StatusInternalServerError)
				return
			}
			h.Logger.Info("📝 Created WhatsAppApp from redirect (metadata will come from webhooks)",
				zap.String("app_id", appID),
				zap.Uint("tenant_id", uint(tenantID)),
				zap.String("status", app.Status))
		} else {
			h.Logger.Error("failed to query WhatsAppApp", zap.Error(err))
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}
	} else {
		// Update existing record - but don't overwrite existing WABA/phone with redirect params
		// Only update if we don't already have the data (redirect params are unreliable)
		if token != "" {
			app.GupshupAppToken = token
		}
		// Only update phone/WABA if we don't already have them (redirect params may be empty/incomplete)
		if phone != "" && app.PhoneNumber == "" {
			app.PhoneNumber = phone
		}
		if wabaID != "" && app.WABAID == "" {
			app.WABAID = wabaID
		}
		if businessID != "" && app.BusinessID == "" {
			app.BusinessID = businessID
		}
		if phoneNumberID != "" && app.PhoneNumberID == "" {
			app.PhoneNumberID = phoneNumberID
		}
		// CRITICAL: Don't update status from redirect - wait for webhook events or polling
		// Status should only change to "live" when we have actual metadata from GetAppDetails
		// Only update if status is explicitly "live" from redirect AND we have metadata
		if status == "live" && (app.WABAID != "" || app.PhoneNumber != "") {
			if app.Status == "pending_onboarding" {
				app.Status = status
				h.Logger.Info("📝 UPDATING EXISTING APP - Status changed to 'live' (has metadata)",
					zap.String("app_id", appID),
					zap.String("waba_id", app.WABAID),
					zap.String("phone", app.PhoneNumber))
			}
		} else if status == "live" || status == "connected" || status == "success" {
			h.Logger.Info("📝 UPDATING EXISTING APP - Ignoring status update from redirect (no metadata yet)",
				zap.String("app_id", appID),
				zap.String("redirect_status", status),
				zap.String("current_db_status", app.Status),
				zap.Bool("has_waba_id", app.WABAID != ""),
				zap.Bool("has_phone", app.PhoneNumber != ""))
		}
		if err := h.DB.Save(&app).Error; err != nil {
			h.Logger.Error("failed to update WhatsAppApp", zap.Error(err))
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}
		h.Logger.Info("📝 Updated WhatsAppApp from redirect (waiting for webhook events for complete metadata)",
			zap.String("app_id", appID),
			zap.String("current_status", app.Status))
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

		// Subscribe to required events: message, message-event, account-event
		subID, err := h.Client.SetSubscription(appID, webhookURL, []string{"message", "message-event", "account-event"})
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
			// CRITICAL: Don't update status to "connected" just because subscription is set
			// Status should only change to "live" when we have actual metadata (WABA ID, phone number)
			// Keep status as "pending_onboarding" until webhook events provide the metadata
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
				Metadata:   "{}", // Initialize Metadata as empty JSON object (required for JSONB)
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
		// Ensure Metadata is valid JSON (empty string is invalid for JSONB)
		if account.Metadata == "" {
			account.Metadata = "{}"
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

	// For GET requests (browser redirect from embedded signup popup)
	// CRITICAL: Only close popup when status is explicitly "live", "connected", or "success"
	// If status is empty, "pending_onboarding", or anything else, keep popup open
	if r.Method == http.MethodGet {
		// Check if this is coming from a popup
		popupParam := r.URL.Query().Get("popup")
		referer := r.Header.Get("Referer")
		isPopup := popupParam == "true" || referer != ""

		h.Logger.Info("🔍 POPUP CLOSING CHECK - Starting evaluation",
			zap.String("app_id", appID),
			zap.String("method", r.Method),
			zap.String("popup_param", popupParam),
			zap.String("referer", referer),
			zap.Bool("is_popup", isPopup),
			zap.String("redirect_status_param", status),
			zap.String("redirect_phone_param", phone),
			zap.String("redirect_waba_id_param", wabaID))

		// Get the actual status from the database (more reliable than query param)
		currentStatus := app.Status

		h.Logger.Info("🔍 POPUP CLOSING CHECK - Database state",
			zap.String("app_id", appID),
			zap.String("db_status", app.Status),
			zap.String("db_waba_id", app.WABAID),
			zap.String("db_phone_number", app.PhoneNumber),
			zap.String("db_business_id", app.BusinessID),
			zap.String("db_phone_number_id", app.PhoneNumberID),
			zap.Bool("db_subscription_active", app.SubscriptionActive),
			zap.String("db_subscription_id", app.SubscriptionID))

		// CRITICAL: Onboarding is complete ONLY if:
		// 1. Status is explicitly "live" (NOT "connected" or "pending_onboarding")
		// 2. AND we have either WABA ID or phone number in the database
		// Don't trust redirect params - they're unreliable during signup
		hasWABAID := app.WABAID != ""
		hasPhoneNumber := app.PhoneNumber != ""

		// Only consider complete if status is "live" (not "connected" or "pending_onboarding")
		// AND we have actual metadata (WABA ID or phone number)
		onboardingComplete := currentStatus == "live" && (hasWABAID || hasPhoneNumber)

		h.Logger.Info("🔍 POPUP CLOSING CHECK - Completion evaluation",
			zap.String("app_id", appID),
			zap.String("current_status", currentStatus),
			zap.Bool("has_waba_id", hasWABAID),
			zap.Bool("has_phone", hasPhoneNumber),
			zap.Bool("onboarding_complete", onboardingComplete),
			zap.String("status_check", fmt.Sprintf("currentStatus == 'live': %v", currentStatus == "live")),
			zap.String("metadata_check", fmt.Sprintf("hasWABAID || hasPhoneNumber: %v", hasWABAID || hasPhoneNumber)))

		if isPopup {
			h.Logger.Info("🔍 POPUP CLOSING CHECK - Is popup, checking completion",
				zap.String("app_id", appID),
				zap.Bool("onboarding_complete", onboardingComplete))
			if onboardingComplete {
				h.Logger.Info("✅ POPUP CLOSING - Onboarding complete, closing popup",
					zap.String("app_id", appID),
					zap.String("status", currentStatus),
					zap.Bool("has_waba_id", hasWABAID),
					zap.Bool("has_phone", hasPhoneNumber))
				// Onboarding is complete - serve HTML page that posts message to parent window and closes popup
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<title>WhatsApp Connected</title>
	<meta charset="utf-8">
</head>
<body>
	<script>
		// Notify parent window that connection is complete
		if (window.opener || window.parent !== window) {
			var targetWindow = window.opener || window.parent;
			targetWindow.postMessage({
				type: 'whatsapp_connected',
				// Also send the old type for backward compatibility
				whatsapp_onboarding_complete: true,
				success: true,
				provider: 'gupshup',
				app_id: '%s',
				status: '%s',
				phone: '%s',
				waba_id: '%s',
				business_id: '%s',
				subscription_id: '%s',
				redirect_url: '%s'
			}, '*');
			
			// Also send the old message type for compatibility (frontend listens for this)
			targetWindow.postMessage({
				type: 'whatsapp_onboarding_complete',
				success: true,
				provider: 'gupshup',
				app_id: '%s',
				status: '%s',
				phone: '%s'
			}, '*');
			
			// Close popup after a short delay
			setTimeout(function() {
				if (window.opener) {
					window.close();
				} else {
					// If in iframe, redirect parent
					window.parent.location.href = '%s';
				}
			}, 1000);
		} else {
			// Not in popup, redirect normally
			window.location.href = '%s';
		}
	</script>
	<div style="text-align: center; padding: 50px; font-family: Arial, sans-serif;">
		<h2>✅ WhatsApp Connected Successfully!</h2>
		<p>This window will close automatically...</p>
		<p><a href="%s">Click here if the window doesn't close</a></p>
	</div>
</body>
</html>`, appID, currentStatus, phone, app.WABAID, app.BusinessID, app.SubscriptionID, frontendSuccessURL, appID, currentStatus, phone, frontendSuccessURL, frontendSuccessURL, frontendSuccessURL)
				w.Write([]byte(html))
				return
			} else {
				h.Logger.Info("⏳ POPUP STAYING OPEN - Onboarding not complete, showing waiting message",
					zap.String("app_id", appID),
					zap.String("current_status", currentStatus),
					zap.Bool("has_waba_id", hasWABAID),
					zap.Bool("has_phone", hasPhoneNumber),
					zap.String("reason", func() string {
						if currentStatus != "live" {
							return fmt.Sprintf("Status is '%s', not 'live'", currentStatus)
						}
						if !hasWABAID && !hasPhoneNumber {
							return "Missing WABA ID and phone number"
						}
						return "Unknown reason"
					}()))
				// Onboarding not complete yet - show waiting message and poll for completion
				// Don't redirect back to embedded signup - user has already completed the signup flow
				// Just wait for webhook events to provide the metadata
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<title>WhatsApp Onboarding</title>
	<meta charset="utf-8">
</head>
<body>
	<div style="text-align: center; padding: 50px; font-family: Arial, sans-serif;">
		<h2>⏳ Completing WhatsApp Onboarding...</h2>
		<p>Please wait while we finalize your WhatsApp connection.</p>
		<p>Status: <strong>%s</strong></p>
		<p>This window will close automatically when setup is complete.</p>
		<p><small>Webhook subscription is active and listening for account events...</small></p>
		<p><small>Waiting for WABA ID and phone number...</small></p>
	</div>
	<script>
		// Poll for completion status every 5 seconds
		// Check if status has changed from pending_onboarding to live
		setTimeout(function() {
			window.location.reload();
		}, 5000);
	</script>
</body>
</html>`, currentStatus)
				w.Write([]byte(html))
				return
			}
		}

		// Not a popup, redirect normally
		h.Logger.Info("🔍 POPUP CLOSING CHECK - Not a popup, redirecting normally",
			zap.String("app_id", appID),
			zap.String("redirect_url", frontendSuccessURL))
		http.Redirect(w, r, frontendSuccessURL, http.StatusFound)
		return
	}

	// For API calls (POST, etc.), return JSON response
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
}

// processWebhookEvent processes webhook events asynchronously
func (h *GupshupWebhookHandler) processWebhookEvent(ctx context.Context, envelope map[string]interface{}, rawBody []byte) {
	eventType, _ := envelope["type"].(string)
	appID, _ := envelope["appId"].(string)
	if appID == "" {
		appID, _ = envelope["app"].(string)
	}

	h.Logger.Info("📨 Processing webhook event",
		zap.String("event_type", eventType),
		zap.String("app_id", appID),
		zap.Any("full_envelope", envelope), // Use zap.Any for better JSON formatting
		zap.String("envelope_json", func() string {
			jsonBytes, _ := json.Marshal(envelope)
			return string(jsonBytes)
		}()))

	// Resolve tenant from appID
	tenantID, err := h.resolveTenantFromAppID(appID)
	if err != nil {
		h.Logger.Warn("failed to resolve tenant from appID",
			zap.String("app_id", appID),
			zap.String("event_type", eventType),
			zap.String("full_envelope", fmt.Sprintf("%+v", envelope)),
			zap.Error(err))
		return
	}

	h.Logger.Info("✅ Tenant resolved for webhook event",
		zap.Uint("tenant_id", tenantID),
		zap.String("app_id", appID),
		zap.String("event_type", eventType))

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
		// Handles: sent, delivered, failed, deleted, warning
		h.handleDeliveryStatus(ctx, tenantID, envelope)
	case "read_status":
		// Handles: read (when customer reads the message)
		h.handleReadStatus(ctx, tenantID, envelope)
	case "deleted":
		// Handles: deleted (when customer deletes a message)
		h.handleDeletedStatus(ctx, tenantID, envelope)
	case "account-event", "system-event":
		// Handle account status changes (WABA approval, phone verification, etc.)
		h.handleAccountEvent(ctx, tenantID, appID, envelope)
	case "onboarding-event":
		// Handle Go-Live event when app completes onboarding via embedded link
		// Payload: {"type": "onboarding-event", "payload": {"type": "docker-status-event", "payload": {"status": "live", "waId": "WABA_ID"}}}
		h.handleOnboardingEvent(ctx, tenantID, appID, envelope)
	default:
		h.Logger.Info("unhandled webhook event type",
			zap.String("type", eventType),
			zap.Uint("tenant_id", tenantID))
	}
}

// resolveTenantFromAppID resolves tenant ID from Gupshup app ID or app name
// Gupshup webhooks may send either UUID (appId) or app name (app field)
func (h *GupshupWebhookHandler) resolveTenantFromAppID(appIDOrName string) (uint, error) {
	// First try: Look up by UUID (gupshup_app_id)
	var app storage.WhatsAppApp
	if err := h.DB.Where("gupshup_app_id = ?", appIDOrName).First(&app).Error; err == nil {
		return app.TenantID, nil
	}

	// Second try: If it looks like a name (not a UUID), try to find by matching app name
	// UUIDs have format: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx (has dashes)
	// App names don't have dashes typically
	isUUID := strings.Contains(appIDOrName, "-") && len(appIDOrName) > 30

	if !isUUID {
		// It's likely an app name - try to get the UUID from Gupshup API
		h.Logger.Info("🔍 App identifier looks like a name, fetching UUID from Gupshup",
			zap.String("app_id_or_name", appIDOrName))

		// Get list of apps from Gupshup to find the UUID for this app name
		if h.Client != nil {
			partnerToken, err := h.Client.GetPartnerToken()
			if err == nil {
				listURL := fmt.Sprintf("%s/partner/account/api/partnerApps", h.Client.Config.GupshupAPIBaseURL)
				req, err := http.NewRequest("GET", listURL, nil)
				if err == nil {
					req.Header.Set("Authorization", partnerToken)
					httpClient := &http.Client{Timeout: 10 * time.Second}
					resp, err := httpClient.Do(req)
					if err == nil && resp.StatusCode < 300 {
						defer resp.Body.Close()
						body, _ := io.ReadAll(resp.Body)
						var listResult struct {
							Status          string `json:"status"`
							PartnerAppsList []struct {
								ID   string `json:"id"`
								Name string `json:"name"`
							} `json:"partnerAppsList"`
						}
						if err := json.Unmarshal(body, &listResult); err == nil {
							// Find app by name
							for _, gupshupApp := range listResult.PartnerAppsList {
								if gupshupApp.Name == appIDOrName {
									// Found it! Now look up by UUID
									h.Logger.Info("✅ Found app UUID from name",
										zap.String("app_name", appIDOrName),
										zap.String("app_uuid", gupshupApp.ID))
									if err := h.DB.Where("gupshup_app_id = ?", gupshupApp.ID).First(&app).Error; err == nil {
										return app.TenantID, nil
									}
									break
								}
							}
						}
					}
				}
			}
		}
	}

	return 0, fmt.Errorf("app not found (tried UUID and name lookup): %s", appIDOrName)
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

	// Extract message type - can be at payload.type or payload.message.type
	messageType, _ := payload["type"].(string)
	
	// Try to get message data - can be at payload.message or payload.payload
	var messageData map[string]interface{}
	if msg, ok := payload["message"].(map[string]interface{}); ok {
		messageData = msg
		// If message.type exists, use it (overrides payload.type)
		if msgType, ok := msg["type"].(string); ok && msgType != "" {
			messageType = msgType
		}
	} else if nestedPayload, ok := payload["payload"].(map[string]interface{}); ok {
		// Some Gupshup formats have nested payload.payload structure
		messageData = nestedPayload
		if msgType, ok := nestedPayload["type"].(string); ok && msgType != "" {
			messageType = msgType
		}
	}

	// Extract sender information (name, phone, country_code, dial_code)
	customerName := ""
	var customerPhone string
	var countryCode string
	var dialCode string
	if sender, ok := payload["sender"].(map[string]interface{}); ok {
		if name, ok := sender["name"].(string); ok {
			customerName = name
		}
		if phone, ok := sender["phone"].(string); ok {
			customerPhone = phone
		}
		if cc, ok := sender["country_code"].(string); ok {
			countryCode = cc
		}
		if dc, ok := sender["dial_code"].(string); ok {
			dialCode = dc
		}
	}

	// Check if this is a media message (audio, image, video, document, sticker)
	isMediaMessage := messageType == "audio" || messageType == "image" || 
		messageType == "video" || messageType == "document" || messageType == "sticker"
	
	// Extract media URL and metadata for media messages
	var mediaURL string
	var mediaMetadata map[string]interface{}
	
	if isMediaMessage {
		// Try to extract media URL from various possible locations
		// Gupshup sends media URL in different places depending on message type
		
		// Method 1: payload.url or payload.payload.url
		if nestedPayload, ok := payload["payload"].(map[string]interface{}); ok {
			if url, ok := nestedPayload["url"].(string); ok && url != "" {
				mediaURL = url
			}
		}
		
		// Method 2: message.audio.url, message.image.url, etc.
		if mediaURL == "" && messageData != nil {
			if mediaObj, ok := messageData[messageType].(map[string]interface{}); ok {
				if url, ok := mediaObj["url"].(string); ok && url != "" {
					mediaURL = url
				}
				// Extract additional metadata
				mediaMetadata = mediaObj
			}
		}
		
		// Method 3: payload.url directly
		if mediaURL == "" {
			if url, ok := payload["url"].(string); ok && url != "" {
				mediaURL = url
			}
		}
		
		// Extract caption if available (for images/videos)
		var caption string
		if messageData != nil {
			if cap, ok := messageData["caption"].(string); ok && cap != "" {
				caption = cap
			}
		}
		if caption == "" && payload["caption"] != nil {
			if cap, ok := payload["caption"].(string); ok {
				caption = cap
			}
		}
		
		// Extract filename for documents
		var filename string
		if messageType == "document" && messageData != nil {
			if doc, ok := messageData["document"].(map[string]interface{}); ok {
				if name, ok := doc["filename"].(string); ok {
					filename = name
				}
			}
		}
		
		// Log media message details
		h.Logger.Info("📎 Media message received",
			zap.Uint("tenant_id", tenantID),
			zap.String("source", source),
			zap.String("message_type", messageType),
			zap.String("media_url", mediaURL),
			zap.String("caption", caption),
			zap.String("filename", filename))
		
		// Store media metadata
		mediaMetadata = map[string]interface{}{
			"media_url":  mediaURL,
			"media_type": messageType,
			"caption":    caption,
		}
		if filename != "" {
			mediaMetadata["filename"] = filename
		}
		
		// For media messages, use caption as content if available, otherwise use a placeholder
		// This allows AI to process the message context even for media
		if caption != "" {
			// Will be set below
		} else {
			// Set a descriptive placeholder based on media type
			switch messageType {
			case "audio":
				// Will be set to placeholder below
			case "image":
				// Will be set to placeholder below
			case "video":
				// Will be set to placeholder below
			case "document":
				if filename != "" {
					// Will use filename if available
				}
			case "sticker":
				// Will be set to placeholder below
			}
		}
	}

	// Extract text content - try multiple formats based on actual Gupshup payload structure
	var content string
	
	// For media messages, try to get caption first
	if isMediaMessage {
		if caption, ok := mediaMetadata["caption"].(string); ok && caption != "" {
			content = caption
		}
	}
	
	// Method 1: payload.payload.text (nested payload structure - from logs)
	if content == "" {
	if nestedPayload, ok := payload["payload"].(map[string]interface{}); ok {
		if textStr, ok := nestedPayload["text"].(string); ok && textStr != "" {
			content = textStr
			}
		}
	}
	
	// Method 2: message.text.body (nested object in message)
	if content == "" && messageData != nil {
		if textData, ok := messageData["text"].(map[string]interface{}); ok {
			if body, ok := textData["body"].(string); ok && body != "" {
				content = body
			}
		}
	}
	
	// Method 3: message.text as direct string
	if content == "" && messageData != nil {
		if textStr, ok := messageData["text"].(string); ok && textStr != "" {
			content = textStr
		}
	}
	
	// Method 4: Check for "body" directly in messageData
	if content == "" && messageData != nil {
		if body, ok := messageData["body"].(string); ok && body != "" {
			content = body
		}
	}
	
	// Method 5: Check for "content" field in messageData
	if content == "" && messageData != nil {
		if contentStr, ok := messageData["content"].(string); ok && contentStr != "" {
			content = contentStr
		}
	}
	
	// Method 6: Check payload.text directly (fallback)
	if content == "" {
		if textStr, ok := payload["text"].(string); ok && textStr != "" {
			content = textStr
		}
	}
	
	// For media messages without text/caption, use a descriptive placeholder
	if content == "" && isMediaMessage {
		switch messageType {
		case "audio":
			content = "[Voice note received]"
		case "image":
			content = "[Image received]"
		case "video":
			content = "[Video received]"
		case "document":
			if filename, ok := mediaMetadata["filename"].(string); ok && filename != "" {
				content = fmt.Sprintf("[Document received: %s]", filename)
			} else {
				content = "[Document received]"
			}
		case "sticker":
			content = "[Sticker received]"
		default:
			content = fmt.Sprintf("[%s received]", messageType)
		}
	}
	
	// CRITICAL: Log if content is still empty (should not happen for media messages now)
	if content == "" {
		h.Logger.Error("❌ CRITICAL: Failed to extract message content from Gupshup webhook",
			zap.Uint("tenant_id", tenantID),
			zap.String("source", source),
			zap.String("message_type", messageType),
			zap.Bool("is_media_message", isMediaMessage),
			zap.String("media_url", mediaURL),
			zap.Any("message_data", messageData),
			zap.Any("full_payload", payload),
			zap.Any("payload_payload", func() interface{} {
				if pp, ok := payload["payload"].(map[string]interface{}); ok {
					return pp
				}
				return nil
			}()),
			zap.String("raw_envelope", fmt.Sprintf("%+v", envelope)))
		// Don't process empty messages - they'll cause random AI responses
		return
	}
	
	h.Logger.Info("✅ Successfully extracted message content",
		zap.Uint("tenant_id", tenantID),
		zap.String("source", source),
		zap.String("message_type", messageType),
		zap.String("content_preview", func() string {
			if len(content) > 100 {
				return content[:100] + "..."
			}
			return content
		}()),
		zap.Int("content_length", len(content)))

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

	// Build metadata with media information and customer details
	metadata := map[string]interface{}{
		"gupshup_app_id": envelope["appId"],
		"raw_payload":    payload,
	}
	
	// Add customer information to metadata for frontend display
	if countryCode != "" {
		metadata["customer_country_code"] = countryCode
	}
	if dialCode != "" {
		metadata["customer_dial_code"] = dialCode
	}
	if customerPhone != "" {
		metadata["customer_phone"] = customerPhone
	}
	
	// Process media if this is a media message and MediaProcessor is available
	// CRITICAL: For audio and images, we need transcription/analysis BEFORE AI reply generation
	// So we process them synchronously (with timeout) for these types
	var mediaProcessingResult *ProcessMediaResult
	if isMediaMessage && mediaURL != "" {
		if h.MediaProcessor == nil {
			h.Logger.Warn("⚠️ MediaProcessor not initialized - media will not be processed",
				zap.String("media_type", messageType),
				zap.String("media_url", mediaURL),
				zap.Uint("tenant_id", tenantID),
				zap.String("note", "Check OPENAI_API_KEY and CLOUDINARY_URL environment variables"))
		} else {
			h.Logger.Info("🔄 Processing media message",
				zap.String("media_type", messageType),
				zap.String("media_url", mediaURL),
				zap.Uint("tenant_id", tenantID),
				zap.String("message_id", waID))
			
			// For audio and images, process synchronously so AI can use transcription/analysis
			// For other media types (video, document), process in background
			if messageType == "audio" || messageType == "image" || messageType == "sticker" {
				processCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
				defer cancel()
				
				result, err := h.MediaProcessor.ProcessMedia(processCtx, tenantID, mediaURL, messageType)
				if err != nil {
					h.Logger.Warn("failed to process media (non-critical, will continue without it)",
						zap.String("media_type", messageType),
						zap.String("media_url", mediaURL),
						zap.Uint("tenant_id", tenantID),
						zap.String("message_id", waID),
						zap.Error(err))
				} else {
					mediaProcessingResult = result
					
					// Update content with transcription/analysis for AI to process
					if messageType == "audio" && result.Transcription != "" {
						content = result.Transcription
						h.Logger.Info("✅ Audio transcribed, using transcription for AI reply",
							zap.String("transcription_preview", func() string {
								if len(result.Transcription) > 50 {
									return result.Transcription[:50] + "..."
								}
								return result.Transcription
							}()))
					} else if (messageType == "image" || messageType == "sticker") && result.ImageAnalysis != "" {
						content = result.ImageAnalysis
						h.Logger.Info("✅ Image analyzed, using analysis for AI reply",
							zap.String("analysis_preview", func() string {
								if len(result.ImageAnalysis) > 50 {
									return result.ImageAnalysis[:50] + "..."
								}
								return result.ImageAnalysis
							}()))
					}
					
					// Store processing results in metadata for later database update
					if result.StoredURL != "" {
						metadata["media_stored_url"] = result.StoredURL
					}
					if result.Transcription != "" {
						metadata["audio_transcription"] = result.Transcription
					}
					if result.ImageAnalysis != "" {
						metadata["image_analysis"] = result.ImageAnalysis
					}
					if result.DocumentText != "" {
						metadata["document_text"] = result.DocumentText
					}
				}
			} else {
				// For video/document, process in background (don't block)
				go func() {
				processCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
				
				result, err := h.MediaProcessor.ProcessMedia(processCtx, tenantID, mediaURL, messageType)
				if err != nil {
					h.Logger.Warn("failed to process media (non-critical)",
						zap.String("media_type", messageType),
						zap.String("media_url", mediaURL),
						zap.Uint("tenant_id", tenantID),
						zap.String("message_id", waID),
						zap.Error(err))
					return
				}
				
				// Wait a bit for the interaction to be created by MessageHandler
				// The interaction is created synchronously, but we give it a moment
				time.Sleep(2 * time.Second)
				
				// Find the interaction in the database and update it with processing results
				// Use waID (message ID) to find the interaction
				// Try multiple times with increasing delays in case the interaction hasn't been created yet
				var interaction storage.Interaction
				var findErr error
				maxRetries := 5
				for i := 0; i < maxRetries; i++ {
					findErr = h.DB.Where("tenant_id = ? AND platform = ? AND platform_message_id = ? AND direction = ?",
						tenantID, "whatsapp", waID, "incoming").First(&interaction).Error
					if findErr == nil {
						break
					}
					if i < maxRetries-1 {
						time.Sleep(time.Duration(i+1) * time.Second)
					}
				}
				
				if findErr != nil {
					h.Logger.Warn("failed to find interaction to update with media processing results",
						zap.String("message_id", waID),
						zap.Uint("tenant_id", tenantID),
						zap.Int("retries", maxRetries),
						zap.Error(findErr),
						zap.String("note", "Interaction may not have been created yet or message ID mismatch"))
					return
				}
			
			// Parse existing metadata
			var existingMetadata map[string]interface{}
			if interaction.Metadata != "" && interaction.Metadata != "{}" {
				if err := json.Unmarshal([]byte(interaction.Metadata), &existingMetadata); err != nil {
					existingMetadata = make(map[string]interface{})
				}
			} else {
				existingMetadata = make(map[string]interface{})
			}
			
			// Update metadata with processing results
			if result.StoredURL != "" {
				existingMetadata["media_stored_url"] = result.StoredURL
			}
			if result.Transcription != "" {
				existingMetadata["audio_transcription"] = result.Transcription
				// Update content with transcription for audio messages
				if messageType == "audio" {
					interaction.Content = fmt.Sprintf("[Voice note]: %s", result.Transcription)
				}
			}
			if result.ImageAnalysis != "" {
				existingMetadata["image_analysis"] = result.ImageAnalysis
				// Update content with analysis for image messages
				if messageType == "image" || messageType == "sticker" {
					interaction.Content = fmt.Sprintf("[Image]: %s", result.ImageAnalysis)
				}
			}
			if result.DocumentText != "" {
				existingMetadata["document_text"] = result.DocumentText
				// Update content with extracted text for document messages
				if messageType == "document" {
					interaction.Content = fmt.Sprintf("[Document]: %s", result.DocumentText)
				}
			}
			
			// Add processing metadata
			for k, v := range result.Metadata {
				existingMetadata["media_"+k] = v
			}
			
			// Save updated metadata and content to database
			metadataJSON, err := json.Marshal(existingMetadata)
			if err == nil {
				interaction.Metadata = string(metadataJSON)
				if err := h.DB.Save(&interaction).Error; err != nil {
					h.Logger.Warn("failed to save media processing results to interaction",
						zap.Uint("interaction_id", interaction.ID),
						zap.Uint("tenant_id", tenantID),
						zap.Error(err))
				} else {
					h.Logger.Info("✅ Media processing completed and saved to database",
						zap.String("media_type", messageType),
						zap.Bool("has_stored_url", result.StoredURL != ""),
						zap.Bool("has_transcription", result.Transcription != ""),
						zap.String("transcription_preview", func() string {
							if len(result.Transcription) > 50 {
								return result.Transcription[:50] + "..."
							}
							return result.Transcription
						}()),
						zap.Bool("has_analysis", result.ImageAnalysis != ""),
						zap.Bool("has_document_text", result.DocumentText != ""),
						zap.Uint("tenant_id", tenantID),
						zap.Uint("interaction_id", interaction.ID),
						zap.String("message_id", waID))
				}
			}
		}()
			}
		}
	} else if isMediaMessage && mediaURL == "" {
		h.Logger.Warn("⚠️ Media message detected but no media URL found",
			zap.String("media_type", messageType),
			zap.Uint("tenant_id", tenantID),
			zap.String("message_id", waID),
			zap.Any("payload", payload))
	}
	
	// Add media metadata if this is a media message
	if isMediaMessage && mediaMetadata != nil {
		metadata["media"] = mediaMetadata
		metadata["media_url"] = mediaURL
		metadata["media_type"] = messageType
		// Add URL expiry if available (Gupshup may provide this)
		if nestedPayload, ok := payload["payload"].(map[string]interface{}); ok {
			if expiry, ok := nestedPayload["urlExpiry"].(string); ok {
				metadata["media_url_expiry"] = expiry
			}
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
		Platform:          "whatsapp",
		TenantID:          tenantID,
		CustomerID:        source, // Phone number in E.164 format
		CustomerName:      customerName,
		CustomerUsername: "",
		CustomerProfilePic: "",
		MessageID:         waID,
		Content:           content,
		MessageType:       messageType, // Will be "audio", "image", "video", "document", "sticker", or "text"
		ThreadID:          source, // Use phone number as thread ID
		Timestamp:         timestamp,
		Metadata:          metadata,
	}

	// Process through existing message handler (handles AI, intent detection, etc.)
	if err := h.MessageHandler.ProcessMessage(ctx, msg); err != nil {
		h.Logger.Error("failed to process incoming message",
			zap.Uint("tenant_id", tenantID),
			zap.String("source", source),
			zap.Error(err))
	}
	
	// Update interaction with media processing results if we processed synchronously
	// This ensures stored_url and other metadata are saved even if processing happened before interaction creation
	if mediaProcessingResult != nil && (messageType == "audio" || messageType == "image" || messageType == "sticker") {
		go func() {
			// Wait a bit for interaction to be created
			time.Sleep(2 * time.Second)
			
			// Find and update interaction with stored URL and other metadata
			var interaction storage.Interaction
			maxRetries := 3
			for i := 0; i < maxRetries; i++ {
				if err := h.DB.Where("tenant_id = ? AND platform = ? AND platform_message_id = ? AND direction = ?",
					tenantID, "whatsapp", waID, "incoming").First(&interaction).Error; err == nil {
					
					// Parse existing metadata
					var existingMetadata map[string]interface{}
					if interaction.Metadata != "" && interaction.Metadata != "{}" {
						json.Unmarshal([]byte(interaction.Metadata), &existingMetadata)
					}
					if existingMetadata == nil {
						existingMetadata = make(map[string]interface{})
					}
					
					// Update with stored URL (Cloudinary permanent URL)
					if mediaProcessingResult.StoredURL != "" {
						existingMetadata["media_stored_url"] = mediaProcessingResult.StoredURL
					}
					
					// Ensure transcription/analysis are in metadata (they should already be from ProcessMessage)
					if mediaProcessingResult.Transcription != "" {
						existingMetadata["audio_transcription"] = mediaProcessingResult.Transcription
					}
					if mediaProcessingResult.ImageAnalysis != "" {
						existingMetadata["image_analysis"] = mediaProcessingResult.ImageAnalysis
					}
					
					// Update content to include transcription/analysis if not already updated
					// This ensures the content field shows the transcription/analysis even if ProcessMessage used placeholder
					if messageType == "audio" && mediaProcessingResult.Transcription != "" {
						if !strings.Contains(interaction.Content, mediaProcessingResult.Transcription) {
							interaction.Content = fmt.Sprintf("[Voice note]: %s", mediaProcessingResult.Transcription)
						}
					} else if (messageType == "image" || messageType == "sticker") && mediaProcessingResult.ImageAnalysis != "" {
						if !strings.Contains(interaction.Content, mediaProcessingResult.ImageAnalysis) {
							interaction.Content = fmt.Sprintf("[Image]: %s", mediaProcessingResult.ImageAnalysis)
						}
					}
					
					// Save updated metadata and content
					metadataJSON, err := json.Marshal(existingMetadata)
					if err != nil {
						h.Logger.Error("failed to marshal metadata for interaction update",
							zap.Uint("interaction_id", interaction.ID),
							zap.Uint("tenant_id", tenantID),
							zap.Error(err))
						// Don't save corrupted metadata - log error and skip update
						return
					}
					interaction.Metadata = string(metadataJSON)
					h.DB.Save(&interaction)
					
					h.Logger.Info("✅ Updated interaction with media processing results",
						zap.Uint("interaction_id", interaction.ID),
						zap.Bool("has_stored_url", mediaProcessingResult.StoredURL != ""),
						zap.Bool("has_transcription", mediaProcessingResult.Transcription != ""),
						zap.Bool("has_analysis", mediaProcessingResult.ImageAnalysis != ""),
						zap.Uint("tenant_id", tenantID))
					break
				}
				if i < maxRetries-1 {
					time.Sleep(time.Duration(i+1) * time.Second)
				}
			}
		}()
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

	// Check for nested payload structure (for failed messages)
	eventType, _ := payload["type"].(string)
	var status string
	var errorCode interface{}
	var errorReason string

	if eventType == "failed" {
		// Failed messages have nested payload with error details
		nestedPayload, ok := payload["payload"].(map[string]interface{})
		if ok {
			if code, ok := nestedPayload["code"].(float64); ok {
				errorCode = int(code)
			}
			errorReason, _ = nestedPayload["reason"].(string)
			status = "failed"

			h.Logger.Error("❌ Message sending failed",
				zap.Uint("tenant_id", tenantID),
				zap.String("message_id", messageID),
				zap.Any("error_code", errorCode),
				zap.String("error_reason", errorReason),
				zap.String("full_payload", func() string {
					jsonBytes, _ := json.Marshal(payload)
					return string(jsonBytes)
				}()))

			// If template mismatch error (4003), it means the template structure doesn't match
			// The template IS approved, but it has the wrong structure (e.g., expects 2 params but we send 1)
			// Solution: Clear the template ID so we can create a simpler one on next attempt
			if errorCode == 4003 && strings.Contains(strings.ToLower(errorReason), "template") {
				h.Logger.Error("❌ Template mismatch error - template structure doesn't match AI message format",
					zap.Uint("tenant_id", tenantID),
					zap.String("message_id", messageID),
					zap.String("error_reason", errorReason),
					zap.String("note", "Template is approved but has wrong structure. Will create simpler template on next attempt."))

				// Clear the template ID so we can create a simpler one
				var app storage.WhatsAppApp
				if err := h.DB.Where("tenant_id = ?", tenantID).First(&app).Error; err == nil {
					if app.DefaultTemplateID != "" {
						h.Logger.Warn("🧹 Clearing template with wrong structure - will create simpler one",
							zap.String("old_template_id", app.DefaultTemplateID),
							zap.Uint("tenant_id", tenantID),
							zap.String("note", "Next message will trigger creation of a simpler template (just {{1}})"))
						app.DefaultTemplateID = ""
						h.DB.Save(&app)
					}
				}
			}
		}
	} else {
		// Normal status update (delivered, sent, deleted, warning, etc.)
		status, _ = payload["status"].(string)

		// Handle "warning" status (catalog item unavailable)
		if status == "warning" {
			h.Logger.Warn("⚠️ Message warning status received",
				zap.Uint("tenant_id", tenantID),
				zap.String("message_id", messageID),
				zap.String("status", status),
				zap.String("note", "Catalog item in message is unavailable or does not exist"))
		}

		// Handle "deleted" status (customer deleted message)
		if status == "deleted" {
			h.Logger.Info("🗑️ Message deleted by customer (from delivery status)",
				zap.Uint("tenant_id", tenantID),
				zap.String("message_id", messageID),
				zap.String("note", "Customer deleted this message - should be removed from system"))
		}
	}

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

		// Add error details if failed
		if status == "failed" {
			metadata["error_code"] = errorCode
			metadata["error_reason"] = errorReason
			// Update response status to failed
			interaction.ResponseStatus = "failed"
		}

		// Handle "warning" status (catalog item unavailable)
		if status == "warning" {
			metadata["warning"] = true
			metadata["warning_note"] = "Catalog item in message is unavailable or does not exist"
		}

		// Handle "deleted" status (customer deleted message)
		if status == "deleted" {
			metadata["deleted_status"] = "deleted"
			metadata["deleted_at"] = time.Now().UTC().Format(time.RFC3339)
			// Mark interaction as deleted (soft delete - keep record but mark as deleted)
			interaction.ResponseStatus = "deleted"
		}

		metadataJSON, err := json.Marshal(metadata)
		if err == nil {
			interaction.Metadata = string(metadataJSON)
			h.DB.Save(&interaction)

			switch status {
			case "failed":
				h.Logger.Error("❌ Updated interaction with failed status",
					zap.Uint("tenant_id", tenantID),
					zap.String("message_id", messageID),
					zap.String("status", status),
					zap.Any("error_code", errorCode),
					zap.String("error_reason", errorReason))
			case "deleted":
				h.Logger.Info("🗑️ Updated interaction with deleted status",
					zap.Uint("tenant_id", tenantID),
					zap.String("message_id", messageID),
					zap.String("status", status),
					zap.String("note", "Customer deleted this message"))
			case "warning":
				h.Logger.Warn("⚠️ Updated interaction with warning status",
					zap.Uint("tenant_id", tenantID),
					zap.String("message_id", messageID),
					zap.String("status", status),
					zap.String("note", "Catalog item unavailable"))
			default:
				h.Logger.Info("✅ Updated delivery status",
					zap.Uint("tenant_id", tenantID),
					zap.String("message_id", messageID),
					zap.String("status", status))
			}
		}
	} else if status == "failed" {
		// Log failed message even if interaction not found
		h.Logger.Warn("⚠️ Failed message event received but interaction not found",
			zap.Uint("tenant_id", tenantID),
			zap.String("message_id", messageID),
			zap.Any("error_code", errorCode),
			zap.String("error_reason", errorReason))
	}
}

// handleDeletedStatus processes deleted message notifications
// When a customer deletes a message, we should delete it from our system
func (h *GupshupWebhookHandler) handleDeletedStatus(ctx context.Context, tenantID uint, envelope map[string]interface{}) {
	payload, ok := envelope["payload"].(map[string]interface{})
	if !ok {
		return
	}

	messageID, _ := payload["id"].(string)
	if messageID == "" {
		messageID, _ = payload["gsId"].(string)
	}

	if messageID == "" {
		h.Logger.Warn("⚠️ Deleted status event received but no message ID found",
			zap.Uint("tenant_id", tenantID),
			zap.Any("payload", payload))
		return
	}

	h.Logger.Info("🗑️ Message deleted by customer",
		zap.Uint("tenant_id", tenantID),
		zap.String("message_id", messageID),
		zap.String("note", "Customer deleted this message - should be removed from system"))

	// Find interaction by platform message ID
	var interaction storage.Interaction
	if err := h.DB.Where("tenant_id = ? AND platform_message_id = ?", tenantID, messageID).First(&interaction).Error; err == nil {
		// Update metadata with deleted status
		var metadata map[string]interface{}
		if interaction.Metadata != "" {
			if err := json.Unmarshal([]byte(interaction.Metadata), &metadata); err != nil {
				metadata = make(map[string]interface{})
			}
		} else {
			metadata = make(map[string]interface{})
		}

		metadata["deleted_status"] = "deleted"
		metadata["deleted_at"] = time.Now().UTC().Format(time.RFC3339)

		metadataJSON, err := json.Marshal(metadata)
		if err == nil {
			interaction.Metadata = string(metadataJSON)
			// Mark interaction as deleted (soft delete - keep record but mark as deleted)
			// Note: We don't actually delete from DB, just mark it
			interaction.ResponseStatus = "deleted"
			h.DB.Save(&interaction)

			h.Logger.Info("✅ Updated interaction with deleted status",
				zap.Uint("tenant_id", tenantID),
				zap.String("message_id", messageID),
				zap.Uint("interaction_id", interaction.ID))
		}
	} else {
		h.Logger.Warn("⚠️ Deleted message event received but interaction not found",
			zap.Uint("tenant_id", tenantID),
			zap.String("message_id", messageID))
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

// handleAccountEvent processes account status events (WABA approval, phone verification, etc.)
// Event types: "account-event", "system-event"
// Payload structure: {"type": "account-event", "payload": {"type": "review-event" | "status-event", ...}}
func (h *GupshupWebhookHandler) handleAccountEvent(ctx context.Context, tenantID uint, appID string, envelope map[string]interface{}) {
	h.Logger.Info("📋 Processing account event",
		zap.Uint("tenant_id", tenantID),
		zap.String("app_id", appID))

	// Extract payload
	payload, ok := envelope["payload"].(map[string]interface{})
	if !ok {
		h.Logger.Warn("invalid account event payload structure",
			zap.Uint("tenant_id", tenantID),
			zap.String("app_id", appID))
		return
	}

	// Check event subtype: "review-event", "status-event", etc.
	eventSubtype, _ := payload["type"].(string)
	status, _ := payload["status"].(string)

	h.Logger.Info("📋 Account event details",
		zap.String("event_subtype", eventSubtype),
		zap.String("status", status),
		zap.Uint("tenant_id", tenantID),
		zap.String("app_id", appID),
		zap.Any("full_payload", payload), // Log full payload to see what data is available
		zap.String("payload_json", func() string {
			jsonBytes, _ := json.Marshal(payload)
			return string(jsonBytes)
		}()))

	// CRITICAL: Always call GetAppDetails on account events to extract complete metadata
	// Don't rely on webhook payload alone - GetAppDetails has the authoritative data
	// This handles: WABA approval, phone verification, business ID, phone number ID, etc.
	// Fetch latest app details from Gupshup API to get complete metadata
	appDetails, err := h.Client.GetAppDetails(appID)
	if err != nil {
		h.Logger.Error("failed to fetch app details after account event",
			zap.String("app_id", appID),
			zap.String("status", status),
			zap.String("event_subtype", eventSubtype),
			zap.Error(err))
		// Continue to update status even if GetAppDetails fails
		appDetails = nil
	}

	// Update WhatsAppApp record with metadata from GetAppDetails
	var app storage.WhatsAppApp
	if err := h.DB.Where("tenant_id = ? AND gupshup_app_id = ?", tenantID, appID).First(&app).Error; err != nil {
		h.Logger.Warn("app not found for account event",
			zap.String("app_id", appID),
			zap.Uint("tenant_id", tenantID),
			zap.Error(err))
		return
	}

	// Extract WhatsApp metadata from app details if available
	// Response structure: {"whatsapp": {"businessId": "...", "formattedPhoneNumber": "...", "normalizedPhoneNumber": "...", ...}}
	if appDetails != nil {
		// Log full app details to debug why metadata is missing
		appDetailsJSON, _ := json.Marshal(appDetails)
		h.Logger.Info("📋 GetAppDetails response structure",
			zap.String("app_id", appID),
			zap.String("response_json", string(appDetailsJSON)),
			zap.Bool("has_whatsapp_key", func() bool {
				_, ok := appDetails["whatsapp"]
				return ok
			}()))

		if whatsapp, ok := appDetails["whatsapp"].(map[string]interface{}); ok {
			// Update WABA ID
			if businessID, ok := whatsapp["businessId"].(string); ok && businessID != "" {
				app.WABAID = businessID
				app.BusinessID = businessID
			}

			// Update phone number
			if phone, ok := whatsapp["formattedPhoneNumber"].(string); ok && phone != "" {
				app.PhoneNumber = phone
			} else if phone, ok := whatsapp["normalizedPhoneNumber"].(string); ok && phone != "" {
				app.PhoneNumber = phone
			}

			// Update phone number ID if available
			if phoneNumberID, ok := whatsapp["phoneNumberId"].(string); ok && phoneNumberID != "" {
				app.PhoneNumberID = phoneNumberID
			}

			// Update status
			if status == "approved" || status == "live" || status == "connected" {
				app.Status = "live"
			}
		}

		// Update status from event if provided
		if status != "" && (status == "approved" || status == "live" || status == "connected") {
			app.Status = "live"
		}

		// Save updated app record
		if err := h.DB.Save(&app).Error; err != nil {
			h.Logger.Error("failed to update WhatsAppApp after account event",
				zap.String("app_id", appID),
				zap.Uint("tenant_id", tenantID),
				zap.Error(err))
			return
		}

		h.Logger.Info("✅ WhatsAppApp updated from account event",
			zap.String("app_id", appID),
			zap.Uint("tenant_id", tenantID),
			zap.String("status", app.Status),
			zap.String("waba_id", app.WABAID),
			zap.String("phone", app.PhoneNumber))

		// Also update SocialAccount for compatibility
		var account storage.SocialAccount
		if err := h.DB.Where("tenant_id = ? AND platform = ?", tenantID, "whatsapp").First(&account).Error; err == nil {
			account.Username = app.PhoneNumber
			account.PageID = app.PhoneNumberID
			account.ExternalID = app.PhoneNumberID
			account.Connected = (app.Status == "live" && app.PhoneNumber != "")
			// Ensure Metadata is valid JSON (empty string is invalid for JSONB)
			if account.Metadata == "" {
				account.Metadata = "{}"
			}
			if err := h.DB.Save(&account).Error; err != nil {
				h.Logger.Warn("failed to update SocialAccount after account event", zap.Error(err))
			}
		}
	}
}

// handleOnboardingEvent processes the Go-Live event when app completes onboarding
// Event type: "onboarding-event"
// Payload structure: {"type": "onboarding-event", "appId": "...", "phone": "...", "payload": {"type": "docker-status-event", "payload": {"status": "live", "waId": "WABA_ID"}}}
func (h *GupshupWebhookHandler) handleOnboardingEvent(ctx context.Context, tenantID uint, appID string, envelope map[string]interface{}) {
	h.Logger.Info("🎉 Processing Go-Live onboarding event",
		zap.Uint("tenant_id", tenantID),
		zap.String("app_id", appID))

	// Extract phone number from envelope (top level)
	phone, _ := envelope["phone"].(string)

	// Extract nested payload structure
	payload, ok := envelope["payload"].(map[string]interface{})
	if !ok {
		h.Logger.Warn("invalid onboarding event payload structure",
			zap.Uint("tenant_id", tenantID),
			zap.String("app_id", appID))
		// Still try to update with phone number if available
		if phone != "" {
			h.updateAppFromOnboardingEvent(tenantID, appID, phone, "", "live")
		}
		return
	}

	// Check if payload.type is "docker-status-event"
	payloadType, _ := payload["type"].(string)
	if payloadType != "docker-status-event" {
		h.Logger.Info("onboarding event with non-docker-status type",
			zap.String("payload_type", payloadType),
			zap.Uint("tenant_id", tenantID),
			zap.String("app_id", appID))
	}

	// Extract nested payload.payload for docker-status-event
	nestedPayload, ok := payload["payload"].(map[string]interface{})
	if !ok {
		h.Logger.Warn("invalid nested payload structure in onboarding event",
			zap.Uint("tenant_id", tenantID),
			zap.String("app_id", appID))
		// Still try to update with phone number if available
		if phone != "" {
			h.updateAppFromOnboardingEvent(tenantID, appID, phone, "", "live")
		}
		return
	}

	// Extract status and WABA ID from nested payload
	status, _ := nestedPayload["status"].(string)
	wabaID, _ := nestedPayload["waId"].(string)

	h.Logger.Info("🎉 Go-Live event details",
		zap.String("phone", phone),
		zap.String("waba_id", wabaID),
		zap.String("status", status),
		zap.Uint("tenant_id", tenantID),
		zap.String("app_id", appID))

	// Update app record with Go-Live event data
	h.updateAppFromOnboardingEvent(tenantID, appID, phone, wabaID, status)

	// Also call GetAppDetails to ensure we have complete metadata
	appDetails, err := h.Client.GetAppDetails(appID)
	if err != nil {
		h.Logger.Warn("failed to fetch app details after onboarding event (non-critical)",
			zap.String("app_id", appID),
			zap.Error(err))
	} else {
		// Update with complete metadata from GetAppDetails
		var app storage.WhatsAppApp
		if err := h.DB.Where("tenant_id = ? AND gupshup_app_id = ?", tenantID, appID).First(&app).Error; err == nil {
			if whatsapp, ok := appDetails["whatsapp"].(map[string]interface{}); ok {
				// Update WABA ID if not already set
				if app.WABAID == "" {
					if businessID, ok := whatsapp["businessId"].(string); ok && businessID != "" {
						app.WABAID = businessID
						app.BusinessID = businessID
					}
				}

				// Update phone number if not already set
				if app.PhoneNumber == "" {
					if phoneNum, ok := whatsapp["formattedPhoneNumber"].(string); ok && phoneNum != "" {
						app.PhoneNumber = phoneNum
					} else if phoneNum, ok := whatsapp["normalizedPhoneNumber"].(string); ok && phoneNum != "" {
						app.PhoneNumber = phoneNum
					}
				}

				// Update phone number ID
				if phoneNumberID, ok := whatsapp["phoneNumberId"].(string); ok && phoneNumberID != "" {
					app.PhoneNumberID = phoneNumberID
				}

				// Ensure status is "live"
				if status == "live" || app.Status == "live" {
					app.Status = "live"
				}

				h.DB.Save(&app)
			}
		}
	}
}

// updateAppFromOnboardingEvent updates the WhatsAppApp record with data from Go-Live event
func (h *GupshupWebhookHandler) updateAppFromOnboardingEvent(tenantID uint, appID, phone, wabaID, status string) {
	var app storage.WhatsAppApp
	if err := h.DB.Where("tenant_id = ? AND gupshup_app_id = ?", tenantID, appID).First(&app).Error; err != nil {
		h.Logger.Warn("app not found for onboarding event",
			zap.String("app_id", appID),
			zap.Uint("tenant_id", tenantID),
			zap.Error(err))
		return
	}

	updated := false

	// Update phone number
	if phone != "" && app.PhoneNumber != phone {
		app.PhoneNumber = phone
		updated = true
	}

	// Update WABA ID
	if wabaID != "" && app.WABAID != wabaID {
		app.WABAID = wabaID
		app.BusinessID = wabaID
		updated = true
	}

	// Update status to "live" if status is "live"
	if status == "live" && app.Status != "live" {
		app.Status = "live"
		updated = true

		// Create AI reply template when app goes live (if not already created)
		if app.DefaultTemplateID == "" && h.Client != nil {
			h.Logger.Info("🎨 Creating AI reply template for live app",
				zap.String("app_id", appID),
				zap.Uint("tenant_id", tenantID))

			templateID, err := h.Client.CreateAITemplate(appID)
			if err != nil {
				h.Logger.Warn("failed to create AI template (non-critical, will retry later)",
					zap.String("app_id", appID),
					zap.Error(err))
			} else {
				app.DefaultTemplateID = templateID
				updated = true
				h.Logger.Info("✅ AI reply template created and stored",
					zap.String("app_id", appID),
					zap.String("template_id", templateID))
			}
		}
	}

	if updated {
		if err := h.DB.Save(&app).Error; err != nil {
			h.Logger.Error("failed to update WhatsAppApp from onboarding event",
				zap.String("app_id", appID),
				zap.Uint("tenant_id", tenantID),
				zap.Error(err))
			return
		}

		h.Logger.Info("✅ WhatsAppApp updated from Go-Live event",
			zap.String("app_id", appID),
			zap.Uint("tenant_id", tenantID),
			zap.String("status", app.Status),
			zap.String("waba_id", app.WABAID),
			zap.String("phone", app.PhoneNumber))

		// Also update SocialAccount for compatibility
		var account storage.SocialAccount
		if err := h.DB.Where("tenant_id = ? AND platform = ?", tenantID, "whatsapp").First(&account).Error; err == nil {
			account.Username = app.PhoneNumber
			account.PageID = app.PhoneNumberID
			account.ExternalID = app.PhoneNumberID
			account.Connected = (app.Status == "live" && app.PhoneNumber != "")
			// Ensure Metadata is valid JSON (empty string is invalid for JSONB)
			if account.Metadata == "" {
				account.Metadata = "{}"
			}
			if err := h.DB.Save(&account).Error; err != nil {
				h.Logger.Warn("failed to update SocialAccount after onboarding event", zap.Error(err))
			}
		}
	}
}

// PollPendingApps checks apps stuck in pending_onboarding status and fetches their latest status
// This should be called periodically (e.g., every 5-10 minutes) as a fallback if webhooks fail
// Call this from a background worker or cron job
func (h *GupshupWebhookHandler) PollPendingApps(ctx context.Context) error {
	// Find apps that are pending and haven't been updated in the last hour
	oneHourAgo := time.Now().Add(-1 * time.Hour)

	var pendingApps []storage.WhatsAppApp
	if err := h.DB.Where("status = ? AND updated_at < ?", "pending_onboarding", oneHourAgo).
		Find(&pendingApps).Error; err != nil {
		return fmt.Errorf("failed to query pending apps: %w", err)
	}

	if len(pendingApps) == 0 {
		return nil // No pending apps to check
	}

	h.Logger.Info("🔄 Polling pending WhatsApp apps",
		zap.Int("count", len(pendingApps)))

	for _, app := range pendingApps {
		// Skip if app was created less than 30 minutes ago (give it time)
		if time.Since(app.CreatedAt) < 30*time.Minute {
			continue
		}

		// Fetch latest app details from Gupshup
		appDetails, err := h.Client.GetAppDetails(app.GupshupAppID)
		if err != nil {
			h.Logger.Warn("failed to fetch app details during polling",
				zap.String("app_id", app.GupshupAppID),
				zap.Uint("tenant_id", app.TenantID),
				zap.Error(err))
			continue
		}

		// Check if app status has changed
		updated := false

		// Extract WhatsApp metadata
		if whatsapp, ok := appDetails["whatsapp"].(map[string]interface{}); ok {
			// Update WABA ID
			if businessID, ok := whatsapp["businessId"].(string); ok && businessID != "" && app.WABAID != businessID {
				app.WABAID = businessID
				app.BusinessID = businessID
				updated = true
			}

			// Update phone number
			if phone, ok := whatsapp["formattedPhoneNumber"].(string); ok && phone != "" && app.PhoneNumber != phone {
				app.PhoneNumber = phone
				updated = true
			} else if phone, ok := whatsapp["normalizedPhoneNumber"].(string); ok && phone != "" && app.PhoneNumber != phone {
				app.PhoneNumber = phone
				updated = true
			}

			// Update phone number ID
			if phoneNumberID, ok := whatsapp["phoneNumberId"].(string); ok && phoneNumberID != "" && app.PhoneNumberID != phoneNumberID {
				app.PhoneNumberID = phoneNumberID
				updated = true
			}
		}

		// Check app status from response
		if status, ok := appDetails["status"].(string); ok {
			if status == "live" || status == "connected" || status == "approved" {
				if app.Status != "live" {
					app.Status = "live"
					updated = true
				}
			} else if status == "failed" || status == "rejected" {
				if app.Status != "failed" {
					app.Status = "failed"
					updated = true
					h.Logger.Warn("⚠️ WhatsApp app onboarding failed",
						zap.String("app_id", app.GupshupAppID),
						zap.Uint("tenant_id", app.TenantID),
						zap.String("status", status))
				}
			}
		}

		// If app has been pending for more than 24 hours with no updates, mark as failed
		if app.Status == "pending_onboarding" && time.Since(app.CreatedAt) > 24*time.Hour {
			app.Status = "failed"
			updated = true
			h.Logger.Warn("⚠️ WhatsApp app onboarding timed out (24+ hours)",
				zap.String("app_id", app.GupshupAppID),
				zap.Uint("tenant_id", app.TenantID))
		}

		if updated {
			if err := h.DB.Save(&app).Error; err != nil {
				h.Logger.Error("failed to update app during polling",
					zap.String("app_id", app.GupshupAppID),
					zap.Uint("tenant_id", app.TenantID),
					zap.Error(err))
			} else {
				h.Logger.Info("✅ Updated app status from polling",
					zap.String("app_id", app.GupshupAppID),
					zap.Uint("tenant_id", app.TenantID),
					zap.String("status", app.Status),
					zap.String("phone", app.PhoneNumber))

				// Update SocialAccount if app is now live
				if app.Status == "live" && app.PhoneNumber != "" {
					var account storage.SocialAccount
					if err := h.DB.Where("tenant_id = ? AND platform = ?", app.TenantID, "whatsapp").First(&account).Error; err == nil {
						account.Username = app.PhoneNumber
						account.PageID = app.PhoneNumberID
						account.ExternalID = app.PhoneNumberID
						account.Connected = true
						// Ensure Metadata is valid JSON (empty string is invalid for JSONB)
						if account.Metadata == "" {
							account.Metadata = "{}"
						}
						h.DB.Save(&account)
					}
				}
			}
		}
	}

	return nil
}

// StartPollingWorker starts a background worker that periodically polls pending apps
// Call this from your app initialization (e.g., in core/app.go)
func (h *GupshupWebhookHandler) StartPollingWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	h.Logger.Info("🔄 Gupshup polling worker started",
		zap.Duration("interval", interval))

	// Run immediately on start
	if err := h.PollPendingApps(ctx); err != nil {
		h.Logger.Error("polling worker error", zap.Error(err))
	}

	for {
		select {
		case <-ctx.Done():
			h.Logger.Info("🔄 Gupshup polling worker stopped")
			return
		case <-ticker.C:
			if err := h.PollPendingApps(ctx); err != nil {
				h.Logger.Error("polling worker error", zap.Error(err))
			}
		}
	}
}
