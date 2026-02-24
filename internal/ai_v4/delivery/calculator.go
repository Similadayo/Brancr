package delivery

import (
	"context"
	"strings"

	"github.com/similadayo/brancr/internal/storage"
)

// CalculateDeliveryFee calculates delivery fee based on address and delivery areas
// Returns: areaName, fee, outsideZone (true if no configured area matched), error
func CalculateDeliveryFee(
	ctx context.Context,
	address string,
	deliveryConfig *storage.DeliveryConfig,
) (areaName string, fee float64, outsideZone bool, err error) {

	if !deliveryConfig.DeliveryEnabled || len(deliveryConfig.DeliveryAreas) == 0 {
		// No delivery areas configured - use default fee if set
		if deliveryConfig.DefaultDeliveryFee > 0 {
			return "Standard Delivery", deliveryConfig.DefaultDeliveryFee, false, nil
		}
		return "", 0, false, nil
	}

	// Fuzzy match area name from address
	matchedArea := fuzzyMatchArea(address, deliveryConfig.DeliveryAreas)

	if matchedArea != "" {
		// Find the matched area and return its price
		for _, area := range deliveryConfig.DeliveryAreas {
			if strings.EqualFold(area.Name, matchedArea) {
				return area.Name, area.Price, false, nil
			}
		}
	}

	// No match - use default fee if set, mark as outside zone
	if deliveryConfig.DefaultDeliveryFee > 0 {
		return "Outside delivery zone", deliveryConfig.DefaultDeliveryFee, true, nil
	}

	// No default fee configured - return zero fee but mark as outside zone
	return "", 0, true, nil
}

// fuzzyMatchArea attempts to find an area name within the address string
func fuzzyMatchArea(address string, areas []storage.DeliveryArea) string {
	addressLower := strings.ToLower(address)

	// Try exact substring match first
	for _, area := range areas {
		areaLower := strings.ToLower(area.Name)
		if strings.Contains(addressLower, areaLower) {
			return area.Name
		}
	}

	// Try word-by-word match
	addressWords := strings.Fields(addressLower)
	for _, area := range areas {
		areaWords := strings.Fields(strings.ToLower(area.Name))
		for _, areaWord := range areaWords {
			for _, addressWord := range addressWords {
				if areaWord == addressWord {
					return area.Name
				}
			}
		}
	}

	return ""
}
