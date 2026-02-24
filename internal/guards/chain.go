package guards

import (
	"github.com/similadayo/brancr/internal/types"
)

// Guard is a function that validates the ResolvedContext.
// Returns an error if the context violates the guard's invariants.
type Guard func(types.ResolvedContext) error

// RunGuards executes a chain of guards in order.
// Stops at the first failure.
func RunGuards(ctx types.ResolvedContext, guards ...Guard) error {
	for _, g := range guards {
		if err := g(ctx); err != nil {
			return err
		}
	}
	return nil
}
