package ai

import (
	"github.com/google/uuid"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// EmotionResult contains emotion classification
type EmotionResult struct {
	Label      string  // e.g. "neutral","positive","frustrated","urgent","angry","sad"
	Confidence float64 // 0.0-1.0
	CheckedAt  time.Time
}

// EmotionAnalyzer uses lightweight heuristics first, then LLM if needed
type EmotionAnalyzer struct {
	Client *AIClient
}

func NewEmotionAnalyzer() *EmotionAnalyzer {
	return &EmotionAnalyzer{
		Client: NewAIClient(),
	}
}

func (e *EmotionAnalyzer) Analyze(ctx context.Context, tenantID uuid.UUID, message string) (*EmotionResult, error) {
	// fast heuristics
	lower := strings.ToLower(message)
	if strings.Contains(lower, "urgent") || strings.Contains(lower, "asap") || strings.Contains(lower, "now!") {
		return &EmotionResult{Label: "urgent", Confidence: 0.99, CheckedAt: time.Now()}, nil
	}
	if strings.Contains(lower, "not working") || strings.Contains(lower, "complain") || strings.Contains(lower, "angry") || strings.Contains(lower, "terrible") {
		return &EmotionResult{Label: "frustrated", Confidence: 0.95, CheckedAt: time.Now()}, nil
	}
	// fallback to LLM for subtle cases
	prompt := buildEmotionPrompt(message)
	msgs := []Message{
		{Role: "system", Content: "Classify the emotion of the message. Reply JSON only."},
		{Role: "user", Content: prompt},
	}
	res, err := e.Client.Generate(ctx, msgs)
	if err != nil {
		// default neutral
		return &EmotionResult{Label: "neutral", Confidence: 0.4, CheckedAt: time.Now()}, nil
	}
	// expect JSON {"label":"frustrated","confidence":0.84}
	var parsed struct {
		Label      string  `json:"label"`
		Confidence float64 `json:"confidence"`
	}
	_ = json.Unmarshal([]byte(res), &parsed)
	if parsed.Label == "" {
		parsed.Label = "neutral"
	}
	if parsed.Confidence == 0 {
		parsed.Confidence = 0.5
	}
	return &EmotionResult{Label: parsed.Label, Confidence: parsed.Confidence, CheckedAt: time.Now()}, nil
}

// buildEmotionPrompt constructs the emotion analysis prompt
// Template: #12 EmotionAnalyzer (structured JSON output)
func buildEmotionPrompt(message string) string {
	return fmt.Sprintf(`Evaluate the sentiment and emotional tone of this customer message.

Return ONLY a JSON object with this exact structure (no additional text):
{
  "emotion": "positive",
  "confidence": 0.85
}

Emotion should be one of: neutral, positive, frustrated, urgent, angry, sad
Confidence should be a number between 0.0 and 1.0

Message: "%s"`, escapeForPrompt(message))
}
