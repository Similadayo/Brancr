package ai

// ============================================================
// INDUSTRY PROFILE LAYER (Blueprint Section 1.1-1.2)
// Purpose: Per-tenant behavior configuration based on industry
// AI behavior is injected explicitly from tenant config, never inferred
// ============================================================

// IndustryProfile defines configurable behavior parameters per industry
type IndustryProfile struct {
	Industry             string `json:"industry"`                  // restaurant, retail, high_ticket_retail, services
	OrderComplexity      string `json:"order_complexity"`          // low, medium, high
	NegotiationEnabled   bool   `json:"negotiation_enabled"`       // Whether price negotiation is allowed
	NegotiationMaxRounds int    `json:"negotiation_max_rounds"`    // Max negotiation rounds (0 = disabled)
	DefaultWindowMs      int    `json:"default_message_window_ms"` // Base message buffer window
	MaxWindowMs          int    `json:"max_message_window_ms"`     // Maximum message buffer window
	ResponseChunkLimit   int    `json:"response_chunk_limit"`      // Max messages per reply sequence
	DiscoveryDepth       string `json:"discovery_depth"`           // shallow, normal, deep
	EscalationThreshold  string `json:"escalation_threshold"`      // low, medium, high
	OrderCreationDelayed bool   `json:"order_creation_delayed"`    // For services - no immediate order
}

// Default profiles by industry (Blueprint Section 1.2)
var industryProfiles = map[string]*IndustryProfile{
	"restaurant": {
		Industry:             "restaurant",
		OrderComplexity:      "low",
		NegotiationEnabled:   false,
		NegotiationMaxRounds: 1,
		DefaultWindowMs:      2000,
		MaxWindowMs:          5000,
		ResponseChunkLimit:   2,
		DiscoveryDepth:       "shallow",
		EscalationThreshold:  "low",
		OrderCreationDelayed: false,
	},
	"retail": {
		Industry:             "retail",
		OrderComplexity:      "medium",
		NegotiationEnabled:   true,
		NegotiationMaxRounds: 2,
		DefaultWindowMs:      2000,
		MaxWindowMs:          8000,
		ResponseChunkLimit:   3,
		DiscoveryDepth:       "normal",
		EscalationThreshold:  "medium",
		OrderCreationDelayed: false,
	},
	"high_ticket_retail": {
		Industry:             "high_ticket_retail",
		OrderComplexity:      "high",
		NegotiationEnabled:   true,
		NegotiationMaxRounds: 4,
		DefaultWindowMs:      3000,
		MaxWindowMs:          10000,
		ResponseChunkLimit:   3,
		DiscoveryDepth:       "deep",
		EscalationThreshold:  "high",
		OrderCreationDelayed: false,
	},
	"services": {
		Industry:             "services",
		OrderComplexity:      "high",
		NegotiationEnabled:   true,
		NegotiationMaxRounds: 3,
		DefaultWindowMs:      3000,
		MaxWindowMs:          10000,
		ResponseChunkLimit:   3,
		DiscoveryDepth:       "deep",
		EscalationThreshold:  "medium",
		OrderCreationDelayed: true, // Services need qualification before order
	},
}

// Default profile for unknown industries
var defaultProfile = &IndustryProfile{
	Industry:             "general",
	OrderComplexity:      "medium",
	NegotiationEnabled:   true,
	NegotiationMaxRounds: 2,
	DefaultWindowMs:      2000,
	MaxWindowMs:          8000,
	ResponseChunkLimit:   3,
	DiscoveryDepth:       "normal",
	EscalationThreshold:  "medium",
	OrderCreationDelayed: false,
}

// GetIndustryProfile returns the profile for a given industry
// Returns default profile if industry not found
func GetIndustryProfile(industry string) *IndustryProfile {
	if profile, ok := industryProfiles[industry]; ok {
		return profile
	}
	return defaultProfile
}

// GetIndustryProfileWithOverrides returns profile with custom overrides applied
// Useful when tenant has custom settings that differ from industry defaults
func GetIndustryProfileWithOverrides(industry string, overrides map[string]interface{}) *IndustryProfile {
	base := GetIndustryProfile(industry)

	// Create a copy to avoid mutating the default
	profile := *base

	// Apply overrides
	if val, ok := overrides["negotiation_enabled"].(bool); ok {
		profile.NegotiationEnabled = val
	}
	if val, ok := overrides["negotiation_max_rounds"].(int); ok {
		profile.NegotiationMaxRounds = val
	}
	if val, ok := overrides["default_window_ms"].(int); ok {
		profile.DefaultWindowMs = val
	}
	if val, ok := overrides["max_window_ms"].(int); ok {
		profile.MaxWindowMs = val
	}
	if val, ok := overrides["response_chunk_limit"].(int); ok {
		profile.ResponseChunkLimit = val
	}
	if val, ok := overrides["discovery_depth"].(string); ok {
		profile.DiscoveryDepth = val
	}
	if val, ok := overrides["escalation_threshold"].(string); ok {
		profile.EscalationThreshold = val
	}

	return &profile
}
