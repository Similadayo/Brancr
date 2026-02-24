package executor

import (
	"github.com/similadayo/brancr/internal/events"
	"github.com/similadayo/brancr/internal/types"
)

// Action defines a deterministic system operation.
// It executes based on current truth and returns a list of events if successful.
// It never mutates state directly (events drives mutation) and never returns text.
type Action interface {
	Execute(ctx types.ResolvedContext) ([]events.Event, error)
}
