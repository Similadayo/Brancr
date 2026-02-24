package executor

import (
	"errors"
	"time"

	"github.com/similadayo/brancr/internal/events"
	"github.com/similadayo/brancr/internal/types"
)

type LockPriceAction struct{}

func (a *LockPriceAction) Execute(ctx types.ResolvedContext) ([]events.Event, error) {
	if ctx.Order == nil {
		return nil, errors.New("no active order to lock")
	}

	// Phase 8 doesn't duplicate complex guard logic (already checked by Phase 6 Rules),
	// but it *can* double-check specific execution invariants like "Price must exist".

	evt := events.Event{
		Type:      events.PriceAccepted,
		Payload:   nil, // Payload might include final price details
		Timestamp: time.Now(),
	}

	return []events.Event{evt}, nil
}
