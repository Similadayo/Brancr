package tenantapi

import (
	"encoding/json"
	"testing"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

func setupKYBHandler(t *testing.T) *Handler {
	db := testutil.TestDB(t)
	if err := db.AutoMigrate(&storage.Tenant{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &Handler{DB: db, Logger: testutil.TestLogger(t)}
}

func TestSubmitKYB(t *testing.T) {
	h := setupKYBHandler(t)
	tenant := &storage.Tenant{Email: "kyb@tenant.com", Name: "KybTenant"}
	if err := h.DB.Create(tenant).Error; err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	body := map[string]interface{}{"data": map[string]interface{}{"docs": []string{"doc1"}}}
	req := testutil.TestRequestWithTenant(t, "POST", "/tenant/onboarding/kyb", body, tenant.ID, tenant.Email)
	rr := testutil.NewTestResponseRecorder()

	h.submitKYB(rr, req)
	rr.AssertStatus(t, 200)
	var resp map[string]any
	rr.BodyJSON(t, &resp)
	if resp["success"] != true {
		t.Fatalf("expected success true, got %v", resp)
	}

	var t2 storage.Tenant
	if err := h.DB.First(&t2, tenant.ID).Error; err != nil {
		t.Fatalf("get tenant: %v", err)
	}
	if t2.KYBStatus != "pending" {
		t.Fatalf("expected pending, got %v", t2.KYBStatus)
	}
	// Ensure kyb_data contains submitted JSON
	if t2.KYBData == "" {
		t.Fatalf("expected kyb_data set")
	}
	var tmp map[string]any
	if err := json.Unmarshal([]byte(t2.KYBData), &tmp); err != nil {
		t.Fatalf("kyb data invalid json: %v", err)
	}
}
