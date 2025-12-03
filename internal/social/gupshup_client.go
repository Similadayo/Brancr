package social

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/config"
	"go.uber.org/zap"
)

// GupshupClient handles Gupshup Partner API calls
type GupshupClient struct {
	Config       *config.Config
	Logger       *zap.Logger
	PartnerToken string // Cached partner token
	TokenExpires time.Time
}

// NewGupshupClient creates a new Gupshup API client
func NewGupshupClient(cfg *config.Config, logger *zap.Logger) *GupshupClient {
	return &GupshupClient{
		Config: cfg,
		Logger: logger,
	}
}

// GetPartnerToken retrieves or refreshes the partner token
// Uses Partner Login API: POST /partner/account/login
// For accounts created after Nov 20, 2024: use Client Secret (from Partner Portal Settings) as password
// For older accounts (pre-Nov 20, 2024): use actual account password
// Both work - client secret is recommended for security
func (c *GupshupClient) GetPartnerToken() (string, error) {
	// Return cached token if still valid (with 5 minute buffer)
	if c.PartnerToken != "" && time.Now().Before(c.TokenExpires.Add(-5*time.Minute)) {
		return c.PartnerToken, nil
	}

	// Use /partner/account/login endpoint as per Gupshup Partner API documentation
	// IMPORTANT: Gupshup requires "email" (NOT "username") and "password"
	apiURL := fmt.Sprintf("%s/partner/account/login", c.Config.GupshupAPIBaseURL)

	// Use form-urlencoded format (official recommended format)
	data := url.Values{}
	data.Set("email", c.Config.GupshupPartnerUsername) // Must be "email", not "username"
	data.Set("password", c.Config.GupshupPartnerPassword)

	req, err := http.NewRequest("POST", apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)

	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		// Log detailed error for debugging
		if c.Logger != nil {
			c.Logger.Error("Gupshup Partner Token request failed",
				zap.Int("status_code", resp.StatusCode),
				zap.String("url", apiURL),
				zap.String("response", string(body)),
				zap.String("email", c.Config.GupshupPartnerUsername))
		}
		return "", fmt.Errorf("partner token request failed (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse response - Gupshup returns status and token
	var result struct {
		Status string `json:"status"`
		Token  string `json:"token"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w, body: %s", err, string(body))
	}

	if result.Token == "" {
		return "", fmt.Errorf("no token in response: %s", string(body))
	}

	// Cache token
	c.PartnerToken = result.Token
	c.TokenExpires = time.Now().Add(55 * time.Minute) // Default 1 hour with buffer

	if c.Logger != nil {
		c.Logger.Info("✅ Gupshup partner token obtained",
			zap.Time("expires_at", c.TokenExpires),
			zap.String("email", c.Config.GupshupPartnerUsername))
	}

	return c.PartnerToken, nil
}

// GetAppAccessToken gets the app-specific access token for sending messages and other app operations
// CRITICAL: Uses /partner/app/{appId}/token endpoint (as per Gupshup Partner API docs)
// Endpoint: GET /partner/app/{appId}/token
// Header: Authorization: {PARTNER_TOKEN} (Partner Token, NOT App Token)
// Response: {"status": "success", "token": {"token": "...", "authoriserId": "...", ...}}
// The token is nested inside a "token" object, not at root level
func (c *GupshupClient) GetAppAccessToken(appID string) (string, error) {
	partnerToken, err := c.GetPartnerToken()
	if err != nil {
		return "", err
	}

	// Use correct endpoint: /partner/app/{appId}/token (NOT /partner/account/api/partnerApps/{appId}/token)
	apiURL := fmt.Sprintf("%s/partner/app/%s/token", c.Config.GupshupAPIBaseURL, appID)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Use Partner Token in Authorization header (NOT App Token)
	req.Header.Set("Authorization", partnerToken)
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("app token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		if c.Logger != nil {
			c.Logger.Error("Gupshup Get App Token failed",
				zap.Int("status_code", resp.StatusCode),
				zap.String("url", apiURL),
				zap.String("app_id", appID),
				zap.String("response", string(body)))
		}
		return "", fmt.Errorf("app token request failed (status %d): %s", resp.StatusCode, string(body))
	}

	// Response format: {"status": "success", "token": {"token": "...", "authoriserId": "...", ...}}
	var result struct {
		Status string `json:"status"`
		Token  struct {
			Token        string `json:"token"`
			AuthoriserID string `json:"authoriserId"`
			RequestorID  string `json:"requestorId"`
			CreatedOn    int64  `json:"createdOn"`
			ModifiedOn   int64  `json:"modifiedOn"`
			ExpiresOn    int64  `json:"expiresOn"`
			Active       bool   `json:"active"`
		} `json:"token"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse app token response: %w, body=%s", err, string(body))
	}

	if result.Status != "success" || result.Token.Token == "" {
		return "", fmt.Errorf("invalid app token response: %s", string(body))
	}

	if c.Logger != nil {
		c.Logger.Info("✅ Gupshup App Token obtained",
			zap.String("app_id", appID),
			zap.Bool("active", result.Token.Active),
			zap.Int64("expires_on", result.Token.ExpiresOn))
	}

	return result.Token.Token, nil
}

// sanitizeAppName removes special characters from app name (only letters and numbers allowed)
func sanitizeAppName(name string) string {
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			result.WriteRune(r)
		}
	}
	sanitized := result.String()
	if sanitized == "" {
		sanitized = "App" // Fallback if name becomes empty
	}
	return sanitized
}

// CreateApp creates a new WhatsApp Partner App for a tenant
// CRITICAL: Uses /partner/app endpoint (as per Gupshup Partner API docs)
// Endpoint: POST /partner/app
// Content-Type: application/x-www-form-urlencoded
// Header: token (NOT Authorization Bearer)
// Required: name (6-150 chars, alphanumeric only, no special characters)
// Optional: templateMessaging, disableOptinPrefUrl
func (c *GupshupClient) CreateApp(appName string) (string, error) {
	// Sanitize app name to remove special characters (Gupshup requirement)
	appName = sanitizeAppName(appName)

	// Validate app name length (6-150 chars as per Gupshup docs)
	if len(appName) < 6 {
		// Pad with numbers if too short
		appName = appName + "12345"
		appName = appName[:6] // Take first 6 chars
	}
	if len(appName) > 150 {
		appName = appName[:150] // Truncate if too long
	}

	partnerToken, err := c.GetPartnerToken()
	if err != nil {
		return "", err
	}

	// Use /partner/app endpoint (as per Gupshup Partner API docs)
	apiURL := fmt.Sprintf("%s/partner/app", c.Config.GupshupAPIBaseURL)

	// Use form-urlencoded format (as per Gupshup docs)
	data := url.Values{}
	data.Set("name", appName)
	// Optional: Enable template messaging by default
	data.Set("templateMessaging", "true")
	// Optional: Disable opt-in preference URL (let Gupshup handle it)
	data.Set("disableOptinPrefUrl", "false")

	// Log the exact request for debugging
	if c.Logger != nil {
		c.Logger.Info("Creating Gupshup WhatsApp Partner App",
			zap.String("url", apiURL),
			zap.String("name", appName),
			zap.String("form_data", data.Encode()))
	}

	req, err := http.NewRequest("POST", apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Use "token" header (NOT "Authorization: Bearer") as per Gupshup docs
	req.Header.Set("token", partnerToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("create app request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		if c.Logger != nil {
			c.Logger.Error("Gupshup Create App failed",
				zap.Int("status_code", resp.StatusCode),
				zap.String("url", apiURL),
				zap.String("name", appName),
				zap.String("form_data", data.Encode()),
				zap.String("response", string(body)))
		}
		return "", fmt.Errorf("create app failed (status %d): %s", resp.StatusCode, string(body))
	}

	// Response format: {"appId": "<app_id>"} (no "status" field)
	var result struct {
		AppID string `json:"appId"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse create app response: %w, resp=%s", err, string(body))
	}

	if result.AppID == "" {
		return "", fmt.Errorf("no appId in response: %s", string(body))
	}

	if c.Logger != nil {
		c.Logger.Info("✅ Gupshup WhatsApp Partner App created",
			zap.String("app_id", result.AppID),
			zap.String("app_name", appName))
	}

	return result.AppID, nil
}

// GenerateEmbedSignedLink generates an embedded signup link for WhatsApp onboarding
// CRITICAL: Uses /partner/app/{appId}/onboarding/embed/link endpoint (as per Gupshup Partner API docs)
// Endpoint: GET /partner/app/{appId}/onboarding/embed/link
// Header: token (NOT Authorization Bearer)
// Query params: regenerate (optional), user (optional), lang (optional)
// Response: {"status": "success", "link": "<embed_link>"}
// Note: The signed link is valid for 5 days only
func (c *GupshupClient) GenerateEmbedSignedLink(appID, redirectURL string) (string, error) {
	partnerToken, err := c.GetPartnerToken()
	if err != nil {
		return "", err
	}

	// Use correct endpoint as per Gupshup Partner API docs
	apiURL := fmt.Sprintf("%s/partner/app/%s/onboarding/embed/link", c.Config.GupshupAPIBaseURL, appID)

	// Build query parameters
	params := url.Values{}
	// Optional: regenerate link (default false)
	params.Set("regenerate", "false")
	// Optional: user name (use partner username if available)
	if c.Config.GupshupPartnerUsername != "" {
		params.Set("user", c.Config.GupshupPartnerUsername)
	}
	// Optional: language (default to English)
	params.Set("lang", "en")

	// Note: redirectUrl might be configured in Gupshup Partner Portal settings
	// or passed as a separate parameter. For now, we'll use query params if provided.
	if redirectURL != "" {
		params.Set("redirectUrl", strings.TrimSpace(redirectURL))
	}

	if len(params) > 0 {
		apiURL += "?" + params.Encode()
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Use "token" header (NOT "Authorization: Bearer") as per Gupshup docs
	req.Header.Set("token", partnerToken)
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("embed link request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		if c.Logger != nil {
			c.Logger.Error("Gupshup Generate Embed Link failed",
				zap.Int("status_code", resp.StatusCode),
				zap.String("url", apiURL),
				zap.String("app_id", appID),
				zap.String("response", string(body)))
		}
		return "", fmt.Errorf("embed link request failed (status %d): %s", resp.StatusCode, string(body))
	}

	// Response format: {"status": "success", "link": "<embed_link>"}
	var result struct {
		Status string `json:"status"`
		Link   string `json:"link"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse embed link response: %w, body=%s", err, string(body))
	}

	if result.Status != "success" || result.Link == "" {
		return "", fmt.Errorf("invalid embed link response: %s", string(body))
	}

	if c.Logger != nil {
		c.Logger.Info("✅ Gupshup embed link generated",
			zap.String("app_id", appID),
			zap.String("link", result.Link))
	}

	return result.Link, nil
}

// SetSubscription creates or updates webhook subscription for an app
// CRITICAL: Uses /partner/app/{appId}/subscription endpoint (as per Gupshup Partner API docs)
// Endpoint: POST /partner/app/{appId}/subscription
// Header: Authorization: {PARTNER_APP_TOKEN} (NOT Partner Token, but App Token)
// Content-Type: application/x-www-form-urlencoded
// Required fields: modes, tag, url, version
// Response: {"status": "success", "subscription": {"id": "...", ...}}
func (c *GupshupClient) SetSubscription(appID, webhookURL string, events []string) (string, error) {
	// Get App Token (NOT Partner Token) - required for subscription API
	appToken, err := c.GetAppAccessToken(appID)
	if err != nil {
		return "", fmt.Errorf("failed to get app token: %w", err)
	}

	apiURL := fmt.Sprintf("%s/partner/app/%s/subscription", c.Config.GupshupAPIBaseURL, appID)

	// Convert events array to modes string
	// For version 3, use modes: MESSAGE (incoming messages), OTHERS (events like read, delivered, sent)
	// or ALL (all messages and events)
	modes := "MESSAGE,OTHERS" // Default: messages + events
	if len(events) > 0 {
		// Map common event names to modes
		hasMessage := false
		hasEvents := false
		for _, event := range events {
			switch event {
			case "message", "messages":
				hasMessage = true
			case "message-event", "status", "read", "delivered", "sent":
				hasEvents = true
			}
		}
		if hasMessage && hasEvents {
			modes = "MESSAGE,OTHERS"
		} else if hasMessage {
			modes = "MESSAGE"
		} else if hasEvents {
			modes = "OTHERS"
		} else {
			// Default to ALL for v3 if unsure
			modes = "ALL"
		}
	}

	// Build form-urlencoded payload
	data := url.Values{}
	data.Set("modes", modes)
	data.Set("tag", fmt.Sprintf("BrancrSubscription-%s", appID[:8])) // Unique tag per app
	data.Set("url", webhookURL)
	data.Set("version", "3") // Use version 3 (supports MESSAGE, OTHERS, ALL, etc.)
	data.Set("showOnUI", "false")

	req, err := http.NewRequest("POST", apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Use App Token in Authorization header (NOT Partner Token)
	req.Header.Set("Authorization", appToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("subscription request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		if c.Logger != nil {
			c.Logger.Error("Gupshup Set Subscription failed",
				zap.Int("status_code", resp.StatusCode),
				zap.String("url", apiURL),
				zap.String("app_id", appID),
				zap.String("webhook_url", webhookURL),
				zap.String("modes", modes),
				zap.String("response", string(body)))
		}
		return "", fmt.Errorf("subscription request failed (status %d): %s", resp.StatusCode, string(body))
	}

	// Response format: {"status": "success", "subscription": {"id": "...", "active": true, ...}}
	var result struct {
		Status       string `json:"status"`
		Subscription struct {
			ID      string `json:"id"`
			Active  bool   `json:"active"`
			AppID   string `json:"appId"`
			Tag     string `json:"tag"`
			URL     string `json:"url"`
			Mode    int    `json:"mode"`
			Version int    `json:"version"`
		} `json:"subscription"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		// Try alternative response format (fallback)
		var altResult map[string]interface{}
		if err2 := json.Unmarshal(body, &altResult); err2 == nil {
			if sub, ok := altResult["subscription"].(map[string]interface{}); ok {
				if id, ok := sub["id"].(string); ok {
					return id, nil
				}
			}
		}
		return "", fmt.Errorf("failed to parse subscription response: %w, body=%s", err, string(body))
	}

	if result.Status != "success" || result.Subscription.ID == "" {
		return "", fmt.Errorf("invalid subscription response: %s", string(body))
	}

	if c.Logger != nil {
		c.Logger.Info("✅ Gupshup webhook subscription created",
			zap.String("app_id", appID),
			zap.String("subscription_id", result.Subscription.ID),
			zap.String("webhook_url", webhookURL),
			zap.String("modes", modes),
			zap.Bool("active", result.Subscription.Active))
	}

	return result.Subscription.ID, nil
}

// GetSubscription retrieves existing subscription
func (c *GupshupClient) GetSubscription(appID string) (string, error) {
	partnerToken, err := c.GetPartnerToken()
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/partner/account/api/partnerApps/%s/subscription", c.Config.GupshupAPIBaseURL, appID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+partnerToken)
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("get subscription request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("get subscription failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status       string `json:"status"`
		Subscription struct {
			SubscriptionID string `json:"subscriptionId"`
		} `json:"subscription"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse subscription response: %w", err)
	}

	return result.Subscription.SubscriptionID, nil
}

// GetAppDetails retrieves app details including WhatsApp metadata (WABA ID, phone number, etc.)
// CRITICAL: Uses /partner/app/{appId} endpoint (as per Gupshup Partner API docs)
// Endpoint: GET /partner/app/{appId}
// Header: Authorization: {PARTNER_TOKEN} (Partner Token, NOT App Token)
// Response contains WhatsApp object with: businessId, formattedPhoneNumber, normalizedPhoneNumber, wabaApprovalTimestamp, etc.
func (c *GupshupClient) GetAppDetails(appID string) (map[string]interface{}, error) {
	partnerToken, err := c.GetPartnerToken()
	if err != nil {
		return nil, err
	}

	// Use correct endpoint: /partner/app/{appId} (NOT /partner/account/api/partnerApps/{appId})
	apiURL := fmt.Sprintf("%s/partner/app/%s", c.Config.GupshupAPIBaseURL, appID)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Use Partner Token in Authorization header (without "Bearer " prefix)
	req.Header.Set("Authorization", partnerToken)
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get app details request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		if c.Logger != nil {
			c.Logger.Error("Gupshup Get App Details failed",
				zap.Int("status_code", resp.StatusCode),
				zap.String("url", apiURL),
				zap.String("app_id", appID),
				zap.String("response", string(body)))
		}
		return nil, fmt.Errorf("get app details failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse app details response: %w, body=%s", err, string(body))
	}

	if c.Logger != nil {
		// Log WhatsApp metadata if present
		if whatsapp, ok := result["whatsapp"].(map[string]interface{}); ok {
			c.Logger.Info("✅ Gupshup App Details retrieved",
				zap.String("app_id", appID),
				zap.Any("waba_id", whatsapp["businessId"]),
				zap.Any("phone_number", whatsapp["formattedPhoneNumber"]),
				zap.Any("normalized_phone", whatsapp["normalizedPhoneNumber"]))
		} else {
			c.Logger.Info("✅ Gupshup App Details retrieved (no WhatsApp metadata yet)",
				zap.String("app_id", appID))
		}
	}

	return result, nil
}

// SendMessage sends a WhatsApp message via Gupshup
func (c *GupshupClient) SendMessage(appID, to, message string) (string, error) {
	appToken, err := c.GetAppAccessToken(appID)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/partner/account/api/partnerApps/%s/v3/messages", c.Config.GupshupAPIBaseURL, appID)
	payload := map[string]interface{}{
		"to":   to,
		"type": "text",
		"text": map[string]string{
			"body": message,
		},
	}

	jsonBody, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+appToken)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("send message request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("send message failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse send message response: %w", err)
	}

	// Extract message ID (field name may vary)
	if msgID, ok := result["gsId"].(string); ok && msgID != "" {
		return msgID, nil
	}
	if msgID, ok := result["id"].(string); ok && msgID != "" {
		return msgID, nil
	}
	if msgID, ok := result["messageId"].(string); ok && msgID != "" {
		return msgID, nil
	}

	return "", fmt.Errorf("message ID not found in response: %s", string(body))
}
