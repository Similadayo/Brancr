# COMPLIANCE & DATA HYGIENE (SINGLE SOURCE OF TRUTH)

1. **No raw chat stored long-term.**
   - Chat logs are ephemeral or short-term retention only (e.g., 30 days).
   - Long-term knowledge is stored as `MemoryRecords` (Facts), which are anonymized or structural.

2. **Memory is factual and opt-outable.**
   - Users can say "forget me".
   - System deletes all `MemoryRecords` associated with `CustomerID`.

3. **AI decisions are auditable.**
   - Every `SuggestedAction` is logged in `AIAuditRecord`.
   - Regulators/admins can trace "Why did it reject this discount?" to the exact timestamp, model, and inputs.

4. **Users can request data deletion.**
   - Standard GDPR/CCPA compliance flow supported by identifying all records by `CustomerID`.

5. **Kill switch exists.**
   - Operationally required for AI safety.
   - Allows instant deactivation of stochastic components without taking down the platform.
