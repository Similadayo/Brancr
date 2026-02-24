package domain

// Offering is a normalized, AI-safe representation of a product or service.
// It contains NO raw DB fields like IDs, timestamps, or availability flags.
type Offering struct {
	Title       string
	Description string
	PriceText   string   // already formatted (e.g. "₦3,500")
	Extras      []string // options, add-ons, notes
}
