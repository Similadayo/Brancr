package executor

import (
	"github.com/similadayo/brancr/internal/events"
	"github.com/similadayo/brancr/internal/types"
)

type QualifyUserAction struct{}

func (a *QualifyUserAction) Execute(ctx types.ResolvedContext) ([]events.Event, error) {
	return []events.Event{
		{
			Type: events.QualificationAsked,
		},
	}, nil
}
