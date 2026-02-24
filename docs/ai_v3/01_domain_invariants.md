# DOMAIN INVARIANTS (SINGLE SOURCE OF TRUTH)

1. An OrderState is immutable except via explicit transitions.
2. There can be only one active OrderState per SessionID.
3. Payment cannot be requested unless NegotiationPhase == ACCEPTED.
4. Negotiation cannot occur after PaymentRequested.
5. AI cannot create or modify OrderState directly.
6. ConversationState must never override OrderState.
7. CustomerMemory stores facts, not conversations.
