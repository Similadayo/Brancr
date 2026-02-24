package admin

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/config"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/tenantapi"
	"github.com/similadayo/brancr/internal/testutil"
	"github.com/similadayo/brancr/pkg/metrics"
)

func TestImpersonationStart_AllowsTenantAccess(t *testing.T) {
	db := testutil.TestDB(t)
	logger := testutil.TestLogger(t)
	cfg := testutil.TestConfigWithJWT()
	defer testutil.CleanupDB(t, db)

	// Create tenant + conversation + interaction
	tenant := testutil.CreateTestTenant(t, db, "int-imp@t.com", "pw", "Impersonation Tenant")
	convo := storage.Conversation{TenantID: tenant.ID, Platform: "whatsapp", Status: "active", CustomerID: uuid.New()}
	if err := db.Create(&convo).Error; err != nil {
		t.Fatalf("create convo: %v", err)
	}
	inter := storage.Interaction{TenantID: tenant.ID, ConversationID: convo.ID, Content: "Top secret content", Direction: "incoming"}
	if err := db.Create(&inter).Error; err != nil {
		t.Fatalf("create interaction: %v", err)
	}

	// Create admin case assigned to admin@example.com
	caseObj := storage.AdminCase{TenantID: tenant.ID, Status: "investigating", AssignedTo: ptrString("admin@example.com")}
	if err := db.Create(&caseObj).Error; err != nil {
		t.Fatalf("create case: %v", err)
	}

	// Admin context
	ac := &AdminContext{DB: db, Config: &config.Config{TenantJWTSecret: cfg.TenantJWTSecret}}

	// Stub auditWriter to succeed
	orig := auditWriter
	auditWriter = func(c context.Context, ac *AdminContext, e AdminAuditEntry) error { return nil }
	defer func() { auditWriter = orig }()

	// Build POST form
	form := fmt.Sprintf("tenant_id=%d", tenant.ID)
	req := httptest.NewRequest("POST", "/admin/support/impersonate", bytes.NewBufferString(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// place case & admin identity in context
	req = req.WithContext(context.WithValue(req.Context(), adminCaseKey, &caseObj))
	req = req.WithContext(context.WithValue(req.Context(), adminEmailKey, "admin@example.com"))
	req = req.WithContext(context.WithValue(req.Context(), adminRoleKey, RolePlatformAdmin))

	w := httptest.NewRecorder()
	handler := ImpersonationStartHandler(ac)
	handler(w, req)

	if w.Result().StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", w.Result().StatusCode)
	}

	var cookieVal string
	for _, c := range w.Result().Cookies() {
		if c.Name == tenantSessionCookieName {
			cookieVal = c.Value
		}
	}
	if cookieVal == "" {
		t.Fatalf("expected tenant session cookie to be set")
	}

	// Mount tenant router and call conversation detail
	th := tenantapi.NewHandler(db, logger, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, metrics.NewMetrics(), nil, nil)
	r := chi.NewRouter()
	th.RegisterRoutes(r)

	convoURL := "/tenant/conversations/" + fmt.Sprintf("%d", convo.ID)
	req2 := httptest.NewRequest("GET", convoURL, nil)
	req2.AddCookie(&http.Cookie{Name: tenantapi.SessionCookie, Value: cookieVal})
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req2)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 accessing conversation as impersonated tenant, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Top secret content") {
		t.Fatalf("expected conversation content visible to impersonated admin")
	}
}
