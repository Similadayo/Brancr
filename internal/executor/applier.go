package executor

import (
	"fmt"
	"log"

	"github.com/similadayo/brancr/internal/events"
)

// Applier handles the side effects of events: State Transition + Persistence + Bus Emission.
func Apply(eventList []events.Event) error {
	for _, evt := range eventList {
		// 1. Apply State Transitions
		// In a real system, we'd load the aggregator, call state.TransitionX(evt), save.
		// For Phase 8 Demo/Skeleton:
		// "Transitions must use Phase 2 state machines" - We would call them here.

		fmt.Printf("Applying Event: %s\n", evt.Type)

		// 2. Persist State
		// Stub: db.Save(order)

		// 3. Emit to Event Bus
		// events.Bus.Publish(evt)
		log.Printf("Emitted event %s", evt.ID)
	}
	return nil
}
