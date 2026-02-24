package social

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

func TestShouldEscalateToHuman_AlertDoesNotCountAsEscalation(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	db.AutoMigrate(&storage.Tenant{}, &storage.Customer{}, &storage.Conversation{}, &storage.Interaction{}, &storage.EventLog{}, &storage.SalesConversation{}, &storage.EscalationEvent{})

	tenant := &storage.Tenant{Email: "t@t.com", Name: "T"}
	db.Create(tenant)
	customer := &storage.Customer{TenantID: tenant.ID, Channel: "whatsapp", PlatformID: "c1", DisplayName: "C1"}
	db.Create(customer)
	conv := &storage.Conversation{TenantID: tenant.ID, CustomerID: customer.ID, Platform: "whatsapp", Status: "active"}
	db.Create(conv)

	// Create a SalesConversation
	salesConv := &storage.SalesConversation{ConversationID: conv.ID, TenantID: tenant.ID, CustomerID: customer.ID, Stage: "negotiation"}
	db.Create(salesConv)

	// Create an EventLog (an alert forwarded) but NOT an EscalationEvent
	ev := &storage.EventLog{TenantID: tenant.ID, EventType: "payment", Message: "I have paid"}
	db.Create(ev)

	h := &MessageHandler{DB: db}
	msg := IncomingMessage{Content: "Any update?", MessageType: "text", TenantID: tenant.ID}
	intent := &ai.DetectedIntent{Intent: "inquiry", Tone: "neutral", Confidence: 0.95}

	should, reason := h.ShouldEscalateToHuman(context.Background(), msg, customer, intent, conv.ID, nil, salesConv)
	if should {
		t.Fatalf("expected NO escalation for simple follow-up when only an alert exists, got reason=%s", reason)
	}
}

func TestShouldEscalateToHuman_PriorEscalationEvent_TriggersFollowupEscalation(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	db.AutoMigrate(&storage.Tenant{}, &storage.Customer{}, &storage.Conversation{}, &storage.Interaction{}, &storage.SalesConversation{}, &storage.EscalationEvent{})

	tenant := &storage.Tenant{Email: "t@t.com", Name: "T"}
	db.Create(tenant)
	customer := &storage.Customer{TenantID: tenant.ID, Channel: "whatsapp", PlatformID: "c1", DisplayName: "C1"}
	db.Create(customer)
	conv := &storage.Conversation{TenantID: tenant.ID, CustomerID: customer.ID, Platform: "whatsapp", Status: "active"}
	db.Create(conv)

	// Create a SalesConversation
	salesConv := &storage.SalesConversation{ConversationID: conv.ID, TenantID: tenant.ID, CustomerID: customer.ID, Stage: "negotiation"}
	db.Create(salesConv)

	// Create an EscalationEvent within the last 7 days
	e := &storage.EscalationEvent{TenantID: tenant.ID, SalesConversationID: salesConv.ID, ProductID: uuid.Nil, ListPrice: 100, MinPrice: 90, Status: "pending"}
	db.Create(e)

	h := &MessageHandler{DB: db}
	msg := IncomingMessage{Content: "Any update?", MessageType: "text", TenantID: tenant.ID}
	intent := &ai.DetectedIntent{Intent: "complaint", Tone: "negative", Confidence: 0.95}

	should, reason := h.ShouldEscalateToHuman(context.Background(), msg, customer, intent, conv.ID, nil, salesConv)
	if !should {
		t.Fatalf("expected escalation when prior EscalationEvent exists and tone negative")
	}
	if reason != "follow_up_after_previous_escalation" {
		t.Fatalf("expected follow_up_after_previous_escalation, got %s", reason)
	}
}
