package tenant

import "github.com/similadayo/brancr/internal/persona"

// OfferType describes what the tenant primarily sells.
type OfferType string

const (
	OfferProduct OfferType = "PRODUCT"
	OfferService OfferType = "SERVICE"
	OfferMixed   OfferType = "MIXED"
)

// PricingStyle describes how pricing works.
type PricingStyle string

const (
	PricingFixed      PricingStyle = "FIXED"
	PricingNegotiable PricingStyle = "NEGOTIABLE"
	PricingQuote      PricingStyle = "QUOTE"
)

// RuntimeProfile is a derived, read-only view of tenant business context.
// It is safe to expose to the AI as a summarized description.
type RuntimeProfile struct {
	TenantID string

	OfferType    OfferType
	OfferSummary string

	PricingStyle PricingStyle

	TonePreset  persona.TonePreset
	BrandTraits []string

	Language string

	// Operational Tags for Feature Flagging
	Tags []string
}
