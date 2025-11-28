package ai

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

type AIClient struct {
	APIKey  string
	BaseURL string
	Model   string
}

func NewAIClient() *AIClient {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		key = os.Getenv("OPENAI_API_KEY")
	}

	base := os.Getenv("OPENROUTER_API_BASE")
	if base == "" {
		base = "https://openrouter.ai/api/v1" // ✅ FIXED: remove /chat/completions
	}

	model := os.Getenv("AI_MODEL")
	if model == "" {
		model = "mistralai/mistral-small" // ✅ stable fallback model
	}

	return &AIClient{
		APIKey:  key,
		BaseURL: base,
		Model:   model,
	}
}

// Generate calls the LLM to get a reply
func (c *AIClient) Generate(ctx context.Context, messages []Message) (string, error) {
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
	req.Header.Set("HTTP-Referer", "http://localhost") // ✅ required by OpenRouter
	req.Header.Set("X-Title", "Brancr AI Automation")  // ✅ required by OpenRouter

	// Increased timeout to 90 seconds for slower AI models
	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("network error: %w", err)
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
				return "", fmt.Errorf("AI API error: Authentication failed (%d). Please check your OPENROUTER_API_KEY or OPENAI_API_KEY. Error: %s", resp.StatusCode, apiError.Error.Message)
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

	return result.Choices[0].Message.Content, nil
}

// AnalyzeImage analyzes an image URL and returns a description
func (c *AIClient) AnalyzeImage(ctx context.Context, imageURL string) (string, error) {
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

// GenerateWithVision generates text using both image and text input
func (c *AIClient) GenerateWithVision(ctx context.Context, systemPrompt string, userText string, imageURLs []string) (string, error) {
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
