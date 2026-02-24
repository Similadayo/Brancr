package negotiation

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

func TestCheckEscalationEligibility_BasicFlow(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	// Auto migrate models
	db.AutoMigrate(&storage.NegotiationState{}, &storage.EscalationEvent{})

	m := NewManager(db, testutil.TestLogger(t))

	// create state with SameOfferCount < 3
	s := &storage.NegotiationState{SalesConversationID: uuid.New(), ProductID: uuid.New(), SameOfferCount: 2, AIConcessionCount: 2, FlexibilityAttempted: true}
	db.Create(s)

	eligible, reason, err := m.CheckEscalationEligibility(context.Background(), uuid.New(), uuid.New(), 90.0, true, 0.95, 2, 100.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eligible {
		t.Fatalf("expected not eligible when SameOfferCount < 3")
	}
	if reason != "persistence_not_reached" {
		t.Fatalf("expected persistence_not_reached, got %s", reason)
	}

	// now bump same offer and fail ai concessions
	db.Model(&storage.NegotiationState{}).Where("sales_conversation_id = ? AND product_id = ?", 1, 1).Updates(map[string]interface{}{"same_offer_count": 3, "ai_concession_count": 1})

	eligible, reason, err = m.CheckEscalationEligibility(context.Background(), uuid.New(), uuid.New(), 95.0, true, 0.95, 2, 100.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eligible {
		t.Fatalf("expected not eligible when AI concessions not exhausted")
	}
	if reason != "ai_concessions_not_exhausted" {
		t.Fatalf("expected ai_concessions_not_exhausted, got %s", reason)
	}

	// now satisfy all conditions
	db.Model(&storage.NegotiationState{}).Where("sales_conversation_id = ? AND product_id = ?", 1, 1).Updates(map[string]interface{}{"ai_concession_count": 2})

	eligible, reason, err = m.CheckEscalationEligibility(context.Background(), uuid.New(), uuid.New(), 95.0, true, 0.95, 2, 100.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !eligible {
		t.Fatalf("expected eligible when all conditions met, got reason %s", reason)
	}
	if reason != "escalation_eligible_all_conditions" {
		t.Fatalf("expected escalation_eligible_all_conditions, got %s", reason)
	}
}

func TestExecuteEscalation_CreatesEventAndLocksState(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	db.AutoMigrate(&storage.NegotiationState{}, &storage.EscalationEvent{})

	m := NewManager(db, testutil.TestLogger(t))

	// create state that should be escalated
	s := &storage.NegotiationState{SalesConversationID: uuid.New(), ProductID: uuid.New(), SameOfferCount: 3, LastCustomerOffer: 92, AIConcessionCount: 2, FlexibilityAttempted: true}
	db.Create(s)

	// Execute escalation
	e, err := m.ExecuteEscalation(context.Background(), s.ID, s.SalesConversationID, s.ProductID, 100.0, 90.0, []float64{92}, []float64{95, 96}, "test_reason")
	if err != nil {
		t.Fatalf("unexpected error executing escalation: %v", err)
	}
	if e.ID == uuid.Nil {
		t.Fatalf("expected escalation event to be persisted")
	}
	if e.Reason != "test_reason" {
		t.Fatalf("expected reason to be set, got %s", e.Reason)
	}
	// reload state and verify it's locked and escalated
	var s2 storage.NegotiationState
	if err := db.Where("sales_conversation_id = ? AND product_id = ?", 10, 5).First(&s2).Error; err != nil {
		t.Fatalf("failed to load negotiation state: %v", err)
	}
	if !s2.LockAIPriceChanges {
		t.Fatalf("expected LockAIPriceChanges to be true after escalation")
	}
	if s2.Status != "escalated" {
		t.Fatalf("expected state status escalated, got %s", s2.Status)
	}
}

func TestRecordCustomerOffer_SameOfferCounting(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	db.AutoMigrate(&storage.NegotiationState{})

	m := NewManager(db, testutil.TestLogger(t))

	// initial state: no flexibility, no ai concessions
	s, err := m.RecordCustomerOffer(context.Background(), uuid.New(), uuid.New(), 500.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.SameOfferCount != 1 {
		t.Fatalf("expected same_offer_count 1 after first offer, got %d", s.SameOfferCount)
	}

	// same offer again should NOT increment because FlexibilityAttempted=false or AIConcessionCount==0
	s, err = m.RecordCustomerOffer(context.Background(), uuid.New(), uuid.New(), 500.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.SameOfferCount != 1 {
		t.Fatalf("expected same_offer_count to remain 1 when flexibility not attempted, got %d", s.SameOfferCount)
	}

	// mark flexibility attempted and add ai concession
	if err := m.MarkFlexibilityAttempted(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := m.RecordAIConcession(context.Background(), uuid.New(), uuid.New(), 480.0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// now same offer should increment
	s, err = m.RecordCustomerOffer(context.Background(), uuid.New(), uuid.New(), 500.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.SameOfferCount != 2 {
		t.Fatalf("expected same_offer_count to increment to 2 after conditions met, got %d", s.SameOfferCount)
	}
}
