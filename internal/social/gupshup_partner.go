package social

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

const GupshupPartnerBaseURL = "https://partner.gupshup.io"

// GupshupPartnerClient handles API requests to Gupshup Partner API
type GupshupPartnerClient struct {
	BaseURL        string
	ClientSecret   string // Partner API key (client secret) - used for login only
	PartnerEmail   string // Partner account email for login
	PartnerID      string // Partner ID (optional, used for some operations)
	Token          string // Generated partner token (short-lived, typically 24 hours)
	TokenExpiresAt time.Time
	tokenMutex     sync.RWMutex // Protects token and expiration
	Logger         *zap.Logger
}

// NewGupshupPartnerClient creates a new Gupshup Partner API client
func NewGupshupPartnerClient(baseURL, clientSecret, partnerEmail, partnerID string, logger *zap.Logger) *GupshupPartnerClient {
	return &GupshupPartnerClient{
		BaseURL:      baseURL,
		ClientSecret: clientSecret,
		PartnerEmail: partnerEmail,
		PartnerID:    partnerID,
		Logger:       logger,
	}
}

// GetPartnerToken generates a temporary PARTNER_TOKEN by logging in with client secret
// Endpoint: POST /partner/account/login
// This token is required for SetCallback, SendMessage, and other API calls
func (c *GupshupPartnerClient) GetPartnerToken(ctx context.Context) error {
	if c.ClientSecret == "" {
		return fmt.Errorf("client secret (Partner API key) is not configured")
	}
	if c.PartnerEmail == "" {
		return fmt.Errorf("partner email is required for login")
	}

	// Check if we have a valid cached token
	c.tokenMutex.RLock()
	if c.Token != "" && time.Now().Before(c.TokenExpiresAt) {
		c.tokenMutex.RUnlock()
		return nil // Token still valid
	}
	c.tokenMutex.RUnlock()

	// Need to get a new token
	endpoint := fmt.Sprintf("%s/partner/account/login", c.BaseURL)

	// Login with email and client secret (password)
	formData := url.Values{}
	formData.Set("email", c.PartnerEmail)
	formData.Set("password", c.ClientSecret) // Client secret acts as password

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(formData.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create login request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if c.Logger != nil {
		c.Logger.Info("🔐 Logging in to Gupshup Partner API to get token",
			zap.String("email", c.PartnerEmail))
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		if c.Logger != nil {
			c.Logger.Error("❌ Failed to get partner token",
				zap.Int("status", resp.StatusCode),
				zap.String("response", string(body)))
		}
		return fmt.Errorf("login failed: status %d, body: %s", resp.StatusCode, string(body))
	}

	// Parse response to extract token
	var loginResponse struct {
		Status string `json:"status"`
		Token  string `json:"token"`
		Data   struct {
			Token string `json:"token"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &loginResponse); err != nil {
		// Try alternative response format
		var altResponse map[string]interface{}
		if err2 := json.Unmarshal(body, &altResponse); err2 == nil {
			if token, ok := altResponse["token"].(string); ok && token != "" {
				loginResponse.Token = token
			} else if data, ok := altResponse["data"].(map[string]interface{}); ok {
				if token, ok := data["token"].(string); ok && token != "" {
					loginResponse.Token = token
				}
			}
		}
		if loginResponse.Token == "" {
			return fmt.Errorf("failed to parse login response: %w, body: %s", err, string(body))
		}
	}

	// Prefer nested token, fallback to top-level
	token := loginResponse.Data.Token
	if token == "" {
		token = loginResponse.Token
	}

	if token == "" {
		return fmt.Errorf("token not found in login response: %s", string(body))
	}

	// Store token with expiration (typically 24 hours, but we'll refresh after 23 hours to be safe)
	c.tokenMutex.Lock()
	c.Token = token
	c.TokenExpiresAt = time.Now().Add(23 * time.Hour) // Refresh before expiration
	c.tokenMutex.Unlock()

	if c.Logger != nil {
		c.Logger.Info("✅ Successfully obtained Gupshup Partner token",
			zap.Time("expires_at", c.TokenExpiresAt))
	}

	return nil
}

// ensureValidToken ensures we have a valid token, refreshing if needed
func (c *GupshupPartnerClient) ensureValidToken(ctx context.Context) error {
	c.tokenMutex.RLock()
	hasValidToken := c.Token != "" && time.Now().Before(c.TokenExpiresAt)
	c.tokenMutex.RUnlock()

	if !hasValidToken {
		return c.GetPartnerToken(ctx)
	}
	return nil
}

// GenerateOnboardingLink generates Partner onboarding redirect link for tenant
// First attempts to generate via authenticated API call, falls back to URL construction
// Returns the link and any validation errors
func (c *GupshupPartnerClient) GenerateOnboardingLink(ctx context.Context, partnerID, redirectURL string) (string, error) {
	// Validate required fields
	if partnerID == "" {
		return "", fmt.Errorf("partner ID is required")
	}
	if redirectURL == "" {
		return "", fmt.Errorf("redirect URL is required")
	}

	// CRITICAL: Generate onboarding link via authenticated API call only
	// The URL method with clientSecret in query params is DEPRECATED and no longer works
	// Gupshup now requires Partner Token with Authorization headers
	if err := c.ensureValidToken(ctx); err != nil {
		return "", fmt.Errorf("failed to get partner token: %w (required for onboarding link generation)", err)
	}

	// Try multiple possible API endpoints for onboarding link generation
	endpoints := []string{
		fmt.Sprintf("%s/partner/app/onboard-link", c.BaseURL),
		fmt.Sprintf("%s/partner/app/generate-onboarding-link", c.BaseURL),
		fmt.Sprintf("%s/partner/app/onboarding-link", c.BaseURL),
		fmt.Sprintf("%s/partner/app/create-onboarding-link", c.BaseURL),
	}

	c.tokenMutex.RLock()
	token := c.Token
	c.tokenMutex.RUnlock()

	payload := map[string]interface{}{
		"partnerId": partnerID,
		"redirect":  redirectURL,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	for _, endpoint := range endpoints {
		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonData))
		if err != nil {
			continue
		}

		// Try Bearer format first (standard OAuth format)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			if c.Logger != nil {
				c.Logger.Debug("Failed to call onboarding link endpoint",
					zap.String("endpoint", endpoint),
					zap.Error(err))
			}
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		// Log response for debugging - use Info level for better visibility
		if c.Logger != nil {
			if resp.StatusCode >= 400 {
				c.Logger.Error("❌ Onboarding link API endpoint failed",
					zap.String("endpoint", endpoint),
					zap.Int("status", resp.StatusCode),
					zap.String("response_body", string(body)), // Log full response for debugging
					zap.String("response_headers", fmt.Sprintf("%+v", resp.Header)),
					zap.String("request_headers", fmt.Sprintf("Authorization: Bearer %s...", truncateString(token, 20))))
			} else {
				c.Logger.Info("🔍 Tried onboarding link API endpoint",
					zap.String("endpoint", endpoint),
					zap.Int("status", resp.StatusCode),
					zap.String("response_body", string(body)))
			}
		}

		// If Bearer format fails with 401, try X-Authorization format
		if resp.StatusCode == 401 {
			if c.Logger != nil {
				c.Logger.Debug("Bearer token format failed with 401, trying X-Authorization",
					zap.String("endpoint", endpoint))
			}

			// Create new request with X-Authorization header
			req2, err2 := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonData))
			if err2 != nil {
				continue
			}

			req2.Header.Set("X-Authorization", token)
			req2.Header.Set("Content-Type", "application/json")

			resp2, err2 := client.Do(req2)
			if err2 != nil {
				continue
			}

			body2, _ := io.ReadAll(resp2.Body)
			resp2.Body.Close()

			if c.Logger != nil {
				if resp2.StatusCode >= 400 {
					c.Logger.Error("❌ X-Authorization retry also failed",
						zap.String("endpoint", endpoint),
						zap.Int("status", resp2.StatusCode),
						zap.String("response_body", string(body2)),
						zap.String("response_status", resp2.Status))
				} else {
					c.Logger.Info("🔍 Retried with X-Authorization header",
						zap.String("endpoint", endpoint),
						zap.Int("status", resp2.StatusCode),
						zap.String("response_body", truncateString(string(body2), 500)))
				}
			}

			if resp2.StatusCode == 200 || resp2.StatusCode == 201 {
				body = body2
				resp.StatusCode = resp2.StatusCode
			}
		}

		if resp.StatusCode == 200 || resp.StatusCode == 201 {
			var response struct {
				OnboardingURL  string `json:"onboardingUrl"`
				OnboardingLink string `json:"onboarding_link"`
				URL            string `json:"url"`
				Link           string `json:"link"`
				OnboardURL     string `json:"onboardUrl"`
				Data           struct {
					URL string `json:"url"`
				} `json:"data"`
			}
			if err := json.Unmarshal(body, &response); err == nil {
				// Try different possible field names
				link := response.OnboardingURL
				if link == "" {
					link = response.OnboardingLink
				}
				if link == "" {
					link = response.URL
				}
				if link == "" {
					link = response.Link
				}
				if link == "" {
					link = response.OnboardURL
				}
				if link == "" {
					link = response.Data.URL
				}
				if link != "" {
					if c.Logger != nil {
						c.Logger.Info("✅ Generated Gupshup onboarding link via API",
							zap.String("partner_id", partnerID),
							zap.String("redirect_url", redirectURL),
							zap.String("endpoint", endpoint),
							zap.Int("status_code", resp.StatusCode))
					}
					return link, nil
				}
			}

			// If JSON parsing failed but status is 200/201, log the raw response
			if c.Logger != nil {
				c.Logger.Warn("⚠️ Got 200/201 response but couldn't parse onboarding URL from response",
					zap.String("endpoint", endpoint),
					zap.String("response_body", truncateString(string(body), 500)))
			}
		}
	}

	// If all API endpoints failed, log all error responses for debugging
	if c.Logger != nil {
		c.Logger.Error("❌ Failed to generate onboarding link via API - all endpoints failed",
			zap.String("partner_id", partnerID),
			zap.String("redirect_url", redirectURL),
			zap.String("note", "Check logs above for individual endpoint error responses to identify the correct endpoint"))
	}

	// CRITICAL: Log what error responses we got from each endpoint to help identify the correct one
	// The full error responses should be in the logs above - check them to see:
	// 1. What status codes were returned (404 = endpoint doesn't exist, 401 = auth issue, etc.)
	// 2. What error messages Gupshup returned
	// 3. Which endpoint (if any) returned a different response that might give us a clue

	return "", fmt.Errorf("failed to generate onboarding link via API: all endpoints returned errors. Check logs for detailed error responses from each endpoint attempt. Partner ID: %s, Partner Email: %s. Possible causes: 1) Endpoints don't exist (check Gupshup docs for correct endpoint), 2) Authentication token not accepted, 3) Wrong request format/payload", partnerID, c.PartnerEmail)
}

// CreateAppResponse represents the response from CreateApp API
type CreateAppResponse struct {
	AppID    string `json:"appId"`
	AppToken string `json:"appToken"`
	APIKey   string `json:"apiKey"`
	Status   string `json:"status"`
	Message  string `json:"message"`
}

// sanitizeAppName removes ALL invalid characters from app name
// Gupshup requires: ONLY letters and numbers, NO spaces, hyphens, underscores, or special characters
// Format like: "BrancrTech" - just concatenated letters/numbers
func sanitizeAppName(name string) string {
	if name == "" {
		return "WhatsAppBusinessApp"
	}

	// Keep ONLY alphanumeric characters (letters and numbers)
	// Remove ALL spaces, hyphens, underscores, and special characters
	var sanitized strings.Builder
	for _, char := range name {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') {
			sanitized.WriteRune(char)
		}
		// All other characters (spaces, hyphens, underscores, special chars, emojis, etc.) are removed
	}

	result := sanitized.String()

	// Ensure minimum length
	if len(result) < 6 {
		result = "WhatsAppApp" + result
		if len(result) < 6 {
			result = "WhatsAppBusinessApp" // Fallback if still too short
		}
	}

	// Truncate if too long (max 150 chars)
	if len(result) > 150 {
		result = result[:150]
	}

	return result
}

// CreateApp creates a new Gupshup App linked to Partner ID
// Endpoint: POST /partner/app
// This must be called FIRST before generating onboarding link
func (c *GupshupPartnerClient) CreateApp(ctx context.Context, appName string) (*CreateAppResponse, error) {
	// Ensure we have a valid token
	if err := c.ensureValidToken(ctx); err != nil {
		return nil, fmt.Errorf("failed to get partner token: %w", err)
	}

	// Sanitize app name to remove invalid characters
	originalName := appName
	appName = sanitizeAppName(appName)

	// Log if name was sanitized
	if originalName != appName && c.Logger != nil {
		c.Logger.Info("🔧 Sanitized app name",
			zap.String("original", originalName),
			zap.String("sanitized", appName))
	}

	endpoint := fmt.Sprintf("%s/partner/app", c.BaseURL)

	// CRITICAL: Gupshup Create App API requires form-encoded data, not JSON!
	// Content-Type: application/x-www-form-urlencoded
	// Header: token: YOUR_PARTNER_TOKEN (not Authorization: Bearer)
	formData := url.Values{}
	formData.Set("name", appName)

	// Optional fields with defaults
	formData.Set("templateMessaging", "false")
	formData.Set("disableOptinPrefUrl", "false")

	// Validate app name length (6-150 characters as per Gupshup requirements)
	if len(appName) < 6 {
		return nil, fmt.Errorf("app name must be at least 6 characters long (after sanitization: %s)", appName)
	}
	if len(appName) > 150 {
		return nil, fmt.Errorf("app name must be at most 150 characters long (after sanitization: %s)", appName)
	}

	c.tokenMutex.RLock()
	token := c.Token
	c.tokenMutex.RUnlock()

	// Log payload for debugging
	if c.Logger != nil {
		c.Logger.Debug("Creating Gupshup app with form data",
			zap.String("app_name", appName),
			zap.String("partner_id", c.PartnerID),
			zap.Int("app_name_length", len(appName)))
	}

	// Create request with form-encoded body
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Use 'token' header (not Authorization: Bearer) as per Gupshup docs
	req.Header.Set("token", token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// If token header fails with 401, try Authorization: Bearer as fallback
	if resp.StatusCode == 401 {
		if c.Logger != nil {
			c.Logger.Debug("Token header failed for CreateApp, retrying with Authorization: Bearer")
		}

		req2, err2 := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(formData.Encode()))
		if err2 != nil {
			return nil, fmt.Errorf("failed to create retry request: %w", err2)
		}

		req2.Header.Set("Authorization", "Bearer "+token)
		req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp2, err2 := client.Do(req2)
		if err2 != nil {
			return nil, fmt.Errorf("API request failed on retry: %w", err2)
		}
		defer resp2.Body.Close()

		body, _ = io.ReadAll(resp2.Body)
		resp = resp2
	}

	if resp.StatusCode >= 400 {
		// Log full error details for debugging
		if c.Logger != nil {
			c.Logger.Error("❌ Failed to create Gupshup app",
				zap.Int("status", resp.StatusCode),
				zap.String("response_body", string(body)),
				zap.String("app_name", appName),
				zap.String("partner_id", c.PartnerID),
				zap.String("endpoint", endpoint),
				zap.String("form_data_sent", formData.Encode()))
		}

		var apiErr struct {
			Status  string                 `json:"status"`
			Error   string                 `json:"error"`
			Message string                 `json:"message"`
			Data    map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(body, &apiErr); err == nil {
			// Try to extract detailed error message
			errorMsg := apiErr.Message
			if errorMsg == "" {
				errorMsg = apiErr.Error
			}
			if errorMsg == "" && apiErr.Status != "" {
				errorMsg = apiErr.Status
			}

			if errorMsg != "" {
				return nil, fmt.Errorf("gupshup Partner API error %d: %s. Full response: %s", resp.StatusCode, errorMsg, string(body))
			}
		}

		return nil, fmt.Errorf("gupshup Partner API error %d: %s. Sent form data: %s", resp.StatusCode, string(body), formData.Encode())
	}

	var response CreateAppResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w, body: %s", err, string(body))
	}

	if response.AppID == "" {
		return nil, fmt.Errorf("app ID not found in response: %s", string(body))
	}

	if c.Logger != nil {
		c.Logger.Info("✅ Created Gupshup app",
			zap.String("app_id", response.AppID),
			zap.String("app_name", appName),
			zap.Bool("has_app_token", response.AppToken != ""),
			zap.Bool("has_api_key", response.APIKey != ""))
	}

	return &response, nil
}

// GetAppDetailsResponse represents app details from Gupshup API
type GetAppDetailsResponse struct {
	AppID         string `json:"appId"`
	WABAID        string `json:"wabaId"`
	PhoneNumber   string `json:"phoneNumber"`
	BusinessID    string `json:"businessId"`
	PhoneNumberID string `json:"phoneNumberId"`
	Status        string `json:"status"`
	Name          string `json:"name"`
}

// GetAppDetails fetches app details including WABA ID and phone number from Gupshup API
// This is useful when the callback redirect doesn't include all the data
func (c *GupshupPartnerClient) GetAppDetails(ctx context.Context, appID string) (*GetAppDetailsResponse, error) {
	// Ensure we have a valid token
	if err := c.ensureValidToken(ctx); err != nil {
		return nil, fmt.Errorf("failed to get partner token: %w", err)
	}

	if appID == "" {
		return nil, fmt.Errorf("app ID is required")
	}

	c.tokenMutex.RLock()
	token := c.Token
	c.tokenMutex.RUnlock()

	// Try multiple possible endpoints for getting app details
	endpoints := []string{
		fmt.Sprintf("%s/partner/app/%s", c.BaseURL, appID),
		fmt.Sprintf("%s/partner/apps/%s", c.BaseURL, appID),
		fmt.Sprintf("%s/partner/app/%s/details", c.BaseURL, appID),
	}

	client := &http.Client{Timeout: 15 * time.Second}

	for _, endpoint := range endpoints {
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		if err != nil {
			continue
		}

		// Try token header first
		req.Header.Set("token", token)

		if c.Logger != nil {
			c.Logger.Info("🔍 Fetching app details from Gupshup API",
				zap.String("endpoint", endpoint),
				zap.String("app_id", appID))
		}

		resp, err := client.Do(req)
		if err != nil {
			if c.Logger != nil {
				c.Logger.Debug("Failed to call app details endpoint",
					zap.String("endpoint", endpoint),
					zap.Error(err))
			}
			continue
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)

		// If 401, try Authorization: Bearer
		if resp.StatusCode == 401 {
			if c.Logger != nil {
				c.Logger.Debug("Token header failed, retrying with Authorization: Bearer")
			}
			req2, err2 := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
			if err2 != nil {
				continue
			}
			req2.Header.Set("Authorization", "Bearer "+token)
			resp2, err2 := client.Do(req2)
			if err2 != nil {
				continue
			}
			defer resp2.Body.Close()
			body, _ = io.ReadAll(resp2.Body)
			resp = resp2
		}

		if resp.StatusCode == 200 || resp.StatusCode == 201 {
			var response GetAppDetailsResponse
			var responseWrapper struct {
				Data GetAppDetailsResponse `json:"data"`
				GetAppDetailsResponse
			}

			// Try parsing as direct response first
			if err := json.Unmarshal(body, &response); err == nil {
				// Check if we got valid data
				if response.AppID != "" || response.WABAID != "" || response.PhoneNumber != "" {
					if c.Logger != nil {
						c.Logger.Info("✅ Successfully fetched app details",
							zap.String("app_id", response.AppID),
							zap.String("waba_id", response.WABAID),
							zap.String("phone_number", response.PhoneNumber))
					}
					return &response, nil
				}
			}

			// Try parsing as wrapped response
			if err := json.Unmarshal(body, &responseWrapper); err == nil {
				if responseWrapper.Data.AppID != "" || responseWrapper.Data.WABAID != "" {
					if c.Logger != nil {
						c.Logger.Info("✅ Successfully fetched app details (wrapped)",
							zap.String("app_id", responseWrapper.Data.AppID),
							zap.String("waba_id", responseWrapper.Data.WABAID))
					}
					return &responseWrapper.Data, nil
				}
			}

			// Log raw response for debugging
			bodyStr := string(body)
			if len(bodyStr) > 500 {
				bodyStr = bodyStr[:500] + "..."
			}
			if c.Logger != nil {
				c.Logger.Warn("⚠️ Got 200 response but couldn't parse app details",
					zap.String("endpoint", endpoint),
					zap.String("response", bodyStr))
			}
		} else {
			bodyStr := string(body)
			if len(bodyStr) > 200 {
				bodyStr = bodyStr[:200] + "..."
			}
			if c.Logger != nil {
				c.Logger.Debug("App details endpoint returned error",
					zap.String("endpoint", endpoint),
					zap.Int("status", resp.StatusCode),
					zap.String("response", bodyStr))
			}
		}
	}

	return nil, fmt.Errorf("failed to fetch app details: all endpoints returned errors or invalid responses")
}

// GenerateEmbedSignedLinkResponse represents the response from GenerateEmbedSignedLink API
type GenerateEmbedSignedLinkResponse struct {
	EmbedSignedLink string `json:"embedSignedLink"`
	SignedURL       string `json:"signedUrl"`
	URL             string `json:"url"`
	Link            string `json:"link"`
	Status          string `json:"status"`
	Message         string `json:"message"`
}

// extractLinkFromResponse extracts the onboarding link from various possible response formats
func extractLinkFromResponse(body string) string {
	// Try parsing as JSON first
	var jsonResp map[string]interface{}
	if err := json.Unmarshal([]byte(body), &jsonResp); err == nil {
		// Try different field names
		if link, ok := jsonResp["embedSignedLink"].(string); ok && link != "" {
			return link
		}
		if link, ok := jsonResp["signedUrl"].(string); ok && link != "" {
			return link
		}
		if link, ok := jsonResp["url"].(string); ok && link != "" {
			return link
		}
		if link, ok := jsonResp["link"].(string); ok && link != "" {
			return link
		}
		if data, ok := jsonResp["data"].(map[string]interface{}); ok {
			if link, ok := data["url"].(string); ok && link != "" {
				return link
			}
			if link, ok := data["link"].(string); ok && link != "" {
				return link
			}
		}
	}

	// If not JSON, check if body is already a URL (simple string)
	if strings.HasPrefix(body, "http://") || strings.HasPrefix(body, "https://") {
		return strings.TrimSpace(body)
	}

	return ""
}

// GenerateEmbedSignedLink generates an embed signed link (onboarding URL) for an existing app
// Endpoint: GET /partner/app/{appId}/onboarding/embed/link
// According to Gupshup Partner API docs: https://partner-docs.gupshup.io/reference/get_partner-app-appid-onboarding-embed-link
// This is the CORRECT way to get the onboarding link - requires an existing app
// REQUIRED query parameters: user (username) and lang (language code)
func (c *GupshupPartnerClient) GenerateEmbedSignedLink(ctx context.Context, appID, redirectURL, username, lang string) (string, error) {
	// Ensure we have a valid token
	if err := c.ensureValidToken(ctx); err != nil {
		return "", fmt.Errorf("failed to get partner token: %w", err)
	}

	if appID == "" {
		return "", fmt.Errorf("app ID is required")
	}
	// Note: redirectURL is not used as a query parameter - it's configured via SetCallback or Meta App settings
	// Keeping it in the function signature for compatibility, but not using it in the request

	c.tokenMutex.RLock()
	token := c.Token
	c.tokenMutex.RUnlock()

	// According to Gupshup Partner API documentation:
	// GET /partner/app/{appId}/onboarding/embed/link
	// REQUIRED query parameters: user (username), lang (language code)
	// Optional query parameters: regenerate (boolean)
	// Headers: token (Partner JWT Token)
	// NOTE: redirectUrl is configured separately via SetCallback or in Meta App OAuth settings
	endpoint := fmt.Sprintf("%s/partner/app/%s/onboarding/embed/link", c.BaseURL, appID)

	// Build query parameters - user and lang are REQUIRED per Gupshup docs
	queryParams := url.Values{}

	// Use provided username or default to "admin" if empty
	if username == "" {
		username = "admin"
	}
	queryParams.Set("user", username)

	// Use provided language or default to "en" (English)
	if lang == "" {
		lang = "en"
	}
	queryParams.Set("lang", lang)

	// Optional: regenerate (defaults to false, so we can omit it)
	// queryParams.Set("regenerate", "false")

	fullURL := fmt.Sprintf("%s?%s", endpoint, queryParams.Encode())

	// Make GET request with token header (as per Gupshup Partner API documentation)
	client := &http.Client{Timeout: 15 * time.Second}

	// Try with 'token' header first (as per CreateApp pattern)
	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("token", token)

	// Log the EXACT request details for debugging (as per user's request)
	if c.Logger != nil {
		c.Logger.Info("🔍 Making embed signed link request - FULL DETAILS",
			zap.String("method", "GET"),
			zap.String("full_url", fullURL),
			zap.String("endpoint_base", endpoint),
			zap.String("query_params", queryParams.Encode()),
			zap.String("username_param", username),
			zap.String("lang_param", lang),
			zap.String("header_name", "token"),
			zap.Int("token_length", len(token)),
			zap.Bool("has_token", token != ""),
			zap.String("app_id", appID))
	}

	resp, err := client.Do(req)
	if err != nil {
		if c.Logger != nil {
			c.Logger.Error("❌ Failed to call embed signed link endpoint",
				zap.String("endpoint", fullURL),
				zap.Error(err))
		}
		return "", fmt.Errorf("failed to call embed signed link endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if c.Logger != nil {
		if resp.StatusCode >= 400 {
			c.Logger.Error("❌ Embed signed link request failed",
				zap.String("endpoint", fullURL),
				zap.Int("status", resp.StatusCode),
				zap.String("response", truncateString(string(body), 500)))
		} else {
			c.Logger.Info("✅ Embed signed link request succeeded",
				zap.String("endpoint", fullURL),
				zap.Int("status", resp.StatusCode),
				zap.String("response_preview", truncateString(string(body), 200)))
		}
	}

	// If 401, try Authorization: Bearer as fallback
	if resp.StatusCode == 401 {
		if c.Logger != nil {
			c.Logger.Debug("Token header returned 401, retrying with Authorization: Bearer")
		}

		req2, err2 := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
		if err2 == nil {
			req2.Header.Set("Authorization", "Bearer "+token)

			resp2, err2 := client.Do(req2)
			if err2 == nil {
				body2, _ := io.ReadAll(resp2.Body)
				resp2.Body.Close()

				if resp2.StatusCode == 200 || resp2.StatusCode == 201 {
					body = body2
					resp.StatusCode = resp2.StatusCode
				} else if c.Logger != nil {
					c.Logger.Error("Authorization: Bearer also failed",
						zap.Int("status", resp2.StatusCode),
						zap.String("response", truncateString(string(body2), 300)))
				}
			}
		}
	}

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		// Enhanced error logging for 400 errors to understand what's wrong
		if c.Logger != nil {
			c.Logger.Error("❌ Embed signed link API error - detailed diagnostics",
				zap.Int("status", resp.StatusCode),
				zap.String("status_text", resp.Status),
				zap.String("app_id", appID),
				zap.String("endpoint", fullURL),
				zap.String("full_response_body", truncateString(string(body), 1000)),
				zap.Bool("has_token", token != ""),
				zap.Int("token_length", len(token)))
		}
		return "", fmt.Errorf("gupshup Partner API error %d: %s. Response: %s", resp.StatusCode, resp.Status, truncateString(string(body), 500))
	}

	// Parse response - expected format: {"status": "success", "link": "https://..."}
	var response struct {
		Status  string `json:"status"`
		Link    string `json:"link"`
		Error   string `json:"error"`
		Message string `json:"message"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		// Try extracting link directly if JSON parsing fails
		if link := extractLinkFromResponse(string(body)); link != "" {
			if c.Logger != nil {
				c.Logger.Info("✅ Extracted embed signed link from response",
					zap.String("app_id", appID),
					zap.String("link", link))
			}
			return link, nil
		}
		return "", fmt.Errorf("failed to parse embed signed link response: %w. Response body: %s", err, truncateString(string(body), 300))
	}

	if response.Link != "" {
		if c.Logger != nil {
			c.Logger.Info("✅ Successfully generated embed signed link",
				zap.String("app_id", appID),
				zap.String("link", response.Link),
				zap.String("status", response.Status))
		}
		return response.Link, nil
	}

	if response.Error != "" || response.Message != "" {
		errorMsg := response.Error
		if errorMsg == "" {
			errorMsg = response.Message
		}
		return "", fmt.Errorf("gupshup Partner API returned error: %s. Full response: %s", errorMsg, string(body))
	}

	// Fallback: try extracting link using helper function
	if link := extractLinkFromResponse(string(body)); link != "" {
		if c.Logger != nil {
			c.Logger.Info("✅ Extracted embed signed link using fallback method",
				zap.String("app_id", appID),
				zap.String("link", link))
		}
		return link, nil
	}

	return "", fmt.Errorf("no link found in response. Full response: %s", truncateString(string(body), 300))
}

// VerifyPartnerCredentials attempts to verify Partner credentials are correct
// This is a best-effort verification - validates format and can test token generation
func (c *GupshupPartnerClient) VerifyPartnerCredentials(ctx context.Context, partnerID string) error {
	if c.ClientSecret == "" {
		return fmt.Errorf("client secret (partner API key) is not configured")
	}
	if c.PartnerEmail == "" {
		return fmt.Errorf("partner email is not configured")
	}
	if partnerID == "" {
		return fmt.Errorf("partner ID is required")
	}

	// Basic format validation
	if len(c.ClientSecret) < 10 {
		return fmt.Errorf("client secret appears too short - verify it's correct")
	}

	// Optionally try to get a token to verify credentials work
	if err := c.GetPartnerToken(ctx); err != nil {
		if c.Logger != nil {
			c.Logger.Warn("⚠️ Failed to verify credentials via token generation",
				zap.String("partner_id", partnerID),
				zap.Error(err))
		}
		return fmt.Errorf("credential verification failed: %w", err)
	}

	if c.Logger != nil {
		c.Logger.Info("✅ Partner credentials verified successfully",
			zap.String("partner_id", partnerID),
			zap.Int("client_secret_length", len(c.ClientSecret)))
	}

	return nil
}

// SetCallback configures webhook callback URL for a Gupshup Partner app
// Endpoint: POST /partner/app/set-callback
func (c *GupshupPartnerClient) SetCallback(ctx context.Context, appID, callbackURL string) error {
	// Ensure we have a valid token
	if err := c.ensureValidToken(ctx); err != nil {
		return fmt.Errorf("failed to get partner token: %w", err)
	}

	endpoint := fmt.Sprintf("%s/partner/app/set-callback", c.BaseURL)

	payload := map[string]interface{}{
		"appId":    appID,
		"callback": callbackURL,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Use Authorization header with generated PARTNER_TOKEN
	// Try Bearer format first (standard OAuth format)
	c.tokenMutex.RLock()
	token := c.Token
	c.tokenMutex.RUnlock()

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	if c.Logger != nil {
		c.Logger.Info("📤 Setting Gupshup webhook callback",
			zap.String("app_id", appID),
			zap.String("callback_url", callbackURL))
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// If Bearer format fails with 401, try X-Authorization format
	if resp.StatusCode == 401 {
		if c.Logger != nil {
			c.Logger.Warn("⚠️ Bearer token format failed with 401, trying X-Authorization header format",
				zap.String("app_id", appID))
		}

		// Create a new request with X-Authorization header
		req2, err2 := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonData))
		if err2 != nil {
			return fmt.Errorf("failed to create retry request: %w", err2)
		}

		req2.Header.Set("X-Authorization", token)
		req2.Header.Set("Content-Type", "application/json")

		resp2, err2 := client.Do(req2)
		if err2 != nil {
			return fmt.Errorf("API request failed on retry: %w", err2)
		}
		defer resp2.Body.Close()

		body2, _ := io.ReadAll(resp2.Body)

		if resp2.StatusCode >= 400 {
			if c.Logger != nil {
				c.Logger.Error("❌ Failed to set Gupshup webhook callback (tried both header formats)",
					zap.Int("status", resp2.StatusCode),
					zap.String("response", string(body2)),
					zap.String("app_id", appID))
			}
			return fmt.Errorf("set callback failed: status %d, body: %s", resp2.StatusCode, string(body2))
		}

		// Success with X-Authorization format
		if c.Logger != nil {
			c.Logger.Info("✅ Gupshup webhook callback configured (using X-Authorization header)",
				zap.String("app_id", appID),
				zap.String("callback_url", callbackURL))
		}
		return nil
	}

	if resp.StatusCode >= 400 {
		if c.Logger != nil {
			c.Logger.Error("❌ Failed to set Gupshup webhook callback",
				zap.Int("status", resp.StatusCode),
				zap.String("response", string(body)),
				zap.String("app_id", appID))
		}
		return fmt.Errorf("set callback failed: status %d, body: %s", resp.StatusCode, string(body))
	}

	if c.Logger != nil {
		c.Logger.Info("✅ Gupshup webhook callback configured",
			zap.String("app_id", appID),
			zap.String("callback_url", callbackURL))
	}

	return nil
}

// SendMessage sends a WhatsApp message via Gupshup Partner API
// Endpoint: POST /partner/app/{appId}/message
func (c *GupshupPartnerClient) SendMessage(ctx context.Context, appID, sourcePhone, destination, text string) error {
	// Ensure we have a valid token
	if err := c.ensureValidToken(ctx); err != nil {
		return fmt.Errorf("failed to get partner token: %w", err)
	}

	if appID == "" {
		return fmt.Errorf("app ID is required")
	}

	if sourcePhone == "" {
		return fmt.Errorf("source phone number is required")
	}

	if destination == "" {
		return fmt.Errorf("destination phone number is required")
	}

	if text == "" {
		return fmt.Errorf("message text is required")
	}

	endpoint := fmt.Sprintf("%s/partner/app/%s/message", c.BaseURL, appID)

	payload := map[string]interface{}{
		"channel":     "whatsapp",
		"source":      sourcePhone, // Tenant's WhatsApp number
		"destination": destination, // Customer's phone number
		"message": map[string]interface{}{
			"type": "text",
			"text": text,
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Use Authorization header with generated PARTNER_TOKEN
	// Try Bearer format first (standard OAuth format)
	c.tokenMutex.RLock()
	token := c.Token
	c.tokenMutex.RUnlock()

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	if c.Logger != nil {
		c.Logger.Info("📤 Sending WhatsApp message via Gupshup Partner API",
			zap.String("app_id", appID),
			zap.String("source", sourcePhone),
			zap.String("destination", destination),
			zap.String("message_preview", truncateString(text, 50)))
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// If Bearer format fails with 401, try X-Authorization format
	if resp.StatusCode == 401 {
		if c.Logger != nil {
			c.Logger.Warn("⚠️ Bearer token format failed with 401, trying X-Authorization header format",
				zap.String("app_id", appID))
		}

		// Create a new request with X-Authorization header
		req2, err2 := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonData))
		if err2 != nil {
			return fmt.Errorf("failed to create retry request: %w", err2)
		}

		req2.Header.Set("X-Authorization", token)
		req2.Header.Set("Content-Type", "application/json")

		resp2, err2 := client.Do(req2)
		if err2 != nil {
			return fmt.Errorf("API request failed on retry: %w", err2)
		}
		defer resp2.Body.Close()

		body2, _ := io.ReadAll(resp2.Body)

		if resp2.StatusCode >= 400 {
			if c.Logger != nil {
				c.Logger.Error("❌ Gupshup Partner API send message failed (tried both header formats)",
					zap.Int("status", resp2.StatusCode),
					zap.String("response", string(body2)),
					zap.String("app_id", appID),
					zap.String("destination", destination))
			}

			// Try to parse error response
			var apiErr struct {
				Status string `json:"status"`
				Error  string `json:"error"`
			}
			if err := json.Unmarshal(body2, &apiErr); err == nil && apiErr.Error != "" {
				return fmt.Errorf("gupshup Partner API error %d: %s", resp2.StatusCode, apiErr.Error)
			}

			return fmt.Errorf("gupshup Partner API error %d: %s", resp2.StatusCode, string(body2))
		}

		// Success with X-Authorization format
		if c.Logger != nil {
			c.Logger.Info("✅ WhatsApp message sent via Gupshup Partner API (using X-Authorization header)",
				zap.String("app_id", appID),
				zap.String("destination", destination))
		}
		return nil
	}

	if resp.StatusCode >= 400 {
		if c.Logger != nil {
			c.Logger.Error("❌ Gupshup Partner API send message failed",
				zap.Int("status", resp.StatusCode),
				zap.String("response", string(body)),
				zap.String("app_id", appID),
				zap.String("destination", destination))
		}

		// Try to parse error response
		var apiErr struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		}
		if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error != "" {
			return fmt.Errorf("gupshup Partner API error %d: %s", resp.StatusCode, apiErr.Error)
		}

		return fmt.Errorf("gupshup Partner API error %d: %s", resp.StatusCode, string(body))
	}

	if c.Logger != nil {
		c.Logger.Info("✅ WhatsApp message sent via Gupshup Partner API",
			zap.String("app_id", appID),
			zap.String("destination", destination))
	}

	return nil
}
