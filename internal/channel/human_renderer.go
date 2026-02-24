package channel

import "github.com/similadayo/brancr/internal/ai/response"

// WARNING:
// - Never send AIResponse directly to any UI
// - Never expose Meta to users
// - Never send JSON, maps, or structs
// - Only RenderForHuman() output may be sent
// Violations break human realism and are bugs.

// RenderForHuman is the FINAL gate before sending messages to users.
// It strips all metadata and guarantees only clean human text leaves the system.
func RenderForHuman(resp response.AIResponse) string {
	return resp.HumanText
}
