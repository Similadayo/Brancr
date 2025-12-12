package ai

import "testing"

func TestExtractEventsAndPolicy_PaymentConfirmation_Escalates(t *testing.T) {
	events := ExtractEvents("I have paid")
	if !hasEvent(events, EventPaymentConfirmation) {
		t.Fatalf("expected %s event", EventPaymentConfirmation)
	}
	d := DecideEscalationFromEvents(events)
	if !d.Handled || !d.ShouldEscalate {
		t.Fatalf("expected handled escalation decision, got %+v", d)
	}
}

func TestExtractEventsAndPolicy_AccountRequest_DoesNotEscalate(t *testing.T) {
	events := ExtractEvents("Please send your account number")
	if !hasEvent(events, EventPaymentInquiry) {
		t.Fatalf("expected %s event", EventPaymentInquiry)
	}
	d := DecideEscalationFromEvents(events)
	if !d.Handled || d.ShouldEscalate {
		t.Fatalf("expected handled non-escalation decision, got %+v", d)
	}
}

func TestExtractEventsAndPolicy_PODCommitment_Escalates(t *testing.T) {
	events := ExtractEvents("pay on delivery")
	if !hasEvent(events, EventPODCommitment) {
		t.Fatalf("expected %s event", EventPODCommitment)
	}
	d := DecideEscalationFromEvents(events)
	if !d.Handled || !d.ShouldEscalate {
		t.Fatalf("expected handled escalation decision, got %+v", d)
	}
}

func TestExtractEventsAndPolicy_PODInquiry_DoesNotEscalate(t *testing.T) {
	events := ExtractEvents("Do you do pay on delivery?")
	if hasEvent(events, EventPODCommitment) {
		t.Fatalf("did not expect %s event", EventPODCommitment)
	}
	d := DecideEscalationFromEvents(events)
	if d.Handled {
		t.Fatalf("expected unhandled decision (fallback), got %+v", d)
	}
}

func TestExtractEventsAndPolicy_InvoiceRequest_DoesNotEscalate(t *testing.T) {
	events := ExtractEvents("Can I get an invoice?")
	if !hasEvent(events, EventInvoiceRequest) {
		t.Fatalf("expected %s event", EventInvoiceRequest)
	}
	d := DecideEscalationFromEvents(events)
	if !d.Handled || d.ShouldEscalate {
		t.Fatalf("expected handled non-escalation decision, got %+v", d)
	}
}

func TestExtractEventsAndPolicy_HumanRequest_Escalates(t *testing.T) {
	events := ExtractEvents("Please connect me to a human")
	if !hasEvent(events, EventHumanRequest) {
		t.Fatalf("expected %s event", EventHumanRequest)
	}
	d := DecideEscalationFromEvents(events)
	if !d.Handled || !d.ShouldEscalate {
		t.Fatalf("expected handled escalation decision, got %+v", d)
	}
}

func TestExtractEventsAndPolicy_RefundPolicy_DoesNotEscalate(t *testing.T) {
	events := ExtractEvents("What's your refund policy?")
	if !hasEvent(events, EventRefundPolicy) {
		t.Fatalf("expected %s event", EventRefundPolicy)
	}
	if hasEvent(events, EventRefundDemand) {
		t.Fatalf("did not expect %s event", EventRefundDemand)
	}
	d := DecideEscalationFromEvents(events)
	if !d.Handled || d.ShouldEscalate {
		t.Fatalf("expected handled non-escalation decision, got %+v", d)
	}
}

func TestExtractEventsAndPolicy_RefundDemand_Escalates(t *testing.T) {
	events := ExtractEvents("I want my money back right now")
	if !hasEvent(events, EventRefundDemand) {
		t.Fatalf("expected %s event", EventRefundDemand)
	}
	d := DecideEscalationFromEvents(events)
	if !d.Handled || !d.ShouldEscalate {
		t.Fatalf("expected handled escalation decision, got %+v", d)
	}
}
