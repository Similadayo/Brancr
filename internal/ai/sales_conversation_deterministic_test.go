package ai

import (
	"testing"

	"github.com/similadayo/brancr/internal/storage"
)

func TestSalesConversationUpdatesFromSignals_StageUpdates(t *testing.T) {
	base := &storage.SalesConversation{Stage: "greeting", CommitmentLevel: "exploring"}

	updates := SalesConversationUpdatesFromSignals(base, "payment", []Event{{Type: EventPaymentConfirmation}})
	if updates["stage"] != "payment_confirmation" {
		t.Fatalf("expected stage=payment_confirmation, got %#v", updates["stage"])
	}
	if updates["last_intent"] != "payment" {
		t.Fatalf("expected last_intent=payment, got %#v", updates["last_intent"])
	}
}

func TestSalesConversationUpdatesFromSignals_HumanRequestOverrides(t *testing.T) {
	base := &storage.SalesConversation{Stage: "payment_instructions", CommitmentLevel: "exploring"}
	updates := SalesConversationUpdatesFromSignals(base, "inquiry", []Event{{Type: EventPaymentInquiry}, {Type: EventHumanRequest}})
	if updates["stage"] != "needs_human" {
		t.Fatalf("expected stage=needs_human, got %#v", updates["stage"])
	}
}

func TestSalesConversationUpdatesFromSignals_CommitmentConservative(t *testing.T) {
	base := &storage.SalesConversation{Stage: "greeting", CommitmentLevel: "exploring"}
	updates := SalesConversationUpdatesFromSignals(base, "order", []Event{{Type: EventOrderSelection}})
	if updates["commitment_level"] != "interested" {
		t.Fatalf("expected commitment_level=interested, got %#v", updates["commitment_level"])
	}
}

func TestSalesConversationUpdatesFromSignals_NoChangeReturnsNil(t *testing.T) {
	base := &storage.SalesConversation{Stage: "greeting", CommitmentLevel: "exploring"}
	updates := SalesConversationUpdatesFromSignals(base, "", nil)
	if updates != nil {
		t.Fatalf("expected nil updates, got %#v", updates)
	}
}
