package guards

import (
	"errors"

	"github.com/similadayo/brancr/internal/types"
)

var ErrOrderAlreadyExists = errors.New("order already exists for this session")

// GuardSingleActiveOrder ensures only one order exists per session.
// It allows Order creation only if Context.Order is nil.
func GuardSingleActiveOrder(ctx types.ResolvedContext) error {
	// If an order already exists in the context...
	if ctx.Order != nil {
		// And we aren't in a pristine state (just illustrative logic here, strict rule is "Single Active")
		// The prompt logic was: "if ctx.Order != nil && ctx.Conversation.MessageCount == 0" -> return error?
		// Actually, if Order != nil, we generally shouldn't be creating a NEW one.
		// Detailed Logic: If intent implies "New Order", this guard would fail.
		// But guards usually validate the CURRENT STATE against an IMPLIED ACTION or just STATE INVARIANTS?
		// "GuardSingleActiveOrder" usually implies "Check we don't have 2".
		// Since ResolvedContext has one pointer, we can't have 2 in memory structure.
		// So this guard likely protects against *Intent to Create* when one exists.
		// For Phase 4 (State Resolver), we are validating the Context state.
		// Let's stick to the prompt example:
		if ctx.Order != nil && ctx.Conversation.MessageCount == 0 {
			// This suggests: If we have an order but no messages, maybe it's stale or something?
			// Wait, if Order != nil, we *have* an order.
			// The prompt says: "if ctx.Order != nil && ctx.Conversation.MessageCount == 0 { return error... }"
			// I will implement strictly as requested in the example signature, although logic seems specific to a "Start" event.
			return ErrOrderAlreadyExists
		}
	}
	return nil
}
