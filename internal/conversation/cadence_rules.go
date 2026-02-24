package conversation

import (
	"strings"

	"github.com/similadayo/brancr/internal/types"
)

// CadenceForContext estimates the appropriate cadence.
// Note: Ideally ResolvedContext would have explicit flags.
// Since we are integrating rapidly, we rely on checking ActionType/Intent if available in context
// or heuristic on the message content if allowed, but strict rules prefer Context.
// We assume we can pass the ActionType or Intent from the context.
func CadenceForContext(ctx types.ResolvedContext, actionType string) Cadence {

	// Critical Actions (Payment, Confirmation, Error) -> INSTANT
	if actionType == "CONFIRM_ORDER" ||
		actionType == "SEND_PAYMENT_LINK" ||
		actionType == "REPORT_ERROR" {
		return CadenceInstant
	}

	// If intent implies urgency
	if strings.Contains(strings.ToUpper(ctx.Conversation.LastIntent), "URGENT") {
		// But wait, step 9 handled wording. Step 10 says "Never delay payments".
		// Urgency in intent doesn't mean machine-gun typing, but "Asap" answers.
		// We'll stick to Natural unless it's a transactional step.
	}

	// Refusals/Boundaries (Step 5) -> Instant? No, can be natural.
	// "Sorry, we don't do that." (Pause) "Here is what we do." -> Natural.

	return CadenceNatural
}
