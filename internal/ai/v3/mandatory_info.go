package v3

// MandatoryInfo defines facts that MUST be present in the response,
// regardless of tone or persona. This enforces the "Hard Separation"
// between what is said (Facts) and how it is said (Style).
type MandatoryInfo struct {
	// Products that must be mentioned (e.g. invalidation of active rec, or menu list)
	Products []Product

	// Terms that MUST NOT appear (e.g. rejected items)
	ForbiddenTerms []string

	// Constraints
	PricesRequired         bool
	ClarifyType            bool // If intent is ambiguous (e.g. "Do you have food?")
	DeliveryInfo           bool
	PaymentInfo            bool
	RecommendationRequired bool
}
