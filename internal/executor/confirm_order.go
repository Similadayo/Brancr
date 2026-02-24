package executor

import (
	"errors"
	"time"

	"github.com/similadayo/brancr/internal/events"
	"github.com/similadayo/brancr/internal/types"
)

type ConfirmOrderAction struct{}

func (a *ConfirmOrderAction) Execute(ctx types.ResolvedContext) ([]events.Event, error) {
	if ctx.Order == nil {
		return nil, errors.New("no active order to confirm")
	}

	// This might be FULFILLMENT_STARTED or similar
	evt := events.Event{
		Type:      events.FulfillmentStarted,
		Payload:   nil,
		Timestamp: time.Now(),
	}

	return []events.Event{evt}, nil
}
