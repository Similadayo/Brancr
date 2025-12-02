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

// GetAppAccessToken gets the app-specific access token for sending messages
func (c *GupshupClient) GetAppAccessToken(appID string) (string, error) {
	partnerToken, err := c.GetPartnerToken()
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/partner/account/api/partnerApps/%s/token", c.Config.GupshupAPIBaseURL, appID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+partnerToken)
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("app token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("app token request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status    string `json:"status"`
		Token     string `json:"token"`
		ExpiresIn int    `json:"expiresIn"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse app token response: %w", err)
	}

	if result.Status != "success" || result.Token == "" {
		return "", fmt.Errorf("invalid app token response: %s", string(body))
	}

	return result.Token, nil
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
// CRITICAL: Must use /partner/account/api/partnerApps (NOT /app)
// The /app endpoint creates generic Gupshup apps, NOT WhatsApp apps
// WhatsApp apps require channel: "whatsapp" in the payload
// App name must be alphanumeric only (no special characters)
func (c *GupshupClient) CreateApp(appName string) (string, error) {
	// Sanitize app name to remove special characters (Gupshup requirement)
	appName = sanitizeAppName(appName)
	partnerToken, err := c.GetPartnerToken()
	if err != nil {
		return "", err
	}

	// Use /partnerApps endpoint for WhatsApp apps (NOT /app)
	apiURL := fmt.Sprintf("%s/partner/account/api/partnerApps", c.Config.GupshupAPIBaseURL)

	// Required payload for WhatsApp Partner App
	// Core required fields only - avoid optional fields that might cause validation errors
	payload := map[string]interface{}{
		"appName":      appName,
		"channel":      "whatsapp", // REQUIRED
		"countryCode":  "NG",       // REQUIRED
		"vertical":     "others",   // REQUIRED
		"category":     "business", // REQUIRED
		"businessName": appName,    // RECOMMENDED
	}

	jsonBody, _ := json.Marshal(payload)

	// Log the exact request for debugging
	if c.Logger != nil {
		c.Logger.Info("Creating Gupshup WhatsApp Partner App",
			zap.String("url", apiURL),
			zap.String("payload", string(jsonBody)),
			zap.String("app_name", appName))
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+partnerToken)
	req.Header.Set("Content-Type", "application/json")
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
				zap.String("request_payload", string(jsonBody)),
				zap.String("response", string(body)),
				zap.String("app_name", appName))
		}
		return "", fmt.Errorf("create app failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status string `json:"status"`
		AppID  string `json:"appId"`
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

// GenerateEmbedSignedLink generates an embedded signup link with redirect URL
// CRITICAL: Must use /partnerApps endpoint (NOT /app) for WhatsApp Partner Apps
func (c *GupshupClient) GenerateEmbedSignedLink(appID, redirectURL string) (string, error) {
	partnerToken, err := c.GetPartnerToken()
	if err != nil {
		return "", err
	}

	// Use /partnerApps endpoint for WhatsApp apps (NOT /app)
	url := fmt.Sprintf("%s/partner/account/api/partnerApps/%s/embedLink", c.Config.GupshupAPIBaseURL, appID)
	if redirectURL != "" {
		url += "?redirectUrl=" + strings.TrimSpace(redirectURL)
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+partnerToken)
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("embed link request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("embed link request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status    string `json:"status"`
		EmbedLink string `json:"embedLink"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse embed link response: %w", err)
	}

	if result.Status != "success" || result.EmbedLink == "" {
		return "", fmt.Errorf("invalid embed link response: %s", string(body))
	}

	return result.EmbedLink, nil
}

// SetSubscription creates or updates webhook subscription for an app
func (c *GupshupClient) SetSubscription(appID, webhookURL string, events []string) (string, error) {
	partnerToken, err := c.GetPartnerToken()
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/partner/account/api/partnerApps/%s/subscription", c.Config.GupshupAPIBaseURL, appID)
	payload := map[string]interface{}{
		"webhookUrl": webhookURL,
		"events":     events,
	}

	jsonBody, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+partnerToken)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("subscription request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("subscription request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status         string `json:"status"`
		SubscriptionID string `json:"subscriptionId"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		// Try alternative response format
		var altResult map[string]interface{}
		if err2 := json.Unmarshal(body, &altResult); err2 == nil {
			if id, ok := altResult["id"].(string); ok {
				return id, nil
			}
		}
		return "", fmt.Errorf("failed to parse subscription response: %w", err)
	}

	if result.Status != "success" {
		// Subscription might already exist, try to get it
		return c.GetSubscription(appID)
	}

	return result.SubscriptionID, nil
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

// GetAppDetails retrieves app details including status
func (c *GupshupClient) GetAppDetails(appID string) (map[string]interface{}, error) {
	partnerToken, err := c.GetPartnerToken()
	if err != nil {
		return nil, err
	}

	// Use /partnerApps endpoint for WhatsApp apps (NOT /app)
	url := fmt.Sprintf("%s/partner/account/api/partnerApps/%s", c.Config.GupshupAPIBaseURL, appID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+partnerToken)
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get app details request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("get app details failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse app details response: %w", err)
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
