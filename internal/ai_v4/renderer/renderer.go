package renderer

import (
	"strings"

	"github.com/similadayo/brancr/internal/ai_v4/types"
)

// OutboundChatMessage is the ONE canonical payload for the UI.
// No internal structs, no media URLs, no complexity.
type OutboundChatMessage struct {
	Text string `json:"text"`
}

// RenderForChat converts an internal AIResponse into a single text string.
// It prioritizes the explicit reply text but falls back to joining segments if needed.
func RenderForChat(response *types.AIResponse) string {
	if response == nil {
		return ""
	}

	// Prefer ReplyText if present (this is the main source of truth)
	if response.ReplyText != "" {
		return response.ReplyText
	}

	// Fallback: join segments safely if ReplyText is empty
	if len(response.ReplySegments) > 0 {
		return strings.Join(response.ReplySegments, "\n")
	}

	return ""
}
