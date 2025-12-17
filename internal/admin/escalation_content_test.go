package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/similadayo/brancr/internal/config"
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

func TestEscalationContent_PermissionDeniedForSupportAdmin(t *testing.T) {
	db := testutil.TestDB(t)
	ctx := &AdminContext{DB: db}

	tenant := storage.Tenant{Name: "T", Email: "t@example.com"}
	_ = db.Create(&tenant).Error
	inter := storage.Interaction{TenantID: tenant.ID, Content: "secret message", ResponseType: "escalated", ResponseStatus: "pending"}
	_ = db.Create(&inter).Error

	caseObj := storage.AdminCase{TenantID: tenant.ID, Status: "investigating", AssignedTo: ptrString("support@example.com")}
	_ = db.Create(&caseObj).Error

	// wrap handler with permission + case gating middleware (same order as router)
	inner := http.HandlerFunc(EscalationContentHandler(ctx))
	wrapped := RequirePermission("content.read")(RequireActiveAdminCase(ctx)(RequireRecentAuth(ctx)(inner)))

	req := httptest.NewRequest("GET", "/admin/escalations/1/content?case_id="+strconv.FormatUint(uint64(caseObj.ID), 10), nil)
	// put admin role and email in context (support role)
	req = req.WithContext(context.WithValue(req.Context(), adminRoleKey, RoleSupportAdmin))
	req = req.WithContext(context.WithValue(req.Context(), adminEmailKey, "support@example.com"))

	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for support admin, got %d", w.Result().StatusCode)
	}
}

func TestEscalationContent_AllowsComplianceAndSuperAdmin(t *testing.T) {
	db := testutil.TestDB(t)
	ctx := &AdminContext{DB: db}

	tenant := storage.Tenant{Name: "T", Email: "t@example.com"}
	_ = db.Create(&tenant).Error
	inter := storage.Interaction{TenantID: tenant.ID, Content: "secret message", ResponseType: "escalated", ResponseStatus: "pending"}
	_ = db.Create(&inter).Error

	caseObj := storage.AdminCase{TenantID: tenant.ID, Status: "investigating", AssignedTo: ptrString("compliance@example.com")}
	_ = db.Create(&caseObj).Error

	// capture audit entries
	var captured []AdminAuditEntry
	orig := auditWriter
	auditWriter = func(c context.Context, ac *AdminContext, e AdminAuditEntry) error {
		captured = append(captured, e)
		return nil
	}
	defer func() { auditWriter = orig }()

	// Obtain reauth token via handler
	reauthReq := httptest.NewRequest("POST", "/admin/reauth", bytes.NewBufferString("password=admin-password"))
	reauthReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reauthReq = reauthReq.WithContext(context.WithValue(reauthReq.Context(), adminRoleKey, RoleComplianceAdmin))
	reauthReq = reauthReq.WithContext(context.WithValue(reauthReq.Context(), adminEmailKey, "compliance@example.com"))
	// Set expected admin password in config for test
	ctx.Config = &config.Config{AdminPassword: "admin-password", AdminReauthTTLMinutes: 5}

	rr := httptest.NewRecorder()
	PostReauthHandler(ctx)(rr, reauthReq)
	if rr.Code != http.StatusOK {
		t.Fatalf("reauth endpoint failed: %d", rr.Code)
	}
	var out map[string]interface{}
	_ = json.NewDecoder(rr.Body).Decode(&out)
	token, _ := out["token"].(string)

	inner := http.HandlerFunc(EscalationContentHandler(ctx))
	wrapped := RequirePermission("content.read")(RequireActiveAdminCase(ctx)(RequireRecentAuth(ctx)(inner)))

	req := httptest.NewRequest("GET", "/admin/escalations/1/content?case_id="+strconv.FormatUint(uint64(caseObj.ID), 10), nil)
	req = req.WithContext(context.WithValue(req.Context(), adminRoleKey, RoleComplianceAdmin))
	req = req.WithContext(context.WithValue(req.Context(), adminEmailKey, "compliance@example.com"))
	req.Header.Set("X-Admin-Reauth-Token", token)

	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for compliance admin, got %d", w.Result().StatusCode)
	}

	// Expect audit entries: case.gate.access and escalation.content.read
	foundContentRead := false
	foundCaseGate := false
	for _, e := range captured {
		if e.Action == "escalation.content.read" {
			foundContentRead = true
		}
		if e.Action == "case.gate.access" {
			foundCaseGate = true
		}
	}
	if !foundCaseGate || !foundContentRead {
		t.Fatalf("expected both case gate and content read audits, got: %+v", captured)
	}
}

func TestContentReadPermission_AssignedCorrectly(t *testing.T) {
	// Ensure only ComplianceAdmin and SuperAdmin (via wildcard) have content.read
	for role, perms := range RolePermissions {
		if role == RoleComplianceAdmin {
			if !perms["content.read"] {
				t.Fatalf("expected %s to have content.read", RoleComplianceAdmin)
			}
			continue
		}
		if role == RoleSuperAdmin {
			if !perms["*"] {
				t.Fatalf("expected %s to have wildcard permission", RoleSuperAdmin)
			}
			continue
		}
		if perms["content.read"] {
			t.Fatalf("role %s should not have content.read permission", role)
		}
	}
}

func TestAdminAPIs_DoNotIncludeContentFields(t *testing.T) {
	db := testutil.TestDB(t)
	ctx := &AdminContext{DB: db}

	// Tenants list
	req := httptest.NewRequest("GET", "/admin/api/tenants", nil)
	req = req.WithContext(context.WithValue(req.Context(), adminRoleKey, RoleSuperAdmin))
	w := httptest.NewRecorder()
	handler := AdminTenantsListAPIHandler(ctx)
	handler(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("tenants API failed: %d", w.Result().StatusCode)
	}
	if strings.Contains(w.Body.String(), "\"content\"") {
		t.Fatalf("tenants API should not include content fields in JSON")
	}

	// Expiring tokens
	req2 := httptest.NewRequest("GET", "/admin/api/expiring-tokens", nil)
	req2 = req2.WithContext(context.WithValue(req2.Context(), adminRoleKey, RoleSuperAdmin))
	w2 := httptest.NewRecorder()
	handler2 := AdminExpiringTokensAPIHandler(ctx)
	handler2(w2, req2)
	if w2.Result().StatusCode != http.StatusOK {
		t.Fatalf("expiring tokens API failed: %d", w2.Result().StatusCode)
	}
	if strings.Contains(w2.Body.String(), "\"content\"") {
		t.Fatalf("expiring tokens API should not include content fields in JSON")
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

func TestEscalationsHandler_RedactsCardAndLongNumbers(t *testing.T) {
	db := testutil.TestDB(t)
	ctx := &AdminContext{DB: db}

	tenant := storage.Tenant{Name: "T", Email: "t@example.com"}
	_ = db.Create(&tenant).Error
	content := "Customer provided card 4111 1111 1111 1111 and account 123456789012"
	inter := storage.Interaction{TenantID: tenant.ID, Content: content, ResponseType: "escalated", ResponseStatus: "pending"}
	_ = db.Create(&inter).Error

	req := httptest.NewRequest("GET", "/admin/escalations", nil)
	w := httptest.NewRecorder()
	handler := EscalationsHandler(ctx)
	handler(w, req)

	body := w.Body.String()
	if strings.Contains(body, "4111") {
		t.Fatalf("expected card digits to be redacted")
	}
	if !strings.Contains(body, "[redacted card]") {
		t.Fatalf("expected card placeholder in output")
	}
	if strings.Contains(body, "123456789012") {
		t.Fatalf("expected long numeric sequence to be redacted")
	}
	if !strings.Contains(body, "[redacted number]") {
		t.Fatalf("expected redacted number placeholder in output")
	}
}
