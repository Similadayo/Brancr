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
	"time"

	"go.uber.org/zap"
)

const (
	// GupshupBaseURL is the base URL for Gupshup WhatsApp Business API
	GupshupBaseURL = "https://api.gupshup.io/sm/api/v1"
)

// GupshupClient handles API requests to Gupshup WhatsApp Business API
type GupshupClient struct {
	APIKey       string
	AppName      string
	SourceNumber string
	Logger       *zap.Logger
}

// NewGupshupClient creates a new Gupshup API client
func NewGupshupClient(apiKey, appName, sourceNumber string, logger *zap.Logger) *GupshupClient {
	return &GupshupClient{
		APIKey:       apiKey,
		AppName:      appName,
		SourceNumber: sourceNumber,
		Logger:       logger,
	}
}

// GupshupSendMessageRequest represents the request payload for sending a message via Gupshup
// Gupshup typically uses form data with these fields:
// - channel: whatsapp
// - source: source phone number
// - destination: recipient phone number
// - message: message content
// - appName: app name
// - apiKey: API key (in query params or headers)
type GupshupSendMessageRequest struct {
	Channel     string `json:"channel"`     // "whatsapp"
	Source      string `json:"source"`      // Source phone number (e.g., "1234567890")
	Destination string `json:"destination"` // Recipient phone number (e.g., "2347039841392")
	Message     string `json:"message"`     // Message content
	AppName     string `json:"appName"`     // App name
}

// GupshupSendMessageResponse represents the response from sending a message
type GupshupSendMessageResponse struct {
	Status   string `json:"status"`   // "success" or "error"
	MessageID string `json:"messageId,omitempty"`
	Error    string `json:"error,omitempty"`
}

// SendMessage sends a WhatsApp message via Gupshup
// to is the recipient phone number (e.g., "+2347039841392" or "2347039841392")
// message is the text content to send
func (c *GupshupClient) SendMessage(ctx context.Context, to string, message string) (*GupshupSendMessageResponse, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("Gupshup API key not configured")
	}

	if c.AppName == "" {
		return nil, fmt.Errorf("Gupshup app name not configured")
	}

	if c.SourceNumber == "" {
		return nil, fmt.Errorf("Gupshup source number not configured - please complete 'Go Live' in Gupshup dashboard to connect your WhatsApp phone number")
	}

	if to == "" {
		return nil, fmt.Errorf("recipient phone number is required")
	}

	if message == "" {
		return nil, fmt.Errorf("message text is required")
	}

	// Normalize phone number (remove + if present, ensure proper format)
	destination := strings.TrimPrefix(to, "+")
	if !strings.HasPrefix(destination, c.SourceNumber[:2]) && len(destination) < 10 {
		// If number doesn't have country code, assume same country as source
		destination = c.SourceNumber[:len(c.SourceNumber)-len(destination)] + destination
	}

	// Normalize source number
	source := strings.TrimPrefix(c.SourceNumber, "+")

	// Gupshup API endpoint
	endpoint := fmt.Sprintf("%s/msg", GupshupBaseURL)

	// Prepare form data payload
	// Gupshup typically accepts form data
	formData := url.Values{}
	formData.Set("channel", "whatsapp")
	formData.Set("source", source)
	formData.Set("destination", destination)
	formData.Set("message", message)
	formData.Set("appName", c.AppName)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("apikey", c.APIKey) // Gupshup typically uses apikey header or query param

	// Add API key as query parameter (alternative method - Gupshup may use this)
	req.URL.RawQuery = url.Values{"apikey": {c.APIKey}}.Encode()

	if c.Logger != nil {
		c.Logger.Info("📤 Sending WhatsApp message via Gupshup",
			zap.String("source", source),
			zap.String("destination", destination),
			zap.String("app_name", c.AppName),
			zap.String("message_preview", truncateString(message, 50)))
	}

	// Execute request
	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// Handle error responses
	if resp.StatusCode >= 400 {
		if c.Logger != nil {
			c.Logger.Error("❌ Gupshup API error",
				zap.Int("status", resp.StatusCode),
				zap.String("response", string(body)),
				zap.String("source", source),
				zap.String("destination", destination))
		}

		// Try to parse error response
		var apiErr struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		}
		if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error != "" {
			return nil, fmt.Errorf("Gupshup API error %d: %s", resp.StatusCode, apiErr.Error)
		}

		return nil, fmt.Errorf("Gupshup API error %d: %s", resp.StatusCode, string(body))
	}

	// Parse success response
	var response GupshupSendMessageResponse
	if err := json.Unmarshal(body, &response); err != nil {
		// Some responses might be plain text or different format
		// Try to extract useful information
		responseBody := string(body)
		if strings.Contains(strings.ToLower(responseBody), "success") || strings.Contains(strings.ToLower(responseBody), "sent") {
			response.Status = "success"
			// Try to extract message ID if present
			if strings.Contains(responseBody, "messageId") {
				// Attempt to parse JSON even if partially
				var rawResponse map[string]interface{}
				if err2 := json.Unmarshal(body, &rawResponse); err2 == nil {
					if msgID, ok := rawResponse["messageId"].(string); ok {
						response.MessageID = msgID
					}
				}
			}
		} else {
			return nil, fmt.Errorf("failed to parse response: %w (body: %s)", err, responseBody)
		}
	}

	if c.Logger != nil {
		c.Logger.Info("✅ WhatsApp message sent via Gupshup",
			zap.String("source", source),
			zap.String("destination", destination),
			zap.String("message_id", response.MessageID),
			zap.String("status", response.Status))
	}

	return &response, nil
}

// Alternative: Send message using JSON format (if Gupshup supports it)
// Some BSPs support both form data and JSON
func (c *GupshupClient) SendMessageJSON(ctx context.Context, to string, message string) (*GupshupSendMessageResponse, error) {
	if c.APIKey == "" || c.AppName == "" || c.SourceNumber == "" {
		return nil, fmt.Errorf("Gupshup credentials not fully configured")
	}

	if to == "" || message == "" {
		return nil, fmt.Errorf("recipient and message are required")
	}

	// Normalize phone numbers
	destination := strings.TrimPrefix(to, "+")
	source := strings.TrimPrefix(c.SourceNumber, "+")

	// Prepare JSON payload
	payload := map[string]interface{}{
		"channel":     "whatsapp",
		"source":      source,
		"destination": destination,
		"message":     message,
		"appName":     c.AppName,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	// API endpoint
	endpoint := fmt.Sprintf("%s/msg?apikey=%s", GupshupBaseURL, url.QueryEscape(c.APIKey))

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	if c.Logger != nil {
		c.Logger.Info("📤 Sending WhatsApp message via Gupshup (JSON)",
			zap.String("source", source),
			zap.String("destination", destination),
			zap.String("message_preview", truncateString(message, 50)))
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		if c.Logger != nil {
			c.Logger.Error("❌ Gupshup API error",
				zap.Int("status", resp.StatusCode),
				zap.String("response", string(body)))
		}
		return nil, fmt.Errorf("Gupshup API error %d: %s", resp.StatusCode, string(body))
	}

	var response GupshupSendMessageResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w (body: %s)", err, string(body))
	}

	if c.Logger != nil {
		c.Logger.Info("✅ WhatsApp message sent via Gupshup (JSON)",
			zap.String("message_id", response.MessageID),
			zap.String("status", response.Status))
	}

	return &response, nil
}

