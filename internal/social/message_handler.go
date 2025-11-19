package social

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// MessageHandler processes incoming messages from social platforms
type MessageHandler struct {
	DB             *gorm.DB
	Logger         *zap.Logger
	IntentDetector *ai.IntentDetector
	Responder      *ai.CustomerResponder
	SystemToken    string // Brancr's system user token for provider-owned WhatsApp model
	EscalationFunc func(ctx context.Context, tenantID uint, interactionID uint, customerName, platform, message, suggestedReply, intent, tone string, confidence float64) error
}

// IncomingMessage represents a message from a social platform
type IncomingMessage struct {
	Platform         string // instagram, facebook, tiktok, youtube, whatsapp
	TenantID         uint
	CustomerID       string // Platform-specific customer ID
	CustomerName     string
	CustomerUsername string
	MessageID        string // Platform-specific message ID
	Content          string
	MessageType      string // text, image, video, comment
	ThreadID         string // For DMs/threads
	Timestamp        time.Time
	Metadata         map[string]interface{} // Platform-specific data
}

// ProcessMessage handles an incoming message: detects intent, decides auto-reply or escalation
func (h *MessageHandler) ProcessMessage(ctx context.Context, msg IncomingMessage) error {
	// Step 1: Track or create customer
	customer, err := h.trackCustomer(ctx, msg)
	if err != nil {
		h.Logger.Error("failed to track customer", zap.Error(err))
		return err
	}

	// Step 2: Detect intent and tone
	intent, err := h.IntentDetector.DetectIntent(ctx, msg.TenantID, msg.Content, msg.Platform)
	if err != nil {
		h.Logger.Error("intent detection failed", zap.Error(err))
		intent = h.IntentDetector.FallbackDetection(msg.Content)
	}

	// Step 3: Log interaction
	interaction := storage.Interaction{
		TenantID:          msg.TenantID,
		CustomerID:        customer.ID,
		Platform:          msg.Platform,
		Direction:         "incoming",
		MessageType:       msg.MessageType,
		Content:           msg.Content,
		PlatformMessageID: msg.MessageID,
		DetectedIntent:    intent.Intent,
		DetectedTone:      intent.Tone,
		Confidence:        intent.Confidence,
		ResponseType:      "auto_reply",
		ResponseStatus:    "pending",
		Metadata:          h.metadataToJSON(msg.Metadata),
	}

	if err := h.DB.Create(&interaction).Error; err != nil {
		h.Logger.Error("failed to log interaction", zap.Error(err))
	}

	// Step 4: Decide: auto-reply or escalate
	if intent.RequiresEscalation || intent.Confidence < 0.6 {
		return h.escalateToTenant(ctx, msg, customer, intent, interaction.ID)
	}

	// Step 5: Generate and send auto-reply
	reply, err := h.Responder.GenerateCustomerReply(ctx, msg.TenantID, customer.ID, msg.Content, intent.Intent, msg.Platform)
	if err != nil {
		h.Logger.Error("failed to generate reply", zap.Error(err))
		return h.escalateToTenant(ctx, msg, customer, intent, interaction.ID)
	}

	// Step 6: Send reply to platform
	if err := h.sendReply(ctx, msg, reply); err != nil {
		h.Logger.Error("❌ Failed to send reply to customer",
			zap.Error(err),
			zap.String("platform", msg.Platform),
			zap.String("customer_id", msg.CustomerID))
		interaction.ResponseStatus = "failed"
		interaction.FinalReply = reply
		h.DB.Save(&interaction)
		// Don't return error - we still want to log the interaction
		return nil
	}

	// Step 7: Update interaction
	interaction.ResponseType = "auto_reply"
	interaction.ResponseStatus = "sent"
	interaction.FinalReply = reply
	h.DB.Save(&interaction)

	// Step 8: Save to conversation memory
	h.saveToMemory(ctx, msg.TenantID, customer.ID, msg.Content, reply)

	h.Logger.Info("✅ Auto-replied to customer",
		zap.Uint("tenant_id", msg.TenantID),
		zap.Uint("customer_id", customer.ID),
		zap.String("platform", msg.Platform),
		zap.String("intent", intent.Intent),
	)

	return nil
}

// trackCustomer ensures customer exists in database
func (h *MessageHandler) trackCustomer(ctx context.Context, msg IncomingMessage) (*storage.Customer, error) {
	var customer storage.Customer
	err := h.DB.Where("tenant_id = ? AND channel = ? AND platform_id = ?",
		msg.TenantID, msg.Platform, msg.CustomerID).First(&customer).Error

	if err == nil {
		// Update last message time
		customer.LastMessageAt = msg.Timestamp
		h.DB.Save(&customer)
		return &customer, nil
	}

	// Create new customer
	customer = storage.Customer{
		TenantID:      msg.TenantID,
		Channel:       msg.Platform,
		PlatformID:    msg.CustomerID,
		DisplayName:   msg.CustomerName,
		Username:      msg.CustomerUsername,
		LastMessageAt: msg.Timestamp,
	}

	if err := h.DB.Create(&customer).Error; err != nil {
		return nil, err
	}

	return &customer, nil
}

// escalateToTenant sends message to tenant's Telegram control center
func (h *MessageHandler) escalateToTenant(ctx context.Context, msg IncomingMessage, customer *storage.Customer, intent *ai.DetectedIntent, interactionID uint) error {
	// Load interaction first
	var interaction storage.Interaction
	if err := h.DB.First(&interaction, interactionID).Error; err != nil {
		return fmt.Errorf("interaction not found: %w", err)
	}

	// Generate suggested reply
	suggestedReply, _ := h.Responder.GenerateCustomerReply(ctx, msg.TenantID, customer.ID, msg.Content, intent.Intent, msg.Platform)

	// Send escalation via escalation handler
	if h.EscalationFunc != nil {
		if err := h.EscalationFunc(ctx, msg.TenantID, interactionID, customer.DisplayName, msg.Platform, msg.Content, suggestedReply, intent.Intent, intent.Tone, intent.Confidence); err != nil {
			h.Logger.Error("failed to escalate", zap.Error(err))
			return err
		}
	}

	// Update interaction
	interaction.ResponseType = "escalated"
	interaction.ResponseStatus = "pending"
	interaction.SuggestedReply = suggestedReply
	now := time.Now()
	interaction.EscalatedAt = &now
	h.DB.Save(&interaction)

	h.Logger.Info("📤 Escalated to tenant",
		zap.Uint("tenant_id", msg.TenantID),
		zap.Uint("interaction_id", interactionID),
		zap.String("intent", intent.Intent),
	)

	return nil
}

// sendReply sends a reply to the customer on the platform
func (h *MessageHandler) sendReply(ctx context.Context, msg IncomingMessage, reply string) error {
	// Get social account with all fields (especially ExternalID)
	var acc storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ? AND connected = ?",
		msg.TenantID, msg.Platform, true).First(&acc).Error; err != nil {
		return fmt.Errorf("no connected %s account", msg.Platform)
	}

	// Log account details for debugging
	h.Logger.Info("📋 Loading social account for reply",
		zap.String("platform", msg.Platform),
		zap.Uint("tenant_id", msg.TenantID),
		zap.String("external_id", acc.ExternalID),
		zap.String("page_id", acc.PageID))

	// Platform-specific reply sending
	// This will be implemented per platform
	switch msg.Platform {
	case "instagram":
		return h.sendInstagramReply(ctx, &acc, msg.ThreadID, reply)
	case "facebook":
		return h.sendFacebookReply(ctx, &acc, msg.ThreadID, reply)
	case "whatsapp":
		return h.sendWhatsAppReply(ctx, &acc, msg.CustomerID, reply)
	default:
		return fmt.Errorf("reply sending not implemented for %s", msg.Platform)
	}
}

// sendInstagramReply sends a reply via Instagram Messaging API
// According to Meta's official documentation: https://developers.facebook.com/docs/messenger-platform/instagram/feature-reference/send-message
func (h *MessageHandler) sendInstagramReply(ctx context.Context, acc *storage.SocialAccount, threadID, reply string) error {
	// Decrypt token
	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		// Try using as-is if decryption fails
		decryptedToken = acc.AccessToken
	}

	if decryptedToken == "" {
		return fmt.Errorf("Instagram access token is empty")
	}

	// Get Instagram Business Account ID (from ExternalID or PageID)
	igAccountID := acc.ExternalID
	if igAccountID == "" {
		igAccountID = acc.PageID
	}
	if igAccountID == "" {
		// Auto-fetch Instagram Business Account ID if missing
		h.Logger.Warn("⚠️ Instagram Business Account ID missing, attempting to auto-fetch",
			zap.Uint("tenant_id", acc.TenantID),
			zap.String("platform", acc.Platform))

		fetchedID, err := h.fetchAndSaveInstagramID(ctx, acc)
		if err != nil {
			h.Logger.Error("❌ Failed to auto-fetch Instagram Business Account ID",
				zap.Error(err),
				zap.Uint("tenant_id", acc.TenantID))
			return fmt.Errorf("Instagram Business Account ID not found. Please run 'fix instagram' or reconnect Instagram. Error: %v", err)
		}
		igAccountID = fetchedID
		h.Logger.Info("✅ Auto-fetched and saved Instagram Business Account ID",
			zap.String("instagram_id", igAccountID),
			zap.Uint("tenant_id", acc.TenantID))
	}

	// Verify token type - Instagram messaging REQUIRES a Page Access Token
	tokenType, pageID, err := h.verifyTokenType(ctx, decryptedToken)
	if err != nil {
		h.Logger.Warn("⚠️ Could not verify token type", zap.Error(err))
	} else {
		h.Logger.Info("🔑 Token type verified",
			zap.String("token_type", tokenType),
			zap.String("page_id", pageID))

		if tokenType != "PAGE" {
			return fmt.Errorf("Instagram messaging requires a Page Access Token, but you're using a %s token. Please generate a Page Access Token from Meta Developer Console → Tools → Graph API Explorer → Select your Page → Generate Token with 'instagram_manage_messages' and 'pages_messaging' permissions", tokenType)
		}
	}

	// Instagram Messaging API endpoint according to Meta's official documentation
	// Endpoint: https://graph.facebook.com/v18.0/{instagram-business-account-id}/messages
	// This requires a Page Access Token with instagram_manage_messages permission
	apiURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages", igAccountID)

	h.Logger.Info("📤 Attempting to send Instagram DM",
		zap.String("instagram_account_id", igAccountID),
		zap.String("recipient_id", threadID),
		zap.String("page_id", pageID),
		zap.String("message_preview", truncateString(reply, 50)))

	// Instagram messaging payload format according to Meta's Send Message API
	// https://developers.facebook.com/docs/messenger-platform/instagram/feature-reference/send-message
	// recipient.id must be the Instagram user's PSID (Page-Scoped ID) received from webhook
	payload := map[string]interface{}{
		"recipient": map[string]string{
			"id": threadID, // PSID (Page-Scoped ID) of the Instagram user
		},
		"message": map[string]interface{}{
			"text": reply,
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Add access token as query parameter (Meta API standard)
	apiURLWithToken := fmt.Sprintf("%s?access_token=%s", apiURL, url.QueryEscape(decryptedToken))

	req, err := http.NewRequestWithContext(ctx, "POST", apiURLWithToken, strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		// Parse error response to provide helpful guidance
		var apiError struct {
			Error struct {
				Message   string `json:"message"`
				Type      string `json:"type"`
				Code      int    `json:"code"`
				SubCode   int    `json:"error_subcode"`
				FbtraceID string `json:"fbtrace_id"`
			} `json:"error"`
		}
		json.Unmarshal(body, &apiError)

		errorMsg := fmt.Sprintf("Instagram API error %d: %s", resp.StatusCode, string(body))

		// Provide specific guidance for common errors based on Meta's error codes
		if apiError.Error.Code == 3 {
			h.Logger.Error("❌ INSTAGRAM MESSAGING CAPABILITY MISSING",
				zap.Int("error_code", apiError.Error.Code),
				zap.String("error_type", apiError.Error.Type),
				zap.String("error_message", apiError.Error.Message),
				zap.String("fbtrace_id", apiError.Error.FbtraceID),
				zap.String("action_required", "Enable Instagram Messaging capability in Meta Developer Console"))
			
			errorMsg += "\n\n❌ INSTAGRAM MESSAGING CAPABILITY MISSING:\n"
			errorMsg += "Your Meta app doesn't have the 'Instagram Messaging' capability enabled.\n"
			errorMsg += "This is REQUIRED even for test/development mode.\n\n"
			errorMsg += "🔧 IMMEDIATE ACTION REQUIRED:\n"
			errorMsg += "1. Go to: https://developers.facebook.com/apps/{YOUR_APP_ID}/dashboard\n"
			errorMsg += "2. Click 'Add Product' → Find 'Instagram Messaging' → Click 'Set Up'\n"
			errorMsg += "3. Configure webhook URL: https://your-domain.com/api/webhooks/meta\n"
			errorMsg += "4. Set verify token (must match META_WEBHOOK_VERIFY_TOKEN)\n"
			errorMsg += "5. Subscribe to 'messages' field\n"
			errorMsg += "6. Go to App Review → Permissions and Features\n"
			errorMsg += "7. Submit 'instagram_manage_messages' for App Review\n"
			errorMsg += "8. Wait for approval (1-7 days typically)\n\n"
			errorMsg += "⚠️ IMPORTANT: Instagram Messaging requires App Review approval\n"
			errorMsg += "   even for development/testing. Facebook Messenger works without\n"
			errorMsg += "   approval, but Instagram does not."
		} else if apiError.Error.Code == 190 {
			errorMsg += "\n\n⚠️ Invalid Access Token:\n"
			errorMsg += "Your Page Access Token may be expired or invalid.\n"
			errorMsg += "Please generate a new Page Access Token with 'instagram_manage_messages' permission.\n"
		} else if apiError.Error.Code == 10 {
			errorMsg += "\n\n⚠️ Permission Denied:\n"
			errorMsg += "Your app doesn't have the 'instagram_manage_messages' permission.\n"
			errorMsg += "Please request this permission in App Review and wait for approval.\n"
		} else if apiError.Error.Code == 200 {
			errorMsg += "\n\n⚠️ Invalid Recipient ID:\n"
			errorMsg += "The recipient ID (PSID) is invalid or the user has blocked your Instagram account.\n"
			errorMsg += "Ensure you're using the PSID from the webhook event (sender.id).\n"
		} else if apiError.Error.Code == 368 {
			errorMsg += "\n\n⚠️ Temporarily Blocked:\n"
			errorMsg += "You've been temporarily blocked from sending messages.\n"
			errorMsg += "This can happen if you send too many messages or violate Meta's messaging policies.\n"
			errorMsg += "Wait before trying again.\n"
		}

		h.Logger.Error("Instagram DM API error",
			zap.Int("status", resp.StatusCode),
			zap.Int("error_code", apiError.Error.Code),
			zap.Int("error_subcode", apiError.Error.SubCode),
			zap.String("error_type", apiError.Error.Type),
			zap.String("error_message", apiError.Error.Message),
			zap.String("fbtrace_id", apiError.Error.FbtraceID),
			zap.String("response", string(body)))

		return errors.New(errorMsg)
	}

	// Parse success response
	var response struct {
		RecipientID string `json:"recipient_id"`
		MessageID   string `json:"message_id"`
	}
	if err := json.Unmarshal(body, &response); err == nil {
		h.Logger.Info("✅ Instagram DM sent successfully",
			zap.String("recipient_id", response.RecipientID),
			zap.String("message_id", response.MessageID),
			zap.String("message_preview", truncateString(reply, 50)))
	} else {
	h.Logger.Info("✅ Instagram DM sent",
		zap.String("recipient_id", threadID),
		zap.String("message_preview", truncateString(reply, 50)))
	}

	return nil
}

// sendFacebookReply sends a reply via Facebook Messenger API
// According to Meta's official documentation: https://developers.facebook.com/docs/messenger-platform/reference/send-api
func (h *MessageHandler) sendFacebookReply(ctx context.Context, acc *storage.SocialAccount, threadID, reply string) error {
	// Decrypt token
	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		// Try using as-is if decryption fails
		decryptedToken = acc.AccessToken
	}

	if decryptedToken == "" {
		return fmt.Errorf("Facebook access token is empty")
	}

	// Get Page ID - prefer ExternalID (which should be the Page ID for Facebook)
	pageID := acc.ExternalID
	if pageID == "" {
		pageID = acc.PageID
	}

	// Facebook Messenger API endpoint
	// According to Meta docs: Use {page-id}/messages for better reliability
	// Fallback to /me/messages if Page ID is not available
	var apiURL string
	if pageID != "" {
		// Use explicit Page ID endpoint (recommended by Meta)
		apiURL = fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages", pageID)
	} else {
		// Fallback to /me/messages (requires Page Access Token)
		apiURL = "https://graph.facebook.com/v18.0/me/messages"
		h.Logger.Warn("⚠️ Using /me/messages endpoint - Page ID not found. Consider setting ExternalID or PageID for better reliability")
	}

	h.Logger.Info("📤 Attempting to send Facebook Messenger message",
		zap.String("page_id", pageID),
		zap.String("recipient_id", threadID),
		zap.String("endpoint", apiURL),
		zap.String("message_preview", truncateString(reply, 50)))

	// Facebook Messenger payload format according to Meta's Send API documentation
	// https://developers.facebook.com/docs/messenger-platform/reference/send-api#request
	payload := map[string]interface{}{
		"recipient": map[string]string{
			"id": threadID, // PSID (Page-Scoped ID) of the recipient
		},
		"message": map[string]interface{}{
			"text": reply,
		},
		"messaging_type": "RESPONSE", // Required: RESPONSE for replying within 24-hour window
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Add access token as query parameter (Meta API standard)
	apiURLWithToken := fmt.Sprintf("%s?access_token=%s", apiURL, url.QueryEscape(decryptedToken))

	req, err := http.NewRequestWithContext(ctx, "POST", apiURLWithToken, strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		// Parse error response to provide helpful guidance
		var apiError struct {
			Error struct {
				Message   string `json:"message"`
				Type      string `json:"type"`
				Code      int    `json:"code"`
				SubCode   int    `json:"error_subcode"`
				FbtraceID string `json:"fbtrace_id"`
			} `json:"error"`
		}
		json.Unmarshal(body, &apiError)

		errorMsg := fmt.Sprintf("Facebook Messenger API error %d: %s", resp.StatusCode, string(body))

		// Provide specific guidance for common errors
		if apiError.Error.Code == 190 {
			errorMsg += "\n\n⚠️ Invalid Access Token:\n"
			errorMsg += "Your Page Access Token may be expired or invalid.\n"
			errorMsg += "Please generate a new Page Access Token from Meta Developer Console.\n"
		} else if apiError.Error.Code == 10 {
			errorMsg += "\n\n⚠️ Permission Denied:\n"
			errorMsg += "Your app doesn't have the 'pages_messaging' permission.\n"
			errorMsg += "Please request this permission in App Review.\n"
		} else if apiError.Error.Code == 200 {
			errorMsg += "\n\n⚠️ Invalid Recipient ID:\n"
			errorMsg += "The recipient ID (PSID) is invalid or the user has blocked your page.\n"
		}

		h.Logger.Error("Facebook Messenger API error",
			zap.Int("status", resp.StatusCode),
			zap.Int("error_code", apiError.Error.Code),
			zap.Int("error_subcode", apiError.Error.SubCode),
			zap.String("error_type", apiError.Error.Type),
			zap.String("error_message", apiError.Error.Message),
			zap.String("fbtrace_id", apiError.Error.FbtraceID),
			zap.String("response", string(body)))

		return errors.New(errorMsg)
	}

	// Parse success response
	var response struct {
		RecipientID string `json:"recipient_id"`
		MessageID   string `json:"message_id"`
	}
	if err := json.Unmarshal(body, &response); err == nil {
		h.Logger.Info("✅ Facebook Messenger message sent successfully",
			zap.String("recipient_id", response.RecipientID),
			zap.String("message_id", response.MessageID),
			zap.String("message_preview", truncateString(reply, 50)))
	} else {
		h.Logger.Info("✅ Facebook Messenger message sent",
			zap.String("recipient_id", threadID),
			zap.String("message_preview", truncateString(reply, 50)))
	}

	return nil
}

// sendWhatsAppReply sends a reply via WhatsApp Business API
// For provider-owned model, uses Brancr's system user token with tenant's assigned phone_number_id
func (h *MessageHandler) sendWhatsAppReply(ctx context.Context, acc *storage.SocialAccount, customerID, reply string) error {
	// Get phone_number_id (this is the tenant's assigned number)
	phoneNumberID := acc.PageID
	if phoneNumberID == "" {
		phoneNumberID = acc.ExternalID
	}
	if phoneNumberID == "" {
		return fmt.Errorf("phone_number_id not found for WhatsApp account")
	}

	// For provider-owned model, use Brancr's system token (not tenant token)
	var accessToken string
	if acc.Mode == "provider_owned" {
		// Use system user token for provider-owned model
		if h.SystemToken == "" {
			return fmt.Errorf("system token not configured for provider-owned WhatsApp model")
		}
		accessToken = h.SystemToken
	} else {
		// Legacy mode: use tenant's token
		if acc.AccessToken != "" {
			decrypted, err := utils.DecryptToken(acc.AccessToken)
			if err != nil {
				decrypted = acc.AccessToken
			}
			accessToken = decrypted
		} else {
			return fmt.Errorf("WhatsApp access token not found for tenant")
		}
	}

	if accessToken == "" {
		return fmt.Errorf("WhatsApp access token not found")
	}

	// WhatsApp Business API endpoint
	apiURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages?access_token=%s", phoneNumberID, accessToken)

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                customerID, // WhatsApp phone number (e.g., "1234567890")
		"type":              "text",
		"text": map[string]string{
			"body": reply,
		},
	}

	jsonData, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	
	// Parse response to check for errors (Meta sometimes returns 200 with errors in body)
	var apiResponse struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
		Error *struct {
			Message   string `json:"message"`
			Type      string `json:"type"`
			Code      int    `json:"code"`
			ErrorData struct {
				MessagingProduct string `json:"messaging_product"`
				Details          string `json:"details"`
			} `json:"error_data"`
		} `json:"error"`
	}
	
	responseStr := string(body)
	if err := json.Unmarshal(body, &apiResponse); err == nil {
		if apiResponse.Error != nil {
			errorMsg := fmt.Sprintf("WhatsApp API error: %s (code: %d, type: %s)",
				apiResponse.Error.Message, apiResponse.Error.Code, apiResponse.Error.Type)
			
			// Specific error code handling
			if apiResponse.Error.Code == 131047 {
				errorMsg += "\n\n⚠️ 24-hour messaging window expired. Customer must initiate conversation first, or use an approved message template."
			} else if apiResponse.Error.Code == 131026 {
				errorMsg += "\n\n⚠️ Invalid recipient phone number. Ensure number is in E.164 format (+[country code][number])."
			} else if apiResponse.Error.Code == 131031 {
				errorMsg += "\n\n⚠️ Phone number not registered on WhatsApp or user has blocked your number."
			}
			
			h.Logger.Error("WhatsApp API error in response",
				zap.String("phone_number_id", phoneNumberID),
				zap.String("to", customerID),
				zap.Int("error_code", apiResponse.Error.Code),
				zap.String("error_type", apiResponse.Error.Type),
				zap.String("error_message", apiResponse.Error.Message),
				zap.String("response", responseStr))
			return errors.New(errorMsg)
		}
	}
	
	if resp.StatusCode >= 400 {
		h.Logger.Error("WhatsApp API error",
			zap.Int("status", resp.StatusCode),
			zap.String("response", responseStr),
			zap.String("phone_number_id", phoneNumberID))
		return fmt.Errorf("WhatsApp API error %d: %s", resp.StatusCode, responseStr)
	}

	// Log message ID for tracking delivery status
	messageID := ""
	if len(apiResponse.Messages) > 0 {
		messageID = apiResponse.Messages[0].ID
	}

	h.Logger.Info("✅ WhatsApp message sent",
		zap.String("phone_number_id", phoneNumberID),
		zap.String("to", customerID),
		zap.String("message_id", messageID),
		zap.String("message_preview", truncateString(reply, 50)),
		zap.String("note", "Check webhook for delivery status (sent/delivered/read)"))

	return nil
}

// saveToMemory saves conversation to memory for learning
func (h *MessageHandler) saveToMemory(ctx context.Context, tenantID, customerID uint, message, reply string) {
	// Save customer conversation
	h.Responder.Memory.SaveCustomerConversation(ctx, tenantID, customerID, message, reply)
}

// metadataToJSON converts metadata map to JSON string
func (h *MessageHandler) metadataToJSON(meta map[string]interface{}) string {
	if meta == nil {
		return "{}"
	}
	data, _ := json.Marshal(meta)
	return string(data)
}

// fetchAndSaveInstagramID fetches the Instagram Business Account ID and saves it to the database
func (h *MessageHandler) fetchAndSaveInstagramID(ctx context.Context, acc *storage.SocialAccount) (string, error) {
	// Decrypt token
	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		decryptedToken = acc.AccessToken
	}

	client := &http.Client{Timeout: 10 * time.Second}

	// Step 1: Get Facebook pages
	pagesURL := fmt.Sprintf("https://graph.facebook.com/v18.0/me/accounts?access_token=%s", decryptedToken)
	resp, err := client.Get(pagesURL)
	if err != nil {
		return "", fmt.Errorf("failed to query pages: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to fetch pages (status %d): %s", resp.StatusCode, string(body))
	}

	var pages struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pages); err != nil {
		return "", fmt.Errorf("failed to parse pages: %w", err)
	}

	if len(pages.Data) == 0 {
		return "", fmt.Errorf("no pages found")
	}

	pageID := pages.Data[0].ID

	// Step 2: Get Instagram Business Account linked to the page
	igURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s?fields=instagram_business_account{id}&access_token=%s", pageID, decryptedToken)
	igResp, err := client.Get(igURL)
	if err != nil {
		return "", fmt.Errorf("failed to get Instagram account: %w", err)
	}
	defer igResp.Body.Close()

	if igResp.StatusCode != 200 {
		body, _ := io.ReadAll(igResp.Body)
		return "", fmt.Errorf("failed to fetch Instagram account (status %d): %s", igResp.StatusCode, string(body))
	}

	var pageData struct {
		InstagramBusinessAccount struct {
			ID string `json:"id"`
		} `json:"instagram_business_account"`
	}
	if err := json.NewDecoder(igResp.Body).Decode(&pageData); err != nil {
		return "", fmt.Errorf("failed to parse Instagram account: %w", err)
	}

	if pageData.InstagramBusinessAccount.ID == "" {
		return "", fmt.Errorf("no Instagram Business Account linked to this page")
	}

	igID := pageData.InstagramBusinessAccount.ID

	// Save to database
	acc.ExternalID = igID
	acc.PageID = igID
	if err := h.DB.Save(acc).Error; err != nil {
		h.Logger.Error("⚠️ Failed to save Instagram Business Account ID to database",
			zap.Error(err),
			zap.String("instagram_id", igID))
		// Return the ID anyway - we can still use it for this request
	}

	return igID, nil
}

// verifyTokenType checks if the token is a Page Access Token or User Access Token
// Returns: tokenType ("PAGE" or "USER"), pageID, error
func (h *MessageHandler) verifyTokenType(ctx context.Context, token string) (string, string, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	// Try to get /me/accounts - this works for both User and Page tokens
	// But for Page tokens, it returns the page info
	pagesURL := fmt.Sprintf("https://graph.facebook.com/v18.0/me/accounts?access_token=%s", token)
	resp, err := client.Get(pagesURL)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		// If /me/accounts fails, try /me to see if it's a Page token
		meURL := fmt.Sprintf("https://graph.facebook.com/v18.0/me?access_token=%s", token)
		meResp, err := client.Get(meURL)
		if err != nil {
			return "", "", fmt.Errorf("failed to verify token: %w", err)
		}
		defer meResp.Body.Close()

		if meResp.StatusCode == 200 {
			// This might be a Page token - try to get page info directly
			var pageInfo struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			meBody, _ := io.ReadAll(meResp.Body)
			if err := json.Unmarshal(meBody, &pageInfo); err == nil && pageInfo.ID != "" {
				// Check if this is actually a page by trying to get page details
				pageDetailURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s?fields=id,name,category&access_token=%s", pageInfo.ID, token)
				pageResp, err := client.Get(pageDetailURL)
				if err == nil {
					defer pageResp.Body.Close()
					if pageResp.StatusCode == 200 {
						return "PAGE", pageInfo.ID, nil
					}
				}
			}
		}

		return "", "", fmt.Errorf("token verification failed (status %d): %s", resp.StatusCode, string(body))
	}

	var pages struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(strings.NewReader(string(body))).Decode(&pages); err != nil {
		return "", "", err
	}

	if len(pages.Data) == 0 {
		// No pages found - this is likely a User token without page access
		return "USER", "", nil
	}

	// If we get pages, it's a User token that has page access
	// But for Instagram messaging, we need the actual Page Access Token
	// So we return USER here to indicate they need to generate a Page token
	return "USER", pages.Data[0].ID, nil
}
