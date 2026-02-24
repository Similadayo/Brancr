package v3

// =============================================================================
// SOCIAL MOVE TRACKER - "What did the AI just do socially?"
// =============================================================================

// SocialMove represents what the AI did in social terms, not technical terms.
// This is the key to preventing repetition and knowing when to stay silent.
type SocialMove string

const (
	MoveNone             SocialMove = "none"
	MoveGreeted          SocialMove = "greeted"
	MoveListedMenu       SocialMove = "listed_menu"
	MoveConfirmedItem    SocialMove = "confirmed_item"
	MoveRequestedPayment SocialMove = "requested_payment"
	MoveNegotiated       SocialMove = "negotiated"
	MoveClarified        SocialMove = "clarified"
	MoveEscalated        SocialMove = "escalated"
	MoveAcknowledged     SocialMove = "acknowledged"
	MoveSilent           SocialMove = "silent"
)

// deriveSocialMove maps technical actions to social moves.
// CRITICAL: The LLM never decides what social move happened - this is deterministic.
func deriveSocialMove(action Action) SocialMove {
	switch action.Type {
	case ActionGreet:
		return MoveGreeted

	case ActionShowProducts:
		return MoveListedMenu

	case ActionAddToCart:
		return MoveConfirmedItem

	case ActionShowPayment:
		return MoveRequestedPayment

	case ActionNegotiate:
		return MoveNegotiated

	case ActionAskClarification:
		return MoveClarified

	case ActionEscalate:
		return MoveEscalated

	case ActionDefer:
		return MoveSilent

	case ActionRemoveFromCart, ActionClearCart:
		return MoveConfirmedItem

	case ActionConfirmOrder:
		return MoveConfirmedItem

	case ActionShowCart, ActionShowStatus:
		return MoveAcknowledged
	}

	return MoveAcknowledged
}

// =============================================================================
// SOCIAL MOVE GUARDS - Prevent repetition and reopening loops
// =============================================================================

// preventRepeatMove limits verbosity when repeating the same social move.
// IMPORTANT: Filters LLM output, doesn't replace it.
func preventRepeatMove(messages []string, ctx *ConversationContext, action Action) []string {
	if len(messages) == 0 {
		return messages
	}

	nextMove := deriveSocialMove(action)

	// If repeating same move, be more concise (trim to 1 message)
	if ctx.LastSocialMove == nextMove && nextMove != MoveNone {
		if len(messages) > 1 {
			return messages[:1] // Keep first LLM message only
		}
	}

	return messages
}

// preventLoopReopen limits talking after closure points.
// IMPORTANT: Filters LLM output, doesn't replace it.
func preventLoopReopen(messages []string, ctx *ConversationContext, action Action) []string {
	if len(messages) == 0 {
		return messages
	}

	// If payment was already requested, be concise
	if ctx.LastSocialMove == MoveRequestedPayment && action.Type != ActionEscalate {
		if len(messages) > 1 {
			return messages[:1] // Keep first LLM message only
		}
	}

	return messages
}

// shouldGoSilent determines if the AI should just acknowledge and stop talking.
// High confidence + forward momentum + closure = silence.
func shouldGoSilent(ctx *ConversationContext, action Action) bool {
	switch ctx.LastSocialMove {
	case MoveConfirmedItem, MoveRequestedPayment:
		return true
	}

	// High confidence + forward momentum = silence
	return ctx.CommitmentGradient > 0.7 && ctx.UserSignals.Momentum > 0
}
