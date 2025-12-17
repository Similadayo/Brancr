package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/tenantapi"
	"github.com/similadayo/brancr/internal/testutil"
	"github.com/similadayo/brancr/pkg/metrics"
)

// TestE2E_TenantSessionCookie_AllowsConversationAccess verifies that a valid tenant session
// cookie (created by tenant token generator) can access tenant conversation endpoints.
func TestE2E_TenantSessionCookie_AllowsConversationAccess(t *testing.T) {
	db := testutil.TestDB(t)
	logger := testutil.TestLogger(t)
	cfg := testutil.TestConfigWithJWT()
	defer testutil.CleanupDB(t, db)

	// Create tenant and conversation with interaction
	tenant := testutil.CreateTestTenant(t, db, "e2e-imp@t.com", "pass", "E2E Imp Tenant")
	convo := storage.Conversation{TenantID: tenant.ID, Platform: "whatsapp", Status: "active", CustomerID: 0}
	if err := db.Create(&convo).Error; err != nil {
		t.Fatalf("failed to create convo: %v", err)
	}
	inter := storage.Interaction{TenantID: tenant.ID, ConversationID: convo.ID, Content: "Sensitive refund details: paid", Direction: "incoming", ResponseType: "escalated", ResponseStatus: "pending"}
	if err := db.Create(&inter).Error; err != nil {
		t.Fatalf("failed to create interaction: %v", err)
	}

	// Create tenant session token directly
	token, err := middlewareCreateToken(cfg, tenant.ID, tenant.Email)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	th := tenantapi.NewHandler(db, logger, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, metrics.NewMetrics())
	r := chi.NewRouter()
	th.RegisterRoutes(r)

	convoURL := "/tenant/conversations/" + fmt.Sprintf("%d", convo.ID)
	req := httptest.NewRequest("GET", convoURL, nil)
	req.AddCookie(&http.Cookie{Name: tenantapi.SessionCookie, Value: token})
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Sensitive refund details") {
		t.Fatalf("expected conversation content visible, got body: %s", rr.Body.String())
	}
}
