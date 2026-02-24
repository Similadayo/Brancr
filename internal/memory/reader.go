package memory

// Summary represents the aggregated, read-only view of a customer's memory context.
// This is passed to the AI as "Advisory Information".
type Summary struct {
	PaymentPreference string
	PriceSensitivity  string
	Notes             []string // Synthesized highlights
}
