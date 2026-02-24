package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ParsedMenuItem represents a menu item extracted from freeform text
type ParsedMenuItem struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Price       float64  `json:"price"`
	Currency    string   `json:"currency"`
	Category    string   `json:"category,omitempty"`
	SpiceLevel  string   `json:"spice_level,omitempty"`
	DietaryInfo []string `json:"dietary_info,omitempty"`
}

// ParseMenuTextResult contains the parsed menu items and any parsing metadata
type ParseMenuTextResult struct {
	Items         []ParsedMenuItem `json:"items"`
	ParsedCount   int              `json:"parsed_count"`
	OriginalText  string           `json:"original_text,omitempty"`
	ParsingErrors []string         `json:"parsing_errors,omitempty"`
}

// ParseMenuText uses AI to extract structured menu items from freeform text input
// This handles various input formats: lists, tables, copy-pasted menus, etc.
func (c *AIClient) ParseMenuText(ctx context.Context, text string, defaultCurrency string) (*ParseMenuTextResult, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("empty text input")
	}

	if defaultCurrency == "" {
		defaultCurrency = "NGN"
	}

	systemPrompt := `You are a menu data extraction assistant. Your task is to parse unstructured text and extract menu items into a structured JSON format.

EXTRACTION RULES:
1. Extract each distinct menu item as a separate object
2. For each item, identify:
   - name: The item name (required)
   - description: Any description or details about the item
   - price: The numeric price (extract the number only, no currency symbols)
   - currency: The currency code (use the default if not specified)
   - category: The food category if mentioned (e.g., "Appetizers", "Main Course", "Drinks")
   - spice_level: If mentioned (mild, medium, hot, extra_hot)
   - dietary_info: Any dietary restrictions mentioned (vegetarian, vegan, gluten-free, halal, etc.)

3. Handle various formats:
   - "Jollof Rice - ₦3,500" → name: "Jollof Rice", price: 3500
   - "Chicken Wings.........$15.99" → name: "Chicken Wings", price: 15.99
   - "1. Peppered Snail (spicy) - N4500" → name: "Peppered Snail", price: 4500, description: "spicy"
   - "Suya (per stick) @200" → name: "Suya", price: 200, description: "per stick"

4. If a price has ranges like "3000-5000", use the lower price
5. Remove currency symbols (₦, $, N, NGN) when extracting the price number
6. Clean up formatting artifacts (dots, dashes used for alignment)

RESPOND WITH ONLY A JSON ARRAY of menu items. No explanations, no markdown, just the JSON array.
Example output:
[
  {"name": "Jollof Rice", "price": 3500, "currency": "NGN", "category": "Main Course"},
  {"name": "Chapman", "price": 1500, "currency": "NGN", "category": "Drinks", "description": "Nigerian cocktail"}
]`

	userPrompt := fmt.Sprintf(`Parse the following menu text and extract all menu items. Use "%s" as the default currency if not specified.

Menu text:
%s`, defaultCurrency, text)

	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	response, err := c.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("AI generation failed: %w", err)
	}

	// Clean up the response (remove markdown code blocks if present)
	response = cleanJSONResponse(response)

	// Parse the JSON response
	var items []ParsedMenuItem
	if err := json.Unmarshal([]byte(response), &items); err != nil {
		// Try to extract JSON array from response if it contains extra text
		if jsonStart := strings.Index(response, "["); jsonStart >= 0 {
			if jsonEnd := strings.LastIndex(response, "]"); jsonEnd > jsonStart {
				cleanJSON := response[jsonStart : jsonEnd+1]
				if err := json.Unmarshal([]byte(cleanJSON), &items); err != nil {
					return &ParseMenuTextResult{
						Items:         []ParsedMenuItem{},
						ParsedCount:   0,
						OriginalText:  text,
						ParsingErrors: []string{fmt.Sprintf("Failed to parse AI response: %v", err)},
					}, nil
				}
			}
		} else {
			return &ParseMenuTextResult{
				Items:         []ParsedMenuItem{},
				ParsedCount:   0,
				OriginalText:  text,
				ParsingErrors: []string{fmt.Sprintf("Invalid JSON response from AI: %v", err)},
			}, nil
		}
	}

	// Validate and clean up items
	validItems := make([]ParsedMenuItem, 0, len(items))
	for _, item := range items {
		if item.Name == "" {
			continue // Skip items without names
		}
		// Set default currency if not provided
		if item.Currency == "" {
			item.Currency = defaultCurrency
		}
		validItems = append(validItems, item)
	}

	return &ParseMenuTextResult{
		Items:        validItems,
		ParsedCount:  len(validItems),
		OriginalText: text,
	}, nil
}

// cleanJSONResponse removes markdown code blocks and extra whitespace from AI response
func cleanJSONResponse(response string) string {
	response = strings.TrimSpace(response)

	// Remove markdown code blocks
	if strings.HasPrefix(response, "```json") {
		response = strings.TrimPrefix(response, "```json")
	}
	if strings.HasPrefix(response, "```") {
		response = strings.TrimPrefix(response, "```")
	}
	if strings.HasSuffix(response, "```") {
		response = strings.TrimSuffix(response, "```")
	}

	return strings.TrimSpace(response)
}

// ParseMenuFromImage uses AI Vision to extract menu items from an image
// imageData should be base64-encoded image data
// mimeType should be "image/jpeg", "image/png", "image/gif", or "image/webp"
func (c *AIClient) ParseMenuFromImage(ctx context.Context, imageData string, mimeType string, defaultCurrency string) (*ParseMenuTextResult, error) {
	if imageData == "" {
		return nil, fmt.Errorf("empty image data")
	}

	if defaultCurrency == "" {
		defaultCurrency = "NGN"
	}

	// Validate mime type
	switch mimeType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		// Valid
	default:
		return nil, fmt.Errorf("unsupported image type: %s", mimeType)
	}

	systemPrompt := `You are a menu data extraction assistant with vision capabilities. Your task is to analyze this menu image and extract all menu items into a structured JSON format.

EXTRACTION RULES:
1. Look for all food/drink items with prices
2. For each item, identify:
   - name: The item name (required)
   - description: Any description or details about the item
   - price: The numeric price (extract the number only, no currency symbols)
   - currency: The currency code (NGN for ₦/N, USD for $, GBP for £, EUR for €)
   - category: The food category if visible (e.g., "Appetizers", "Main Course", "Drinks")
   - spice_level: If mentioned (mild, medium, hot, extra_hot)
   - dietary_info: Any dietary restrictions mentioned (vegetarian, vegan, gluten-free, halal, etc.)

3. If prices show ranges like "3000-5000", use the lower price
4. Handle handwritten menus if legible
5. Extract items from tables, lists, or any format visible

RESPOND WITH ONLY A JSON ARRAY of menu items. No explanations, no markdown, just the JSON array.
Example output:
[
  {"name": "Jollof Rice", "price": 3500, "currency": "NGN", "category": "Main Course"},
  {"name": "Chapman", "price": 1500, "currency": "NGN", "category": "Drinks", "description": "Nigerian cocktail"}
]`

	userPrompt := fmt.Sprintf(`Extract all menu items from this image. Use "%s" as the default currency if not clearly visible.`, defaultCurrency)

	// Build message with image content using ContentPart structure
	// Format: data:image/jpeg;base64,<base64_data>
	imageURL := fmt.Sprintf("data:%s;base64,%s", mimeType, imageData)

	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{
			Role: "user",
			Content: []ContentPart{
				{Type: "text", Text: userPrompt},
				{
					Type: "image_url",
					ImageURL: &struct {
						URL string `json:"url"`
					}{
						URL: imageURL,
					},
				},
			},
		},
	}

	response, err := c.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("AI vision generation failed: %w", err)
	}

	// Clean and parse response (reuse existing logic)
	response = cleanJSONResponse(response)

	var items []ParsedMenuItem
	if err := json.Unmarshal([]byte(response), &items); err != nil {
		// Try to extract JSON array from response
		if jsonStart := strings.Index(response, "["); jsonStart >= 0 {
			if jsonEnd := strings.LastIndex(response, "]"); jsonEnd > jsonStart {
				cleanJSON := response[jsonStart : jsonEnd+1]
				if err := json.Unmarshal([]byte(cleanJSON), &items); err != nil {
					return &ParseMenuTextResult{
						Items:         []ParsedMenuItem{},
						ParsedCount:   0,
						ParsingErrors: []string{fmt.Sprintf("Failed to parse AI vision response: %v", err)},
					}, nil
				}
			}
		} else {
			return &ParseMenuTextResult{
				Items:         []ParsedMenuItem{},
				ParsedCount:   0,
				ParsingErrors: []string{fmt.Sprintf("Invalid JSON response from AI vision: %v", err)},
			}, nil
		}
	}

	// Validate and clean up items
	validItems := make([]ParsedMenuItem, 0, len(items))
	for _, item := range items {
		if item.Name == "" {
			continue
		}
		if item.Currency == "" {
			item.Currency = defaultCurrency
		}
		validItems = append(validItems, item)
	}

	return &ParseMenuTextResult{
		Items:       validItems,
		ParsedCount: len(validItems),
	}, nil
}
