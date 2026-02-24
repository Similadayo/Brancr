package observability

// GetSafeModeResponse returns a polite, static message when AI is disabled.
// It ensures business continuity (mostly) without intelligence.
func GetSafeModeResponse(reason string) string {
	// We can log the reason internally but show generic text to user.
	return "I’m having a quick hiccup right now, but your order is safe. A human will assist you shortly."
}
