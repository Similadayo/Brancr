# INTENT CONFIDENCE RULES (SINGLE SOURCE OF TRUTH)

1. **Confidence < 0.6 -> IntentUnknown**
   - The system NEVER acts on low confidence.
   - Low confidence triggers a clarification flow later (not here).

2. **IntentUnknown triggers clarification.**
   - If intent is unknown, the system must ask the customer to rephrase or offer options.

3. **System never guesses intent under low confidence.**
   - Better to ask "Did you mean X?" than to execute X wrongly.

4. **Rule Matches are High Confidence.**
   - Deterministic rule matches generally have Confidence >= 0.9.

5. **AI Classification MUST return Confidence.**
   - GPT output must include a probability score.
