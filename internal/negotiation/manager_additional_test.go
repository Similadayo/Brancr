package negotiation

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

func TestGetOrCreateState_CreatesAndRetrieves(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	db.AutoMigrate(&storage.NegotiationState{})

	m := NewManager(db, testutil.TestLogger(t))

	// Create
	scID := uuid.New()
	pID := uuid.New()
	s, err := m.GetOrCreateState(context.Background(), scID, pID)
	if err != nil {
		t.Fatalf("unexpected error creating state: %v", err)
	}
	if s.SalesConversationID != scID || s.ProductID != pID {
		t.Fatalf("created state has wrong ids: %+v", s)
	}

	// Retrieve
	s2, err := m.GetOrCreateState(context.Background(), scID, pID)
	if err != nil {
		t.Fatalf("unexpected error retrieving state: %v", err)
	}
	if s2.ID != s.ID {
		t.Fatalf("expected to retrieve same state, got %s vs %s", s2.ID, s.ID)
	}
}

func TestCheckEscalationEligibility_NoProductMinPrice(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	db.AutoMigrate(&storage.NegotiationState{})

	m := NewManager(db, testutil.TestLogger(t))

	// Prepare state with persistence and concessions
	scID := uuid.New()
	pID := uuid.New()
	s := &storage.NegotiationState{SalesConversationID: scID, ProductID: pID, SameOfferCount: 3, AIConcessionCount: 2, FlexibilityAttempted: true}
	db.Create(s)

	eligible, reason, err := m.CheckEscalationEligibility(context.Background(), scID, pID, 95.0, true, 0.95, 2, 0.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eligible {
		t.Fatalf("expected not eligible when product min price is missing")
	}
	if reason != "no_product_min_price" {
		t.Fatalf("expected reason no_product_min_price, got %s", reason)
	}
}

func TestCheckEscalationEligibility_ProximityNotMet(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	db.AutoMigrate(&storage.NegotiationState{})

	m := NewManager(db, testutil.TestLogger(t))

	// Prepare state with persistence and concessions
	scID := uuid.New()
	pID := uuid.New()
	s := &storage.NegotiationState{SalesConversationID: scID, ProductID: pID, SameOfferCount: 3, AIConcessionCount: 2, FlexibilityAttempted: true}
	db.Create(s)

	// Offer too low relative to min price
	eligible, reason, err := m.CheckEscalationEligibility(context.Background(), scID, pID, 80.0, true, 0.95, 2, 100.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eligible {
		t.Fatalf("expected not eligible when proximity not met")
	}
	if reason != "proximity_not_met" {
		t.Fatalf("expected reason proximity_not_met, got %s", reason)
	}
}

func TestExecuteEscalation_PayloadFields(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	db.AutoMigrate(&storage.NegotiationState{}, &storage.EscalationEvent{})

	m := NewManager(db, testutil.TestLogger(t))

	// create state
	scID := uuid.New()
	pID := uuid.New()
	tenantID := uuid.New()
	s := &storage.NegotiationState{SalesConversationID: scID, ProductID: pID, SameOfferCount: 3, LastCustomerOffer: 95, AIConcessionCount: 2, FlexibilityAttempted: true}
	db.Create(s)

	custOffers := []float64{95, 96}
	aiOffers := []float64{97}
	e, err := m.ExecuteEscalation(context.Background(), tenantID, scID, pID, 120.0, 90.0, custOffers, aiOffers, "payload_test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Status != "pending" {
		t.Fatalf("expected EscalationEvent status pending, got %s", e.Status)
	}
	// verify cust and ai offers json
	var parsedCust []float64
	if err := json.Unmarshal([]byte(e.CustomerOffers), &parsedCust); err != nil {
		t.Fatalf("failed to parse customer offers json: %v", err)
	}
	if len(parsedCust) != len(custOffers) {
		t.Fatalf("expected customer offers length %d, got %d", len(custOffers), len(parsedCust))
	}
	var parsedAI []float64
	if err := json.Unmarshal([]byte(e.AIOffers), &parsedAI); err != nil {
		t.Fatalf("failed to parse ai offers json: %v", err)
	}
	if len(parsedAI) != len(aiOffers) {
		t.Fatalf("expected ai offers length %d, got %d", len(aiOffers), len(parsedAI))
	}
}
