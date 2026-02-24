package ai

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

func TestGenerateCustomerReply_RecordsNegotiationHistory(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	// Create tenant and sales conversation
	tenant := &storage.Tenant{}
	db.Create(tenant)

	// Create sales conversation with price discussed 1000
	sc := &storage.SalesConversation{ConversationID: uuid.New(), TenantID: tenant.ID, CustomerID: uuid.New(), Stage: "negotiation", PriceDiscussed: 1000, ItemsDiscussed: "[]", NegotiationCount: 0}
	db.Create(sc)

	// Setup services
	logger := testutil.TestLogger(t)
	scManager := NewSalesConversationManager(db, nil, logger)
	r := NewCustomerResponder(nil, db, logger, nil, nil, nil)
	r.SalesConversationManager = scManager

	// Send offer "I have 950"
	_, err := r.GenerateCustomerReply(context.Background(), tenant.ID, uuid.New(), "I have 950", "inquiry", "whatsapp", uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Allow small delay for DB writes (not necessary but safe)
	time.Sleep(10 * time.Millisecond)

	var negotiations []storage.NegotiationHistory
	db.Where("sales_conversation_id = ?", sc.ID).Find(&negotiations)
	if len(negotiations) == 0 {
		t.Fatalf("expected negotiation record, got none")
	}
	n := negotiations[0]
	if n.CustomerOffer != 950 {
		t.Fatalf("expected customer offer 950, got %v", n.CustomerOffer)
	}
	if n.Status != "proposed" {
		t.Fatalf("expected status proposed, got %s", n.Status)
	}
}
