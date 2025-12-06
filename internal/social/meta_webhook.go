package social

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
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

	if h.Logger != nil {
		h.Logger.Info("Meta verification incoming from", zap.String("remote_addr", r.RemoteAddr))
	}

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
	if h.Logger != nil {
		h.Logger.Info("Meta webhook verification request",
			zap.String("mode", mode),
			zap.String("token", token),
			zap.String("challenge", challenge),
			zap.String("expected_token", expectedToken))
	}

	if mode != "subscribe" {
		if h.Logger != nil {
			h.Logger.Warn("Meta webhook verification rejected: invalid mode",
				zap.String("received_mode", mode))
		}
		h.Logger.Warn("❌ Webhook verification failed — invalid mode",
			zap.String("mode", mode),
			zap.String("expected", "subscribe"))
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if token == "" {
		if h.Logger != nil {
			h.Logger.Warn("Meta webhook verification rejected: token missing")
		}
		h.Logger.Warn("❌ Webhook verification failed — no verify token provided")
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Verify token matches exactly (case-sensitive, no trimming)
	if token != expectedToken {
		if h.Logger != nil {
			h.Logger.Warn("Meta webhook verification rejected: token mismatch",
				zap.String("received", token),
				zap.Int("received_len", len(token)),
				zap.Int("expected_len", len(expectedToken)))
		}
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
		if h.Logger != nil {
			h.Logger.Warn("Meta webhook verification rejected: challenge missing")
		}
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
// Instagram messaging uses both "messaging" format (traditional) and "changes" format (newer/test webhooks)
func (h *MetaWebhookHandler) processInstagramWebhook(ctx context.Context, body []byte) {
	h.Logger.Info("📱 Parsing Instagram webhook payload")

	// Log full payload for debugging
	var rawPayload map[string]interface{}
	if err := json.Unmarshal(body, &rawPayload); err == nil {
		payloadJSON, _ := json.MarshalIndent(rawPayload, "", "  ")
		h.Logger.Info("📋 Full Instagram webhook payload",
			zap.String("payload", string(payloadJSON)))
	}

	// Try both formats: "messaging" (traditional) and "changes" (test/newer webhooks)
	var payload struct {
		Object string `json:"object"`
		Entry  []struct {
			ID        string `json:"id"` // This is the instagram_business_account.id (or "0" in test webhooks)
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
					MID         string                 `json:"mid"`
					Text        string                 `json:"text"`
					Attachments map[string]interface{} `json:"attachments,omitempty"`
					Reaction    map[string]interface{} `json:"reaction,omitempty"`
				} `json:"message"`
			} `json:"messaging"`
			Changes []struct {
				Field string `json:"field"`
				Value struct {
					Sender struct {
						ID string `json:"id"`
					} `json:"sender"`
					Recipient struct {
						ID string `json:"id"` // This is the instagram_business_account.id
					} `json:"recipient"`
					Timestamp FlexibleTimestamp `json:"timestamp"` // Can be string or int64
					Message   struct {
						MID         string                 `json:"mid"`
						Text        string                 `json:"text"`
						Attachments map[string]interface{} `json:"attachments,omitempty"`
						Reaction    map[string]interface{} `json:"reaction,omitempty"`
					} `json:"message"`
				} `json:"value"`
			} `json:"changes"`
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
			zap.Int("messaging_count", len(entry.Messaging)),
			zap.Int("changes_count", len(entry.Changes)))

		// Determine Instagram Business Account ID from entry ID
		// Test webhooks use "0", real webhooks use actual instagram_business_account.id
		igAccountID := entry.ID
		if igAccountID == "0" || igAccountID == "" {
			// Test webhook - try to resolve from recipient ID or find connected Instagram account
			h.Logger.Info("🔍 Test webhook detected, attempting to resolve Instagram account ID")
			// For test webhooks, we'll try to find any connected Instagram account for testing
			// In production, entry.ID will be the actual instagram_business_account.id
		}

		// Process messages from "messaging" array (traditional format)
		messagesProcessed := 0
		for _, msg := range entry.Messaging {
			messagesProcessed++

			// Use recipient ID if entry ID is "0" (test webhook)
			if igAccountID == "0" || igAccountID == "" {
				igAccountID = msg.Recipient.ID
				h.Logger.Info("✅ Using recipient ID as Instagram account ID for test webhook",
					zap.String("instagram_account_id", igAccountID))
			}

			// Resolve tenant from Instagram Business Account ID
			tenantID, err := h.resolveTenantFromExternalID(igAccountID, "instagram")
			if err != nil {
				h.Logger.Warn("⚠️ Could not resolve tenant from Instagram webhook",
					zap.String("platform", "instagram"),
					zap.String("external_id", igAccountID),
					zap.String("entry_id", entry.ID),
					zap.Error(err))
				// For test webhooks, try using recipient ID if entry ID doesn't work
				if igAccountID == msg.Recipient.ID {
					// Test mode fallback: find ANY connected Instagram account
					// This allows test webhooks to work even if recipient ID doesn't match exactly
					tenantID, igAccountID, err = h.resolveTenantFromAnyInstagramAccount()
					if err != nil {
						h.Logger.Warn("⚠️ Could not find any connected Instagram account for test mode",
							zap.Error(err))
						continue
					}
					h.Logger.Info("✅ Using any connected Instagram account for test mode",
						zap.Uint("tenant_id", tenantID),
						zap.String("instagram_account_id", igAccountID))
				} else {
					// Try recipient ID as fallback
					tenantID, err = h.resolveTenantFromExternalID(msg.Recipient.ID, "instagram")
					if err != nil {
						// Test mode fallback: find ANY connected Instagram account
						tenantID, igAccountID, err = h.resolveTenantFromAnyInstagramAccount()
						if err != nil {
							h.Logger.Warn("⚠️ Could not find any connected Instagram account for test mode",
								zap.Error(err))
							continue
						}
						h.Logger.Info("✅ Using any connected Instagram account for test mode (fallback)",
							zap.Uint("tenant_id", tenantID),
							zap.String("instagram_account_id", igAccountID))
					} else {
						igAccountID = msg.Recipient.ID
					}
				}
			}

			h.Logger.Info("✅ Tenant resolved for Instagram",
				zap.Uint("tenant_id", tenantID),
				zap.String("external_id", igAccountID),
				zap.String("entry_id", entry.ID),
				zap.String("platform", "instagram"))

			// Extract message content - handle text, media, reactions, etc.
			messageContent := h.extractInstagramMessageContent(msg.Message)
			if messageContent == "" {
				// Skip non-text messages (media, reactions, stickers) - they don't need AI processing
				h.Logger.Info("⏭️ Skipping non-text Instagram message",
					zap.Uint("tenant_id", tenantID),
					zap.String("message_id", msg.Message.MID),
					zap.Bool("has_attachments", len(msg.Message.Attachments) > 0),
					zap.Bool("has_reaction", len(msg.Message.Reaction) > 0))
				continue
			}
			h.processInstagramMessage(ctx, tenantID, igAccountID, msg.Sender.ID, msg.Message.MID, messageContent, msg.Timestamp, entry.ID)
		}

		// Process messages from "changes" array (test/newer webhook format)
		for _, change := range entry.Changes {
			if change.Field != "messages" {
				continue // Skip non-message changes
			}

			// Use recipient ID if entry ID is "0" (test webhook)
			if igAccountID == "0" || igAccountID == "" {
				igAccountID = change.Value.Recipient.ID
				h.Logger.Info("✅ Using recipient ID as Instagram account ID for test webhook (changes format)",
					zap.String("instagram_account_id", igAccountID))
			}

			// Resolve tenant from Instagram Business Account ID
			tenantID, err := h.resolveTenantFromExternalID(igAccountID, "instagram")
			if err != nil {
				h.Logger.Warn("⚠️ Could not resolve tenant from Instagram webhook (changes format)",
					zap.String("platform", "instagram"),
					zap.String("external_id", igAccountID),
					zap.String("entry_id", entry.ID),
					zap.Error(err))
				// Try recipient ID as fallback
				tenantID, err = h.resolveTenantFromExternalID(change.Value.Recipient.ID, "instagram")
				if err != nil {
					// Test mode fallback: find ANY connected Instagram account
					tenantID, igAccountID, err = h.resolveTenantFromAnyInstagramAccount()
					if err != nil {
						h.Logger.Warn("⚠️ Could not find any connected Instagram account for test mode (changes format)",
							zap.Error(err))
						continue
					}
					h.Logger.Info("✅ Using any connected Instagram account for test mode (changes format)",
						zap.Uint("tenant_id", tenantID),
						zap.String("instagram_account_id", igAccountID))
				} else {
					igAccountID = change.Value.Recipient.ID
				}
			}

			h.Logger.Info("✅ Tenant resolved for Instagram (changes format)",
				zap.Uint("tenant_id", tenantID),
				zap.String("external_id", igAccountID),
				zap.String("entry_id", entry.ID),
				zap.String("platform", "instagram"))

			timestamp := change.Value.Timestamp.Int64()
			// Extract message content - handle text, media, reactions, etc.
			messageContent := h.extractInstagramMessageContent(change.Value.Message)
			if messageContent == "" {
				// Skip non-text messages (media, reactions, stickers) - they don't need AI processing
				h.Logger.Info("⏭️ Skipping non-text Instagram message (changes format)",
					zap.Uint("tenant_id", tenantID),
					zap.String("message_id", change.Value.Message.MID),
					zap.Bool("has_attachments", len(change.Value.Message.Attachments) > 0),
					zap.Bool("has_reaction", len(change.Value.Message.Reaction) > 0))
				continue
			}
			h.processInstagramMessage(ctx, tenantID, igAccountID, change.Value.Sender.ID, change.Value.Message.MID, messageContent, timestamp, entry.ID)
			messagesProcessed++
		}

		if messagesProcessed == 0 {
			h.Logger.Warn("⚠️ Instagram entry has no messaging events",
				zap.String("entry_id", entry.ID),
				zap.Int("messaging_count", len(entry.Messaging)),
				zap.Int("changes_count", len(entry.Changes)))
		}
	}
}

// fetchInstagramUserProfile fetches Instagram user profile information
// For Instagram Login: uses graph.instagram.com/v24.0
// For Facebook Login: uses graph.facebook.com/v18.0
func (h *MetaWebhookHandler) fetchInstagramUserProfile(ctx context.Context, senderID string, account *storage.SocialAccount) (name, username string) {
	if account == nil || account.AccessToken == "" {
		return "", ""
	}

	// Decrypt token
	decryptedToken, err := utils.DecryptToken(account.AccessToken)
	if err != nil {
		decryptedToken = account.AccessToken
	}

	if decryptedToken == "" {
		return "", ""
	}

	// Determine API endpoint based on login method
	var apiURL string
	if account.LoginMethod == "instagram_login" {
		// Instagram Login: Use graph.instagram.com/v24.0
		// Note: Instagram Login API doesn't support fetching user profiles by IGSID directly
		// We can only get the account owner's profile, not customer profiles
		// For now, return empty - customer name will need to come from webhook payload or be set manually
		return "", ""
	} else {
		// Facebook Login: Use Messenger Platform API to get user profile
		// Endpoint: GET /{PSID}?fields=name,first_name,last_name&access_token={PAGE_ACCESS_TOKEN}
		apiURL = fmt.Sprintf("https://graph.facebook.com/v18.0/%s?fields=name,first_name,last_name&access_token=%s", senderID, url.QueryEscape(decryptedToken))
	}

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", ""
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", ""
	}

	var profile struct {
		Name      string `json:"name"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return "", ""
	}

	// Use name if available, otherwise construct from first/last name
	if profile.Name != "" {
		name = profile.Name
	} else if profile.FirstName != "" {
		name = profile.FirstName
		if profile.LastName != "" {
			name += " " + profile.LastName
		}
	}

	return name, ""
}

// extractInstagramMessageContent extracts text content from an Instagram message
// Returns empty string for non-text messages (media, reactions, stickers) which should be skipped
func (h *MetaWebhookHandler) extractInstagramMessageContent(msg struct {
	MID         string                 `json:"mid"`
	Text        string                 `json:"text"`
	Attachments map[string]interface{} `json:"attachments,omitempty"`
	Reaction    map[string]interface{} `json:"reaction,omitempty"`
}) string {
	// If message has text, return it
	if strings.TrimSpace(msg.Text) != "" {
		return msg.Text
	}

	// If message has attachments (media), create a descriptive placeholder
	if len(msg.Attachments) > 0 {
		// Check attachment type
		if attachmentType, ok := msg.Attachments["type"].(string); ok {
			switch strings.ToLower(attachmentType) {
			case "image":
				return "[Image]"
			case "video":
				return "[Video]"
			case "audio":
				return "[Audio]"
			case "file":
				return "[File]"
			default:
				return "[Media]"
			}
		}
		return "[Media]"
	}

	// If message has a reaction, skip it (reactions don't need AI processing)
	if len(msg.Reaction) > 0 {
		return "" // Empty string signals to skip this message
	}

	// If message has no text, attachments, or reactions, it might be a sticker or other non-text content
	// Return empty to skip it
	return ""
}

// processInstagramMessage processes a single Instagram message
func (h *MetaWebhookHandler) processInstagramMessage(ctx context.Context, tenantID uint, igAccountID, senderID, messageID, text string, timestamp int64, entryID string) {
	h.Logger.Info("💬 Processing Instagram message",
		zap.Uint("tenant_id", tenantID),
		zap.String("sender_id", senderID),
		zap.String("recipient_id", igAccountID),
		zap.String("message_id", messageID),
		zap.String("message_preview", truncateString(text, 100)),
		zap.Int64("timestamp", timestamp))

	// Fetch customer profile information (name, username)
	// Get the social account to determine login method
	var account storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ? AND external_id = ? AND connected = ?", tenantID, "instagram", igAccountID, true).First(&account).Error; err == nil {
		customerName, customerUsername := h.fetchInstagramUserProfile(ctx, senderID, &account)
		if customerName != "" || customerUsername != "" {
			h.Logger.Info("✅ Fetched Instagram customer profile",
				zap.String("sender_id", senderID),
				zap.String("name", customerName),
				zap.String("username", customerUsername))
		}

		// Use senderID as ThreadID to ensure all messages from same customer are grouped
		// For Instagram, ThreadID should be the customer's IGSID (senderID)
		incomingMsg := IncomingMessage{
			Platform:         "instagram",
			TenantID:         tenantID,
			CustomerID:       senderID,
			CustomerName:     customerName,
			CustomerUsername: customerUsername,
			MessageID:        messageID,
			Content:          text,
			MessageType:      "text",
			ThreadID:         senderID, // Use senderID as ThreadID to group all messages from same customer
			Timestamp:        time.Unix(timestamp/1000, 0),
			Metadata:         map[string]interface{}{"entry_id": entryID},
		}

		h.Logger.Info("🚀 Sending Instagram message to MessageHandler",
			zap.Uint("tenant_id", tenantID),
			zap.String("customer_id", senderID),
			zap.String("customer_name", customerName),
			zap.String("thread_id", senderID))

		go h.MessageHandler.ProcessMessage(ctx, incomingMsg)
	} else {
		// Account not found - still process message but without profile info
		h.Logger.Warn("⚠️ Instagram account not found for profile fetch",
			zap.Uint("tenant_id", tenantID),
			zap.String("ig_account_id", igAccountID))

		// Still process the message even without account info
		// The customer name might be available from webhook payload or can be set manually
		incomingMsg := IncomingMessage{
			Platform:         "instagram",
			TenantID:         tenantID,
			CustomerID:       senderID,
			CustomerName:     "", // Will be empty, but customer can be updated later
			CustomerUsername: "",
			MessageID:        messageID,
			Content:          text,
			MessageType:      "text",
			ThreadID:         senderID, // Use senderID as ThreadID to group all messages from same customer
			Timestamp:        time.Unix(timestamp/1000, 0),
			Metadata:         map[string]interface{}{"entry_id": entryID},
		}

		h.Logger.Info("🚀 Sending Instagram message to MessageHandler (without account info)",
			zap.Uint("tenant_id", tenantID),
			zap.String("customer_id", senderID),
			zap.String("thread_id", senderID))

		go h.MessageHandler.ProcessMessage(ctx, incomingMsg)
	}
}

// FlexibleTimestamp handles both string and int64 timestamps from Meta webhooks
type FlexibleTimestamp int64

// UnmarshalJSON handles both string and int64 timestamps
func (ft *FlexibleTimestamp) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		// Try parsing as string (Unix timestamp in milliseconds)
		val, err := strconv.ParseInt(str, 10, 64)
		if err != nil {
			return err
		}
		*ft = FlexibleTimestamp(val)
		return nil
	}

	// Try parsing as int64 directly
	var val int64
	if err := json.Unmarshal(data, &val); err != nil {
		return err
	}
	*ft = FlexibleTimestamp(val)
	return nil
}

// Int64 returns the timestamp as int64
func (ft FlexibleTimestamp) Int64() int64 {
	return int64(ft)
}

// processFacebookWebhook processes Facebook webhook events
func (h *MetaWebhookHandler) processFacebookWebhook(ctx context.Context, body []byte) {
	h.Logger.Info("📱 Parsing Facebook webhook payload")

	// First, log raw payload for debugging
	var rawPayload map[string]interface{}
	if err := json.Unmarshal(body, &rawPayload); err == nil {
		payloadJSON, _ := json.MarshalIndent(rawPayload, "", "  ")
		h.Logger.Info("📋 Full Facebook webhook payload",
			zap.String("payload", string(payloadJSON)))
	}

	var payload struct {
		Object string `json:"object"`
		Entry  []struct {
			ID        string            `json:"id"` // This is the page_id
			Time      FlexibleTimestamp `json:"time"`
			Messaging []struct {
				Sender struct {
					ID string `json:"id"`
				} `json:"sender"`
				Recipient struct {
					ID string `json:"id"` // This is the page_id
				} `json:"recipient"`
				Timestamp FlexibleTimestamp `json:"timestamp"`
				Message   struct {
					MID  string `json:"mid"`
					Text string `json:"text"`
				} `json:"message"`
			} `json:"messaging"`
		} `json:"entry"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		h.Logger.Error("❌ Failed to decode Facebook webhook payload",
			zap.Error(err),
			zap.String("raw_body", string(body)))
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
				Timestamp:        time.Unix(msg.Timestamp.Int64()/1000, 0),
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

	// For WhatsApp provider-owned model, also check WhatsAppPhoneNumber table
	if platform == "whatsapp" {
		var phoneNum storage.WhatsAppPhoneNumber
		if err := h.DB.Where("phone_number_id = ? AND tenant_id IS NOT NULL AND status = ?", externalID, "assigned").First(&phoneNum).Error; err == nil {
			if phoneNum.TenantID != nil {
				h.Logger.Info("✅ Tenant found via WhatsAppPhoneNumber",
					zap.Uint("tenant_id", *phoneNum.TenantID),
					zap.String("phone_number_id", externalID))
				return *phoneNum.TenantID, nil
			}
		}
	}

	h.Logger.Warn("⚠️ No tenant found",
		zap.String("external_id", externalID),
		zap.String("platform", platform))
	return 0, fmt.Errorf("no tenant found for external_id=%s platform=%s", externalID, platform)
}

// resolveTenantFromAnyInstagramAccount finds ANY connected Instagram account (used for test mode fallback)
// Returns tenantID, instagramAccountID, error
func (h *MetaWebhookHandler) resolveTenantFromAnyInstagramAccount() (uint, string, error) {
	var account storage.SocialAccount

	// Find any connected Instagram account
	if err := h.DB.Where("platform = ? AND connected = ?", "instagram", true).First(&account).Error; err != nil {
		return 0, "", fmt.Errorf("no connected Instagram account found: %w", err)
	}

	// Use ExternalID if available, otherwise PageID
	igAccountID := account.ExternalID
	if igAccountID == "" {
		igAccountID = account.PageID
	}
	if igAccountID == "" {
		return 0, "", fmt.Errorf("Instagram account has no ExternalID or PageID")
	}

	h.Logger.Info("✅ Found Instagram account for test mode",
		zap.Uint("tenant_id", account.TenantID),
		zap.String("instagram_account_id", igAccountID))

	return account.TenantID, igAccountID, nil
}

// truncateString truncates a string to a maximum length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
