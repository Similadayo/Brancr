package industry

type Capability struct {
	SupportsOrdering    bool
	SupportsDelivery    bool
	SupportsPayment     bool
	SupportsBooking     bool
	SupportsNegotiation bool
}

// CapabilityMap defines features available per industry
var CapabilityMap = map[string]Capability{
	"Food & Beverage": {
		SupportsOrdering:    true,
		SupportsDelivery:    true,
		SupportsPayment:     true,
		SupportsNegotiation: false,
	},
	"Fashion & Retail": {
		SupportsOrdering:    true,
		SupportsDelivery:    true,
		SupportsPayment:     true,
		SupportsNegotiation: true,
	},
	"E-commerce": {
		SupportsOrdering:    true,
		SupportsDelivery:    true,
		SupportsPayment:     true,
		SupportsNegotiation: true,
	},
	"Agency": {
		SupportsBooking:     true,
		SupportsOrdering:    false, // Service booking, not product ordering
		SupportsDelivery:    false,
		SupportsNegotiation: true,
	},
	"Consultancy": {
		SupportsBooking:     true,
		SupportsOrdering:    false,
		SupportsDelivery:    false,
		SupportsNegotiation: true,
	},
	"Education": {
		SupportsBooking:     true,
		SupportsOrdering:    false,
		SupportsDelivery:    false,
		SupportsNegotiation: false,
	},
	"Services": {
		SupportsBooking:     true,
		SupportsOrdering:    false,
		SupportsDelivery:    false,
		SupportsNegotiation: true,
	},
	// Default fallback
	"Generic": {
		SupportsOrdering: true,
		SupportsPayment:  true,
	},
}

// GetCapability returns the capabilities for a given industry
func GetCapability(industryName string) Capability {
	if cap, ok := CapabilityMap[industryName]; ok {
		return cap
	}
	// Try case-insensitive matching if direct lookup fails
	// For now, return generic fallback
	return CapabilityMap["Generic"]
}
