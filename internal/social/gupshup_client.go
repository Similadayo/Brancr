package social

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
// Uses Partner Login API: POST /partner/account/api/token
// For accounts created after Nov 20, 2024: use Client Secret (from Partner Portal Settings) as password
// For older accounts (pre-Nov 20, 2024): use actual account password
// Both work - client secret is recommended for security
func (c *GupshupClient) GetPartnerToken() (string, error) {
	// Return cached token if still valid (with 5 minute buffer)
	if c.PartnerToken != "" && time.Now().Before(c.TokenExpires.Add(-5*time.Minute)) {
		return c.PartnerToken, nil
	}

	url := fmt.Sprintf("%s/partner/account/api/token", c.Config.GupshupAPIBaseURL)
	// Note: "password" parameter accepts either:
	// - Actual account password (for accounts created before Nov 20, 2024)
	// - Client Secret (generated in Partner Portal Settings, recommended for newer accounts)
	payload := map[string]string{
		"username": c.Config.GupshupPartnerUsername,
		"password": c.Config.GupshupPartnerPassword, // Can be password or client secret
	}

	jsonBody, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("token request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status    string `json:"status"`
		Token     string `json:"token"`
		ExpiresIn int    `json:"expiresIn"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}

	if result.Status != "success" || result.Token == "" {
		return "", fmt.Errorf("invalid token response: %s", string(body))
	}

	// Cache token
	c.PartnerToken = result.Token
	if result.ExpiresIn > 0 {
		c.TokenExpires = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
	} else {
		c.TokenExpires = time.Now().Add(55 * time.Minute) // Default 1 hour with buffer
	}

	if c.Logger != nil {
		c.Logger.Info("✅ Gupshup partner token obtained",
			zap.Time("expires_at", c.TokenExpires))
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

// CreateApp creates a new Gupshup app for a tenant
func (c *GupshupClient) CreateApp(appName string) (string, error) {
	partnerToken, err := c.GetPartnerToken()
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/partner/account/api/app", c.Config.GupshupAPIBaseURL)
	payload := map[string]string{
		"appName": appName,
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
		return "", fmt.Errorf("create app request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("create app failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status string `json:"status"`
		AppID  string `json:"appId"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		// Try alternative response format
		var altResult map[string]interface{}
		if err2 := json.Unmarshal(body, &altResult); err2 == nil {
			if id, ok := altResult["id"].(string); ok {
				return id, nil
			}
		}
		return "", fmt.Errorf("failed to parse create app response: %w", err)
	}

	if result.Status != "success" || result.AppID == "" {
		return "", fmt.Errorf("invalid create app response: %s", string(body))
	}

	return result.AppID, nil
}

// GenerateEmbedSignedLink generates an embedded signup link with redirect URL
func (c *GupshupClient) GenerateEmbedSignedLink(appID, redirectURL string) (string, error) {
	partnerToken, err := c.GetPartnerToken()
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/partner/account/api/app/%s/embedLink", c.Config.GupshupAPIBaseURL, appID)
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

	url := fmt.Sprintf("%s/partner/account/api/app/%s", c.Config.GupshupAPIBaseURL, appID)
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
