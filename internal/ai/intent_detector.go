package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// IntentDetector analyzes messages to detect intent and tone
type IntentDetector struct {
	Client *AIClient
	DB     *gorm.DB
	Logger *zap.Logger
}

// NewIntentDetector creates a new IntentDetector
func NewIntentDetector(client *AIClient, db *gorm.DB, logger *zap.Logger) *IntentDetector {
	return &IntentDetector{
		Client: client,
		DB:     db,
		Logger: logger,
	}
}

// DetectedIntent represents the analysis result
type DetectedIntent struct {
	Intent      string  // inquiry, order, payment, complaint, casual, refund, delivery_issue
	Tone        string  // positive, neutral, negative, urgent
	Confidence  float64 // 0-1
	RequiresEscalation bool // true if sensitive/uncertain
	Reasoning   string  // Why this intent was detected
}

// DetectIntent analyzes a message to determine intent, tone, and whether it needs escalation
func (d *IntentDetector) DetectIntent(ctx context.Context, tenantID uint, message string, platform string) (*DetectedIntent, error) {
	// Load business context
	var biz storage.BusinessProfile
	if err := d.DB.First(&biz, "tenant_id = ?", tenantID).Error; err != nil {
		biz.Description = ""
	}

	// Build detection prompt
	systemPrompt := fmt.Sprintf(`You are an expert at analyzing customer messages for a %s business.

Business: %s
Niche: %s

Analyze this customer message and determine:
1. Intent: inquiry, order, payment, complaint, casual, refund, delivery_issue
2. Tone: positive, neutral, negative, urgent
3. Confidence: 0.0-1.0
4. Requires escalation: true if refund, complaint, delivery_issue, or uncertain

Return ONLY a JSON object with this exact structure:
{
  "intent": "inquiry",
  "tone": "neutral",
  "confidence": 0.85,
  "requires_escalation": false,
  "reasoning": "Customer asking about product availability"
}

Message: "%s"
Platform: %s
`, biz.Niche, biz.Description, biz.Niche, message, strings.ToUpper(platform))

	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: message},
	}

	rawReply, err := d.Client.Generate(ctx, messages)
	if err != nil {
		// Fallback to keyword-based detection
		return d.FallbackDetection(message), nil
	}

	var result DetectedIntent
	if err := json.Unmarshal([]byte(rawReply), &result); err != nil {
		// Fallback if JSON parsing fails
		return d.FallbackDetection(message), nil
	}

	// Validate and normalize
	if result.Intent == "" {
		result = *d.FallbackDetection(message)
	}
	if result.Confidence == 0 {
		result.Confidence = 0.7 // Default confidence
	}

	return &result, nil
}

// FallbackDetection provides keyword-based intent detection as fallback
func (d *IntentDetector) FallbackDetection(message string) *DetectedIntent {
	lower := strings.ToLower(message)
	
	intent := "casual"
	tone := "neutral"
	requiresEscalation := false
	
	// Intent detection
	if strings.Contains(lower, "refund") || strings.Contains(lower, "return") || strings.Contains(lower, "money back") {
		intent = "refund"
		requiresEscalation = true
		tone = "negative"
	} else if strings.Contains(lower, "complaint") || strings.Contains(lower, "wrong") || strings.Contains(lower, "bad") || strings.Contains(lower, "terrible") {
		intent = "complaint"
		requiresEscalation = true
		tone = "negative"
	} else if strings.Contains(lower, "delivery") || strings.Contains(lower, "ship") || strings.Contains(lower, "where is") || strings.Contains(lower, "not received") {
		intent = "delivery_issue"
		requiresEscalation = true
		tone = "urgent"
	} else if strings.Contains(lower, "order") || strings.Contains(lower, "buy") || strings.Contains(lower, "purchase") {
		intent = "order"
	} else if strings.Contains(lower, "pay") || strings.Contains(lower, "price") || strings.Contains(lower, "cost") || strings.Contains(lower, "how much") {
		intent = "payment"
	} else if strings.Contains(lower, "?") || strings.Contains(lower, "what") || strings.Contains(lower, "how") || strings.Contains(lower, "when") {
		intent = "inquiry"
	}
	
	// Tone detection
	if strings.Contains(lower, "urgent") || strings.Contains(lower, "asap") || strings.Contains(lower, "immediately") {
		tone = "urgent"
	} else if strings.Contains(lower, "thank") || strings.Contains(lower, "great") || strings.Contains(lower, "love") || strings.Contains(lower, "amazing") {
		tone = "positive"
	} else if strings.Contains(lower, "angry") || strings.Contains(lower, "disappointed") || strings.Contains(lower, "frustrated") {
		tone = "negative"
	}
	
	return &DetectedIntent{
		Intent:            intent,
		Tone:              tone,
		Confidence:        0.65, // Lower confidence for keyword-based
		RequiresEscalation: requiresEscalation,
		Reasoning:         "Keyword-based detection (AI unavailable)",
	}
}

