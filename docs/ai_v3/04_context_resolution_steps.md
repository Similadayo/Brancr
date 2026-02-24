# CONTEXT RESOLUTION STEPS (SINGLE SOURCE OF TRUTH)

1. Identify SessionID from event payload.
2. Load OrderState (if exists) matching SessionID/OrderID.
3. Load ConversationState matching SessionID.
4. Load CustomerMemory matching CustomerID.
5. Load MerchantConfig matching MerchantID.
6. Assemble ResolvedContext.
7. Validate invariants (e.g. Memory consistency).
8. Return context or error.

## DO NOT SKIP OR REORDER.
This ensures that every event is processed against the exact same reality model.
