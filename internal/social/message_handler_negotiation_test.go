package social

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/similadayo/brancr/internal/negotiation"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

func TestProcessMessage_OfferTriggersEscalationViaNegotiationManager(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	// Auto migrate needed tables
	db.AutoMigrate(&storage.Tenant{}, &storage.Customer{}, &storage.Conversation{}, &storage.Interaction{}, &storage.SalesConversation{}, &storage.NegotiationState{}, &storage.EscalationEvent{}, &storage.TenantPolicy{})

	// setup tenant and policy
	tenant := &storage.Tenant{Email: "t@t.com", Name: "T"}
	db.Create(tenant)
	policy := &storage.TenantPolicy{TenantID: tenant.ID, NegotiationProximityPct: 0.05}
	db.Create(policy)

	// customer and conversation
	customer := &storage.Customer{TenantID: tenant.ID, Channel: "whatsapp", PlatformID: "cust_1", DisplayName: "Jane"}
	db.Create(customer)
	conv := &storage.Conversation{TenantID: tenant.ID, CustomerID: customer.ID, Platform: "whatsapp", Status: "active"}
	db.Create(conv)

	// sales conversation with price and item-level min
	itemsJSON := `[{"product_id":1,"negotiation_min_price":90}]`
	salesConv := &storage.SalesConversation{ConversationID: conv.ID, TenantID: tenant.ID, CustomerID: customer.ID, Stage: "negotiation", PriceDiscussed: 100, ItemsDiscussed: itemsJSON}
	db.Create(salesConv)

	// Create negotiation state that already matches escalation criteria
	s := &storage.NegotiationState{SalesConversationID: salesConv.ID, ProductID: uuid.New(), SameOfferCount: 3, LastCustomerOffer: 92, AIConcessionCount: 2, FlexibilityAttempted: true}
	db.Create(s)

	// capture escalation call
	var receivedReason string
	escFn := func(ctx context.Context, tenantID uuid.UUID, interactionID uuid.UUID, customerName, platform, message, suggestedReply, intent, tone string, confidence float64, reason string) error {
		receivedReason = reason
		return nil
	}

	// handler with real negotiation manager
	h := &MessageHandler{DB: db, NegotiationManager: negotiation.NewManager(db, testutil.TestLogger(t)), EscalationFunc: escFn}

	msg := IncomingMessage{Content: "I can do 92", MessageType: "text", TenantID: tenant.ID, CustomerID: customer.PlatformID, Platform: "whatsapp"}

	// process - this should trigger escalation via negotiation manager
	if err := h.ProcessMessage(context.Background(), msg); err != nil {
		t.Fatalf("ProcessMessage returned error: %v", err)
	}

	if receivedReason == "" {
		t.Fatalf("expected escalation function to be called with a reason")
	}
}
