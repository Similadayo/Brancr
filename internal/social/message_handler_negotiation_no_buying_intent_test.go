package social

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/similadayo/brancr/internal/negotiation"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

func TestProcessMessage_NoEscalationWhenBuyingIntentMissing(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	db.AutoMigrate(&storage.Tenant{}, &storage.Customer{}, &storage.Conversation{}, &storage.Interaction{}, &storage.SalesConversation{}, &storage.NegotiationState{}, &storage.EscalationEvent{}, &storage.TenantPolicy{})

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

	// negotiation state meets persistence and concessions, but incoming message lacks buying intent keyword
	s := &storage.NegotiationState{SalesConversationID: salesConv.ID, ProductID: uuid.New(), SameOfferCount: 3, LastCustomerOffer: 92, AIConcessionCount: 2, FlexibilityAttempted: true}
	db.Create(s)

	var called bool
	escFn := func(ctx context.Context, tenantID uuid.UUID, interactionID uuid.UUID, customerName, platform, message, suggestedReply, intent, tone string, confidence float64, reason string) error {
		called = true
		return nil
	}

	h := &MessageHandler{DB: db, NegotiationManager: negotiation.NewManager(db, testutil.TestLogger(t)), EscalationFunc: escFn}

	// content "I can do 92" does not include buying intent keywords like 'cash' or 'buy now'
	msg := IncomingMessage{Content: "I can do 92", MessageType: "text", TenantID: tenant.ID, CustomerID: customer.PlatformID, Platform: "whatsapp"}

	if err := h.ProcessMessage(context.Background(), msg); err != nil {
		t.Fatalf("ProcessMessage returned error: %v", err)
	}

	if called {
		t.Fatalf("expected no escalation when buying intent is missing")
	}
}
