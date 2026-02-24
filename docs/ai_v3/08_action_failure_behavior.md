# ACTION EXECUTION FAILURE BEHAVIOR (SINGLE SOURCE OF TRUTH)

1. **Execution failure does NOT crash conversation.**
   - If `Execute()` fails or `Apply()` fails, the system logs the error.
   - The user sees a "Something went wrong" generic message (handled by Response Layer fallback).

2. **Partial execution is forbidden.**
   - Actions typically emit a single atomic event or a batch. 
   - If one event fails to persist, none should persist (transactional).

3. **No state mutation on failure.**
   - If execution fails validation (e.g., registry lookup, internal guard), NO event is emitted.
   - State remains unchanged.

4. **User receives polite explanation later.**
   - Since GPT is just "suggesting", if the suggestion fails, we can either:
     - Retry with GPT (feeding back the error).
     - Just Apologize.
   - Brancr v3 prefers explicit clarification: "I tried to process that but ran into an issue."
