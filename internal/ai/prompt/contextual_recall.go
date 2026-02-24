package prompt

import (
	"fmt"
	"strings"

	"github.com/similadayo/brancr/internal/types"
)

// ContextualRecallBlock generates "Memory Hooks" to reference past interactions.
// ContextualRecallBlock generates "Memory Hooks" to reference past interactions.
func ContextualRecallBlock(ctx *types.ResolvedContext) string {
	b := ""

	// Notes & Preferences
	if len(ctx.Memory.Preferences) > 0 {
		b += fmt.Sprintf("MEMORY PREFERENCES: %s\n", strings.Join(ctx.Memory.Preferences, ", "))
	}

	if len(ctx.Memory.BehavioralNotes) > 0 {
		b += fmt.Sprintf("MEMORY NOTES: %s\n", strings.Join(ctx.Memory.BehavioralNotes, ", "))
	}

	if ctx.Memory.PaymentPreference != "" {
		b += fmt.Sprintf("PAYMENT PREF: %s\n", ctx.Memory.PaymentPreference)
	}

	if b != "" {
		return "### MEMORY (PAST HISTORY / PREFERENCES) ###\n(Use this only for personalization. Do not confuse with current order):\n" + b
	}
	return ""
}
