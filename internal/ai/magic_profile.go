package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type BusinessProfilePersona struct {
	Tone     string `json:"tone"`
	Audience string `json:"audience"`
	Values   string `json:"values"`
}

type BusinessProfileResult struct {
	Name             string                 `json:"name"`
	Description      string                 `json:"description"`
	Industry         string                 `json:"industry"`
	Persona          BusinessProfilePersona `json:"persona"`
	SuggestedTopics  []string               `json:"suggested_content_topics"`
	ConfidenceScore  string                 `json:"confidence"`         // "high", "medium", "low"
	LowConfidenceMsg string                 `json:"low_confidence_msg"` // Reason/Message if confidence is low
}

func (c *AIClient) GenerateBusinessProfile(ctx context.Context, content string, userDescription string) (*BusinessProfileResult, error) {
	systemPrompt := `You are an expert brand strategist. Your goal is to analyze the provided input (website content and/or user description) and generate a comprehensive business profile.

Return a JSON object with the following structure:
{
  "name": "Business Name",
  "description": "A polished 1-sentence elevator pitch.",
  "industry": "One of: retail, food_beverage, services, fashion, real_estate, health_beauty, tech, entertainment, automotive, other",
  "persona": {
    "tone": "e.g. Professional & Trustworthy, or Friendly & Energetic",
    "audience": "Target audience description",
    "values": "Key brand values"
  },
  "suggested_content_topics": ["Topic 1", "Topic 2", "Topic 3"],
  "confidence": "high" | "medium" | "low",
  "low_confidence_msg": "Optional message if confidence is low, e.g. 'We couldn't find much info, so we guessed based on...'"
}

Rules:
1. If the input is sparse or generic, set confidence to "low".
2. If the input strongly indicates a specific industry, select the best fit.
3. The description should be marketing-ready.
4. Do NOT include markdown formatting (like ` + "`json`" + `) in the response, just the raw JSON string.`

	userPrompt := fmt.Sprintf("Website Content/Scraped Text:\n%s\n\nUser Description:\n%s", content, userDescription)

	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	rawResponse, err := c.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("failed to generate business profile: %w", err)
	}

	// Clean partial markdown if present
	cleaned := strings.TrimSpace(rawResponse)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")

	var result BusinessProfileResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w. Response: %s", err, cleaned)
	}

	return &result, nil
}
