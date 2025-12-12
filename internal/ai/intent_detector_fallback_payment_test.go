package ai

import "testing"

func TestIntentDetector_FallbackDetection_AccountVsPaymentConfirmation(t *testing.T) {
	d := &IntentDetector{}

	acct := d.FallbackDetection("Please send your account number")
	if acct.Intent != "account" {
		t.Fatalf("expected intent=account, got %s", acct.Intent)
	}
	if acct.RequiresEscalation {
		t.Fatalf("expected no escalation for account intent")
	}

	paid := d.FallbackDetection("I have paid")
	if paid.Intent != "payment" {
		t.Fatalf("expected intent=payment, got %s", paid.Intent)
	}
	if !paid.RequiresEscalation {
		t.Fatalf("expected escalation for payment confirmation")
	}
}
