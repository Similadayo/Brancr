package executor

import (
	"errors"
	"time"

	"github.com/similadayo/brancr/internal/events"
	"github.com/similadayo/brancr/internal/types"
)

type InitializeOrderAction struct{}

func (a *InitializeOrderAction) Execute(ctx types.ResolvedContext) ([]events.Event, error) {
	if ctx.Order != nil {
		return nil, errors.New("order already exists")
	}

	// Assuming we can derive IDs from context or generate new ones.
	// For Phase 8 logic, we emit the event which causes creation.
	evt := events.Event{
		Type: events.OrderInitialized,
		Payload: events.OrderInitializedPayload{
			SessionID:  ctx.Conversation.SessionID, // Assuming ConversationState has ID, or we use a session ID from somewhere
			CustomerID: ctx.Conversation.CustomerID,
			// MerchantID from context
		},
		Timestamp: time.Now(),
	}

	return []events.Event{evt}, nil
}
