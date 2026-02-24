# BRANCR AI v3 — CORE PRINCIPLES (SINGLE SOURCE OF TRUTH)

1. AI NEVER mutates business state directly.
2. There is exactly ONE canonical OrderState per session.
3. All system changes happen via explicit events.
4. Memory is factual and long-term; conversation is ephemeral.
5. State transitions are validated before execution.
6. GPT can suggest actions, but the system decides execution.
7. AI must never ask for information already present in state.
8. AI must never recreate an existing order.
9. Language generation is free; business logic is locked.

NOTE: Any future code violating this file is invalid.
