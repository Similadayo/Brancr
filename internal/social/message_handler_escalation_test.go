package social

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

// stubResponderType implements a fake GenerateCustomerReply for tests
type stubResponderType struct{}

func (s *stubResponderType) GenerateCustomerReply(ctx context.Context, tenantID uuid.UUID, customerID uuid.UUID, message string, intent string, platform string, conversationID ...uuid.UUID) (string, error) {
	return "Suggested reply", nil
}

func TestShouldEscalateToHuman_AccountDetails_NoEscalation(t *testing.T) {
	h := &MessageHandler{}
	msg := IncomingMessage{Content: "Please send your account number", MessageType: "text"}
	intent := &ai.DetectedIntent{Intent: "account", Tone: "neutral", Confidence: 0.9}

	should, _ := h.ShouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, uuid.Nil, ai.ExtractEvents(msg.Content), nil)
	if should {
		t.Fatalf("expected no escalation for account details request")
	}
}

func TestShouldEscalateToHuman_PaymentConfirmation_Escalates(t *testing.T) {
	h := &MessageHandler{}
	msg := IncomingMessage{Content: "I have paid", MessageType: "text"}
	intent := &ai.DetectedIntent{Intent: "payment", Tone: "neutral", Confidence: 0.9}

	should, reason := h.ShouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, uuid.Nil, ai.ExtractEvents(msg.Content), nil)
	if !should {
		t.Fatalf("expected escalation for payment confirmation")
	}
	if reason == "" {
		t.Fatalf("expected a non-empty escalation reason")
	}
}

func TestShouldEscalateToHuman_PaymentConfirmation_Escalates_EvenIfIntentOrder(t *testing.T) {
	h := &MessageHandler{}
	msg := IncomingMessage{Content: "I already made the payment 50000", MessageType: "text"}
	intent := &ai.DetectedIntent{Intent: "order", Tone: "neutral", Confidence: 0.9}

	should, _ := h.ShouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, uuid.Nil, ai.ExtractEvents(msg.Content), nil)
	if !should {
		t.Fatalf("expected escalation for payment confirmation even when intent=order")
	}
}

func TestShouldEscalateToHuman_PayOnDelivery_NoEscalation(t *testing.T) {
	h := &MessageHandler{}
	msg := IncomingMessage{Content: "pay on delivery", MessageType: "text"}
	intent := &ai.DetectedIntent{Intent: "inquiry", Tone: "neutral", Confidence: 0.9}

	should, _ := h.ShouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, uuid.Nil, ai.ExtractEvents(msg.Content), nil)
	if !should {
		t.Fatalf("expected escalation for pay-on-delivery approval")
	}
}

func TestShouldEscalateToHuman_PayOnDelivery_Inquiry_DoesNotEscalate(t *testing.T) {
	h := &MessageHandler{}
	msg := IncomingMessage{Content: "Do you do pay on delivery?", MessageType: "text"}
	intent := &ai.DetectedIntent{Intent: "inquiry", Tone: "neutral", Confidence: 0.9}

	should, _ := h.ShouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, uuid.Nil, ai.ExtractEvents(msg.Content), nil)
	if should {
		t.Fatalf("expected no escalation for POD inquiry")
	}
}

func TestShouldEscalateToHuman_InvoiceRequest_DoesNotEscalate(t *testing.T) {
	h := &MessageHandler{}
	msg := IncomingMessage{Content: "Can I get an invoice?", MessageType: "text"}
	intent := &ai.DetectedIntent{Intent: "billing", Tone: "neutral", Confidence: 0.9}

	should, _ := h.ShouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, uuid.Nil, ai.ExtractEvents(msg.Content), nil)
	if should {
		t.Fatalf("expected no escalation for invoice request")
	}
}

func TestShouldEscalateToHuman_ReceiptRequest_DoesNotEscalate(t *testing.T) {
	h := &MessageHandler{}
	msg := IncomingMessage{Content: "Please send receipt", MessageType: "text"}
	intent := &ai.DetectedIntent{Intent: "billing", Tone: "neutral", Confidence: 0.9}

	should, _ := h.ShouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, uuid.Nil, ai.ExtractEvents(msg.Content), nil)
	if should {
		t.Fatalf("expected no escalation for receipt request")
	}
}

func TestShouldEscalateToHuman_PaymentConfirmationWithReceipt_Escalates(t *testing.T) {
	h := &MessageHandler{}
	msg := IncomingMessage{Content: "I have paid, please send receipt", MessageType: "text"}
	intent := &ai.DetectedIntent{Intent: "billing", Tone: "neutral", Confidence: 0.9}

	should, _ := h.ShouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, uuid.Nil, ai.ExtractEvents(msg.Content), nil)
	if !should {
		t.Fatalf("expected escalation for payment confirmation even if receipt is mentioned")
	}
}

func TestShouldEscalateToHuman_PriceQuestion_DoesNotEscalate(t *testing.T) {
	h := &MessageHandler{}
	msg := IncomingMessage{Content: "How much is this?", MessageType: "text"}
	// Even if the detector mislabels this as payment, we should not escalate without a payment-confirmation signal.
	intent := &ai.DetectedIntent{Intent: "payment", Tone: "neutral", Confidence: 0.95}

	should, _ := h.ShouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, uuid.Nil, ai.ExtractEvents(msg.Content), nil)
	if should {
		t.Fatalf("expected no escalation for price question")
	}
}

func TestShouldEscalateToHuman_IPhoneModelQuestion_NotTreatedAsOffer(t *testing.T) {
	h := &MessageHandler{}
	msg := IncomingMessage{Content: "How much is your iPhone 15?", MessageType: "text"}
	intent := &ai.DetectedIntent{Intent: "inquiry", Tone: "neutral", Confidence: 0.95}

	should, _ := h.ShouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, uuid.Nil, ai.ExtractEvents(msg.Content), nil)
	if should {
		t.Fatalf("expected no escalation for product model price inquiry")
	}
}

func TestShouldEscalateToHuman_OfferWithinProximity_EscalatesNow(t *testing.T) {
	// Use DB-backed handler so we can set tenant policy
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	// Migrate tenant and policy
	db.AutoMigrate(&storage.Tenant{}, &storage.TenantPolicy{}, &storage.SalesConversation{})

	tenant := &storage.Tenant{Email: "t@t.com", Name: "T"}
	db.Create(tenant)
	// small proximity so offers near the list price trigger escalation
	policy := &storage.TenantPolicy{TenantID: tenant.ID, NegotiationProximityPct: 0.05}
	db.Create(policy)

	// sales conversation with listed price 750000
	salesConv := &storage.SalesConversation{ConversationID: uuid.New(), TenantID: tenant.ID, CustomerID: uuid.New(), Stage: "negotiation", PriceDiscussed: 750000}
	db.Create(salesConv)

	h := &MessageHandler{DB: db}
	msg := IncomingMessage{Content: "I can do 720000", MessageType: "text", TenantID: tenant.ID}
	intent := &ai.DetectedIntent{Intent: "price_objection", Tone: "neutral", Confidence: 0.95}

	should, reason := h.ShouldEscalateToHuman(context.Background(), msg, &storage.Customer{TenantID: tenant.ID}, intent, uuid.Nil, ai.ExtractEvents(msg.Content), salesConv)
	if !should {
		t.Fatalf("expected escalation for offer within proximity, got false")
	}
	if reason != "offer_in_range_for_approval" {
		t.Fatalf("expected reason offer_in_range_for_approval, got %s", reason)
	}
}

func TestShouldEscalateToHuman_OrderOptionSelection_DoesNotEscalate(t *testing.T) {
	h := &MessageHandler{}
	msg := IncomingMessage{Content: "I will like to go for 1", MessageType: "text"}
	intent := &ai.DetectedIntent{Intent: "order", Tone: "neutral", Confidence: 0.9}

	should, _ := h.ShouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, uuid.Nil, ai.ExtractEvents(msg.Content), nil)
	if should {
		t.Fatalf("expected no escalation for simple option selection")
	}
}

func TestShouldEscalateToHuman_PersistentNegotiation_EscalatesWhenCriteriaMet(t *testing.T) {
	// Setup DB and sales conversation
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	// Migrate sales conversation table
	db.AutoMigrate(&storage.SalesConversation{})

	// Create sales conversation with counters set to meet escalation criteria
	salesConv := &storage.SalesConversation{ConversationID: uuid.New(), TenantID: uuid.New(), CustomerID: uuid.New(), Stage: "negotiation", PriceDiscussed: 1000000, SameOfferCount: 3, LastOfferAmount: 920000, AIConcessionCount: 2, AIPersuasionCount: 3}
	db.Create(salesConv)

	h := &MessageHandler{DB: db}
	msg := IncomingMessage{Content: "Bro 920k cash now", MessageType: "text"}
	intent := &ai.DetectedIntent{Intent: "price_objection", Tone: "negative", Confidence: 0.9}

	should, reason := h.ShouldEscalateToHuman(context.Background(), msg, &storage.Customer{TenantID: uuid.New()}, intent, uuid.Nil, ai.ExtractEvents(msg.Content), salesConv)
	if !should {
		t.Fatalf("expected escalation for persistent negotiation, got false")
	}
	if reason != "persistent_negotiation_persistent_offer" {
		t.Fatalf("expected reason persistent_negotiation_persistent_offer, got %s", reason)
	}
}

func TestShouldEscalateToHuman_StatefulFollowup_PaymentConfirmationStage_Escalates(t *testing.T) {
	h := &MessageHandler{}
	msg := IncomingMessage{Content: "Any update?", MessageType: "text"}
	intent := &ai.DetectedIntent{Intent: "inquiry", Tone: "neutral", Confidence: 0.9}
	salesConv := &storage.SalesConversation{Stage: "payment_confirmation"}

	should, _ := h.ShouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, uuid.Nil, nil, salesConv)
	if should {
		t.Fatalf("expected NO escalation for simple inquiry even when stage=payment_confirmation (AI should handle update requests)")
	}
}

func TestShouldEscalateToHuman_TenantModeHuman_Escalates(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	// Auto-migrate Tenant so we can insert
	db.AutoMigrate(&storage.Tenant{})

	// Create tenant in human mode
	tenant := &storage.Tenant{AIHumanMode: "human"}
	db.Create(tenant)

	h := &MessageHandler{DB: db}
	msg := IncomingMessage{Content: "Hello", MessageType: "text", TenantID: tenant.ID}
	intent := &ai.DetectedIntent{Intent: "inquiry", Tone: "neutral", Confidence: 0.9}

	should, reason := h.ShouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, uuid.Nil, nil, nil)
	if !should {
		t.Fatalf("expected escalation when tenant is in human mode")
	}
	if reason != "tenant_mode_human" {
		t.Fatalf("expected reason tenant_mode_human, got %s", reason)
	}
}

func TestShouldEscalateToHuman_InboxModeHuman_Escalates(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	// Auto-migrate SocialAccount so we can insert
	db.AutoMigrate(&storage.SocialAccount{})

	// Create a connected social account with ai_mode human
	acc := &storage.SocialAccount{TenantID: uuid.New(), Platform: "whatsapp", Connected: true, AiMode: "human"}
	db.Create(acc)

	h := &MessageHandler{DB: db}
	msg := IncomingMessage{Content: "Hello", MessageType: "text", TenantID: uuid.New(), Platform: "whatsapp"}
	intent := &ai.DetectedIntent{Intent: "inquiry", Tone: "neutral", Confidence: 0.9}

	should, reason := h.ShouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, uuid.Nil, nil, nil)
	if !should {
		t.Fatalf("expected escalation when inbox is in human mode")
	}
	if reason != "inbox_mode_human" {
		t.Fatalf("expected reason inbox_mode_human, got %s", reason)
	}
}

func TestEscalateToTenant_PersistsReasonAndCallsEscalationFunc(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	// Migrate necessary tables
	db.AutoMigrate(&storage.Tenant{}, &storage.Customer{}, &storage.Conversation{}, &storage.Interaction{})

	// Create tenant and customer
	tenant := &storage.Tenant{AIHumanMode: "ai"}
	db.Create(tenant)

	customer := &storage.Customer{TenantID: tenant.ID, Channel: "whatsapp", PlatformID: "cust_123", DisplayName: "Jane"}
	db.Create(customer)

	// Create an incoming interaction to escalate
	interaction := &storage.Interaction{TenantID: tenant.ID, CustomerID: customer.ID, Platform: "whatsapp", Direction: "incoming", Content: "I have paid", ResponseType: "auto_reply", ResponseStatus: "pending"}
	db.Create(interaction)

	// Create a message handler with a fake responder and DB
	stubResponder := &stubResponderType{}

	var receivedReason string
	escalationFunc := func(ctx context.Context, tenantID uuid.UUID, interactionID uuid.UUID, customerName, platform, message, suggestedReply, intent, tone string, confidence float64, reason string) error {
		receivedReason = reason
		return nil
	}

	h := &MessageHandler{DB: db, Responder: stubResponder, EscalationFunc: escalationFunc}

	msg := IncomingMessage{TenantID: tenant.ID, Platform: "whatsapp", Content: "I have paid"}
	intent := &ai.DetectedIntent{Intent: "payment", Tone: "neutral", Confidence: 0.9}

	if err := h.escalateToTenant(context.Background(), msg, customer, intent, interaction.ID, nil, "tenant_mode_human"); err != nil {
		t.Fatalf("escalateToTenant returned error: %v", err)
	}

	// Reload interaction
	var updated storage.Interaction
	db.First(&updated, interaction.ID)
	if updated.EscalationReason != "tenant_mode_human" {
		t.Fatalf("expected interaction.EscalationReason to be set, got %s", updated.EscalationReason)
	}
	if receivedReason != "tenant_mode_human" {
		t.Fatalf("expected escalationFunc to be called with reason, got %s", receivedReason)
	}
}

func TestShouldEscalateToHuman_PersistentNegotiation_Escalates(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	// Migrate necessary tables
	db.AutoMigrate(&storage.Tenant{}, &storage.Customer{}, &storage.Conversation{}, &storage.Interaction{})

	// Create tenant and customer
	tenant := &storage.Tenant{AIHumanMode: "ai"}
	db.Create(tenant)

	customer := &storage.Customer{TenantID: tenant.ID, Channel: "whatsapp", PlatformID: "cust_123", DisplayName: "Jane"}
	db.Create(customer)

	// Create conversation and two incoming negotiation messages in the last 24 hours
	conv := &storage.Conversation{TenantID: tenant.ID, CustomerID: customer.ID, Platform: "whatsapp", Status: "active"}
	db.Create(conv)

	inter1 := &storage.Interaction{TenantID: tenant.ID, CustomerID: customer.ID, ConversationID: conv.ID, Platform: "whatsapp", Direction: "incoming", Content: "I have 550k", CreatedAt: time.Now().Add(-2 * time.Hour)}
	inter2 := &storage.Interaction{TenantID: tenant.ID, CustomerID: customer.ID, ConversationID: conv.ID, Platform: "whatsapp", Direction: "incoming", Content: "I can do 500k", CreatedAt: time.Now().Add(-1 * time.Hour)}
	db.Create(inter1)
	db.Create(inter2)

	h := &MessageHandler{DB: db}
	msg := IncomingMessage{Content: "Any chance of discount?", MessageType: "text", TenantID: tenant.ID}
	intent := &ai.DetectedIntent{Intent: "inquiry", Tone: "neutral", Confidence: 0.9}

	should, reason := h.ShouldEscalateToHuman(context.Background(), msg, customer, intent, conv.ID, nil, nil)
	if !should {
		t.Fatalf("expected escalation for persistent negotiation")
	}
	if !strings.Contains(reason, "persistent_negotiation") {
		t.Fatalf("expected persistent_negotiation reason, got %s", reason)
	}
}
