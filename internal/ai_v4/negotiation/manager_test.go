package negotiation

import (
	"context"
	"strings"
	"testing"

	"github.com/similadayo/brancr/internal/storage"
)

func TestDecide_AcceptsWhenOfferMatchesLastAIOffer(t *testing.T) {
	m := &Manager{}
	sc := &storage.SalesConversation{
		NegotiationCount: 2,
		LastAIOffer:      1081000,
		LastOfferAmount:  950000,
	}

	decision, err := m.Decide(context.Background(), sc, 1081000, 1150000, Policy{
		MinPrice:       977500,
		MaxConcessions: 2,
		ProximityPct:   0.90,
	}, false)
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}

	if decision.Phase != PhaseAccept {
		t.Fatalf("expected phase %q, got %q", PhaseAccept, decision.Phase)
	}
	if decision.Offer != 1081000 {
		t.Fatalf("expected accepted offer 1081000, got %.0f", decision.Offer)
	}
	if !strings.Contains(decision.Reason, "matches_last_ai_offer") {
		t.Fatalf("expected acceptance reason to mention last AI offer match, got %q", decision.Reason)
	}
}

func TestDecide_UsesPreviousCustomerOfferForDelta(t *testing.T) {
	m := &Manager{}
	sc := &storage.SalesConversation{
		NegotiationCount:  2,
		LastAIOffer:       1100000,
		LastOfferAmount:   900000,
		AIConcessionCount: 0,
	}

	decision, err := m.Decide(context.Background(), sc, 950000, 1150000, Policy{
		MinPrice:       900000,
		MaxConcessions: 2,
		ProximityPct:   0.90,
	}, false)
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}

	if decision.Phase != PhaseConcede {
		t.Fatalf("expected phase %q, got %q", PhaseConcede, decision.Phase)
	}
	if !strings.Contains(decision.Reason, "smart_concession_delta=50000") {
		t.Fatalf("expected delta-based concession reason, got %q", decision.Reason)
	}
}

func TestDecide_AcceptsAtLastAIOfferWhenCustomerOffersHigher(t *testing.T) {
	m := &Manager{}
	sc := &storage.SalesConversation{
		NegotiationCount: 2,
		LastAIOffer:      1090000,
		LastOfferAmount:  1050000,
	}

	decision, err := m.Decide(context.Background(), sc, 1200000, 1150000, Policy{
		MinPrice:       1050000,
		MaxConcessions: 2,
		ProximityPct:   0.90,
	}, false)
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}
	if decision.Phase != PhaseAccept {
		t.Fatalf("expected phase %q, got %q", PhaseAccept, decision.Phase)
	}
	if decision.Offer != 1090000 {
		t.Fatalf("expected accept offer to stay at last AI offer 1090000, got %.0f", decision.Offer)
	}
}

func TestDecide_DoesNotBacktrackUpAfterBestPriceQuoted(t *testing.T) {
	m := &Manager{}
	sc := &storage.SalesConversation{
		NegotiationCount: 0,
		LastAIOffer:      1090000,
	}

	decision, err := m.Decide(context.Background(), sc, 850000, 1150000, Policy{
		MinPrice:       1050000,
		MaxConcessions: 2,
		ProximityPct:   0.90,
	}, false)
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}

	if decision.Offer > 1090000 {
		t.Fatalf("expected no backtrack above 1090000, got %.0f", decision.Offer)
	}
}

func TestDecide_BestPriceRequestDoesNotRepeatOutOfRangeLastAIOffer(t *testing.T) {
	m := &Manager{}
	sc := &storage.SalesConversation{
		NegotiationCount: 1,
		LastAIOffer:      1050000, // out of range for this product
	}

	decision, err := m.Decide(context.Background(), sc, 0, 1350000, Policy{
		MinPrice:       1250000,
		MaxConcessions: 2,
		ProximityPct:   0.90,
	}, true)
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}

	if decision.Offer < 1250000 {
		t.Fatalf("expected best-price offer >= 1250000, got %.0f", decision.Offer)
	}
	if sc.LastAIOffer < 1250000 {
		t.Fatalf("expected persisted LastAIOffer >= 1250000, got %.0f", sc.LastAIOffer)
	}
	if decision.Phase != PhaseConcede && decision.Phase != PhaseHold && decision.Phase != PhaseAccept {
		t.Fatalf("unexpected phase %q", decision.Phase)
	}
}

// TestDecide_ContinuesNegotiatingBeyondTwoConcessions verifies the AI does NOT lock up
// after 2 drops when MaxConcessions is higher (default 5).
func TestDecide_ContinuesNegotiatingBeyondTwoConcessions(t *testing.T) {
	m := &Manager{}
	sc := &storage.SalesConversation{
		NegotiationCount:  3,
		LastAIOffer:       1040000, // AI has already dropped twice toward floor
		LastOfferAmount:   950000,
		AIConcessionCount: 2, // Two concessions already made
	}

	// Customer pushes again — AI should still concede (floor = 980,000, headroom remains)
	decision, err := m.Decide(context.Background(), sc, 1000000, 1050000, Policy{
		MinPrice:       980000,
		MaxConcessions: 5, // Default raised value
		ProximityPct:   0.90,
	}, false)
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}

	if decision.Phase == PhaseHold {
		t.Fatalf("expected AI to keep negotiating (PhaseConcede), but got PhaseHold after only 2 concessions")
	}
	if decision.Offer >= 1040000 {
		t.Fatalf("expected next offer < 1040000 (existing LastAIOffer), got %.0f", decision.Offer)
	}
	if decision.Offer < 980000 {
		t.Fatalf("expected next offer >= floor 980000, got %.0f", decision.Offer)
	}
}

// TestDecide_AcceptsWithinToleranceWindow verifies that a customer offer within ±500
// of the last AI offer is treated as acceptance (handles "1.030m" parsing imprecision).
func TestDecide_AcceptsWithinToleranceWindow(t *testing.T) {
	m := &Manager{}
	sc := &storage.SalesConversation{
		NegotiationCount:  2,
		LastAIOffer:       1030000,
		LastOfferAmount:   900000,
		AIConcessionCount: 1,
	}

	// Customer says "1.030m" which may parse to 1029700 or similar
	decision, err := m.Decide(context.Background(), sc, 1029700, 1050000, Policy{
		MinPrice:       980000,
		MaxConcessions: 5,
		ProximityPct:   0.90,
	}, false)
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}

	if decision.Phase != PhaseAccept {
		t.Fatalf("expected PhaseAccept for offer within ±500 of LastAIOffer 1030000, got %q (offer=%.0f)", decision.Phase, decision.Offer)
	}
	if decision.Offer != 1030000 {
		t.Fatalf("expected accepted price to be LastAIOffer=1030000, got %.0f", decision.Offer)
	}
}
