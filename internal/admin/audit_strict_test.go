package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/similadayo/brancr/internal/config"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

func TestCreateAdminCase_FailsWhenAuditFails(t *testing.T) {
	db := testutil.TestDB(t)
	ctx := &AdminContext{DB: db}

	// stub auditWriter to fail
	orig := auditWriter
	auditWriter = func(c context.Context, ac *AdminContext, e AdminAuditEntry) error {
		return errors.New("db down")
	}
	defer func() { auditWriter = orig }()

	body := CreateCaseRequest{TenantID: 1, Source: "support", Reason: "test"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/admin/cases", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler := CreateAdminCaseHandler(ctx)
	handler(w, req)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Result().StatusCode)
	}

	var count int64
	_ = db.Model(&storage.AdminCase{}).Count(&count).Error
	if count != 0 {
		t.Fatalf("case should not be created when audit fails")
	}
}

func TestImpersonationStart_FailsWhenAuditFails(t *testing.T) {
	db := testutil.TestDB(t)
	// create tenant record
	tenant := storage.Tenant{Email: "a@b.com", Name: "Test"}
	_ = db.Create(&tenant).Error

	// create admin case matching tenant
	caseObj := storage.AdminCase{TenantID: tenant.ID, Status: "investigating", AssignedTo: ptrString("admin@example.com")}
	_ = db.Create(&caseObj).Error

	ctx := &AdminContext{DB: db, Config: &config.Config{TenantJWTSecret: "secretsecretsecretsecret"}}

	// stub auditWriter to fail
	orig := auditWriter
	auditWriter = func(c context.Context, ac *AdminContext, e AdminAuditEntry) error {
		return errors.New("audit fail")
	}
	defer func() { auditWriter = orig }()

	form := "tenant_id=" + strconv.Itoa(int(tenant.ID))
	req := httptest.NewRequest("POST", "/admin/support/impersonate", bytes.NewBufferString(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// place case in request context as middleware would
	req = req.WithContext(context.WithValue(req.Context(), adminCaseKey, &caseObj))
	req = req.WithContext(context.WithValue(req.Context(), adminEmailKey, "admin@example.com"))

	w := httptest.NewRecorder()
	handler := ImpersonationStartHandler(ctx)
	handler(w, req)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Result().StatusCode)
	}
	// cookie should not be set
	setCookies := w.Result().Header["Set-Cookie"]
	found := false
	for _, c := range setCookies {
		if strings.Contains(c, tenantSessionCookieName+"=") {
			found = true
			break
		}
	}
	if found {
		t.Fatalf("cookie should not be set when audit fails")
	}
}

func TestEscalationsList_NotAffectedByAuditFailure(t *testing.T) {
	db := testutil.TestDB(t)
	ctx := &AdminContext{DB: db}

	// stub auditWriter to fail
	orig := auditWriter
	auditWriter = func(c context.Context, ac *AdminContext, e AdminAuditEntry) error {
		return errors.New("audit fail")
	}
	defer func() { auditWriter = orig }()

	req := httptest.NewRequest("GET", "/admin/escalations", nil)
	w := httptest.NewRecorder()
	handler := EscalationsHandler(ctx)
	handler(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
}

// helper
func ptrString(s string) *string { return &s }
