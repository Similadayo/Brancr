package social

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/similadayo/brancr/internal/negotiation"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

// This test simulates a full negotiation flow where the AI attempts flexibility, makes concessions,
// the customer repeats the same offer 3 times, and the final incoming message triggers escalation.
func TestNegotiation_FullFlow_LeadsToEscalation(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	// Auto-migrate tables used in flow
	db.AutoMigrate(&storage.Tenant{}, &storage.Customer{}, &storage.Conversation{}, &storage.Interaction{}, &storage.SalesConversation{}, &storage.NegotiationState{}, &storage.EscalationEvent{}, &storage.TenantPolicy{})

	// create tenant, policy, customer, conv and sales conv
	tenant := &storage.Tenant{Email: "t@t.com", Name: "T"}
	db.Create(tenant)
	policy := &storage.TenantPolicy{TenantID: tenant.ID, NegotiationProximityPct: 0.05}
	db.Create(policy)

	customer := &storage.Customer{TenantID: tenant.ID, Channel: "whatsapp", PlatformID: "c1", DisplayName: "C1"}
	db.Create(customer)
	conv := &storage.Conversation{TenantID: tenant.ID, CustomerID: customer.ID, Platform: "whatsapp", Status: "active"}
	db.Create(conv)

	itemsJSON := `[{"product_id":2,"negotiation_min_price":90}]`
	salesConv := &storage.SalesConversation{ConversationID: conv.ID, TenantID: tenant.ID, CustomerID: customer.ID, Stage: "negotiation", PriceDiscussed: 100, ItemsDiscussed: itemsJSON}
	db.Create(salesConv)

	m := negotiation.NewManager(db, testutil.TestLogger(t))

	// Step 1: customer offers 92 - first offer
	if _, err := m.RecordCustomerOffer(context.Background(), salesConv.ID, uuid.New(), 92); err != nil {
		t.Fatalf("record offer failed: %v", err)
	}

	// Ensure same_offer_count == 1
	var s storage.NegotiationState
	if err := db.Where("sales_conversation_id = ? AND product_id = ?", salesConv.ID, 2).First(&s).Error; err != nil {
		t.Fatalf("load state failed: %v", err)
	}
	if s.SameOfferCount != 1 {
		t.Fatalf("expected same_offer_count 1 after first offer, got %d", s.SameOfferCount)
	}

	// Step 2: AI attempts flexibility (non-price lever)
	if err := m.MarkFlexibilityAttempted(context.Background(), salesConv.ID, uuid.New()); err != nil {
		t.Fatalf("mark flexibility failed: %v", err)
	}

	// Step 3: AI makes 2 concessions (exhausts max_concessions)
	if _, err := m.RecordAIConcession(context.Background(), salesConv.ID, uuid.New(), 95); err != nil {
		t.Fatalf("record ai concession failed: %v", err)
	}
	if _, err := m.RecordAIConcession(context.Background(), salesConv.ID, uuid.New(), 94); err != nil {
		t.Fatalf("record ai concession failed: %v", err)
	}

	// Step 4: customer repeats same offer to reach persistence
	if _, err := m.RecordCustomerOffer(context.Background(), salesConv.ID, uuid.New(), 92); err != nil {
		t.Fatalf("record offer failed: %v", err)
	}
	if _, err := m.RecordCustomerOffer(context.Background(), salesConv.ID, uuid.New(), 92); err != nil {
		t.Fatalf("record offer failed: %v", err)
	}

	// verify same offer count now 3
	if err := db.Where("sales_conversation_id = ? AND product_id = ?", salesConv.ID, 2).First(&s).Error; err != nil {
		t.Fatalf("load state failed: %v", err)
	}
	if s.SameOfferCount < 3 {
		t.Fatalf("expected same_offer_count >= 3 to indicate persistence, got %d", s.SameOfferCount)
	}

	// Step 5: final message processed by MessageHandler should call ExecuteEscalation. Use handler with real negotiation manager and capture escalation function call.
	var receivedReason string
	escFn := func(ctx context.Context, tenantID uuid.UUID, interactionID uuid.UUID, customerName, platform, message, suggestedReply, intent, tone string, confidence float64, reason string) error {
		receivedReason = reason
		return nil
	}

	h := &MessageHandler{DB: db, NegotiationManager: m, EscalationFunc: escFn}

	msg := IncomingMessage{Content: "I can do 92", MessageType: "text", TenantID: tenant.ID, CustomerID: customer.PlatformID, Platform: "whatsapp", Timestamp: time.Now()}

	if err := h.ProcessMessage(context.Background(), msg); err != nil {
		t.Fatalf("ProcessMessage failed: %v", err)
	}

	if receivedReason == "" {
		t.Fatalf("expected escalation to be triggered and EscalationFunc to be called with reason")
	}
}
