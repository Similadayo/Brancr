Negotiation API

This document describes API endpoints to review and approve negotiation concessions.

1) List negotiations for a sales conversation
- Method: GET
- Path: /api/tenant/sales-conversations/{id}/negotiations
- Auth: Tenant session
- Response: { "negotiations": [ { "id": 1, "sales_conversation_id": 1, "type": "price", "customer_offer": 950, "counter_offer": 900, "final_agreement": 0, "status": "proposed", "created_at": "..." }, ... ] }

2) Approve a negotiation (tenant action)
- Method: POST
- Path: /api/tenant/negotiations/{id}/approve
- Auth: Tenant session
- Request body: { "final_price": 900 }
- Response: { "success": true }

Behavior:
- Approving sets negotiation.status = "agreed" and negotiation.final_agreement to the approved price.
- If a final_price is provided, the sales conversation's price_discussed will be updated to this value and pending_order_status set to "approved".
- The frontend should call this endpoint from the agent UI when the tenant approves a conditional concession (e.g., "Reserve for ₦X for 2 hours").

Notes:
- Negotiation attempts are recorded automatically in the database (NegotiationHistory) when the AI proposes a concession.
- Tenant approval is required to actually update conversation state and proceed to payment instructions.