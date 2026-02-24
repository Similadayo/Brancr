package catalog

import (
	"strings"

	"github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/ai_v4/types"
)

// UnifyOfferings converts known items from snapshot into unified Offering slice
func UnifyOfferings(k *context.TenantKnowledgeSnapshot) []types.Offering {
	var out []types.Offering
	for _, m := range k.MenuItems {
		isAvailable := strings.ToLower(strings.TrimSpace(m.Availability)) != "unavailable"
		out = append(out, types.Offering{
			Name:        m.Name,
			Description: m.Description,
			Price:       m.Price,
			Type:        "menu",
			IsAvailable: isAvailable,
		})
	}
	for _, p := range k.Products {
		isAvailable := strings.ToLower(strings.TrimSpace(p.Availability)) != "out_of_stock" && p.StockCount != 0
		out = append(out, types.Offering{
			Name:        p.Name,
			Description: p.Description,
			Price:       p.Price,
			Type:        "product",
			IsAvailable: isAvailable,
		})
	}
	for _, s := range k.Services {
		// Services often have non-numeric pricing, Offering.Price is float64.
		// Best effort mapping for Name/Desc. Price might be 0 if string.
		out = append(out, types.Offering{Name: s.Name, Description: s.Description, Type: "service"})
	}
	return out
}

// DetectCatalogQueryMode determines if the user is browsing or asking for details (Step 3)
func DetectCatalogQueryMode(
	message string,
	offerings []types.Offering,
) types.CatalogQueryMode {

	msg := strings.ToLower(message)

	detailKeywords := []string{
		"explain", "details", "detail",
		"tell me more", "what is", "what does",
		"include", "included", "describe",
		"specs", "specifications", // retail-safe
		"features", "break down",
	}

	if ResolveFocusedItem(message, offerings) != "" {
		// If an item is clearly referenced (even with typos), require detail keywords
		// before switching to detail mode.
		for _, kw := range detailKeywords {
			if strings.Contains(msg, kw) {
				return types.CatalogDetail
			}
		}
	}

	return types.CatalogBrowse
}

// ResolveFocusedItem extracts the exact item name being discussed (Step 4)
func ResolveFocusedItem(
	message string,
	offerings []types.Offering,
) string {

	rawNormalized := normalizeForMatching(message)
	queryTokens := tokenize(rawNormalized)
	bestName := ""
	bestScore := 0
	for _, o := range offerings {
		score := scoreCatalogNameMatch(rawNormalized, queryTokens, o.Name)
		if score > bestScore {
			bestScore = score
			bestName = o.Name
		}
	}

	if bestScore < 220 {
		return ""
	}
	return bestName
}
