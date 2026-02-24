package response

// AIResponse is INTERNAL ONLY.
// It must NEVER be sent directly to any UI or channel.
type AIResponse struct {
	HumanText string         // Only user-safe content
	Meta      map[string]any // Internal use ONLY
}
