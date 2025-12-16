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

const (
	// RespondIOBaseURL is the base URL for Respond.io API v2
	RespondIOBaseURL = "https://api.respond.io/v2"
)

// RespondIOClient handles API requests to Respond.io
type RespondIOClient struct {
	APIKey string
	Logger *zap.Logger
}

// NewRespondIOClient creates a new Respond.io API client
func NewRespondIOClient(apiKey string, logger *zap.Logger) *RespondIOClient {
	return &RespondIOClient{
		APIKey: apiKey,
		Logger: logger,
	}
}

// RespondIOSendMessageRequest represents the request payload for sending a message
type RespondIOSendMessageRequest struct {
	Text      string `json:"text"`
	ChannelID string `json:"channelId,omitempty"` // Optional: WhatsApp channel ID
}

// RespondIOSendMessageResponse represents the response from sending a message
type RespondIOSendMessageResponse struct {
	ID        string `json:"id"`
	MessageID string `json:"messageId,omitempty"`
	Status    string `json:"status,omitempty"`
}

// APIError represents an error response from Respond.io API
type APIError struct {
	Message string `json:"message"`
	Code    int    `json:"code,omitempty"`
	Type    string `json:"type,omitempty"`
}

// SendMessage sends a message to a contact via Respond.io
// identifier can be a phone number (e.g., "+1234567890"), email, or contact ID
func (c *RespondIOClient) SendMessage(ctx context.Context, identifier string, text string, channelID string) (*RespondIOSendMessageResponse, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("Respond.io API key not configured")
	}

	if identifier == "" {
		return nil, fmt.Errorf("contact identifier is required")
	}

	if text == "" {
		return nil, fmt.Errorf("message text is required")
	}

	// Prepare request payload
	payload := RespondIOSendMessageRequest{
		Text: text,
	}
	if channelID != "" {
		payload.ChannelID = channelID
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Construct API endpoint
	// identifier must be URL-encoded (phone numbers with + need encoding)
	// Use url.PathEscape to properly encode the phone number
	endpoint := fmt.Sprintf("%s/contact/%s/message", RespondIOBaseURL, url.PathEscape(identifier))

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.APIKey))
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	if c.Logger != nil {
		c.Logger.Info("📤 Sending message via Respond.io",
			zap.String("identifier", identifier),
			zap.String("endpoint", endpoint),
			zap.String("message_preview", truncateStringForRespondIO(text, 50)))
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// Handle error responses
	if resp.StatusCode >= 400 {
		var apiErr APIError
		if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Message != "" {
			return nil, fmt.Errorf("Respond.io API error %d: %s", resp.StatusCode, apiErr.Message)
		}
		return nil, fmt.Errorf("Respond.io API error %d: %s", resp.StatusCode, string(body))
	}

	// Parse success response
	var response RespondIOSendMessageResponse
	if err := json.Unmarshal(body, &response); err != nil {
		// Some responses might not have the expected structure
		// Try to extract message ID from different formats
		var rawResponse map[string]interface{}
		if err2 := json.Unmarshal(body, &rawResponse); err2 == nil {
			if id, ok := rawResponse["id"].(string); ok {
				response.ID = id
			}
			if msgID, ok := rawResponse["messageId"].(string); ok {
				response.MessageID = msgID
			}
		} else {
			return nil, fmt.Errorf("failed to parse response: %w (body: %s)", err, string(body))
		}
	}

	if c.Logger != nil {
		c.Logger.Info("✅ Message sent via Respond.io",
			zap.String("identifier", identifier),
			zap.String("message_id", response.ID),
			zap.String("status", response.Status))
	}

	return &response, nil
}

// GetContact retrieves contact information by identifier
func (c *RespondIOClient) GetContact(ctx context.Context, identifier string) (map[string]interface{}, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("Respond.io API key not configured")
	}

	// URL-encode identifier (phone numbers with + need encoding)
	endpoint := fmt.Sprintf("%s/contact/%s", RespondIOBaseURL, url.PathEscape(identifier))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.APIKey))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Respond.io API error %d: %s", resp.StatusCode, string(body))
	}

	var contact map[string]interface{}
	if err := json.Unmarshal(body, &contact); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return contact, nil
}

// CreateOrUpdateContact creates or updates a contact
func (c *RespondIOClient) CreateOrUpdateContact(ctx context.Context, identifier string, contactData map[string]interface{}) error {
	if c.APIKey == "" {
		return fmt.Errorf("Respond.io API key not configured")
	}

	// URL-encode identifier (phone numbers with + need encoding)
	endpoint := fmt.Sprintf("%s/contact/create_or_update/%s", RespondIOBaseURL, url.PathEscape(identifier))

	jsonData, err := json.Marshal(contactData)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.APIKey))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("Respond.io API error %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// ListChannels lists all channels in the workspace
func (c *RespondIOClient) ListChannels(ctx context.Context) ([]map[string]interface{}, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("Respond.io API key not configured")
	}

	endpoint := fmt.Sprintf("%s/space/channel", RespondIOBaseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.APIKey))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	// Always log the raw response for debugging
	if c.Logger != nil {
		c.Logger.Info("Respond.io list channels API response",
			zap.Int("status", resp.StatusCode),
			zap.String("endpoint", endpoint),
			zap.Int("body_length", len(bodyStr)),
			zap.String("response_preview", truncateStringForRespondIO(bodyStr, 1000)))
	}

	if resp.StatusCode >= 400 {
		// Log detailed error for debugging
		if c.Logger != nil {
			c.Logger.Error("Respond.io list channels API error",
				zap.Int("status", resp.StatusCode),
				zap.String("response", bodyStr),
				zap.String("endpoint", endpoint))
		}
		return nil, fmt.Errorf("Respond.io API error %d: %s", resp.StatusCode, bodyStr)
	}

	// Try parsing as standard response structure
	var response struct {
		Data     []map[string]interface{} `json:"data"`
		Channels []map[string]interface{} `json:"channels"` // Alternative field name
		Results  []map[string]interface{} `json:"results"`  // Another possible field name
	}

	if err := json.Unmarshal(body, &response); err != nil {
		// Maybe it's a different structure - try direct array
		var directArray []map[string]interface{}
		if err2 := json.Unmarshal(body, &directArray); err2 == nil {
			if c.Logger != nil {
				c.Logger.Info("Respond.io channels returned as direct array",
					zap.Int("count", len(directArray)),
					zap.String("raw_response", truncateStringForRespondIO(bodyStr, 500)))
			}
			return directArray, nil
		}

		// Log raw response for debugging
		if c.Logger != nil {
			c.Logger.Warn("Failed to parse Respond.io channels response",
				zap.Error(err),
				zap.String("response_preview", truncateStringForRespondIO(bodyStr, 1000)),
				zap.String("full_response", bodyStr))
		}
		return nil, fmt.Errorf("failed to parse channels response: %w (response: %s)", err, truncateStringForRespondIO(bodyStr, 500))
	}

	// Check multiple possible response formats
	var channels []map[string]interface{}
	if len(response.Data) > 0 {
		channels = response.Data
	} else if len(response.Channels) > 0 {
		channels = response.Channels
	} else if len(response.Results) > 0 {
		channels = response.Results
	}

	if c.Logger != nil {
		if len(channels) == 0 {
			c.Logger.Warn("⚠️ Respond.io API returned 0 channels",
				zap.String("endpoint", endpoint),
				zap.String("response_structure", truncateStringForRespondIO(bodyStr, 1000)),
				zap.String("note", "Channel exists in dashboard (ID: 440617) but API returned empty. This could be a permissions issue or the API endpoint might need space ID."))
		} else {
			c.Logger.Info("✅ Successfully fetched Respond.io channels",
				zap.Int("count", len(channels)))
		}
	}

	return channels, nil
}

// truncateStringForRespondIO truncates a string to a maximum length
// Note: This is a duplicate of truncateString in meta_webhook.go, but we keep it here for now
func truncateStringForRespondIO(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
