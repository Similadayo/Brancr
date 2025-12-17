package admin

import (
    "context"
    "net/http"
    "net/http/httptest"
    "strconv"
    "strings"
    "testing"

    "github.com/similadayo/brancr/internal/config"
    "github.com/similadayo/brancr/internal/storage"
    "github.com/similadayo/brancr/internal/testutil"
)

func TestImpersonationStart_ForbiddenWhenNoCase(t *testing.T) {
    db := testutil.TestDB(t)
    tenant := storage.Tenant{Email: "a@b.com", Name: "Test"}
    _ = db.Create(&tenant).Error

    ctx := &AdminContext{DB: db, Config: &config.Config{TenantJWTSecret: "secretsecretsecretsecret"}}

    form := "tenant_id=" + strconv.Itoa(int(tenant.ID))
    req := httptest.NewRequest("POST", "/admin/support/impersonate", strings.NewReader(form))
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    w := httptest.NewRecorder()

    handler := ImpersonationStartHandler(ctx)
    handler(w, req)

    if w.Result().StatusCode != http.StatusForbidden {
        t.Fatalf("expected 403, got %d", w.Result().StatusCode)
    }
}

func TestImpersonationStart_ForbiddenWhenNotAssigned(t *testing.T) {
    db := testutil.TestDB(t)
    tenant := storage.Tenant{Email: "a@b.com", Name: "Test"}
    _ = db.Create(&tenant).Error
    // case exists but not assigned to this admin
    caseObj := storage.AdminCase{TenantID: tenant.ID, Status: "investigating", AssignedTo: ptrString("other@example.com")}
    _ = db.Create(&caseObj).Error

    ctx := &AdminContext{DB: db, Config: &config.Config{TenantJWTSecret: "secretsecretsecretsecret"}}
    form := "tenant_id=" + strconv.Itoa(int(tenant.ID))
    req := httptest.NewRequest("POST", "/admin/support/impersonate", strings.NewReader(form))
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    // place case and admin email in context
    req = req.WithContext(context.WithValue(req.Context(), adminCaseKey, &caseObj))
    req = req.WithContext(context.WithValue(req.Context(), adminEmailKey, "admin@example.com"))

    w := httptest.NewRecorder()
    handler := ImpersonationStartHandler(ctx)
    handler(w, req)

    if w.Result().StatusCode != http.StatusForbidden {
        t.Fatalf("expected 403, got %d", w.Result().StatusCode)
    }
}

func TestImpersonationStart_SetsCookiePathAdmin(t *testing.T) {
    db := testutil.TestDB(t)
    tenant := storage.Tenant{Email: "a@b.com", Name: "Test"}
    _ = db.Create(&tenant).Error
    caseObj := storage.AdminCase{TenantID: tenant.ID, Status: "investigating", AssignedTo: ptrString("admin@example.com")}
    _ = db.Create(&caseObj).Error

    ctx := &AdminContext{DB: db, Config: &config.Config{TenantJWTSecret: "secretsecretsecretsecret", TenantImpersonationTTLMinutes: 5}}

    form := "tenant_id=" + strconv.Itoa(int(tenant.ID))
    req := httptest.NewRequest("POST", "/admin/support/impersonate", strings.NewReader(form))
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    req = req.WithContext(context.WithValue(req.Context(), adminCaseKey, &caseObj))
    req = req.WithContext(context.WithValue(req.Context(), adminEmailKey, "admin@example.com"))

    // stub auditWriter to succeed quickly
    orig := auditWriter
    auditWriter = func(c context.Context, ac *AdminContext, e AdminAuditEntry) error { return nil }
    defer func() { auditWriter = orig }()

    w := httptest.NewRecorder()
    handler := ImpersonationStartHandler(ctx)
    handler(w, req)

    if w.Result().StatusCode != http.StatusSeeOther {
        t.Fatalf("expected 303 redirect, got %d", w.Result().StatusCode)
    }

    found := false
    for _, c := range w.Result().Cookies() {
        if c.Name == tenantSessionCookieName {
            if c.Path != "/admin" {
                t.Fatalf("expected cookie path /admin, got %s", c.Path)
            }
            if c.MaxAge != 5*60 {
                t.Fatalf("expected cookie MaxAge %d, got %d", 5*60, c.MaxAge)
            }
            found = true
        }
    }
    if !found {
        t.Fatalf("expected tenant session cookie to be set")
    }
}
