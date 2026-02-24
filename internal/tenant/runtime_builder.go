package tenant

import (
	"context"

	"github.com/similadayo/brancr/internal/persona"
	"gorm.io/gorm"
)

// BuildRuntimeProfile constructs the profile from DB data
func BuildRuntimeProfile(ctx context.Context, db *gorm.DB, tenantID string) (RuntimeProfile, error) {
	// 1. Fetch Tenant basic info
	// In a real app, you'd fetch from 'tenants' table.
	// We'll mock the extraction from a theoretical model or just default it
	// based on the assumption that 'tenantID' is valid.

	// Placeholder for DB lookup
	// var t TenantModel
	// if err := db.First(&t, "id = ?", tenantID).Error; err != nil { return ... }

	// 2. Construct Profile
	// Defaults for now as we transition.
	// Step 3 asks for "Tone Source of Truth" = RuntimeProfile.
	// We'll default to Professional.
	// Real implementation would read t.Tone, t.Industry, etc.

	return RuntimeProfile{
		TenantID:     tenantID,
		OfferType:    OfferMixed,               // Default safe value
		OfferSummary: "Catalog Items",          // Generic
		PricingStyle: PricingQuote,             // Safer default than Fixed
		TonePreset:   persona.ToneProfessional, // Default per Step 3
		Language:     "en",
		BrandTraits:  []string{},
		Tags:         []string{}, // Empty by default
	}, nil
}
