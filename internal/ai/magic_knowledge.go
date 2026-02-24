package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type KnowledgeResult struct {
	FAQs []struct {
		Question string `json:"question"`
		Answer   string `json:"answer"`
	} `json:"faqs"`
	Keywords      []string `json:"keywords"`
	KnowledgeBase []struct {
		Title    string `json:"title"`
		Category string `json:"category"` // policy, process, service
		Content  string `json:"content"`
	} `json:"knowledge_base"`
}

func (c *AIClient) GenerateBusinessKnowledge(ctx context.Context, content string, userDescription string) (*KnowledgeResult, error) {
	systemPrompt := `You are an expert business analyst. Your goal is to analyze the provided input (website content and/or user description) and generate essential business knowledge for a customer support AI.

Return a JSON object with the following structure:
{
  "faqs": [
    { "question": "What is your return policy?", "answer": "We offer returns within 30 days..." },
    { "question": "Do you deliver?", "answer": "Yes, we deliver nationwide..." }
  ],
  "keywords": ["fashion", "dresses", "abuja delivery"],
  "knowledge_base": [
    {
      "title": "Return Policy",
      "category": "policy",
      "content": "Detailed return policy text..."
    },
    {
      "title": "Delivery Process",
      "category": "process",
      "content": "We use GIG Logistics for all deliveries..."
    }
  ]
}

Rules:
1. Generate at least 3 common FAQs based on the business type.
2. Generate 5-10 relevant search keywords/tags.
3. Generate 1-2 generic knowledge base articles (e.g. general policies) if specific info is missing.
4. Do NOT include markdown formatting, just raw JSON.
`

	userPrompt := fmt.Sprintf("Website Content/Scraped Text:\n%s\n\nUser Description:\n%s", content, userDescription)

	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	rawResponse, err := c.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("failed to generate knowledge: %w", err)
	}

	// Clean partial markdown if present
	cleaned := strings.TrimSpace(rawResponse)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")

	var result KnowledgeResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w. Response: %s", err, cleaned)
	}

	return &result, nil
}
