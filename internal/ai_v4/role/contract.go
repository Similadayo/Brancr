package role

import "github.com/google/uuid"

// RoleContract defines the explicit job description and constraints for the AI
// acting on behalf of a specific tenant. It is the "Rule of Law" for the AI.
type RoleContract struct {
	TenantID uuid.UUID

	// Core capabilities (Can I do this?)
	CanSell           bool // Can I proactively sell/recommend?
	CanNegotiate      bool // Can I negotiate prices?
	CanTakeOrders     bool // Can I accept and process orders?
	CanHandlePayments bool // Can I display payment info?

	// Escalation rules (Must I stop?)
	MustEscalateOnPayment bool // If potential payment, must I handoff?
	MustEscalateOnRefund  bool // If refund mentioned, must I handoff?
	MustEscalateOnAnger   bool // If anger detected, must I handoff?

	// Forbidden behaviors (Never do this)
	ForbiddenPromises []string
	ForbiddenTopics   []string

	// Industry hint (informational)
	Industry string
}
