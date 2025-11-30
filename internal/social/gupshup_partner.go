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
func NewGupshupPartnerClient(baseURL, clientSecret, partnerEmail string, logger *zap.Logger) *GupshupPartnerClient {
	return &GupshupPartnerClient{
		BaseURL:      baseURL,
		ClientSecret: clientSecret,
		PartnerEmail: partnerEmail,
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

	// Try to generate via authenticated API call using PARTNER_TOKEN
	// This is the recommended method per Gupshup's recent API updates
	if err := c.ensureValidToken(ctx); err == nil {
		// Try multiple possible API endpoints for onboarding link generation
		endpoints := []string{
			fmt.Sprintf("%s/partner/app/onboard-link", c.BaseURL),
			fmt.Sprintf("%s/partner/app/generate-onboarding-link", c.BaseURL),
			fmt.Sprintf("%s/partner/app/onboarding-link", c.BaseURL),
		}

		c.tokenMutex.RLock()
		token := c.Token
		c.tokenMutex.RUnlock()

		payload := map[string]interface{}{
			"partnerId": partnerID,
			"redirect":  redirectURL,
		}

		jsonData, _ := json.Marshal(payload)

		for _, endpoint := range endpoints {
			req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonData))
			if err != nil {
				continue
			}

			req.Header.Set("Authorization", token)
			req.Header.Set("Content-Type", "application/json")

			client := &http.Client{Timeout: 15 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				continue
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)

			// Log response for debugging
			if c.Logger != nil {
				c.Logger.Debug("Tried onboarding link API endpoint",
					zap.String("endpoint", endpoint),
					zap.Int("status", resp.StatusCode),
					zap.String("response_preview", truncateString(string(body), 200)))
			}

			if resp.StatusCode == 200 || resp.StatusCode == 201 {
				var response struct {
					OnboardingURL  string `json:"onboardingUrl"`
					OnboardingLink string `json:"onboarding_link"`
					URL            string `json:"url"`
					Link           string `json:"link"`
					OnboardURL     string `json:"onboardUrl"`
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
					if link != "" {
						if c.Logger != nil {
							c.Logger.Info("✅ Generated Gupshup onboarding link via API",
								zap.String("partner_id", partnerID),
								zap.String("redirect_url", redirectURL),
								zap.String("endpoint", endpoint))
						}
						return link, nil
					}
				}
			}
		}

		if c.Logger != nil {
			c.Logger.Warn("⚠️ Failed to generate onboarding link via API - using fallback method",
				zap.String("partner_id", partnerID))
		}
	}

	// Fallback: Construct URL with clientSecret in query params
	// NOTE: This method may fail if Gupshup changed validation - try token-based first
	// If this fails, the clientSecret might need to match exactly what's in Partner Portal
	if c.ClientSecret == "" {
		return "", fmt.Errorf("gupshup Partner API key (client secret) is not configured")
	}

	// IMPORTANT: Based on Gupshup's recent changes, the onboarding link might need
	// to be generated via API call using PARTNER_TOKEN, not clientSecret in URL
	// However, we'll try the URL method as fallback for backward compatibility

	// URL encode parameters properly - url.Values handles this automatically
	params := url.Values{}
	params.Set("partnerId", partnerID)
	params.Set("redirect", redirectURL)
	params.Set("clientSecret", c.ClientSecret) // This might be rejected by Gupshup

	link := fmt.Sprintf("%s/partner/app/onboard?%s", c.BaseURL, params.Encode())

	// Log warnings about potential issues
	if c.Logger != nil {
		// Check for common issues
		if len(c.ClientSecret) < 20 {
			c.Logger.Warn("⚠️ Client secret seems short - verify it's the full key from Partner Console",
				zap.Int("length", len(c.ClientSecret)))
		}
		if strings.Contains(c.ClientSecret, " ") {
			c.Logger.Warn("⚠️ Client secret contains spaces - this might cause issues")
		}

		c.Logger.Warn("⚠️ Using fallback onboarding link method with clientSecret in URL",
			zap.String("partner_id", partnerID),
			zap.String("note", "If you get 'Unauthorised access', the clientSecret might be invalid or Gupshup changed their validation"))
	}

	if c.Logger != nil {
		c.Logger.Info("🔗 Generated Gupshup onboarding link (fallback method - clientSecret in URL)",
			zap.String("partner_id", partnerID),
			zap.String("redirect_url", redirectURL),
			zap.String("link_preview", truncateString(link, 100)),
			zap.Bool("has_client_secret", c.ClientSecret != ""),
			zap.String("warning", "If link fails, verify clientSecret matches Partner Portal exactly"))
	}

	return link, nil
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
	c.tokenMutex.RLock()
	token := c.Token
	c.tokenMutex.RUnlock()

	req.Header.Set("Authorization", token)
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
	c.tokenMutex.RLock()
	token := c.Token
	c.tokenMutex.RUnlock()

	req.Header.Set("Authorization", token)
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
