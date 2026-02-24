# RULES DECISION MATRIX (SINGLE SOURCE OF TRUTH)

| Intent | Order Phase | Payment Status | Negotiation Status | Allowed Actions | Reason |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **PRICE_INQUIRY** | NONE | NOT_REQUESTED | NONE | INIT_ORDER, EXPLAIN_PRICE | Start new flow |
| **GREETING** | ANY | ANY | ANY | EXPLAIN_PRICE | Polite intro |
| **NEGOTIATE_PRICE** | NEGOTIATING | NOT_REQUESTED | ATTEMPTS < MAX | PROPOSE_DISCOUNT, REJECT_DISCOUNT | Valid negotiation |
| **NEGOTIATE_PRICE** | LOCKED | ANY | LOCKED | REJECT_DISCOUNT | Cannot negotiate locked price |
| **ACCEPT_PRICE** | NEGOTIATING | NOT_REQUESTED | - | LOCK_PRICE, REQUEST_PAYMENT | Lock in agreement |
| **REQUEST_PAYMENT** | NEGOTIATING | NOT_REQUESTED | STARTED | EXPLAIN_PRICE | Premature, need agreement |
| **REQUEST_PAYMENT** | PRICE_LOCKED | NOT_REQUESTED | LOCKED | REQUEST_PAYMENT | Valid request |
| **REQUEST_PAYMENT** | CONFIRMED | COMPLETED | - | CONFIRM_ORDER | Already done |
| **CANCEL_ORDER** | ANY | !COMPLETED | - | CANCEL_ORDER | Allow exit |

## CONSTRAINTS
- **MaxNegotiationAttempts**: Enforced by Rule Engine.
- **MaxDiscountPercent**: Enforced when calculating PROPOSE_DISCOUNT payload (in Strategy Layer).
- **Payment Request**: ONLY allowed if Price is Agreed (None, Accepted, Locked).
