package tenantapi

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

func setupNegotiationHandler(t *testing.T) *Handler {
	db := testutil.TestDB(t)
	if err := db.AutoMigrate(&storage.Tenant{}, &storage.TenantPolicy{}, &storage.SalesConversation{}, &storage.NegotiationHistory{}); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	logger := testutil.TestLogger(t)
	h := &Handler{DB: db, Logger: logger}
	return h
}

func TestNegotiationPolicy_GetAndUpdate(t *testing.T) {
	h := setupNegotiationHandler(t)
	tenant := &storage.Tenant{Email: "t@t.com", Name: "T"}
	h.DB.Create(tenant)

	// GET defaults
	req := testutil.TestRequestWithTenant(t, "GET", "/tenant/negotiation/policy", nil, tenant.ID, tenant.Email)
	rr := testutil.NewTestResponseRecorder()
	h.getNegotiationPolicy(rr, req)
	rr.AssertStatus(t, 200)

	// Update policy (negotiation thresholds only)
	body := map[string]any{"negotiation_same_offer_required": 4}
	req2 := testutil.TestRequestWithTenant(t, "PUT", "/tenant/negotiation/policy", body, tenant.ID, tenant.Email)
	rr2 := testutil.NewTestResponseRecorder()
	h.updateNegotiationPolicy(rr2, req2)
	rr2.AssertStatus(t, 200)

	var saved storage.TenantPolicy
	h.DB.Where("tenant_id = ?", tenant.ID).First(&saved)
	if saved.NegotiationSameOfferRequired != 4 {
		t.Fatalf("expected negotiation_same_offer_required saved=4, got %d", saved.NegotiationSameOfferRequired)
	}
}

func TestSalesNegotiations_ListAndCreate(t *testing.T) {
	h := setupNegotiationHandler(t)
	tenant := &storage.Tenant{Email: "s@t.com", Name: "S"}
	h.DB.Create(tenant)

	sc := &storage.SalesConversation{ConversationID: uuid.New(), TenantID: tenant.ID, CustomerID: uuid.New(), Stage: "negotiation", PriceDiscussed: 1000000}
	h.DB.Create(sc)

	// Create via manager
	mgr := ai.NewSalesConversationManager(h.DB, nil, nil)
	if err := mgr.RecordNegotiation(context.Background(), sc.ID, "price", 900000, 50000, 0, "proposed"); err != nil {
		t.Fatalf("record negotiation: %v", err)
	}
	// Another entry
	if err := mgr.RecordNegotiation(context.Background(), sc.ID, "price", 900000, 48000, 0, "proposed"); err != nil {
		t.Fatalf("record negotiation: %v", err)
	}

	// List
	req := testutil.TestRequestWithTenant(t, "GET", "/tenant/sales-conversations/"+fmt.Sprintf("%d", sc.ID)+"/negotiations", nil, tenant.ID, tenant.Email)
	rr := testutil.NewTestResponseRecorder()
	h.getSalesNegotiations(rr, req)
	rr.AssertStatus(t, 200)

	var rows []storage.NegotiationHistory
	rr.BodyJSON(t, &rows)
	if len(rows) != 2 {
		t.Fatalf("expected 2 negotiations, got %d", len(rows))
	}

	// Create via endpoint
	body := map[string]any{"type": "price", "customer_offer": 920000, "counter_offer": 0, "final_agreement": 0, "status": "proposed"}
	req2 := testutil.TestRequestWithTenant(t, "POST", "/tenant/sales-conversations/"+fmt.Sprintf("%d", sc.ID)+"/negotiations", body, tenant.ID, tenant.Email)
	rr2 := testutil.NewTestResponseRecorder()
	h.createSalesNegotiation(rr2, req2)
	rr2.AssertStatus(t, 201)

	// Ensure record exists
	var rows2 []storage.NegotiationHistory
	h.DB.Where("sales_conversation_id = ?", sc.ID).Find(&rows2)
	if len(rows2) != 3 {
		t.Fatalf("expected 3 negotiations, got %d", len(rows2))
	}

	// Ensure counters updated on recording
	var updated storage.SalesConversation
	h.DB.First(&updated, sc.ID)
	if updated.SameOfferCount < 1 {
		t.Fatalf("expected SameOfferCount >=1, got %d", updated.SameOfferCount)
	}
	// ensure timestamps are recent
	if time.Since(rows2[0].CreatedAt) > time.Minute*5 {
		t.Fatalf("expected recent created_at")
	}
}
