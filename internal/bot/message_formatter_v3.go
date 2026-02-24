package bot

import (
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
)

// ============================================================
// MESSAGE FORMATTER V3 - Unified, Concise Messaging
// ============================================================
// Design Principles:
// 1. Max 3 lines for any message
// 2. Action-focused, not information-heavy
// 3. Emoji for quick visual scanning
// 4. Truncate with "..." rather than overflow
// ============================================================

// V3FeatureFlag controls whether V3 flows are active
// Set via environment variable BRANCR_V3_ENABLED=true
func IsV3Enabled() bool {
	return os.Getenv("BRANCR_V3_ENABLED") == "true"
}

// IsV3EnabledForTenant checks if V3 is enabled for a specific tenant
// Allows gradual rollout by tenant ID
func IsV3EnabledForTenant(tenantID uuid.UUID) bool {
	// Check global flag first
	if !IsV3Enabled() {
		return false
	}

	// Check for tenant-specific opt-out
	optOut := os.Getenv("BRANCR_V3_OPTOUT_TENANTS")
	if optOut != "" {
		tenantStr := tenantID.String()
		if strings.Contains(optOut, tenantStr) {
			return false
		}
	}

	return true
}

// ============================================================
// UNIFIED MESSAGE FORMATTERS
// ============================================================

// FormatSuccessV3 creates a short success message (1 line)
func FormatSuccessV3(action string) string {
	return fmt.Sprintf("✅ %s", action)
}

// FormatErrorV3 creates a short error message (1 line)
func FormatErrorV3(action string) string {
	return fmt.Sprintf("❌ %s", action)
}

// FormatConfirmV3 creates a confirmation message (2 lines max)
func FormatConfirmV3(what, when string) string {
	return fmt.Sprintf("✅ %s\n→ %s", what, when)
}

// FormatQuestionV3 creates a question (2 lines max)
func FormatQuestionV3(question string) string {
	return question
}

// FormatListV3 creates a short list (3 items max visible)
func FormatListV3(title string, items []string) string {
	if len(items) == 0 {
		return fmt.Sprintf("%s\nNone", title)
	}

	// Show max 3 items
	shown := items
	more := 0
	if len(items) > 3 {
		shown = items[:3]
		more = len(items) - 3
	}

	lines := []string{title}
	for _, item := range shown {
		lines = append(lines, fmt.Sprintf("• %s", TruncateV3(item, 40)))
	}

	if more > 0 {
		lines = append(lines, fmt.Sprintf("...and %d more", more))
	}

	return strings.Join(lines, "\n")
}

// FormatStatusV3 creates a status update (2 lines)
func FormatStatusV3(emoji, status, detail string) string {
	return fmt.Sprintf("%s %s\n%s", emoji, status, detail)
}

// ============================================================
// TEXT UTILITIES
// ============================================================

// TruncateV3 truncates text with ellipsis
func TruncateV3(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return "..."
	}
	return s[:maxLen-3] + "..."
}

// CleanMessageV3 removes excessive newlines and trims
func CleanMessageV3(s string) string {
	// Replace multiple newlines with double newline
	s = strings.TrimSpace(s)
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return s
}

// ============================================================
// PLATFORM HELPERS
// ============================================================

// GetPlatformShortV3 returns abbreviated platform name
func GetPlatformShortV3(platform string) string {
	switch strings.ToLower(platform) {
	case "instagram":
		return "IG"
	case "facebook":
		return "FB"
	case "twitter", "x":
		return "X"
	case "tiktok":
		return "TT"
	case "whatsapp":
		return "WA"
	case "telegram":
		return "TG"
	default:
		return platform
	}
}

// GetPlatformEmojiV3 returns emoji for platform
func GetPlatformEmojiV3(platform string) string {
	switch strings.ToLower(platform) {
	case "instagram":
		return "📷"
	case "facebook":
		return "📘"
	case "twitter", "x":
		return "🐦"
	case "tiktok":
		return "🎵"
	case "whatsapp":
		return "💬"
	case "telegram":
		return "✈️"
	default:
		return "📱"
	}
}

// ============================================================
// PRE-BUILT MESSAGES
// ============================================================

// StandardMessages contains reusable V3 messages
var StandardMessagesV3 = struct {
	PhotoReceived  string
	VideoReceived  string
	Processing     string
	Cancelled      string
	UndoExpired    string
	NotFound       string
	SessionExpired string
	NoConnections  string
	AllCaughtUp    string
}{
	PhotoReceived:  "📸 Got it!",
	VideoReceived:  "🎬 Got it!",
	Processing:     "⏳ Processing...",
	Cancelled:      "👍 Cancelled.",
	UndoExpired:    "⏰ Undo window expired.",
	NotFound:       "❌ Not found.",
	SessionExpired: "⏰ Session expired. Start again?",
	NoConnections:  "No accounts connected. Say \"connect\" to add one!",
	AllCaughtUp:    "All caught up! 🎉",
}

// ============================================================
// DEPRECATION MARKERS
// ============================================================
// Add these comments to old V2 files to mark for deprecation:
//
// // DEPRECATED: Use posting_v3.go instead
// // This file will be removed after V3 migration is complete
// // Migration date target: 2 weeks after V3 launch
//
// Files to deprecate:
// - post_flow.go (original, complex)
// - post_flow_v2.go (intermediate)
// - ux_formatter.go (replaced by message_formatter_v3.go)
// ============================================================
