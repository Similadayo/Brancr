package ai_client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/similadayo/brancr/internal/ai_contract"
	"go.uber.org/zap"
)

// =============================================================================
// PYTHON AI CLIENT - HTTP-Backed Implementation
// =============================================================================
// This client calls the Python AI service via HTTP.
//
// PHASE-2: Introduces process separation and HTTP communication.
// Falls back to V3 adapter if Python service is unavailable.
//
// RULES:
// - Timeout after 5 seconds
// - Fall back to V3 on any error
// - Log all failures and fallbacks
// =============================================================================

// pythonClient implements the Client interface by calling Python AI service.
type pythonClient struct {
	serviceURL string
	httpClient *http.Client
	fallback   Client // V3 adapter for fallback
	logger     *zap.Logger
}

// NewPythonClient creates a new Python AI client with V3 fallback.
// PHASE-2: This is the primary AI client when Python service is available.
func NewPythonClient(fallback Client, logger *zap.Logger) Client {
	if logger == nil {
		logger = zap.NewNop()
	}

	// Get service URL from environment or use default
	serviceURL := os.Getenv("AI_SERVICE_URL")
	if serviceURL == "" {
		serviceURL = "http://localhost:8000"
	}

	return &pythonClient{
		serviceURL: serviceURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second, // PHASE-2: 30 second timeout for LLM
		},
		fallback: fallback,
		logger:   logger,
	}
}

// Analyze implements the Client interface by calling Python AI service.
// PHASE-2: Falls back to V3 adapter on any error.
func (c *pythonClient) Analyze(ctx context.Context, input AnalyzeInput) (*ai_contract.AISuggestion, error) {
	start := time.Now()

	// PHASE-2: Attempt Python service call
	suggestion, err := c.callPythonService(ctx, input)

	if err != nil {
		// PHASE-2 FALLBACK: Use V3 adapter
		c.logger.Warn("python_ai_failed_falling_back_to_v3",
			zap.Error(err),
			zap.String("service_url", c.serviceURL),
			zap.Duration("attempt_duration", time.Since(start)),
		)

		// Call fallback (V3 adapter)
		fallbackStart := time.Now()
		suggestion, fallbackErr := c.fallback.Analyze(ctx, input)

		if fallbackErr != nil {
			c.logger.Error("fallback_also_failed",
				zap.Error(fallbackErr),
			)
			return nil, fmt.Errorf("both python and fallback failed: python=%w, fallback=%v", err, fallbackErr)
		}

		// Log successful fallback
		c.logger.Info("fallback_to_v3_successful",
			zap.Duration("fallback_duration", time.Since(fallbackStart)),
			zap.String("ai_source", "legacy_ai"),
		)

		return suggestion, nil
	}

	// PHASE-2: Python service succeeded
	c.logger.Info("python_ai_successful",
		zap.Duration("duration", time.Since(start)),
		zap.String("ai_source", "python_ai"),
		zap.String("intent", suggestion.Intent),
		zap.Float64("confidence", suggestion.Confidence),
	)

	return suggestion, nil
}

// Generate implements the AIClient interface by calling Python /api/generate.
// PHASE-8: Generates context-aware responses using memory HUD.
func (c *pythonClient) Generate(ctx context.Context, input interface{}) (string, error) {
	start := time.Now()

	// Call Python /generate endpoint
	message, err := c.callGenerateService(ctx, input)

	if err != nil {
		c.logger.Warn("python_generate_failed",
			zap.Error(err),
			zap.String("service_url", c.serviceURL),
			zap.Duration("attempt_duration", time.Since(start)),
		)
		return "", err
	}

	// Log successful generation
	c.logger.Info("python_generate_successful",
		zap.Duration("duration", time.Since(start)),
		zap.Int("message_len", len(message)),
	)

	return message, nil
}

// callPythonService makes the HTTP call to Python AI service.
func (c *pythonClient) callPythonService(ctx context.Context, input AnalyzeInput) (*ai_contract.AISuggestion, error) {
	// Serialize request
	// PHASE-3: Include intent_hint from Go regex
	requestBody, err := json.Marshal(map[string]interface{}{
		"tenant_id":       input.TenantID.String(),
		"conversation_id": input.ConversationID.String(),
		"customer_id":     input.CustomerID.String(),
		"message":         input.Message,
		"context":         c.serializeContext(input.Context),
		"platform":        input.Platform,
		"intent_hint":     input.IntentHint, // PHASE-3: Send regex hint
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	url := c.serviceURL + "/api/analyze"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Make request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("python service returned %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var pythonResponse struct {
		Intent          string                          `json:"intent"`
		Confidence      float64                         `json:"confidence"`
		Actions         []ai_contract.AISuggestedAction `json:"actions"`
		Explanation     string                          `json:"explanation"`
		DetectionMethod string                          `json:"detection_method"`
		Emotion         *ai_contract.EmotionSuggestion  `json:"emotion"`
		Entities        map[string]interface{}          `json:"entities"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&pythonResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert to AISuggestion
	suggestion := &ai_contract.AISuggestion{
		Intent:          pythonResponse.Intent,
		Confidence:      pythonResponse.Confidence,
		Actions:         pythonResponse.Actions,
		Explanation:     pythonResponse.Explanation,
		DetectionMethod: pythonResponse.DetectionMethod,
		Entities:        pythonResponse.Entities,
	}

	if pythonResponse.Emotion != nil {
		suggestion.Emotion = *pythonResponse.Emotion
	}

	return suggestion, nil
}

// callGenerateService makes the HTTP call to Python /api/generate endpoint.
// PHASE-8: Generates context-aware responses using memory HUD.
func (c *pythonClient) callGenerateService(ctx context.Context, input interface{}) (string, error) {
	// Serialize request
	reqBody, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("failed to marshal generate request: %w", err)
	}

	// Build request
	url := c.serviceURL + "/api/generate"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Make request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("python generate returned %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var generateResponse struct {
		Message          string `json:"message"`
		GenerationMethod string `json:"generation_method"`
		ProcessingMs     int    `json:"processing_ms"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&generateResponse); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return generateResponse.Message, nil
}

// serializeContext converts ConversationContext to a JSON-serializable map.
// PHASE-2: Python service doesn't inspect this yet, but we send it for future use.
func (c *pythonClient) serializeContext(ctx ConversationContext) map[string]interface{} {
	return map[string]interface{}{
		"recent_messages":    ctx.RecentMessages,
		"order_summary":      ctx.OrderSummary,
		"available_products": ctx.AvailableProducts,
		"available_services": ctx.AvailableServices,
		"payment_info":       ctx.PaymentInfo,
		"business_profile":   ctx.BusinessProfile,
		"customer_profile":   ctx.CustomerProfile,
	}
}
