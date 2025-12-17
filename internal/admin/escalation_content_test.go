package admin

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/similadayo/brancr/internal/storage"
    "github.com/similadayo/brancr/internal/testutil"
)

func TestEscalationContentHandler_ForbiddenWhenNoCase(t *testing.T) {
    db := testutil.TestDB(t)
    ctx := &AdminContext{DB: db}

    req := httptest.NewRequest("GET", "/admin/escalations/1/content", nil)
    w := httptest.NewRecorder()

    handler := EscalationContentHandler(ctx)
    handler(w, req)

    if w.Result().StatusCode != http.StatusForbidden {
        t.Fatalf("expected 403, got %d", w.Result().StatusCode)
    }
}

func TestEscalationContentHandler_ReturnsContentAndAuditedWhenCasePresent(t *testing.T) {
    db := testutil.TestDB(t)
    ctx := &AdminContext{DB: db}

    // create tenant and interaction
    tenant := storage.Tenant{Name: "T", Email: "t@example.com"}
    _ = db.Create(&tenant).Error
    inter := storage.Interaction{TenantID: tenant.ID, Content: "secret message", ResponseType: "escalated", ResponseStatus: "pending"}
    _ = db.Create(&inter).Error

    // create admin case for tenant
    caseObj := storage.AdminCase{TenantID: tenant.ID, Status: "investigating", AssignedTo: ptrString("admin@example.com")}
    _ = db.Create(&caseObj).Error

    // stub auditWriter to capture entry
    var captured *AdminAuditEntry
    orig := auditWriter
    auditWriter = func(c context.Context, ac *AdminContext, e AdminAuditEntry) error {
        captured = &e
        return nil
    }
    defer func() { auditWriter = orig }()

    req := httptest.NewRequest("GET", "/admin/escalations/1/content", nil)
    // place case & admin email in context as middleware would
    req = req.WithContext(context.WithValue(req.Context(), adminCaseKey, &caseObj))
    req = req.WithContext(context.WithValue(req.Context(), adminEmailKey, "admin@example.com"))

    w := httptest.NewRecorder()
    handler := EscalationContentHandler(ctx)
    handler(w, req)

    if w.Result().StatusCode != http.StatusOK {
        t.Fatalf("expected 200, got %d", w.Result().StatusCode)
    }

    var it storage.Interaction
    if err := json.NewDecoder(w.Body).Decode(&it); err != nil {
        t.Fatalf("failed to decode response: %v", err)
    }
    if it.Content != "secret message" {
        t.Fatalf("expected content to be returned, got: %q", it.Content)
    }

    if captured == nil {
        t.Fatalf("expected audit to be written")
    }
    if captured.Action != "escalation.content.read" {
        t.Fatalf("unexpected audit action: %s", captured.Action)
    }
}

func TestEscalationsHandler_RedactsPII(t *testing.T) {
    db := testutil.TestDB(t)
    ctx := &AdminContext{DB: db}

    tenant := storage.Tenant{Name: "T", Email: "t@example.com"}
    _ = db.Create(&tenant).Error
    content := "Customer says contact me at +1234567890 or email foo@bar.com for refund"
    inter := storage.Interaction{TenantID: tenant.ID, Content: content, ResponseType: "escalated", ResponseStatus: "pending"}
    _ = db.Create(&inter).Error

    req := httptest.NewRequest("GET", "/admin/escalations", nil)
    w := httptest.NewRecorder()
    handler := EscalationsHandler(ctx)
    handler(w, req)

    body := w.Body.String()
    if body == "" {
        t.Fatalf("expected non-empty body")
    }
    if contains := strings.Contains(body, "foo@bar.com"); contains {
        t.Fatalf("expected email to be redacted in output")
    }
    if contains := strings.Contains(body, "+1234567890"); contains {
        t.Fatalf("expected phone to be redacted in output")
    }
    if !strings.Contains(body, "[redacted email]") || !strings.Contains(body, "[redacted phone]") {
        t.Fatalf("expected masked placeholders in output")
    }
}
