package executor

import (
	"errors"
	"time"

	"github.com/similadayo/brancr/internal/events"
	"github.com/similadayo/brancr/internal/types"
)

type CancelOrderAction struct{}

func (a *CancelOrderAction) Execute(ctx types.ResolvedContext) ([]events.Event, error) {
	if ctx.Order == nil {
		return nil, errors.New("no active order to cancel")
	}

	evt := events.Event{
		Type:      events.OrderCancelled,
		Payload:   nil,
		Timestamp: time.Now(),
	}

	return []events.Event{evt}, nil
}
