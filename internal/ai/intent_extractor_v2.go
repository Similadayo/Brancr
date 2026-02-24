package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// IntentExtractorV2 is Stage 2 of the v2.0 pipeline.
// It constrains AI output to ONLY canonical intents with structured JSON.
// NO free-text intents. NO invented intents. One message → one intent.
type IntentExtractorV2 struct {
	AIClient *AIClient
	Logger   *zap.Logger
}

// NewIntentExtractorV2 creates a new structured intent extractor.
func NewIntentExtractorV2(client *AIClient, logger *zap.Logger) *IntentExtractorV2 {
	return &IntentExtractorV2{
		AIClient: client,
		Logger:   logger,
	}
}

// IntentExtractionResult is the structured output from intent extraction.
// This is the ONLY format AI is allowed to output.
type IntentExtractionResult struct {
	Intent     CanonicalIntent        `json:"intent"`
	Entities   map[string]interface{} `json:"entities"`
	Confidence float64                `json:"confidence"`
}

// ExtractIntent analyzes a message and returns a structured intent.
// If AI fails or returns invalid output, defaults to IntentUnknown.
func (e *IntentExtractorV2) ExtractIntent(ctx context.Context, tenantID uuid.UUID, message string, recentHistory []Message, productCatalog []string) (*IntentExtractionResult, error) {
	// Build the extraction prompt
	prompt := e.buildExtractionPrompt(message, recentHistory, productCatalog)

	// Call AI for intent extraction
	response, err := e.AIClient.Generate(ctx, prompt)
	if err != nil {
		e.Logger.Warn("AI intent extraction failed, using fallback", zap.Error(err))
		return e.fallbackResult(), nil
	}

	// Parse JSON response
	result, parseErr := e.parseResponse(response)
	if parseErr != nil {
		e.Logger.Warn("Failed to parse AI intent response, using fallback",
			zap.Error(parseErr),
			zap.String("response", response))
		return e.fallbackResult(), nil
	}

	// Validate intent is canonical
	if !IsValidIntent(string(result.Intent)) {
		e.Logger.Warn("AI returned non-canonical intent, using unknown",
			zap.String("invalid_intent", string(result.Intent)))
		result.Intent = IntentUnknown
	}

	// Enforce confidence threshold
	if result.Confidence < ConfidenceThreshold {
		e.Logger.Info("Low confidence intent",
			zap.String("intent", string(result.Intent)),
			zap.Float64("confidence", result.Confidence))
		result.Intent = IntentUnknown
	}

	e.Logger.Info("🎯 V2 Intent extracted",
		zap.String("intent", string(result.Intent)),
		zap.Float64("confidence", result.Confidence),
		zap.Any("entities", result.Entities))

	return result, nil
}

// buildExtractionPrompt creates the constrained prompt for AI.
func (e *IntentExtractorV2) buildExtractionPrompt(message string, recentHistory []Message, productCatalog []string) []Message {
	// System prompt with strict JSON contract
	systemPrompt := `You are an intent classifier. Analyze the customer message and respond with ONLY a JSON object.

VALID INTENTS (you MUST use one of these EXACTLY):
- greeting: Hello, hi, good morning
- goodbye: Bye, thanks, see you
- affirm: Yes, okay, sure, I agree
- reject: No, never mind, cancel that
- clarify: Asking for clarification
- unknown: Cannot determine intent
- browse_products: Asking what's available
- ask_product_info: Asking details about a product
- ask_price: Asking for price
- add_to_cart: Adding item to cart ("I want", "give me", "order")
- remove_from_cart: Removing item from cart
- view_cart: Asking what's in cart
- checkout: Ready to finalize
- ask_payment_method: Asking how to pay
- confirm_payment: Customer says they paid
- cancel_order: Cancel the order
- track_order: Asking about order status

RESPONSE FORMAT (JSON ONLY, no other text):
{
  "intent": "add_to_cart",
  "entities": {
    "product_name": "Large Shawarma",
    "quantity": 2
  },
  "confidence": 0.85
}

RULES:
- One message = one intent
- confidence must be 0.0 to 1.0
- If unsure, use "unknown" with low confidence
- entities are optional, include only if clearly stated
- DO NOT include any text outside the JSON object`

	messages := []Message{
		{Role: "system", Content: systemPrompt},
	}

	// Add recent history for context (max 3 turns)
	historyLimit := 3
	if len(recentHistory) < historyLimit {
		historyLimit = len(recentHistory)
	}
	if historyLimit > 0 {
		for _, msg := range recentHistory[len(recentHistory)-historyLimit:] {
			messages = append(messages, msg)
		}
	}

	// Add product catalog hint if available
	if len(productCatalog) > 0 {
		productHint := fmt.Sprintf("Available products: %s", strings.Join(productCatalog[:min(10, len(productCatalog))], ", "))
		messages = append(messages, Message{Role: "system", Content: productHint})
	}

	// Add the current message
	messages = append(messages, Message{Role: "user", Content: fmt.Sprintf("Classify this message:\n\"%s\"", message)})

	return messages
}

// parseResponse extracts structured result from AI response.
func (e *IntentExtractorV2) parseResponse(response string) (*IntentExtractionResult, error) {
	// Clean up response - remove markdown code blocks if present
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	// Try to find JSON object in response
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no JSON object found in response")
	}
	jsonStr := response[start : end+1]

	// Parse JSON
	var result IntentExtractionResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Ensure entities map exists
	if result.Entities == nil {
		result.Entities = make(map[string]interface{})
	}

	return &result, nil
}

// fallbackResult returns a safe default when AI fails.
func (e *IntentExtractorV2) fallbackResult() *IntentExtractionResult {
	return &IntentExtractionResult{
		Intent:     IntentUnknown,
		Entities:   make(map[string]interface{}),
		Confidence: 0.0,
	}
}

// QuickClassify provides fast heuristic classification for common patterns.
// Use this before AI for obvious cases to save tokens and latency.
// IMPORTANT: Pattern order matters! More specific patterns (cancel, new order) must come BEFORE
// generic patterns (order, buy) to prevent false positives.
func (e *IntentExtractorV2) QuickClassify(message string) *IntentExtractionResult {
	lower := strings.ToLower(strings.TrimSpace(message))

	// Greeting patterns
	if matchesAny(lower, []string{"hi", "hello", "hey", "good morning", "good afternoon", "good evening"}) {
		return &IntentExtractionResult{Intent: IntentGreeting, Confidence: 0.95, Entities: make(map[string]interface{})}
	}

	// Goodbye patterns
	if matchesAny(lower, []string{"bye", "goodbye", "thanks", "thank you", "see you", "later"}) {
		return &IntentExtractionResult{Intent: IntentGoodbye, Confidence: 0.90, Entities: make(map[string]interface{})}
	}

	// Affirm patterns
	if matchesAny(lower, []string{"yes", "yeah", "yep", "sure", "okay", "ok", "alright", "proceed", "go ahead"}) {
		return &IntentExtractionResult{Intent: IntentAffirm, Confidence: 0.90, Entities: make(map[string]interface{})}
	}

	// Reject patterns (but NOT cancel - that's handled below with order context)
	if matchesAny(lower, []string{"no", "nope", "never mind", "stop", "don't"}) {
		return &IntentExtractionResult{Intent: IntentReject, Confidence: 0.85, Entities: make(map[string]interface{})}
	}

	// Payment confirmation patterns
	if containsAny(lower, []string{"i've paid", "i paid", "payment sent", "transferred", "sent the money", "just paid"}) {
		return &IntentExtractionResult{Intent: IntentConfirmPayment, Confidence: 0.90, Entities: make(map[string]interface{})}
	}

	// Payment inquiry patterns
	if containsAny(lower, []string{"how do i pay", "payment method", "account number", "bank details", "how to pay"}) {
		return &IntentExtractionResult{Intent: IntentAskPaymentMethod, Confidence: 0.90, Entities: make(map[string]interface{})}
	}

	// ==========================================================================
	// CRITICAL: Cancel patterns MUST come BEFORE order patterns
	// Otherwise "cancel existing order" matches "order" and becomes add_to_cart!
	// ==========================================================================
	if containsAny(lower, []string{"cancel order", "cancel my order", "cancel existing order", "cancel the order", "don't want anymore", "cancel this order"}) {
		return &IntentExtractionResult{Intent: IntentCancelOrder, Confidence: 0.90, Entities: make(map[string]interface{})}
	}

	// Also catch "cancel" followed by order-related words in various forms
	if strings.Contains(lower, "cancel") && containsAny(lower, []string{"order", "purchase", "transaction"}) {
		return &IntentExtractionResult{Intent: IntentCancelOrder, Confidence: 0.85, Entities: make(map[string]interface{})}
	}

	// ==========================================================================
	// New order / start shopping intent - customer wants to START, not add fake product
	// ==========================================================================
	if containsAny(lower, []string{"make a new order", "start a new order", "place a new order", "new order please", "i want to order", "want to make order", "make an order"}) {
		return &IntentExtractionResult{Intent: IntentBrowseProducts, Confidence: 0.85, Entities: make(map[string]interface{})}
	}

	// ==========================================================================
	// Add to cart patterns - requires PURCHASE INTENT signals
	// "order" alone is NOT enough - must have "i want", "give me", "buy", etc.
	// ==========================================================================
	hasPurchaseIntent := containsAny(lower, []string{"i want", "i need", "give me", "buy", "get me", "add", "i'll take", "i'd like"})

	// Blacklist: concept words that should NOT trigger add_to_cart without explicit purchase intent
	isConceptOnly := !hasPurchaseIntent && matchesAnyContains(lower, []string{"order", "new order", "existing order", "my order", "the order"})

	if hasPurchaseIntent && !isConceptOnly {
		return &IntentExtractionResult{Intent: IntentAddToCart, Confidence: 0.80, Entities: make(map[string]interface{})}
	}

	// Price patterns
	if containsAny(lower, []string{"how much", "price", "cost", "pricing"}) {
		return &IntentExtractionResult{Intent: IntentAskPrice, Confidence: 0.85, Entities: make(map[string]interface{})}
	}

	// No quick match - return nil to indicate AI should be used
	return nil
}

// matchesAnyContains checks if string contains any of the patterns (for blacklist)
func matchesAnyContains(s string, patterns []string) bool {
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

// Helper functions
func matchesAny(s string, patterns []string) bool {
	for _, p := range patterns {
		if s == p {
			return true
		}
	}
	return false
}

func containsAny(s string, patterns []string) bool {
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}
