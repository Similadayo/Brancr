package executor

import (
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/events"
	"github.com/similadayo/brancr/internal/types"
)

type ResumeConversationAction struct{}

func (a *ResumeConversationAction) Execute(
	ctx types.ResolvedContext,
) ([]events.Event, error) {

	sessionUUID, _ := uuid.Parse(ctx.Conversation.SessionID)

	return []events.Event{
		{
			Type: events.ConversationResumed,
			Payload: events.ConversationResumedPayload{
				SessionID: sessionUUID,
			},
			Timestamp: time.Now(),
		},
	}, nil
}
