package guard

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/similadayo/brancr/internal/domain"
	"go.uber.org/zap"
)

// ValidateFactGrounding checks if LLM output contains hallucinated business facts.
// PHASE 4.75B: This is the hard safety wall that prevents fact hallucination.
//
// CRITICAL: This guard should ONLY be applied to fact-bearing responses.
// Free-form responses (greetings, empathy) should skip validation.
func ValidateFactGrounding(
	output string,
	catalog []domain.Offering,
	logger *zap.Logger,
) error {
	// Log catalog size for debugging
	if logger != nil {
		logger.Debug("fact_guard_validation",
			zap.Int("catalog_size", len(catalog)),
			zap.Int("output_length", len(output)),
		)
	}

	// 1. Detect prices in output
	priceRegex := regexp.MustCompile(`₦\s*\d+[,\d]*|\bNGN\s*\d+[,\d]*|\$\s*\d+[,\d]*`)
	detectedPrices := priceRegex.FindAllString(output, -1)

	if len(detectedPrices) > 0 {
		// Validate each detected price against catalog
		for _, priceStr := range detectedPrices {
			if !isPriceInCatalog(priceStr, catalog) {
				if logger != nil {
					logger.Warn("fact_violation_price_mismatch",
						zap.String("detected_price", priceStr),
						zap.String("output", output),
					)
				}
				return &FactViolationError{
					Type:   "price_mismatch",
					Detail: priceStr,
				}
			}
		}
	}

	// 2. Detect negative availability claims
	negativeKeywords := []string{
		"we don't have",
		"not available",
		"out of stock",
		"unavailable",
		"we don't offer",
		"we don't sell",
	}

	lowerOutput := strings.ToLower(output)
	for _, keyword := range negativeKeywords {
		if strings.Contains(lowerOutput, keyword) {
			// Check if catalog explicitly marks something as unavailable
			// For now, log and reject (stricter approach)
			if logger != nil {
				logger.Warn("fact_violation_negative_availability",
					zap.String("keyword", keyword),
					zap.String("output", output),
				)
			}
			return &FactViolationError{
				Type:   "negative_availability",
				Detail: keyword,
			}
		}
	}

	// 3. Detect product names not in catalog
	// This is more complex - for now, we rely on price and availability checks
	// TODO: Implement fuzzy product name matching

	return nil
}

// isPriceInCatalog checks if a price string matches any catalog item
func isPriceInCatalog(priceStr string, catalog []domain.Offering) bool {
	// Extract numeric value from price string
	priceValue := extractNumericPrice(priceStr)
	if priceValue == 0 {
		return false // Couldn't parse price
	}

	// Check against all catalog prices
	for _, item := range catalog {
		catalogPrice := extractNumericPrice(item.PriceText)
		if catalogPrice == priceValue {
			return true
		}
	}

	return false
}

// extractNumericPrice extracts the numeric value from a price string
// Examples: "₦3,500" -> 3500, "NGN 4500" -> 4500
func extractNumericPrice(priceStr string) int {
	// Remove currency symbols and spaces
	cleaned := strings.ReplaceAll(priceStr, "₦", "")
	cleaned = strings.ReplaceAll(cleaned, "NGN", "")
	cleaned = strings.ReplaceAll(cleaned, "$", "")
	cleaned = strings.ReplaceAll(cleaned, ",", "")
	cleaned = strings.TrimSpace(cleaned)

	// Extract first number
	numRegex := regexp.MustCompile(`\d+`)
	match := numRegex.FindString(cleaned)
	if match == "" {
		return 0
	}

	// Convert to int
	value := 0
	fmt.Sscanf(match, "%d", &value)
	return value
}

// FactViolationError represents a fact grounding violation
type FactViolationError struct {
	Type   string // "price_mismatch", "negative_availability", "product_not_found"
	Detail string
}

func (e *FactViolationError) Error() string {
	return "fact grounding violation: " + e.Type + " - " + e.Detail
}
