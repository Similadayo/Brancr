package ai

type EscalationDecision struct {
	Handled        bool
	ShouldEscalate bool
	Reason         string
}

// DecideEscalationFromEvents returns a decision when the extracted events are sufficient.
// If Handled=false, callers should fall back to legacy heuristics.
func DecideEscalationFromEvents(events []Event) EscalationDecision {
	if len(events) == 0 {
		return EscalationDecision{Handled: false}
	}

	// Explicit human request.
	if hasEvent(events, EventHumanRequest) {
		return EscalationDecision{Handled: true, ShouldEscalate: true, Reason: "customer_explicitly_requested_human"}
	}

	// Highest priority: payment confirmation requires tenant verification.
	if hasEvent(events, EventPaymentConfirmation) {
		return EscalationDecision{Handled: true, ShouldEscalate: true, Reason: "payment_confirmation_requires_tenant_verification"}
	}

	// Refund: policy questions are informational; demands require human judgment.
	if hasEvent(events, EventRefundDemand) {
		return EscalationDecision{Handled: true, ShouldEscalate: true, Reason: "refund_requires_human_judgment"}
	}
	if hasEvent(events, EventRefundPolicy) {
		return EscalationDecision{Handled: true, ShouldEscalate: false, Reason: ""}
	}

	// Pay-on-delivery commitment (not a question) can require tenant approval/handling.
	if hasEvent(events, EventPODCommitment) {
		return EscalationDecision{Handled: true, ShouldEscalate: true, Reason: "payment_mode_confirmation_needed"}
	}

	// Payment inquiries should be auto-replied using stored account details.
	if hasEvent(events, EventPaymentInquiry) {
		return EscalationDecision{Handled: true, ShouldEscalate: false, Reason: ""}
	}

	// Invoice/receipt requests are informational by default.
	if hasEvent(events, EventInvoiceRequest) {
		return EscalationDecision{Handled: true, ShouldEscalate: false, Reason: ""}
	}

	// Simple order selections should be handled by the AI (no escalation).
	if hasEvent(events, EventOrderSelection) {
		return EscalationDecision{Handled: true, ShouldEscalate: false, Reason: ""}
	}

	return EscalationDecision{Handled: false}
}

func hasEvent(events []Event, t EventType) bool {
	for _, e := range events {
		if e.Type == t {
			return true
		}
	}
	return false
}
