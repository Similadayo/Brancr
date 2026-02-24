package executor

import (
	"github.com/similadayo/brancr/internal/events"
	"github.com/similadayo/brancr/internal/types"
)

// ClarifyIntentAction handles low-confidence responses.
// It is primarily a no-op side-effect wise, but ensures the pipeline proceeds.
type ClarifyIntentAction struct{}

func (a *ClarifyIntentAction) Execute(resolved types.ResolvedContext) ([]events.Event, error) {
	// No side effects needed. The text response asks for clarification.
	return nil, nil
}
