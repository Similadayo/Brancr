package context

import (
	"github.com/similadayo/brancr/internal/events"
	"github.com/similadayo/brancr/internal/types"
)

// Resolver defines the interface for loading the current truth based on an event.
type Resolver interface {
	Resolve(event events.Event) (types.ResolvedContext, error)
}
