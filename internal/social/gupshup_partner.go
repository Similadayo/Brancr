package social

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"go.uber.org/zap"
)

const GupshupPartnerBaseURL = "https://partner.gupshup.io"

// GupshupPartnerClient handles API requests to Gupshup Partner API
type GupshupPartnerClient struct {
	BaseURL       string
	PartnerAPIKey string // Static Partner API key from Partner Console
	Logger        *zap.Logger
}

// NewGupshupPartnerClient creates a new Gupshup Partner API client
func NewGupshupPartnerClient(baseURL, partnerAPIKey string, logger *zap.Logger) *GupshupPartnerClient {
	return &GupshupPartnerClient{
		BaseURL:       baseURL,
		PartnerAPIKey: partnerAPIKey,
		Logger:        logger,
	}
}

// GenerateOnboardingLink generates Partner onboarding redirect link for tenant
// This is the ONLY correct method for WhatsApp Partner app onboarding
// Returns the link and any validation errors
func (c *GupshupPartnerClient) GenerateOnboardingLink(partnerID, redirectURL string) (string, error) {
	// Validate required fields
	if partnerID == "" {
		return "", fmt.Errorf("partner ID is required")
	}
	if redirectURL == "" {
		return "", fmt.Errorf("redirect URL is required")
	}
	if c.PartnerAPIKey == "" {
		return "", fmt.Errorf("gupshup Partner API key is not configured")
	}

	params := url.Values{}
	params.Set("partnerId", partnerID)
	params.Set("redirect", redirectURL)
	params.Set("clientSecret", c.PartnerAPIKey) // REQUIRED: Validates partner auth in query params

	link := fmt.Sprintf("%s/partner/app/onboard?%s", c.BaseURL, params.Encode())

	if c.Logger != nil {
		c.Logger.Info("🔗 Generated Gupshup onboarding link",
			zap.String("partner_id", partnerID),
			zap.String("redirect_url", redirectURL),
			zap.String("link_preview", truncateString(link, 100)),
			zap.Bool("has_api_key", c.PartnerAPIKey != ""))
	}

	return link, nil
}

// VerifyPartnerCredentials attempts to verify Partner API Key and Partner ID are correct
// This is a best-effort verification - there's no dedicated endpoint, so we try a simple API call
func (c *GupshupPartnerClient) VerifyPartnerCredentials(ctx context.Context, partnerID string) error {
	if c.PartnerAPIKey == "" {
		return fmt.Errorf("Partner API key is not configured")
	}
	if partnerID == "" {
		return fmt.Errorf("Partner ID is required")
	}

	// Try to get partner info or list apps (if such endpoint exists)
	// Since there's no direct verification endpoint, we'll validate the link format
	// and check if we can make a basic authenticated request
	
	// For now, just validate the credentials are non-empty and have expected format
	if len(c.PartnerAPIKey) < 10 {
		return fmt.Errorf("Partner API key appears too short - verify it's correct")
	}
	
	if c.Logger != nil {
		c.Logger.Info("⚠️ Partner credential verification is limited - credentials will be validated when used",
			zap.String("partner_id", partnerID),
			zap.Int("api_key_length", len(c.PartnerAPIKey)))
	}

	return nil
}

// SetCallback configures webhook callback URL for a Gupshup Partner app
// Endpoint: POST /partner/app/set-callback
func (c *GupshupPartnerClient) SetCallback(ctx context.Context, appID, callbackURL string) error {
	if c.PartnerAPIKey == "" {
		return fmt.Errorf("Gupshup Partner API key not configured")
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

	// Use X-Authorization header with Partner API key
	req.Header.Set("X-Authorization", c.PartnerAPIKey)
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
	if c.PartnerAPIKey == "" {
		return fmt.Errorf("Gupshup Partner API key not configured")
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

	// Use X-Authorization header with Partner API key
	req.Header.Set("X-Authorization", c.PartnerAPIKey)
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
			return fmt.Errorf("Gupshup Partner API error %d: %s", resp.StatusCode, apiErr.Error)
		}

		return fmt.Errorf("Gupshup Partner API error %d: %s", resp.StatusCode, string(body))
	}

	if c.Logger != nil {
		c.Logger.Info("✅ WhatsApp message sent via Gupshup Partner API",
			zap.String("app_id", appID),
			zap.String("destination", destination))
	}

	return nil
}
