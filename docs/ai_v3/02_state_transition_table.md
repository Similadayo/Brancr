# STATE TRANSITION TABLE (SINGLE SOURCE OF TRUTH)

| Current State (Phase) | Sub-State (Payment) | Intent | Allowed | Next State | Guard Condition |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **INITIALIZED** | NOT_REQUESTED | ADD_ITEM | YES | NEGOTIATING | - |
| **INITIALIZED** | NOT_REQUESTED | NEGOTIATE | YES | NEGOTIATING | - |
| **NEGOTIATING** | NOT_REQUESTED | NEGOTIATE (Counter) | YES | NEGOTIATING | Attempts < Max |
| **NEGOTIATING** | NOT_REQUESTED | ACCEPT_PRICE | YES | PRICE_LOCKED | - |
| **PRICE_LOCKED** | NOT_REQUESTED | NEGOTIATE | NO | ❌ | GuardNoNegotiationAfterPayment |
| **PRICE_LOCKED** | NOT_REQUESTED | REQUEST_PAYMENT | YES | PAYMENT_PENDING | GuardPaymentAfterPriceLock |
| **PAYMENT_PENDING** | REQUESTED | CONFIRM_PAYMENT | YES | CONFIRMED | - |
| **PAYMENT_PENDING** | PENDING | CONFIRM_PAYMENT | YES | CONFIRMED | - |
| **PAYMENT_PENDING** | PENDING | NEGOTIATE | NO | ❌ | GuardNoNegotiationAfterPayment |
| **CONFIRMED** | COMPLETED | CANCEL | NO | ❌ | FulfillmentStarted |

## LEGEND
- **Transition**: `TransitionFunction(current, target)`
- **Guard**: `GuardFunction(state)` -> Error if failed
- **❌**: Strictly Forbidden (Return Error)
