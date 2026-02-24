package quota

import (
	"fmt"
)

// ModelPricing defines cost per 1M tokens for different models
// Prices are in USD per million tokens (as of 2026, update as needed)
var ModelPricing = map[string]struct {
	InputCostPer1M  float64 // Cost per 1M input tokens
	OutputCostPer1M float64 // Cost per 1M output tokens
}{
	// GPT-5 Series (Primary models in use)
	"gpt-5-nano": {
		InputCostPer1M:  0.15, // $0.15 per 1M input tokens (matches gpt-4o-mini)
		OutputCostPer1M: 0.60, // $0.60 per 1M output tokens
	},
	"gpt-5-mini": {
		InputCostPer1M:  2.50,  // $2.50 per 1M input tokens (matches gpt-4o)
		OutputCostPer1M: 10.00, // $10.00 per 1M output tokens
	},
	"gpt-5": {
		InputCostPer1M:  1.25,  // $1.25 per 1M input tokens
		OutputCostPer1M: 10.00, // $10.00 per 1M output tokens
	},
	"gpt-5.1": {
		InputCostPer1M:  1.25,
		OutputCostPer1M: 10.00,
	},
	"gpt-5.2": {
		InputCostPer1M:  1.75,
		OutputCostPer1M: 14.00,
	},
	// GPT-4o models (legacy fallback)
	"gpt-4o": {
		InputCostPer1M:  2.50,  // $2.50 per 1M input tokens
		OutputCostPer1M: 10.00, // $10.00 per 1M output tokens
	},
	"gpt-4o-mini": {
		InputCostPer1M:  0.15, // $0.15 per 1M input tokens
		OutputCostPer1M: 0.60, // $0.60 per 1M output tokens
	},
	"gpt-4o-2024-08-06": {
		InputCostPer1M:  2.50,
		OutputCostPer1M: 10.00,
	},
	"gpt-4o-mini-2024-07-18": {
		InputCostPer1M:  0.15,
		OutputCostPer1M: 0.60,
	},
	// GPT-4 Turbo (legacy)
	"gpt-4-turbo": {
		InputCostPer1M:  10.00,
		OutputCostPer1M: 30.00,
	},
	"gpt-4-turbo-preview": {
		InputCostPer1M:  10.00,
		OutputCostPer1M: 30.00,
	},
	// GPT-4 (legacy - expensive)
	"gpt-4": {
		InputCostPer1M:  30.00,
		OutputCostPer1M: 60.00,
	},
	// GPT-3.5 Turbo (legacy)
	"gpt-3.5-turbo": {
		InputCostPer1M:  0.50,
		OutputCostPer1M: 1.50,
	},
	"gpt-3.5-turbo-16k": {
		InputCostPer1M:  3.00,
		OutputCostPer1M: 4.00,
	},
	// Default fallback (use gpt-5-nano pricing since it's the primary model)
	"default": {
		InputCostPer1M:  0.05,
		OutputCostPer1M: 0.40,
	},
}

// TokenUsage represents token usage from an AI API call
type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// CostCalculator calculates dollar costs from token usage
type CostCalculator struct{}

// NewCostCalculator creates a new cost calculator
func NewCostCalculator() *CostCalculator {
	return &CostCalculator{}
}

// CalculateCost calculates the dollar cost for a given model and token usage
// Returns cost in USD
func (c *CostCalculator) CalculateCost(model string, usage *TokenUsage) (float64, error) {
	if usage == nil {
		return 0, fmt.Errorf("token usage is required")
	}

	// Get pricing for model (with fallback)
	pricing, exists := ModelPricing[model]
	if !exists {
		// Try to match partial model name (e.g., "gpt-4o" matches "gpt-4o-2024-08-06")
		pricing = c.findMatchingPricing(model)
		if pricing.InputCostPer1M == 0 && pricing.OutputCostPer1M == 0 {
			// Use default pricing as last resort
			pricing = ModelPricing["default"]
		}
	}

	// Calculate costs
	inputCost := (float64(usage.PromptTokens) / 1_000_000.0) * pricing.InputCostPer1M
	outputCost := (float64(usage.CompletionTokens) / 1_000_000.0) * pricing.OutputCostPer1M
	totalCost := inputCost + outputCost

	return totalCost, nil
}

// findMatchingPricing tries to find pricing for a model by partial match
func (c *CostCalculator) findMatchingPricing(model string) struct {
	InputCostPer1M  float64
	OutputCostPer1M float64
} {
	// Try to match by prefix (e.g., "gpt-4o" matches "gpt-4o-mini")
	for key, pricing := range ModelPricing {
		if key == "default" {
			continue
		}
		// Check if model starts with key or key starts with model
		if len(model) >= len(key) && model[:len(key)] == key {
			return pricing
		}
		if len(key) >= len(model) && key[:len(model)] == model {
			return pricing
		}
	}
	return struct {
		InputCostPer1M  float64
		OutputCostPer1M float64
	}{0, 0}
}

// EstimateCost estimates cost based on message length (for pre-call checks)
// This is a rough estimate before the actual API call
func (c *CostCalculator) EstimateCost(model string, estimatedInputTokens, estimatedOutputTokens int) float64 {
	pricing, exists := ModelPricing[model]
	if !exists {
		pricing = c.findMatchingPricing(model)
		if pricing.InputCostPer1M == 0 && pricing.OutputCostPer1M == 0 {
			pricing = ModelPricing["default"]
		}
	}

	inputCost := (float64(estimatedInputTokens) / 1_000_000.0) * pricing.InputCostPer1M
	outputCost := (float64(estimatedOutputTokens) / 1_000_000.0) * pricing.OutputCostPer1M
	return inputCost + outputCost
}
