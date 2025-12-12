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
	Intent             string  // inquiry, order, payment, complaint, casual, refund, delivery_issue
	Tone               string  // positive, neutral, negative, urgent
	Confidence         float64 // 0-1
	RequiresEscalation bool    // true if sensitive/uncertain
	Reasoning          string  // Why this intent was detected
}

// DetectIntent analyzes a message to determine intent, tone, and whether it needs escalation
func (d *IntentDetector) DetectIntent(ctx context.Context, tenantID uint, message string, platform string) (*DetectedIntent, error) {
	// Check tenant's intent catalog first for custom intents
	var intentCatalog []storage.IntentCatalog
	d.DB.Where("tenant_id = ?", tenantID).Find(&intentCatalog)

	// Load business context
	var biz storage.BusinessProfile
	if err := d.DB.First(&biz, "tenant_id = ?", tenantID).Error; err != nil {
		biz.Description = ""
	}

	// Build intent options list
	intentOptions := []string{"inquiry", "order", "payment", "complaint", "casual", "refund", "delivery_issue", "account", "billing"}
	if len(intentCatalog) > 0 {
		for _, intent := range intentCatalog {
			intentOptions = append(intentOptions, intent.Name)
		}
	}
	intentOptionsStr := strings.Join(intentOptions, ", ")

	// Build detection prompt
	// Template: #10 DetectIntent (structured JSON output)
	systemPrompt := fmt.Sprintf(`You are an expert at analyzing customer messages for a %s business.

Business: %s
Niche: %s

Analyze this customer message and identify the user's main intent.

Available intent options: %s

Return ONLY a JSON object with this exact structure (no additional text):
{
  "intent": "inquiry",
  "tone": "neutral",
  "confidence": 0.85,
  "requires_escalation": false,
  "reasoning": "Customer asking about product availability"
}

Intent should be one of: %s
Tone should be one of: positive, neutral, negative, urgent
Confidence should be a number between 0.0 and 1.0

IMPORTANT: Context-aware escalation rules:
- "account" intent = Customer asking for account/bank details to PAY
  → Set requires_escalation=false (system will auto-reply with stored details)
	- "payment" intent = Customer CONFIRMING they have already paid (e.g. "I have paid", "I transferred")
  → Set requires_escalation=true (tenant must verify and send receipt)
	- Price questions like "how much", "price", "cost" are NOT payment confirmations
	  → Prefer intent="inquiry" (or "billing" if about invoice/receipt)
- "refund" intent = Customer requesting refund
  → Set requires_escalation=true (requires human judgment)
- "complaint" intent = Customer complaining
  → Set requires_escalation=true (requires human judgment)
- "billing" intent = Customer asking about invoice/billing
  → Set requires_escalation=false (unless combined with complaint)

Escalation Rules:
1. Confidence below 0.25 (very uncertain) → requires_escalation=true
2. "payment" (confirmation only) OR "refund" OR "complaint" with confidence > 0.5 → requires_escalation=true
3. "delivery_issue" with confidence > 0.85 AND negative/urgent tone → requires_escalation=true
4. "account" OR "billing" asking for information → requires_escalation=false (bot can handle)
5. Message tone is "urgent" with confidence > 0.75 → requires_escalation=true

Message: "%s"
Platform: %s
`, biz.Niche, biz.Description, biz.Niche, intentOptionsStr, intentOptionsStr, message, strings.ToUpper(platform))

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
	// NOTE: We intentionally reserve intent="payment" for payment confirmations.
	// Price questions / payment methods are treated as inquiry/account/billing, not payment.
	if strings.Contains(lower, "refund") || strings.Contains(lower, "return") || strings.Contains(lower, "money back") {
		intent = "refund"
		// Only escalate if it's clearly negative/urgent, not just asking about policy
		if strings.Contains(lower, "want") || strings.Contains(lower, "need") || strings.Contains(lower, "demand") ||
			strings.Contains(lower, "terrible") || strings.Contains(lower, "angry") || strings.Contains(lower, "disappointed") {
			requiresEscalation = true
			tone = "negative"
		} else {
			tone = "neutral" // Routine question about refund policy
		}
	} else if strings.Contains(lower, "complaint") || strings.Contains(lower, "wrong") || strings.Contains(lower, "bad") || strings.Contains(lower, "terrible") {
		intent = "complaint"
		requiresEscalation = true
		tone = "negative"
	} else if strings.Contains(lower, "delivery") || strings.Contains(lower, "ship") || strings.Contains(lower, "where is") || strings.Contains(lower, "not received") {
		intent = "delivery_issue"
		// Only escalate if urgent/negative, not just asking "when will it arrive?"
		if strings.Contains(lower, "not received") || strings.Contains(lower, "missing") || strings.Contains(lower, "lost") ||
			strings.Contains(lower, "urgent") || strings.Contains(lower, "asap") {
			requiresEscalation = true
			tone = "urgent"
		} else {
			tone = "neutral" // Routine delivery inquiry
		}
	} else if strings.Contains(lower, "account number") || strings.Contains(lower, "bank details") || strings.Contains(lower, "payment details") ||
		strings.Contains(lower, "send account") || strings.Contains(lower, "where to send") || strings.Contains(lower, "how to pay") {
		intent = "account"
		requiresEscalation = false
	} else if strings.Contains(lower, "i have paid") || strings.Contains(lower, "i've paid") || strings.Contains(lower, "paid") ||
		strings.Contains(lower, "payment made") || strings.Contains(lower, "sent payment") || strings.Contains(lower, "transferred") {
		intent = "payment"
		requiresEscalation = true
	} else if strings.Contains(lower, "invoice") || strings.Contains(lower, "billing") || strings.Contains(lower, "receipt") {
		intent = "billing"
		requiresEscalation = false
	} else if strings.Contains(lower, "order") || strings.Contains(lower, "buy") || strings.Contains(lower, "purchase") {
		intent = "order"
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
		Intent:             intent,
		Tone:               tone,
		Confidence:         0.65, // Lower confidence for keyword-based
		RequiresEscalation: requiresEscalation,
		Reasoning:          "Keyword-based detection (AI unavailable)",
	}
}
