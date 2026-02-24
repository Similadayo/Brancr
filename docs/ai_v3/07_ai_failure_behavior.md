# AI FAILURE BEHAVIOR (SINGLE SOURCE OF TRUTH)

1. **GPT Failure -> Safe Clarification**
   - If the AI API fails or timeouts, do NOT crash.
   - Return a safe generic error message: "I'm having trouble connecting right now. Can you try again?"
   - Action: `CLARIFY_INTENT` or `NONE`.

2. **Invalid JSON -> Safe Clarification**
   - If GPT outputs invalid JSON (violating contract), it is treated as a hallucination.
   - The system rejects it.
   - Fallback: "I didn't quite catch that. Could you rephrase?"
   - Action: `CLARIFY_INTENT`.

3. **Forbidden Action -> Explanation + Clarification**
   - If GPT suggests an action NOT in `AllowedActions`, the Reasoner hard-blocks it.
   - The system logs the violation.
   - Fallback to user: "I can't do that right now [State Reason]."

4. **GPT never blocks system execution.**
   - All AI calls must have timeouts.
   - The system state remains consistent regardless of AI outcome.

5. **Low Confidence (< 0.6) -> Explicit Clarification.**
   - The AI must admit it's unsure rather than guessing.
   - "Can you clarify...?" is always better than a wrong order.
