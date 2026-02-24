package executor

import (
	"time"

	"github.com/similadayo/brancr/internal/events"
	"github.com/similadayo/brancr/internal/types"
)

type SuggestNextStepAction struct{}

func (a *SuggestNextStepAction) Execute(
	ctx types.ResolvedContext,
) ([]events.Event, error) {

	return []events.Event{
		{
			Type:      events.NextStepSuggested,
			Timestamp: time.Now(),
		},
	}, nil
}
