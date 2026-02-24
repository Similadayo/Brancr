package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/similadayo/brancr/internal/ai_v4/types"
)

// Client is an HTTP client for the brancr-ai Python service
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new LLM client
func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = "http://localhost:8001" // Default Python AI service URL
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ClassifyIntent classifies customer intent
func (c *Client) ClassifyIntent(message string, contextStr string) (*types.IntentResult, error) {
	payload := map[string]string{
		"message": message,
		"context": contextStr,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal error: %w", err)
	}

	resp, err := c.httpClient.Post(c.baseURL+"/api/classify-intent", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result struct {
		Intent     string         `json:"intent"`
		Confidence float64        `json:"confidence"`
		Entities   map[string]any `json:"entities,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode error: %w", err)
	}

	return &types.IntentResult{
		Primary:    mapStringToIntent(result.Intent),
		Confidence: result.Confidence,
		Entities:   result.Entities,
	}, nil
}

// Generate generates text using the LLM
func (c *Client) Generate(prompt string) (string, error) {
	payload := map[string]string{
		"prompt": prompt,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal error: %w", err)
	}

	resp, err := c.httpClient.Post(c.baseURL+"/api/generate", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result struct {
		Text string `json:"text"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode error: %w", err)
	}

	return result.Text, nil
}

// ExtractEntities extracts entities from a message
func (c *Client) ExtractEntities(message string) (map[string]any, error) {
	payload := map[string]string{
		"message": message,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal error: %w", err)
	}

	resp, err := c.httpClient.Post(c.baseURL+"/api/extract-entities", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode error: %w", err)
	}

	return result, nil
}

// GenerateWithContext generates a response with full context
func (c *Client) GenerateWithContext(ctx context.Context, req types.GenerateRequest) (*types.GenerateResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal error: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v4/generate", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result types.GenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode error: %w", err)
	}

	return &result, nil
}

// DetectEmotion calls the /api/v4/emotion endpoint
func (c *Client) DetectEmotion(ctx context.Context, message string, contextStr string) (*types.EmotionResponse, error) {
	body, err := json.Marshal(types.EmotionRequest{
		Message: message,
		Context: contextStr,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal error: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v4/emotion", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result types.EmotionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode error: %w", err)
	}

	return &result, nil
}

// SplitMessage calls the /api/v4/split endpoint
func (c *Client) SplitMessage(ctx context.Context, message string, platform string, maxLength int) (*types.SplitResponse, error) {
	if maxLength == 0 {
		maxLength = 300
	}

	body, err := json.Marshal(types.SplitRequest{
		Message:   message,
		Platform:  platform,
		MaxLength: maxLength,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal error: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v4/split", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result types.SplitResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode error: %w", err)
	}

	return &result, nil
}

// Negotiate calls the /api/v4/negotiate endpoint
func (c *Client) Negotiate(ctx context.Context, req types.NegotiateRequest) (*types.NegotiateResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal error: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v4/negotiate", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result types.NegotiateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode error: %w", err)
	}

	return &result, nil
}

// InterpretSemantic calls the /api/semantic-interpret endpoint
func (c *Client) InterpretSemantic(ctx context.Context, message string, lastIntent string, businessType string) (*types.SemanticUnderstanding, error) {
	// Build payload matching Python SemanticInterpretInput
	payload := map[string]any{
		"message": message,
		"conversation_context": map[string]string{
			"last_intent":   lastIntent,
			"business_type": businessType,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal error: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/semantic-interpret", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result types.SemanticUnderstanding
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode error: %w", err)
	}

	return &result, nil
}

func mapStringToIntent(s string) types.Intent {
	switch s {
	case "greeting", "GREETING":
		return types.IntentGreeting
	case "inquiry", "INQUIRY", "PRODUCT_INQUIRY", "ITEM_INQUIRY":
		return types.IntentInquiry
	case "price_check", "PRICE_CHECK", "PRICE_INQUIRY", "REQUEST_PRICES":
		return types.IntentPriceCheck
	case "purchase", "PURCHASE", "PLACE_ORDER", "ADD_ITEM":
		return types.IntentPurchase
	case "order_status", "ORDER_STATUS", "CHECK_ORDER", "STATUS_INQUIRY":
		return types.IntentOrderStatus
	case "complaint", "COMPLAINT":
		return types.IntentComplaint
	case "refund", "REFUND", "CANCEL_ORDER":
		return types.IntentRefund
	case "negotiation", "NEGOTIATION", "NEGOTIATE_PRICE":
		return types.IntentNegotiation
	case "human_request", "HUMAN_REQUEST", "ESCALATE_TO_HUMAN":
		return types.IntentHumanRequest
	case "thank_you", "THANK_YOU", "AFFIRM":
		return types.IntentThankYou
	case "goodbye", "GOODBYE":
		return types.IntentGoodbye
	default:
		return types.IntentUnknown
	}
}
