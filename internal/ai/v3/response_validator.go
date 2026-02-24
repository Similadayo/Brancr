package v3

import (
	"fmt"
	"regexp"
	"strings"
)

// ValidateResponse checks if the AI's output complies with mandatory facts.
func ValidateResponse(text string, info MandatoryInfo) error {
	lower := strings.ToLower(text)

	// 1. Check for Forbidden Terms (Rejections)
	for _, term := range info.ForbiddenTerms {
		if strings.Contains(lower, strings.ToLower(term)) {
			// Exception: "Not serving [Term]" is okay, but "Try [Term]" is not.
			// For strict safety, we ban it entirely for now unless context confirms rejection.
			// But user might say "I hate pizza" and AI says "Okay, no Pizza". That is fine.
			// So simple containment check is too aggressive?
			// Refined rule: We ban "recommend [Term]", "have [Term]", "try [Term]".
			// OR we trust Phase 7 prompt logic and use this as a hard fail-safe.
			// User plan says: "No rejected products mentioned".
			// I'll stick to strict containment for now to prove control.
			// If false positive occurs (e.g. "Okay, skipping Pizza"), we accept the fail-safe handles it (Regenerate).
			// Ideally, we want to allow "No [Term]".
			// Let's rely on the fact that if we regenerate with "You mentioned a rejected item", the AI fixes it.
			return fmt.Errorf("response mentions forbidden item: %s", term)
		}
	}

	// 2. Check for Required Information
	if info.PricesRequired {
		hasDigits := regexp.MustCompile(`\d+`).MatchString(text)
		if !hasDigits {
			return fmt.Errorf("response missing required prices/digits")
		}
	}

	if info.PaymentInfo {
		if !strings.Contains(lower, "bank") && !strings.Contains(lower, "account") && !strings.Contains(lower, "pay") {
			return fmt.Errorf("response missing payment context")
		}
	}

	// 3. Check for Product Mentions (Phase 9: Evasiveness Detector)
	// If we are strictly required to recommend or list prices, we MUST mention at least one product.
	if (info.RecommendationRequired || info.PricesRequired) && len(info.Products) > 0 {
		found := false
		for _, p := range info.Products {
			if strings.Contains(lower, strings.ToLower(p.Name)) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("response is evasive: MUST mention at least one valid product from context")
		}
	}

	return nil
}
