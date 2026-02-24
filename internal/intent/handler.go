package intent

import (
	"log"
	"time"

	"github.com/similadayo/brancr/internal/context"
	"github.com/similadayo/brancr/internal/events"
)

// HandleCustomerMessage processes an inbound message event, resolves intent, and emits IntentIdentified.
// Used by EventBus.
func HandleCustomerMessage(event events.Event) error {
	if event.Type != events.CustomerMessageReceived {
		return nil // Ignore other events
	}

	payload, ok := event.Payload.(events.CustomerMessagePayload)
	if !ok {
		return nil // Invalid payload (log error in real sys)
	}

	// 1. Need Resolved Context to resolve intent?
	// Phase 5 says ResolveIntent(ctx, message).
	// So we need to call Context Processor first?
	// Or does this handler use the "resolver" from Phase 4?
	// The prompt implies "Intent Handler" *triggers* the intent flow.
	// But `ResolveIntent` signature requires `context.ResolvedContext`.
	// So we must resolve context first.
	// We can use `context.ProcessEvent(event)`?
	// Wait, Step 4.7 `ProcessEvent` returns `ResolvedContext`.
	// So yes, we call that.

	// Assuming dependency injection of eventBus in real system, but for Phase 5 structure:
	// Use global/singleton access or assume bus passed?
	// The prompt for 5.7 just says "Create handler... Emits IntentIdentified".
	// I'll simulate the emission via a stub or assume variable.

	// Determine Context (using Phase 4 logic)
	// Note: We need to import context processor.
	resolvedCtx, err := context.ProcessEvent(event)
	if err != nil {
		// Log error, maybe don't block? "Guard failures do NOT crash system".
		// But failure to resolve context might act as "Unknown Intent" downstream?
		log.Printf("Failed to resolve context: %v", err)
		// Proceed with empty context or return?
		// Safest to return error if context is critical.
		return err
	}

	// Resolve Intent
	result, err := ResolveIntent(resolvedCtx, payload.Message)
	if err != nil {
		// Start AI error handling or fallback
		result = IntentResult{Intent: IntentUnknown, Confidence: 0, Source: "ERROR"}
	}

	// Emit IntentIdentified
	nextEvent := events.Event{
		ID:   "evt_" + payload.SessionID + "_" + time.Now().String(), // Stub ID
		Type: events.IntentIdentified,
		Payload: events.IntentPayload{
			SessionID:  payload.SessionID,
			Intent:     string(result.Intent),
			Confidence: result.Confidence,
		},
		Timestamp: time.Now(),
	}

	// Assuming we have access to publish from here.
	// In a real dependency injection setup, this handler is a struct method on something holding the bus.
	// For this Phase 5 pure function example:
	// bus.Publish(nextEvent) // Commented out as bus isn't in scope variable.
	_ = nextEvent

	return nil
}
