package prompt

import (
	"fmt"
	"strings"

	"github.com/similadayo/brancr/internal/domain"
)

// CatalogBlock generates the "CATALOG" section of the prompt.
// It summarizes available products/services.
func CatalogBlock(offerings []domain.Offering) string {
	if len(offerings) == 0 {
		return ""
	}

	b := strings.Builder{}

	// Phase 4.75A: Fact Authority Declaration
	b.WriteString("CRITICAL RULES (NON-NEGOTIABLE):\n")
	b.WriteString("- You MUST ONLY refer to products explicitly listed in the PRODUCT CATALOG below\n")
	b.WriteString("- You MUST use EXACT prices as provided\n")
	b.WriteString("- You MUST NOT infer availability, variants, toppings, or exclusions\n")
	b.WriteString("- You MUST NOT say \"we don't have X\" unless X is explicitly marked unavailable\n")
	b.WriteString("- If information is missing, you MUST ask for clarification\n")
	b.WriteString("- NEVER guess, estimate, or invent business facts\n\n")

	b.WriteString("CATALOG / MENU:\n")

	// Limit to ~20 items for prompt safety if list is huge (naive truncation)
	// In production, RAG should pre-filter this list.
	limit := 20
	count := 0

	for _, item := range offerings {
		if count >= limit {
			b.WriteString(fmt.Sprintf("...and %d more items.\n", len(offerings)-count))
			break
		}

		// Full Detail Format:
		// - Name (Price)
		//   Description: ...
		//   Options/Toppings: ...
		b.WriteString(fmt.Sprintf("- %s (%s)\n", item.Title, item.PriceText))

		if item.Description != "" {
			b.WriteString(fmt.Sprintf("  Description: %s\n", item.Description))
		}

		if len(item.Extras) > 0 {
			b.WriteString(fmt.Sprintf("  Options/Toppings: %s\n", strings.Join(item.Extras, ", ")))
		}

		count++
	}

	return b.String()
}
