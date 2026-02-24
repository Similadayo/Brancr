# EVENT FLOW RULES (SINGLE SOURCE OF TRUTH)

1. Events are immutable.
2. Events are append-only.
3. Events cannot be skipped.
4. State changes only occur in response to events.
5. One event may trigger another event.
6. Events must not call AI directly.
7. Events must not mutate state directly (State mutation happens in Handlers *reacting* to events).

## EVENT OWNERSHIP
- CustomerMessageReceived → External (customer)
- IntentIdentified → System
- OrderInitialized → System
- PriceAccepted → System
- PaymentCompleted → External (payment provider)

## FORBIDDEN
- Handlers calling GPT logic.
- Handlers updating DB directly without state machine validation.
- Generating messages inside Event Bus logic.
