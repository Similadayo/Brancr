package ai

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

func TestRecordNegotiation_UpdatesCounters(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	// Migrate SalesConversation and NegotiationHistory
	db.AutoMigrate(&storage.SalesConversation{}, &storage.NegotiationHistory{})

	// Create a sales conversation
	sc := &storage.SalesConversation{ConversationID: uuid.New(), TenantID: uuid.New(), CustomerID: uuid.New(), Stage: "negotiation", PriceDiscussed: 1000000}
	db.Create(sc)

	mgr := &SalesConversationManager{DB: db}

	// 1st offer
	_ = mgr.RecordNegotiation(context.Background(), sc.ID, "price", 900000, 0, 0, "proposed")
	var updated storage.SalesConversation
	db.First(&updated, sc.ID)
	if updated.SameOfferCount != 1 {
		t.Fatalf("expected SameOfferCount=1 after first offer, got %d", updated.SameOfferCount)
	}

	// 2nd identical offer -> should increment SameOfferCount
	_ = mgr.RecordNegotiation(context.Background(), sc.ID, "price", 900000, 50000, 0, "proposed")
	db.First(&updated, sc.ID)
	if updated.SameOfferCount != 2 {
		t.Fatalf("expected SameOfferCount=2 after repeated identical offer, got %d", updated.SameOfferCount)
	}
	if updated.AIConcessionCount != 1 {
		t.Fatalf("expected AIConcessionCount=1 after a counter offer, got %d", updated.AIConcessionCount)
	}

	// 3rd identical offer with another AI counter -> SameOfferCount=3, AIConcessionCount=2
	_ = mgr.RecordNegotiation(context.Background(), sc.ID, "price", 900000, 48000, 0, "proposed")
	db.First(&updated, sc.ID)
	if updated.SameOfferCount != 3 {
		t.Fatalf("expected SameOfferCount=3 after third identical offer, got %d", updated.SameOfferCount)
	}
	if updated.AIConcessionCount != 2 {
		t.Fatalf("expected AIConcessionCount=2 after second counter offer, got %d", updated.AIConcessionCount)
	}
}
