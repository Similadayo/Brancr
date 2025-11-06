package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// IntentResult returned by classifier
type IntentResult struct {
	Intent     string  // e.g. "order", "payment", "greeting", "unknown"
	Confidence float64 // 0.0 - 1.0
	Metadata   map[string]any
	CheckedAt  time.Time
}

// Classifier is responsible for intent detection
type Classifier struct {
	Client *AIClient
}

func NewClassifier() *Classifier {
	return &Classifier{
		Client: NewAIClient(), // reuse the same LLM client for classification
	}
}

// Classify runs LLM classification with a compact prompt and fallback rules
func (c *Classifier) Classify(ctx context.Context, tenantID uint, userMsg string) (*IntentResult, error) {
	// 1) quick local rules (fast path)
	lower := strings.ToLower(strings.TrimSpace(userMsg))
	if local := localIntentRule(lower); local != "" {
		return &IntentResult{Intent: local, Confidence: 0.98, CheckedAt: time.Now()}, nil
	}

	// 2) compose LLM prompt
	prompt := buildIntentPrompt(tenantID, userMsg)

	// call LLM (use same Generate helper)
	msgs := []Message{
		{Role: "system", Content: "You are a very small intent classifier. Respond JSON only."},
		{Role: "user", Content: prompt},
	}
	res, err := c.Client.Generate(ctx, msgs)
	if err != nil {
		return nil, err
	}

	// Expect JSON like: {"intent":"order","confidence":0.87,"explain":"..."}
	var parsed struct {
		Intent     string  `json:"intent"`
		Confidence float64 `json:"confidence"`
		Explain    string  `json:"explain,omitempty"`
	}
	if err := json.Unmarshal([]byte(res), &parsed); err != nil {
		// fallback: unknown low confidence
		return &IntentResult{Intent: "unknown", Confidence: 0.3, Metadata: map[string]any{"raw": res}, CheckedAt: time.Now()}, nil
	}
	return &IntentResult{
		Intent:     parsed.Intent,
		Confidence: parsed.Confidence,
		Metadata:   map[string]any{"explain": parsed.Explain},
		CheckedAt:  time.Now(),
	}, nil
}

// buildIntentPrompt constructs a short classification prompt using tenant memory (if needed)
func buildIntentPrompt(tenantID uint, message string) string {
	// minimal: ask to classify into these classes; prefer short responses in JSON
	classes := []string{"greeting", "order", "payment", "delivery", "appointment", "complaint", "question", "smalltalk", "schedule_post", "connect_account", "unknown"}
	return fmt.Sprintf(
		"Classify this message into one of: %s.\nReturn JSON: {\"intent\":\"<intent>\",\"confidence\":<0-1>,\"explain\":\"short explanation\"}\n\nMessage:\n\"%s\"",
		strings.Join(classes, ","), escapeForPrompt(message),
	)
}

// localIntentRule: super-fast rules
func localIntentRule(lower string) string {
	if strings.HasPrefix(lower, "hi") || strings.HasPrefix(lower, "hello") || strings.HasPrefix(lower, "hey") {
		return "greeting"
	}
	if strings.Contains(lower, "schedule") || strings.Contains(lower, "post now") {
		return "schedule_post"
	}
	if strings.Contains(lower, "connect instagram") || strings.Contains(lower, "connect facebook") {
		return "connect_account"
	}
	if strings.Contains(lower, "how much") || strings.Contains(lower, "price") {
		return "question"
	}
	if strings.Contains(lower, "payment") || strings.Contains(lower, "paid") {
		return "payment"
	}
	return ""
}

func escapeForPrompt(s string) string {
	// basic prompt safety
	tmp := strings.ReplaceAll(s, "\n", "\\n")
	tmp = strings.ReplaceAll(tmp, "\"", "\\\"")
	return tmp
}
