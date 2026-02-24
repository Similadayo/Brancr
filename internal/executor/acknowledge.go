package executor

import (
	"time"

	"github.com/similadayo/brancr/internal/events"
	"github.com/similadayo/brancr/internal/types"
)

type AcknowledgeAction struct{}

func (a *AcknowledgeAction) Execute(
	ctx types.ResolvedContext,
) ([]events.Event, error) {

	// No state change. Optional analytics event only.
	return []events.Event{
		{
			Type:      events.ConversationAcknowledged,
			Timestamp: time.Now(),
		},
	}, nil
}
