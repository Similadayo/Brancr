package context

import (
	"fmt"

	"github.com/similadayo/brancr/internal/tenant"
)

// BuildTenantSummary converts runtime tenant data
// into a plain-language summary for AI consumption.
func BuildTenantSummary(p tenant.RuntimeProfile) string {
	return fmt.Sprintf(
		`Business context:
- Offering: %s
- Pricing style: %s
- Brand tone: %s`,
		p.OfferSummary,
		p.PricingStyle,
		p.TonePreset,
	)
}
