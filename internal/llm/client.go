package llm

// Package llm is the low-level LLM transport client (OpenAI chat/completions + vision).
//
// It must NOT contain:
// - business logic
// - decision logic
// - state awareness
// - action selection
//
// Higher-level reasoning, intent, and domain-specific generation live in their own
// packages and use this client as a dependency.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Model tier constants for cost optimization
const (
	ModelNano = "gpt-5-nano" // Cheapest: $0.05/$0.40 per 1M tokens - simple tasks
	ModelMini = "gpt-5-mini" // Budget: $0.25/$2.00 per 1M tokens - most tasks
	ModelFull = "gpt-5"      // Premium: $1.25/$10.00 per 1M tokens - complex tasks
)

// ModerationError represents an AI API moderation error
type ModerationError struct {
	Message    string
	Reasons    []string
	StatusCode int
}

func (e *ModerationError) Error() string {
	return fmt.Sprintf("AI moderation error: %s (reasons: %v)", e.Message, e.Reasons)
}

// IsModerationError checks if an error is a moderation error
func IsModerationError(err error) bool {
	_, ok := err.(*ModerationError)
	return ok
}

type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // Can be string or []ContentPart for vision
}

// ContentPart represents a part of message content (text or image)
type ContentPart struct {
	Type     string `json:"type"` // "text" or "image_url"
	Text     string `json:"text,omitempty"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url,omitempty"`
}

// TokenUsage represents token usage from an AI API call
type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// GenerateResult contains both the generated text and token usage
type GenerateResult struct {
	Content    string
	TokenUsage *TokenUsage
}

// Client is the low-level LLM transport client.
type Client struct {
	APIKey         string
	BaseURL        string
	Model          string
	IsOpenRouter   bool // Track if using OpenRouter vs OpenAI direct
	CircuitBreaker interface {
		Execute(ctx context.Context, fn func() error) error
		IsOpen() bool
	}
}

// NewClient builds a transport client from environment configuration.
func NewClient() *Client {
	// OpenAI Direct API (OpenRouter removed)
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		key = "" // Will fail at runtime with clear error
	}

	base := os.Getenv("OPENAI_API_BASE")
	if base == "" {
		base = "https://api.openai.com/v1"
	}

	model := os.Getenv("AI_MODEL")
	if model == "" {
		model = "gpt-5-nano" // Default: cheapest model for cost efficiency
	} else {
		// Remove "openai/" prefix if present (for backward compatibility)
		if len(model) > 7 && model[:7] == "openai/" {
			model = model[7:]
		}
	}

	return &Client{
		APIKey:       key,
		BaseURL:      base,
		Model:        model,
		IsOpenRouter: false, // Always false now - OpenAI only
	}
}

// calculateHTTPTimeout calculates the HTTP client timeout from the context deadline
// It respects the context deadline while ensuring reasonable bounds for safety
// The HTTP client timeout will always be less than or equal to the context deadline
func (c *Client) calculateHTTPTimeout(ctx context.Context) time.Duration {
	const (
		defaultTimeout = 120 * time.Second // Default timeout when no deadline (increased for batch operations)
		minTimeout     = 60 * time.Second  // Minimum timeout for safety - increased from 10s for batch parsing
		maxTimeout     = 8 * time.Minute   // Maximum timeout to prevent excessive waits (increased for parsing)
		bufferTime     = 2 * time.Second   // Buffer to ensure context deadline is respected
	)

	// Check if context has a deadline
	deadline, ok := ctx.Deadline()
	if !ok {
		// No deadline, use default timeout
		return defaultTimeout
	}

	// Calculate remaining time until deadline
	remaining := time.Until(deadline)

	// If deadline has already passed, return 0 to fail immediately
	if remaining <= 0 {
		return 0
	}

	// If deadline is too close (less than buffer), use a small fraction to fail fast
	if remaining <= bufferTime {
		return remaining / 2
	}

	// Calculate timeout with buffer to ensure we timeout before context deadline
	// This is the maximum timeout we can use without exceeding the deadline
	maxAllowedTimeout := remaining - bufferTime

	// Determine the base timeout: use minimum if we have enough time, otherwise use maxAllowedTimeout
	var timeout time.Duration
	// Check if we have enough remaining time to enforce minimum timeout
	if remaining >= minTimeout+bufferTime {
		// We have enough time - use at least minTimeout, but not more than maxAllowedTimeout
		timeout = minTimeout
		if timeout > maxAllowedTimeout {
			timeout = maxAllowedTimeout
		}
	} else {
		// Not enough time - use maxAllowedTimeout (which will be < minTimeout)
		timeout = maxAllowedTimeout
	}

	// Apply maximum bound
	if timeout > maxTimeout {
		timeout = maxTimeout
	}

	// Final safety check: never exceed the maximum allowed timeout
	// This ensures we always respect the context deadline
	if timeout > maxAllowedTimeout {
		timeout = maxAllowedTimeout
	}

	return timeout
}

// Generate calls the LLM to get a reply
func (c *Client) Generate(ctx context.Context, messages []Message) (string, error) {
	// Convert messages to API format (handle both text and vision)
	apiMessages := make([]map[string]any, len(messages))
	for i, msg := range messages {
		apiMsg := map[string]any{"role": msg.Role}

		// Handle content - can be string or ContentPart array
		switch content := msg.Content.(type) {
		case string:
			apiMsg["content"] = content
		case []ContentPart:
			apiMsg["content"] = content
		default:
			// Convert string to ContentPart array if needed
			if str, ok := msg.Content.(string); ok {
				apiMsg["content"] = str
			}
		}
		apiMessages[i] = apiMsg
	}

	payload := map[string]any{
		"model":    c.Model,
		"messages": apiMessages,
	}
	body, _ := json.Marshal(payload)

	url := fmt.Sprintf("%s/chat/completions", c.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return "", fmt.Errorf("failed to build request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	// OpenAI API doesn't require additional headers

	// Calculate HTTP client timeout from context deadline, with reasonable bounds
	httpTimeout := c.calculateHTTPTimeout(ctx)

	// Use circuit breaker if available
	var resp *http.Response
	if c.CircuitBreaker != nil {
		err := c.CircuitBreaker.Execute(ctx, func() error {
			client := &http.Client{Timeout: httpTimeout}
			var httpErr error
			resp, httpErr = client.Do(req)
			if httpErr != nil {
				return fmt.Errorf("network error: %w", httpErr)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	} else {
		// Fallback without circuit breaker
		client := &http.Client{Timeout: httpTimeout}
		var httpErr error
		resp, httpErr = client.Do(req)
		if httpErr != nil {
			return "", fmt.Errorf("network error: %w", httpErr)
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)

		// Parse error response to detect moderation issues
		var apiError struct {
			Error struct {
				Message  string `json:"message"`
				Code     int    `json:"code"`
				Type     string `json:"type"`
				Metadata *struct {
					Reasons      []string `json:"reasons"`
					FlaggedInput string   `json:"flagged_input"`
				} `json:"metadata"`
			} `json:"error"`
		}

		// Try to parse as JSON error
		if err := json.Unmarshal(body, &apiError); err == nil {
			// Check if it's a moderation error
			if apiError.Error.Metadata != nil && len(apiError.Error.Metadata.Reasons) > 0 {
				return "", &ModerationError{
					Message:    apiError.Error.Message,
					Reasons:    apiError.Error.Metadata.Reasons,
					StatusCode: resp.StatusCode,
				}
			}

			// Provide helpful error messages for common issues
			if resp.StatusCode == 404 {
				return "", fmt.Errorf("AI API error: Model '%s' not found (404). The model may be invalid, deprecated, or unavailable. Please check your AI_MODEL environment variable. Error: %s", c.Model, apiError.Error.Message)
			} else if resp.StatusCode == 401 || resp.StatusCode == 403 {
				return "", fmt.Errorf("AI API error: Authentication failed (%d). Please check your OPENAI_API_KEY. Error: %s", resp.StatusCode, apiError.Error.Message)
			}

			return "", fmt.Errorf("AI API error: %s (Status: %d, Type: %s). Model: %s, URL: %s", apiError.Error.Message, resp.StatusCode, apiError.Error.Type, c.Model, url)
		}

		// If we can't parse the error, return raw response
		return "", fmt.Errorf("AI API error: %s. Model: %s, URL: %s, Response: %s", resp.Status, c.Model, url, string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage,omitempty"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode error: %w", err)
	}
	if result.Error != nil {
		return "", fmt.Errorf("AI API error: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no AI response received")
	}

	content := result.Choices[0].Message.Content

	// Store token usage in context for cost tracking (backward compatibility)
	if result.Usage != nil {
		ctx = context.WithValue(ctx, "ai_usage", result.Usage)
	}

	return content, nil
}

// GenerateWithUsage calls the LLM and returns both content and token usage
func (c *Client) GenerateWithUsage(ctx context.Context, messages []Message) (*GenerateResult, error) {
	// Convert messages to API format (handle both text and vision)
	apiMessages := make([]map[string]any, len(messages))
	for i, msg := range messages {
		apiMsg := map[string]any{"role": msg.Role}

		// Handle content - can be string or ContentPart array
		switch content := msg.Content.(type) {
		case string:
			apiMsg["content"] = content
		case []ContentPart:
			apiMsg["content"] = content
		default:
			// Convert string to ContentPart array if needed
			if str, ok := msg.Content.(string); ok {
				apiMsg["content"] = str
			}
		}
		apiMessages[i] = apiMsg
	}

	payload := map[string]any{
		"model":    c.Model,
		"messages": apiMessages,
	}
	body, _ := json.Marshal(payload)

	url := fmt.Sprintf("%s/chat/completions", c.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	// Calculate HTTP client timeout from context deadline
	httpTimeout := c.calculateHTTPTimeout(ctx)

	// Use circuit breaker if available
	var resp *http.Response
	if c.CircuitBreaker != nil {
		err := c.CircuitBreaker.Execute(ctx, func() error {
			client := &http.Client{Timeout: httpTimeout}
			var httpErr error
			resp, httpErr = client.Do(req)
			if httpErr != nil {
				return fmt.Errorf("network error: %w", httpErr)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		// Fallback without circuit breaker
		client := &http.Client{Timeout: httpTimeout}
		var httpErr error
		resp, httpErr = client.Do(req)
		if httpErr != nil {
			return nil, fmt.Errorf("network error: %w", httpErr)
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)

		// Parse error response to detect moderation issues
		var apiError struct {
			Error struct {
				Message  string `json:"message"`
				Code     int    `json:"code"`
				Type     string `json:"type"`
				Metadata *struct {
					Reasons      []string `json:"reasons"`
					FlaggedInput string   `json:"flagged_input"`
				} `json:"metadata"`
			} `json:"error"`
		}

		// Try to parse as JSON error
		if err := json.Unmarshal(body, &apiError); err == nil {
			// Check if it's a moderation error
			if apiError.Error.Metadata != nil && len(apiError.Error.Metadata.Reasons) > 0 {
				return nil, &ModerationError{
					Message:    apiError.Error.Message,
					Reasons:    apiError.Error.Metadata.Reasons,
					StatusCode: resp.StatusCode,
				}
			}

			// Provide helpful error messages for common issues
			if resp.StatusCode == 404 {
				return nil, fmt.Errorf("AI API error: Model '%s' not found (404). The model may be invalid, deprecated, or unavailable. Please check your AI_MODEL environment variable. Error: %s", c.Model, apiError.Error.Message)
			} else if resp.StatusCode == 401 || resp.StatusCode == 403 {
				return nil, fmt.Errorf("AI API error: Authentication failed (%d). Please check your OPENAI_API_KEY. Error: %s", resp.StatusCode, apiError.Error.Message)
			}

			return nil, fmt.Errorf("AI API error: %s (Status: %d, Type: %s). Model: %s, URL: %s", apiError.Error.Message, resp.StatusCode, apiError.Error.Type, c.Model, url)
		}

		// If we can't parse the error, return raw response
		return nil, fmt.Errorf("AI API error: %s. Model: %s, URL: %s, Response: %s", resp.Status, c.Model, url, string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage,omitempty"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode error: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("AI API error: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("no AI response received")
	}

	content := result.Choices[0].Message.Content
	var tokenUsage *TokenUsage
	if result.Usage != nil {
		tokenUsage = &TokenUsage{
			PromptTokens:     result.Usage.PromptTokens,
			CompletionTokens: result.Usage.CompletionTokens,
			TotalTokens:      result.Usage.TotalTokens,
		}
	}

	return &GenerateResult{
		Content:    content,
		TokenUsage: tokenUsage,
	}, nil
}

// AnalyzeImage analyzes an image URL and returns a description
func (c *Client) AnalyzeImage(ctx context.Context, imageURL string) (string, error) {
	content := []ContentPart{
		{
			Type: "text",
			Text: "Analyze this image in detail. Describe what you see: objects, people, actions, setting, mood, colors, text, and any business-related elements. Be specific and detailed.",
		},
		{
			Type: "image_url",
			ImageURL: &struct {
				URL string `json:"url"`
			}{
				URL: imageURL,
			},
		},
	}

	messages := []Message{
		{
			Role:    "user",
			Content: content,
		},
	}

	analysis, err := c.Generate(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("failed to analyze image: %w", err)
	}

	return analysis, nil
}

// GenerateWithVisionWithUsage generates text using both image and text input and returns token usage
func (c *Client) GenerateWithVisionWithUsage(ctx context.Context, systemPrompt string, userText string, imageURLs []string) (string, *TokenUsage, error) {
	var contentParts []ContentPart

	// Add text instruction
	contentParts = append(contentParts, ContentPart{
		Type: "text",
		Text: userText,
	})

	// Add images
	for _, url := range imageURLs {
		contentParts = append(contentParts, ContentPart{
			Type: "image_url",
			ImageURL: &struct {
				URL string `json:"url"`
			}{
				URL: url,
			},
		})
	}

	messages := []Message{
		{
			Role:    "system",
			Content: systemPrompt,
		},
		{
			Role:    "user",
			Content: contentParts,
		},
	}

	result, err := c.GenerateWithUsage(ctx, messages)
	if err != nil {
		return "", nil, err
	}

	return result.Content, result.TokenUsage, nil
}

// GenerateWithVision generates text using both image and text input
func (c *Client) GenerateWithVision(ctx context.Context, systemPrompt string, userText string, imageURLs []string) (string, error) {
	var contentParts []ContentPart

	// Add text instruction
	contentParts = append(contentParts, ContentPart{
		Type: "text",
		Text: userText,
	})

	// Add images
	for _, url := range imageURLs {
		contentParts = append(contentParts, ContentPart{
			Type: "image_url",
			ImageURL: &struct {
				URL string `json:"url"`
			}{
				URL: url,
			},
		})
	}

	messages := []Message{
		{
			Role:    "system",
			Content: systemPrompt,
		},
		{
			Role:    "user",
			Content: contentParts,
		},
	}

	return c.Generate(ctx, messages)
}
