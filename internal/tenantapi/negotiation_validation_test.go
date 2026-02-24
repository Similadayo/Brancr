package tenantapi

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

func TestCreateProduct_NegotiableRequiresPrices(t *testing.T) {
	h, db := setupTestHandler(t)
	defer testutil.CleanupDB(t, db)

	tenant := testutil.CreateTestTenant(t, db, "p@example.com", "pass", "Prod Tenant")

	// Missing prices - canonical value
	req := testutil.TestRequestWithTenant(t, "POST", "/tenant/products", map[string]interface{}{"name": "P1", "negotiation_mode": "negotiable"}, tenant.ID, tenant.Email)
	rr := testutil.NewTestResponseRecorder()
	h.createProduct(rr, req)
	rr.AssertStatus(t, http.StatusBadRequest)

	var body map[string]interface{}
	rr.BodyJSON(t, &body)
	if body["error"] == nil {
		t.Fatalf("expected error message in response")
	}

	// Missing prices - frontend uses "range" synonym
	req2 := testutil.TestRequestWithTenant(t, "POST", "/tenant/products", map[string]interface{}{"name": "P1", "negotiation_mode": "range"}, tenant.ID, tenant.Email)
	rr2 := testutil.NewTestResponseRecorder()
	h.createProduct(rr2, req2)
	rr2.AssertStatus(t, http.StatusBadRequest)

	var body2 map[string]interface{}
	rr2.BodyJSON(t, &body2)
	if body2["error"] == nil {
		t.Fatalf("expected error message in response for range synonym")
	}
}

func TestUpdateProduct_NegotiableRequiresPrices(t *testing.T) {
	h, db := setupTestHandler(t)
	defer testutil.CleanupDB(t, db)

	tenant := testutil.CreateTestTenant(t, db, "up@example.com", "pass", "Upd Tenant")
	p := storage.Product{TenantID: tenant.ID, Name: "P2", Price: 10, Currency: "NGN"}
	if err := h.DB.Create(&p).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}

	// Attempt to set negotiable without prices
	path := "/tenant/products/" + fmt.Sprint(p.ID)
	req := testutil.TestRequestWithTenant(t, "PUT", path, map[string]interface{}{"negotiation_mode": "negotiable"}, tenant.ID, tenant.Email)
	rr := testutil.NewTestResponseRecorder()
	h.updateProduct(rr, req)
	rr.AssertStatus(t, http.StatusBadRequest)

	var body map[string]interface{}
	rr.BodyJSON(t, &body)
	if body["error"] == nil {
		t.Fatalf("expected error message in response")
	}
}

func TestUpdateNegotiationSettings_RequirePricesWhenNegotiable(t *testing.T) {
	h, db := setupTestHandler(t)
	defer testutil.CleanupDB(t, db)

	tenant := testutil.CreateTestTenant(t, db, "ns@example.com", "pass", "NS Tenant")

	req := testutil.TestRequestWithTenant(t, "PUT", "/tenant/settings/negotiation", map[string]interface{}{"negotiation_default": map[string]any{"mode": "negotiable"}}, tenant.ID, tenant.Email)
	rr := testutil.NewTestResponseRecorder()
	h.updateNegotiationSettings(rr, req)
	rr.AssertStatus(t, http.StatusBadRequest)

	var body map[string]interface{}
	rr.BodyJSON(t, &body)
	if body["error"] == nil {
		t.Fatalf("expected error message in response")
	}
}
