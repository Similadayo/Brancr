package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

func setupAdminCtx(t *testing.T) *AdminContext {
	db := testutil.TestDB(t)
	if err := db.AutoMigrate(&storage.Tenant{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &AdminContext{DB: db, Logger: testutil.TestLogger(t)}
}

func TestAdminVerifyKYB(t *testing.T) {
	ctx := setupAdminCtx(t)
	tenant := &storage.Tenant{Email: "a@b.com", Name: "T1"}
	if err := ctx.DB.Create(tenant).Error; err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	req := httptest.NewRequest("POST", "/admin/api/tenants/1/kyb/verify", nil)
	req = req.WithContext(context.WithValue(req.Context(), "admin_email", "admin@x"))
	w := httptest.NewRecorder()

	handler := AdminVerifyTenantKYBHandler(ctx)
	handler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body: %s", w.Code, w.Body.String())
	}
	var t2 storage.Tenant
	if err := ctx.DB.First(&t2, tenant.ID).Error; err != nil {
		t.Fatalf("get tenant: %v", err)
	}
	if t2.KYBStatus != "verified" {
		t.Fatalf("expected verified, got %v", t2.KYBStatus)
	}
}
