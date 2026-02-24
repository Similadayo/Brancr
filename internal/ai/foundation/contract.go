package foundation

// NOTE:
// AIResponse is advisory only.
// It does NOT mutate system state.
// Execution is handled by the core system.
type AIResponse struct {
	MessageToUser   string
	SuggestedAction string
	Confidence      float64
}
