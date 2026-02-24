# GUARD FAILURE BEHAVIOR (SINGLE SOURCE OF TRUTH)

1. **Guard failures do NOT crash the system.**
   - They are expected control flow for invalid actions.

2. **Guard failures block state transitions.**
   - If a guard returns an error, the event processor must NOT trigger any state change.
   - The event is effectively "rejected".

3. **Guard failures still allow AI to respond informationally later.**
   - The error is returned to the Intent Engine (Phase 5).
   - The AI will see "Error: Payment not allowed yet" and can generate a polite explanation: "I can't take payment until we agree on a price."

4. **Guard failures must be explainable in plain language.**
   - Error messages should be static and clear (e.g., "cannot negotiate after payment requested").

This behavior ensures Brancr is **polite but firm**. The AI cannot override the Guard.
