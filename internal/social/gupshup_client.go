package social

import (
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
	// CRITICAL: For Go-Live events (onboarding-event), we need ACCOUNT mode
	// Docs say "for version 3: TEMPLATE, ACCOUNT, COPY are not supported" BUT
	// ACCOUNT mode is still listed as valid and needed for account events
	// Try using ACCOUNT mode explicitly - if it fails, fall back to OTHERS
	needsAccountEvents := false
	hasMessage := false
	hasEvents := false

	if len(events) > 0 {
		for _, event := range events {
			switch event {
			case "message", "messages":
				hasMessage = true
			case "message-event", "status", "read", "delivered", "sent":
				hasEvents = true
			case "account-event", "system-event", "status-event", "review-event", "onboarding-event":
				// onboarding-event is the Go-Live event - REQUIRES ACCOUNT mode
				needsAccountEvents = true
			}
		}
	} else {
		// If no events specified, default to all
		needsAccountEvents = true
		hasMessage = true
		hasEvents = true
	}

	// Build modes string - CRITICAL: Include ACCOUNT for Go-Live events
	var modes string
	if needsAccountEvents {
		// Try ACCOUNT mode explicitly (even though docs say not supported in v3, it might work)
		// If ACCOUNT doesn't work, OTHERS might catch account events
		if hasMessage && hasEvents {
			modes = "MESSAGE,OTHERS,ACCOUNT" // Try ACCOUNT mode
		} else if hasMessage {
			modes = "MESSAGE,ACCOUNT"
		} else if hasEvents {
			modes = "OTHERS,ACCOUNT"
		} else {
			modes = "ACCOUNT" // Just account events
		}
	} else if hasMessage && hasEvents {
		modes = "MESSAGE,OTHERS"
	} else if hasMessage {
		modes = "MESSAGE"
	} else if hasEvents {
		modes = "OTHERS"
	} else {
		modes = "ALL" // Fallback
	}

	// Build form-urlencoded payload
	data := url.Values{}
	data.Set("modes", modes)
	data.Set("tag", fmt.Sprintf("BrancrSubscription-%s", appID[:8])) // Unique tag per app
	data.Set("url", webhookURL)
	// CRITICAL: Try version 2 first if using ACCOUNT mode (v3 docs say ACCOUNT not supported)
	// If ACCOUNT mode fails, we'll fall back to version 3 with OTHERS
	version := "3"
	if needsAccountEvents && strings.Contains(modes, "ACCOUNT") {
		// Try version 2 which explicitly supports ACCOUNT mode
		version = "2"
	}
	data.Set("version", version)
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
		// If ACCOUNT mode failed with version 2, try version 3 with OTHERS mode as fallback
		if version == "2" && strings.Contains(modes, "ACCOUNT") && needsAccountEvents {
			if c.Logger != nil {
				c.Logger.Warn("⚠️ Subscription with ACCOUNT mode (v2) failed, retrying with OTHERS mode (v3)",
					zap.Int("status_code", resp.StatusCode),
					zap.String("response", string(body)))
			}
			// Retry with version 3 and OTHERS mode (which might catch account events)
			fallbackModes := strings.ReplaceAll(modes, "ACCOUNT", "OTHERS")
			fallbackModes = strings.ReplaceAll(fallbackModes, ",,", ",")
			fallbackModes = strings.Trim(fallbackModes, ",")
			if fallbackModes == "" {
				fallbackModes = "MESSAGE,OTHERS"
			}

			data.Set("modes", fallbackModes)
			data.Set("version", "3")

			req2, err := http.NewRequest("POST", apiURL, strings.NewReader(data.Encode()))
			if err == nil {
				req2.Header.Set("Authorization", appToken)
				req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				resp2, err2 := client.Do(req2)
				if err2 == nil {
					defer resp2.Body.Close()
					body2, _ := io.ReadAll(resp2.Body)
					if resp2.StatusCode < 300 {
						if c.Logger != nil {
							c.Logger.Info("✅ Subscription created with fallback (v3, OTHERS mode)",
								zap.String("app_id", appID),
								zap.String("modes", fallbackModes))
						}
						// Parse response and return
						var result2 struct {
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
						if err := json.Unmarshal(body2, &result2); err == nil {
							return result2.Subscription.ID, nil
						}
					}
				}
			}
		}

		if c.Logger != nil {
			c.Logger.Error("Gupshup Set Subscription failed",
				zap.Int("status_code", resp.StatusCode),
				zap.String("url", apiURL),
				zap.String("app_id", appID),
				zap.String("webhook_url", webhookURL),
				zap.String("modes", modes),
				zap.String("version", version),
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
		c.Logger.Info("✅ Gupshup webhook subscription created/updated",
			zap.String("app_id", appID),
			zap.String("subscription_id", result.Subscription.ID),
			zap.String("webhook_url", webhookURL),
			zap.String("modes", modes),
			zap.Bool("active", result.Subscription.Active),
			zap.Int("version", result.Subscription.Version),
			zap.String("tag", result.Subscription.Tag),
			zap.String("version_used", version),
			zap.Bool("needs_account_events", needsAccountEvents),
			zap.String("note", fmt.Sprintf("Subscription configured with modes='%s' (version %s) to receive Go-Live events (onboarding-event)", modes, version)))
	}

	return result.Subscription.ID, nil
}

// GetSubscription retrieves existing subscription details
// Returns subscription ID, webhook URL, modes, and active status
func (c *GupshupClient) GetSubscription(appID string) (map[string]interface{}, error) {
	// Get App Token (NOT Partner Token) - required for subscription API
	appToken, err := c.GetAppAccessToken(appID)
	if err != nil {
		return nil, fmt.Errorf("failed to get app token: %w", err)
	}

	apiURL := fmt.Sprintf("%s/partner/app/%s/subscription", c.Config.GupshupAPIBaseURL, appID)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", appToken)
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get subscription request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		if c.Logger != nil {
			c.Logger.Error("Gupshup Get Subscription failed",
				zap.Int("status_code", resp.StatusCode),
				zap.String("url", apiURL),
				zap.String("app_id", appID),
				zap.String("response", string(body)))
		}
		return nil, fmt.Errorf("get subscription failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse subscription response: %w, body=%s", err, string(body))
	}

	if c.Logger != nil {
		if subscription, ok := result["subscription"].(map[string]interface{}); ok {
			c.Logger.Info("✅ Gupshup subscription retrieved",
				zap.String("app_id", appID),
				zap.Any("subscription_id", subscription["id"]),
				zap.Any("webhook_url", subscription["url"]),
				zap.Any("modes", subscription["modes"]),
				zap.Any("active", subscription["active"]),
				zap.Any("version", subscription["version"]))
		}
	}

	return result, nil
}

// GetAppDetails retrieves app details including WhatsApp metadata (WABA ID, phone number, etc.)
// Uses two approaches:
// 1. First tries /partner/account/api/partnerApps list to get live status and phone
// 2. Then tries /partner/app/{appId} for detailed metadata
// Header: Authorization: {PARTNER_TOKEN} for list, {APP_TOKEN} for details
func (c *GupshupClient) GetAppDetails(appID string) (map[string]interface{}, error) {
	// First, try to get app from the list endpoint which includes live status and phone
	partnerToken, err := c.GetPartnerToken()
	if err != nil {
		return nil, fmt.Errorf("failed to get partner token: %w", err)
	}

	// Get list of apps to find our app (this endpoint includes live and phone fields)
	listURL := fmt.Sprintf("%s/partner/account/api/partnerApps", c.Config.GupshupAPIBaseURL)
	req, err := http.NewRequest("GET", listURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create list request: %w", err)
	}

	req.Header.Set("Authorization", partnerToken)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err == nil && resp.StatusCode < 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var listResult struct {
			Status          string `json:"status"`
			PartnerAppsList []struct {
				ID         string `json:"id"`
				Name       string `json:"name"`
				Phone      string `json:"phone"`
				Live       bool   `json:"live"`
				CustomerID string `json:"customerId"`
			} `json:"partnerAppsList"`
		}
		if err := json.Unmarshal(body, &listResult); err == nil {
			// Find our app in the list
			for _, app := range listResult.PartnerAppsList {
				if app.ID == appID {
					// Found our app - build result with live status and phone
					result := map[string]interface{}{
						"id":    app.ID,
						"name":  app.Name,
						"live":  app.Live,
						"phone": app.Phone,
					}

					// If app is live and has phone, mark as having WhatsApp metadata
					if app.Live && app.Phone != "" {
						result["whatsapp"] = map[string]interface{}{
							"formattedPhoneNumber":  app.Phone,
							"normalizedPhoneNumber": app.Phone,
						}
						result["status"] = "live"
					}

					if c.Logger != nil {
						c.Logger.Info("✅ Found app in partner apps list",
							zap.String("app_id", appID),
							zap.Bool("live", app.Live),
							zap.String("phone", app.Phone))
					}

					// Still try to get detailed metadata from /partner/app/{appId}
					// This will add WABA ID, business ID, etc. if available
					detailedResult, err := c.getAppDetailsDetailed(appID)
					if err == nil && detailedResult != nil {
						// Merge detailed metadata into result
						if whatsapp, ok := detailedResult["whatsapp"].(map[string]interface{}); ok {
							// Merge WhatsApp metadata
							if result["whatsapp"] == nil {
								result["whatsapp"] = make(map[string]interface{})
							}
							resultWhatsapp := result["whatsapp"].(map[string]interface{})
							for k, v := range whatsapp {
								resultWhatsapp[k] = v
							}
						}
						// Merge other fields
						for k, v := range detailedResult {
							if k != "whatsapp" && k != "id" && k != "name" {
								result[k] = v
							}
						}
					}

					return result, nil
				}
			}
		}
	}

	// Fallback: Try detailed endpoint
	return c.getAppDetailsDetailed(appID)
}

// getAppDetailsDetailed gets detailed app metadata from /partner/app/{appId}
func (c *GupshupClient) getAppDetailsDetailed(appID string) (map[string]interface{}, error) {
	// Use App Token (NOT Partner Token) - App Token has permission to access app-specific details
	appToken, err := c.GetAppAccessToken(appID)
	if err != nil {
		return nil, fmt.Errorf("failed to get app token: %w", err)
	}

	// Use correct endpoint: /partner/app/{appId} (NOT /partner/account/api/partnerApps/{appId})
	apiURL := fmt.Sprintf("%s/partner/app/%s", c.Config.GupshupAPIBaseURL, appID)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Use App Token in Authorization header (without "Bearer " prefix)
	req.Header.Set("Authorization", appToken)
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
// GetTemplatesFromLibrary fetches pre-approved Meta Library Templates
// Endpoint: GET /partner/app/{appId}/template/metalibrary
func (c *GupshupClient) GetTemplatesFromLibrary(appID string, filters map[string]string) ([]map[string]interface{}, error) {
	appToken, err := c.GetAppAccessToken(appID)
	if err != nil {
		return nil, fmt.Errorf("failed to get app token: %w", err)
	}

	apiURL := fmt.Sprintf("%s/partner/app/%s/template/metalibrary", c.Config.GupshupAPIBaseURL, appID)

	// Build query parameters
	params := url.Values{}
	for k, v := range filters {
		if v != "" {
			params.Set(k, v)
		}
	}
	if len(params) > 0 {
		apiURL += "?" + params.Encode()
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", appToken)
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get templates request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		if c.Logger != nil {
			c.Logger.Error("Gupshup Get Templates failed",
				zap.Int("status_code", resp.StatusCode),
				zap.String("url", apiURL),
				zap.String("app_id", appID),
				zap.String("response", string(body)))
		}
		return nil, fmt.Errorf("get templates failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status    string                   `json:"status"`
		Templates []map[string]interface{} `json:"templates"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse templates response: %w, body=%s", err, string(body))
	}

	return result.Templates, nil
}

// CreateTemplateFromLibrary creates a template from Meta Library template
// This is used to create a simple text template for AI replies
// Endpoint: POST /partner/app/{appId}/template/metalibrary
// Header: Authorization: {PARTNER_APP_TOKEN} (App Token)
// Content-Type: application/x-www-form-urlencoded
func (c *GupshupClient) CreateTemplateFromLibrary(appID, elementName, category, languageCode, libraryTemplateName, buttons string) (map[string]interface{}, error) {
	appToken, err := c.GetAppAccessToken(appID)
	if err != nil {
		return nil, fmt.Errorf("failed to get app token: %w", err)
	}

	apiURL := fmt.Sprintf("%s/partner/app/%s/template/metalibrary", c.Config.GupshupAPIBaseURL, appID)

	data := url.Values{}
	data.Set("elementName", elementName)
	data.Set("category", category)
	data.Set("languageCode", languageCode)
	data.Set("libraryTemplateName", libraryTemplateName)
	if buttons != "" {
		data.Set("buttons", buttons)
	}

	req, err := http.NewRequest("POST", apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", appToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 30 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create template request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		if c.Logger != nil {
			c.Logger.Error("Gupshup Create Template failed",
				zap.Int("status_code", resp.StatusCode),
				zap.String("url", apiURL),
				zap.String("app_id", appID),
				zap.String("element_name", elementName),
				zap.String("response", string(body)))
		}
		return nil, fmt.Errorf("create template failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status    string                 `json:"status"`
		Templates map[string]interface{} `json:"templates"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse create template response: %w, body=%s", err, string(body))
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("invalid create template response: %s", string(body))
	}

	if c.Logger != nil {
		templateID := ""
		if id, ok := result.Templates["id"].(string); ok {
			templateID = id
		}
		elementNameCreated := ""
		if en, ok := result.Templates["elementName"].(string); ok {
			elementNameCreated = en
		}
		c.Logger.Info("✅ Gupshup template created from Meta Library",
			zap.String("app_id", appID),
			zap.String("template_id", templateID),
			zap.String("element_name", elementNameCreated),
			zap.String("category", category),
			zap.String("language_code", languageCode))
	}

	return result.Templates, nil
}

// CreateAITemplate creates a simple text template for AI replies
// It first finds a simple text template from Meta Library, then creates a new one based on it
// The new template will have a variable parameter {{1}} for AI-generated text
func (c *GupshupClient) CreateAITemplate(appID string) (string, error) {
	// Step 1: Get available templates from Meta Library
	// Try without filters first (get all templates)
	templates, err := c.GetTemplatesFromLibrary(appID, map[string]string{})
	if err != nil {
		// If that fails, try with language code only
		if c.Logger != nil {
			c.Logger.Warn("Failed to get templates without filters, trying with language code",
				zap.String("app_id", appID),
				zap.Error(err))
		}
		templates, err = c.GetTemplatesFromLibrary(appID, map[string]string{
			"languageCode": "en",
		})
		if err != nil {
			return "", fmt.Errorf("failed to get templates from library: %w", err)
		}
	}

	if len(templates) == 0 {
		// Try with category filter
		templates, err = c.GetTemplatesFromLibrary(appID, map[string]string{
			"category": "UTILITY",
		})
		if err != nil || len(templates) == 0 {
			return "", fmt.Errorf("no templates found in Meta Library for app (tried multiple filters)")
		}
	}

	// Step 2: Find a simple text template (prefer UTILITY category, TEXT type, with parameters)
	// IMPORTANT: Avoid AUTHENTICATION category - it requires exactly one OTP button
	var baseTemplate map[string]interface{}

	// First priority: UTILITY category, TEXT type, with {{1}} parameter
	for _, t := range templates {
		category, _ := t["category"].(string)
		templateType, _ := t["templateType"].(string)
		if category == "UTILITY" && templateType == "TEXT" {
			if data, ok := t["data"].(string); ok && strings.Contains(data, "{{1}}") {
				baseTemplate = t
				break
			}
		}
	}

	// Second priority: Any UTILITY category template (even without {{1}})
	if baseTemplate == nil {
		for _, t := range templates {
			if category, ok := t["category"].(string); ok && category == "UTILITY" {
				baseTemplate = t
				break
			}
		}
	}

	// Third priority: TEXT type with {{1}} (any category except AUTHENTICATION)
	if baseTemplate == nil {
		for _, t := range templates {
			category, _ := t["category"].(string)
			templateType, _ := t["templateType"].(string)
			if category != "AUTHENTICATION" && templateType == "TEXT" {
				if data, ok := t["data"].(string); ok && strings.Contains(data, "{{1}}") {
					baseTemplate = t
					break
				}
			}
		}
	}

	// Last resort: First non-AUTHENTICATION template
	if baseTemplate == nil {
		for _, t := range templates {
			if category, ok := t["category"].(string); ok && category != "AUTHENTICATION" {
				baseTemplate = t
				break
			}
		}
	}

	// If still no template, use first available (but log warning)
	if baseTemplate == nil && len(templates) > 0 {
		baseTemplate = templates[0]
		if c.Logger != nil {
			c.Logger.Warn("⚠️ Using first available template (may be AUTHENTICATION category)",
				zap.String("app_id", appID))
		}
	}

	if baseTemplate == nil {
		return "", fmt.Errorf("no suitable template found in Meta Library for app")
	}

	// Extract library template name (elementName)
	libraryTemplateName, ok := baseTemplate["elementName"].(string)
	if !ok || libraryTemplateName == "" {
		return "", fmt.Errorf("library template name not found in template")
	}

	// Extract category - FORCE UTILITY for AI replies (avoid AUTHENTICATION requirements)
	category := "UTILITY"
	originalCategory, _ := baseTemplate["category"].(string)
	if originalCategory == "AUTHENTICATION" {
		if c.Logger != nil {
			c.Logger.Warn("⚠️ Base template is AUTHENTICATION category, forcing UTILITY for AI reply",
				zap.String("base_template", libraryTemplateName),
				zap.String("app_id", appID))
		}
	}

	// Extract language code
	languageCode := "en"
	if lang, ok := baseTemplate["languageCode"].(string); ok && lang != "" {
		languageCode = lang
	}

	// Extract buttons (if any)
	// IMPORTANT: For UTILITY category, we can create templates without buttons
	// For AUTHENTICATION category, we'd need exactly one OTP button, which we don't want for AI replies
	buttons := ""
	if category == "UTILITY" {
		// For UTILITY category, we can omit buttons (empty string)
		// This allows us to create a simple text template for AI replies
		buttons = ""
	} else {
		// For other categories, try to preserve buttons if they exist
		if buttonsData, ok := baseTemplate["buttons"].(string); ok && buttonsData != "" {
			buttons = buttonsData
		}
	}

	// Step 3: Create new template with unique element name
	// Use app ID prefix + timestamp for uniqueness
	elementName := fmt.Sprintf("brancr_ai_reply_%s_%d", appID[:8], time.Now().Unix())

	// Create the template
	templateData, err := c.CreateTemplateFromLibrary(appID, elementName, category, languageCode, libraryTemplateName, buttons)
	if err != nil {
		return "", fmt.Errorf("failed to create template from library: %w", err)
	}

	// Return the elementName (this is the template ID we'll use)
	createdElementName, ok := templateData["elementName"].(string)
	if !ok || createdElementName == "" {
		return "", fmt.Errorf("created template elementName not found in response")
	}

	if c.Logger != nil {
		c.Logger.Info("✅ AI reply template created successfully",
			zap.String("app_id", appID),
			zap.String("template_element_name", createdElementName),
			zap.String("base_library_template", libraryTemplateName))
	}

	return createdElementName, nil
}

// SendMessage sends a WhatsApp message via Gupshup using the template message API
// For plain text messages, we first try to get a simple text template from Meta Library
// Endpoint: POST /partner/app/{appId}/template/msg
// Header: token: {PARTNER_APP_TOKEN} (App Token)
// Content-Type: application/x-www-form-urlencoded
func (c *GupshupClient) SendMessage(appID, to, message string, defaultTemplateID ...string) (string, error) {
	appToken, err := c.GetAppAccessToken(appID)
	if err != nil {
		return "", fmt.Errorf("failed to get app token: %w", err)
	}

	// Get app details to extract source phone number and app name
	appDetails, err := c.GetAppDetails(appID)
	if err != nil {
		return "", fmt.Errorf("failed to get app details: %w", err)
	}

	// Extract source phone number and app name
	var sourcePhone string
	var appName string

	// Get phone from whatsapp object or from top-level
	if whatsapp, ok := appDetails["whatsapp"].(map[string]interface{}); ok {
		if phone, ok := whatsapp["formattedPhoneNumber"].(string); ok && phone != "" {
			sourcePhone = phone
		} else if phone, ok := whatsapp["normalizedPhoneNumber"].(string); ok && phone != "" {
			sourcePhone = phone
		}
	}

	// Get phone from top-level if not in whatsapp object
	if sourcePhone == "" {
		if phone, ok := appDetails["phone"].(string); ok && phone != "" {
			sourcePhone = phone
		}
	}

	// Get app name
	if name, ok := appDetails["name"].(string); ok && name != "" {
		appName = name
	}

	if sourcePhone == "" {
		return "", fmt.Errorf("source phone number not found for app")
	}
	if appName == "" {
		return "", fmt.Errorf("app name not found for app")
	}

	// Use correct endpoint: /partner/app/{appId}/template/msg
	apiURL := fmt.Sprintf("%s/partner/app/%s/template/msg", c.Config.GupshupAPIBaseURL, appID)

	// Build form-urlencoded payload
	data := url.Values{}
	data.Set("channel", "whatsapp")
	data.Set("source", sourcePhone)
	data.Set("destination", to)
	data.Set("src.name", appName)
	data.Set("sandbox", "false")

	// For AI-generated replies, we need a flexible template
	// Strategy: Use a simple text template with a variable parameter
	// The template should have format like: "{{1}}" where we pass the AI reply as param

	// For AI-generated replies, we need a flexible template approach
	// Strategy: Use a simple text template with a variable parameter
	// The template should have format like: "{{1}}" where we pass the AI reply as param

	var templateID string
	var templateParams []string

	// Priority 1: Use default template ID from database (if provided)
	if len(defaultTemplateID) > 0 && defaultTemplateID[0] != "" {
		templateID = defaultTemplateID[0]
		// Pass AI-generated message as the first parameter
		templateParams = []string{message}
		if c.Logger != nil {
			c.Logger.Info("✅ Using default template ID for AI reply",
				zap.String("template_id", templateID))
		}
	} else {
		// Priority 2: Try to get from Meta Library (fallback - may not work well for AI)
		// Try without filters first to get all available templates
		templates, err := c.GetTemplatesFromLibrary(appID, map[string]string{})
		if err != nil || len(templates) == 0 {
			// Try with language code only
			templates, err = c.GetTemplatesFromLibrary(appID, map[string]string{
				"languageCode": "en",
			})
		}

		if err == nil && len(templates) > 0 {
			// Find a simple TEXT template that can accept a parameter
			var selectedTemplate map[string]interface{}
			for _, t := range templates {
				if templateType, ok := t["templateType"].(string); ok && templateType == "TEXT" {
					// Check if it has at least one parameter placeholder
					if data, ok := t["data"].(string); ok && (strings.Contains(data, "{{1}}") || strings.Contains(data, "{{ 1 }}")) {
						selectedTemplate = t
						break
					}
				}
			}

			// If no TEXT template with parameters, use first TEXT template
			if selectedTemplate == nil {
				for _, t := range templates {
					if templateType, ok := t["templateType"].(string); ok && templateType == "TEXT" {
						selectedTemplate = t
						break
					}
				}
			}

			// If still no TEXT template, use first available (not ideal)
			if selectedTemplate == nil && len(templates) > 0 {
				selectedTemplate = templates[0]
			}

			if selectedTemplate != nil {
				if elementName, ok := selectedTemplate["elementName"].(string); ok && elementName != "" {
					templateID = elementName
					// Only pass message as parameter if template has parameter placeholders
					if data, ok := selectedTemplate["data"].(string); ok && (strings.Contains(data, "{{1}}") || strings.Contains(data, "{{ 1 }}")) {
						templateParams = []string{message}
					} else {
						// Template doesn't have parameters - this won't work well
						// But we'll try anyway (may fail)
						templateParams = []string{}
						if c.Logger != nil {
							c.Logger.Warn("⚠️ Template doesn't have parameters - message may not be sent correctly",
								zap.String("template_id", templateID),
								zap.String("template_data", data))
						}
					}
					if c.Logger != nil {
						c.Logger.Warn("⚠️ Using Meta Library template for AI reply (not ideal)",
							zap.String("template_id", templateID),
							zap.String("template_type", func() string {
								if tt, ok := selectedTemplate["templateType"].(string); ok {
									return tt
								}
								return "unknown"
							}()),
							zap.String("note", "Consider creating a dedicated text template for AI replies"))
					}
				}
			}
		}
	}

	// If no template found, we can't send the message
	if templateID == "" {
		return "", fmt.Errorf("no template available for sending AI reply - need to create a text template for app %s and store its ID in WhatsAppApp.DefaultTemplateID", appID)
	}

	// Use template with AI-generated message as parameter
	paramsJSON, _ := json.Marshal(templateParams)
	templateJSON := fmt.Sprintf(`{"id": "%s", "params": %s}`, templateID, string(paramsJSON))
	data.Set("template", templateJSON)

	// Message field is also required for text templates
	// Note: For templates with parameters, the message field might be ignored
	// But we include it anyway as per API docs
	messageJSON := fmt.Sprintf(`{"type": "text", "text": "%s"}`, escapeJSONString(message))
	data.Set("message", messageJSON)

	if c.Logger != nil {
		c.Logger.Info("📤 Preparing to send message with template",
			zap.String("template_id", templateID),
			zap.Int("params_count", len(templateParams)),
			zap.String("template_json", templateJSON),
			zap.String("message_preview", func() string {
				if len(message) > 100 {
					return message[:100] + "..."
				}
				return message
			}()))
	}

	req, err := http.NewRequest("POST", apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Use token header (as per Gupshup Partner API docs)
	// Docs show: --header 'token: {PARTNER_APP_TOKEN}'
	req.Header.Set("token", appToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("accept", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("send message request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		if c.Logger != nil {
			c.Logger.Error("Gupshup Send Message failed",
				zap.Int("status_code", resp.StatusCode),
				zap.String("url", apiURL),
				zap.String("app_id", appID),
				zap.String("source", sourcePhone),
				zap.String("destination", to),
				zap.String("response", string(body)))
		}
		return "", fmt.Errorf("send message failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status    string `json:"status"`
		MessageID string `json:"messageId"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse send message response: %w, body=%s", err, string(body))
	}

	if result.Status != "submitted" || result.MessageID == "" {
		return "", fmt.Errorf("invalid send message response: %s", string(body))
	}

	if c.Logger != nil {
		c.Logger.Info("✅ Gupshup message sent successfully",
			zap.String("app_id", appID),
			zap.String("message_id", result.MessageID),
			zap.String("destination", to))
	}

	return result.MessageID, nil
}

// escapeJSONString escapes special characters in JSON string
func escapeJSONString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return s
}
