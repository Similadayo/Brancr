# MEMORY WRITE POLICY (SINGLE SOURCE OF TRUTH)

1. **Memory is written ONLY after successful action execution.**
   - We do not store "promises" or "intents". We store *completed deeds*.
   - Example: Store "Prefers Bank Transfer" ONLY after `FulfillmentStarted` (implying valid payment).

2. **Memory is NEVER written from AI output alone.**
   - GPT cannot say "User seems rich". 
   - Extractors must rely on canonical EVENTS.

3. **Memory confidence increases only with repetition.**
   - 1st observation: Confidence = 0.6 (Tentative).
   - 2nd observation: Confidence = 0.8 (Probable).
   - 3rd observation: Confidence = 0.95 (Confirmed).

4. **Memory can be overridden by explicit user instruction.**
   - "Don't save my card" -> Deletes record (or marks as forbidden).
   - "I moved to London" -> Updates location immediately (Explicit Source).

5. **Memory must be merchant-scoped.**
   - Preferences for one merchant do not leak to another.
