package executor

import (
	"errors"
	"time"

	"github.com/similadayo/brancr/internal/events"
	"github.com/similadayo/brancr/internal/types"
)

type RequestPaymentAction struct{}

func (a *RequestPaymentAction) Execute(ctx types.ResolvedContext) ([]events.Event, error) {
	if ctx.Order == nil {
		return nil, errors.New("no active order")
	}

	evt := events.Event{
		Type:      events.PaymentRequested,
		Payload:   nil,
		Timestamp: time.Now(),
	}

	return []events.Event{evt}, nil
}
