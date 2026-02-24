package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

func TestPaymentsHandler_NoSensitiveFieldsReturned(t *testing.T) {
	db := testutil.TestDB(t)
	ctx := &AdminContext{DB: db}

	tenant := storage.Tenant{Name: "PayTenant", Email: "pay@t.com"}
	_ = db.Create(&tenant).Error
	// Create a payment with portal token and URL that should NOT be shown in admin list
	p := storage.Payment{TenantID: tenant.ID, PaymentReference: "REF123", Amount: 1000, Currency: "NGN", PortalToken: "secret-token", PortalURL: "https://portal.example/token/secret", ReceiptURL: "https://receipts.example/r/1", CreatedAt: time.Now()}
	_ = db.Create(&p).Error

	req := httptest.NewRequest("GET", "/admin/payments", nil)
	req = req.WithContext(context.WithValue(req.Context(), adminRoleKey, RoleBillingAdmin))
	w := httptest.NewRecorder()
	handler := PaymentsHandler(ctx)
	handler(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
	body := w.Body.String()
	if strings.Contains(body, "secret-token") || strings.Contains(body, "portal.example") || strings.Contains(body, "receipts.example") {
		t.Fatalf("expected sensitive payment fields not to be rendered in admin payments page")
	}
}

func TestPaymentsHandler_ForbiddenForNonBilling(t *testing.T) {
	db := testutil.TestDB(t)
	ctx := &AdminContext{DB: db}

	req := httptest.NewRequest("GET", "/admin/payments", nil)
	req = req.WithContext(context.WithValue(req.Context(), adminRoleKey, RoleSupportAgent))
	w := httptest.NewRecorder()
	handler := PaymentsHandler(ctx)
	handler(w, req)

	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-billing role, got %d", w.Result().StatusCode)
	}
}
