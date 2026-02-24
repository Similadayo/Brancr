# MEMORY + AI INTERACTION RULES

1. **AI may reference memory conversationally.**
   - OK: "Would you like to pay via Bank Transfer like last time?"
   - NO: "I have already charged your Bank Account." (Hallucination/Authority breach)

2. **AI must not assume memory is always correct.**
   - Memory is past data. The present may be different.
   - Always verify critical assumptions (e.g., address) before locking.

3. **AI must not act solely based on memory.**
   - Memory suggests options; it does not authorize actions.
   - Rules Engine checks must still pass.

4. **Memory can shape tone, not decisions.**
   - "VIP" memory -> Politer tone.
   - "Budget" memory -> Highlight discounts.
   - But Price Calculation is strictly Rule-Based.
