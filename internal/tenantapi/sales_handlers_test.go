package tenantapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

func TestApproveNegotiationHandler(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	// create tenant and sales conversation + negotiation
	tenant := &storage.Tenant{}
	db.Create(tenant)

	conversationID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	customerID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	sc := &storage.SalesConversation{
		ConversationID: conversationID,
		TenantID:       tenant.ID,
		CustomerID:     customerID,
		Stage:          "negotiation",
		PriceDiscussed: 1000,
	}
	db.Create(sc)

	neg := storage.NegotiationHistory{
		SalesConversationID: sc.ID,
		Type:                "price",
		CustomerOffer:       950,
		CounterOffer:        900,
		FinalAgreement:      0,
		Status:              "proposed",
	}
	db.Create(&neg)

	h := NewHandler(db, testutil.TestLogger(t), testutil.TestConfig(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	// Build request
	b, _ := json.Marshal(map[string]any{"final_price": 900})
	req := httptest.NewRequest("POST", "/tenant/negotiations/1/approve", bytes.NewReader(b))
	// Add tenant context
	claims := &middleware.TenantClaims{TenantID: tenant.ID}
	req = req.WithContext(middleware.WithTenantContext(context.Background(), claims))
	w := httptest.NewRecorder()

	h.approveNegotiation(w, req)
	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	// Check DB updated
	var updated storage.NegotiationHistory
	db.First(&updated, neg.ID)
	if updated.Status != "agreed" {
		t.Fatalf("expected negotiation status agreed, got %s", updated.Status)
	}
	if updated.FinalAgreement != 900 {
		t.Fatalf("expected final agreement 900, got %v", updated.FinalAgreement)
	}
	// Check sales conversation updated
	var sc2 storage.SalesConversation
	db.First(&sc2, sc.ID)
	if sc2.PriceDiscussed != 900 {
		t.Fatalf("expected sales conv price updated to 900, got %v", sc2.PriceDiscussed)
	}
}
