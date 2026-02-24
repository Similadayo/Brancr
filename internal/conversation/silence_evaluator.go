package conversation

import (
	"time"

	"github.com/similadayo/brancr/internal/domain"
)

const (
	SoftReengageAfter = 3 * time.Minute
	StopAfter         = 15 * time.Minute
)

func EvaluateSilence(now time.Time, ctx *domain.ConversationState) domain.SilenceState {

	if ctx.SilenceState == domain.SilenceStop {
		return domain.SilenceStop
	}

	// Calculate idle time based on LastUserMessageAt (which should be updated on receive)
	// If LastUserMessageAt is zero, we assume no silence logic yet or fresh convo.
	if ctx.LastMessageTime.IsZero() {
		return domain.SilenceWaitingUser
	}

	idle := now.Sub(ctx.LastMessageTime)

	if idle >= StopAfter {
		return domain.SilenceStop
	}

	// Only re-engage if we were strictly waiting.
	if idle >= SoftReengageAfter && ctx.SilenceState == domain.SilenceWaitingUser {
		return domain.SilenceSoftReengage
	}

	return domain.SilenceWaitingUser
}
