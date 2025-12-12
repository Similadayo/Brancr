package tenantapi

import (
	"fmt"
	"strings"
)

const (
	negotiationModeDefault    = "default"
	negotiationModeFixed      = "fixed"
	negotiationModeNegotiable = "negotiable"
)

func normalizeNegotiationMode(mode string) (string, bool) {
	m := strings.TrimSpace(strings.ToLower(mode))
	if m == "" {
		return negotiationModeDefault, true
	}
	switch m {
	case negotiationModeDefault, negotiationModeFixed, negotiationModeNegotiable:
		return m, true
	default:
		return "", false
	}
}

func validateNegotiation(mode string, minPrice, maxPrice float64) error {
	switch mode {
	case negotiationModeDefault, negotiationModeFixed:
		if minPrice != 0 || maxPrice != 0 {
			return fmt.Errorf("negotiation_min_price and negotiation_max_price must be 0 unless negotiation_mode=negotiable")
		}
		return nil
	case negotiationModeNegotiable:
		if minPrice <= 0 || maxPrice <= 0 {
			return fmt.Errorf("negotiation_min_price and negotiation_max_price must be > 0 when negotiation_mode=negotiable")
		}
		if minPrice > maxPrice {
			return fmt.Errorf("negotiation_min_price must be <= negotiation_max_price")
		}
		return nil
	default:
		return fmt.Errorf("invalid negotiation_mode")
	}
}
