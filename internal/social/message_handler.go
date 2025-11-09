package social

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

// sendInstagramReply sends a reply via Instagram Graph API
func (h *MessageHandler) sendInstagramReply(ctx context.Context, acc *storage.SocialAccount, threadID, reply string) error {
	// Decrypt token
	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		// Try using as-is if decryption fails
		decryptedToken = acc.AccessToken
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

	// First, verify token type - Instagram messaging REQUIRES a Page Access Token
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

	// Instagram DM API endpoint - use Instagram Business Account ID
	// Note: This requires a Page Access Token with instagram_manage_messages permission
	apiURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages", igAccountID)

	h.Logger.Info("📤 Attempting to send Instagram DM",
		zap.String("instagram_account_id", igAccountID),
		zap.String("recipient_id", threadID),
		zap.String("page_id", pageID),
		zap.String("message_preview", truncateString(reply, 50)))

	// Instagram messaging payload format
	// Note: recipient.id should be the Instagram user's PSID (Page-Scoped ID)
	payload := map[string]interface{}{
		"recipient": map[string]string{
			"id": threadID, // This should be the customer's Instagram PSID
		},
		"message": map[string]interface{}{
			"text": reply,
		},
	}

	// Add access token as query parameter (Instagram API prefers this)
	apiURLWithToken := fmt.Sprintf("%s?access_token=%s", apiURL, decryptedToken)

	jsonData, _ := json.Marshal(payload)
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
				Message string `json:"message"`
				Type    string `json:"type"`
				Code    int    `json:"code"`
			} `json:"error"`
		}
		json.Unmarshal(body, &apiError)

		errorMsg := fmt.Sprintf("Instagram API error %d: %s", resp.StatusCode, string(body))

		// Provide specific guidance for common errors
		if apiError.Error.Code == 3 {
			errorMsg += "\n\n⚠️ Meta App Capability Issue:\n"
			errorMsg += "Your Meta app doesn't have the 'Instagram Messaging' capability enabled.\n"
			errorMsg += "This is different from permissions - capabilities are app-level features.\n\n"
			errorMsg += "🔧 Fix this in Meta Developer Console:\n"
			errorMsg += "1. Go to your Meta App → Products\n"
			errorMsg += "2. Click 'Add Product' → Find 'Instagram Messaging' → Click 'Set Up'\n"
			errorMsg += "3. Complete the setup flow (webhook URL, verify token, etc.)\n"
			errorMsg += "4. Go to App Review → Permissions and Features\n"
			errorMsg += "5. Submit 'instagram_manage_messages' for App Review (REQUIRED)\n"
			errorMsg += "6. Wait for approval (can take 1-7 days)\n"
			errorMsg += "7. Ensure your app is in 'Live' mode (not Development) after approval\n\n"
			errorMsg += "📝 Note: Instagram Messaging is a restricted feature that requires:\n"
			errorMsg += "   - App Review approval\n"
			errorMsg += "   - Live mode (not Development mode)\n"
			errorMsg += "   - Valid business verification (for some use cases)"
		}

		h.Logger.Error("Instagram DM API error",
			zap.Int("status", resp.StatusCode),
			zap.String("error_code", fmt.Sprintf("%d", apiError.Error.Code)),
			zap.String("error_type", apiError.Error.Type),
			zap.String("error_message", apiError.Error.Message),
			zap.String("response", string(body)))

		return errors.New(errorMsg)
	}

	h.Logger.Info("✅ Instagram DM sent",
		zap.String("recipient_id", threadID),
		zap.String("message_preview", truncateString(reply, 50)))

	return nil
}

// sendFacebookReply sends a reply via Facebook Messenger API
func (h *MessageHandler) sendFacebookReply(ctx context.Context, acc *storage.SocialAccount, threadID, reply string) error {
	// Decrypt token
	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		// Try using as-is if decryption fails
		decryptedToken = acc.AccessToken
	}

	// Facebook Messenger API endpoint
	apiURL := fmt.Sprintf("https://graph.facebook.com/v18.0/me/messages?access_token=%s", decryptedToken)

	payload := map[string]interface{}{
		"recipient": map[string]string{
			"id": threadID,
		},
		"message": map[string]string{
			"text": reply,
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
	if resp.StatusCode >= 400 {
		h.Logger.Error("Facebook Messenger API error",
			zap.Int("status", resp.StatusCode),
			zap.String("response", string(body)))
		return fmt.Errorf("Facebook API error %d: %s", resp.StatusCode, string(body))
	}

	h.Logger.Info("✅ Facebook Messenger reply sent",
		zap.String("recipient_id", threadID),
		zap.String("message_preview", truncateString(reply, 50)))

	return nil
}

// sendWhatsAppReply sends a reply via WhatsApp Business API
func (h *MessageHandler) sendWhatsAppReply(ctx context.Context, acc *storage.SocialAccount, customerID, reply string) error {
	// TODO: Implement WhatsApp reply
	return fmt.Errorf("WhatsApp reply not yet implemented")
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
