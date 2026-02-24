package middleware_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/quota"
	"github.com/similadayo/brancr/internal/testutil"
	"github.com/similadayo/brancr/pkg/metrics"
)

func TestAPIQuotaMiddleware_EnforcesLimit(t *testing.T) {
	db := testutil.TestDB(t)
	logger := testutil.TestLogger(t)
	defer testutil.CleanupDB(t, db)

	// Create tenant and set API limit = 1
	tenant := testutil.CreateTestTenant(t, db, "apitenant@example.com", "password", "API Tenant")
	qs := quota.NewQuotaService(db, logger, nil)
	if err := qs.UpdateQuotaLimits(context.Background(), tenant.ID, map[string]int{"api_calls_limit": 1}, 0.0, ""); err != nil {
		t.Fatalf("failed to set quota: %v", err)
	}

	m := metrics.NewMetrics()
	mw := middleware.APIQuota(qs, m)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", bytes.NewReader(nil))
	// Attach tenant claims in context
	req = req.WithContext(middleware.WithTenantContext(req.Context(), &middleware.TenantClaims{TenantID: tenant.ID}))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 first request, got %d", rr.Code)
	}

	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 second request, got %d", rr2.Code)
	}

	if m.QuotaRejections.Value() == 0 {
		t.Fatalf("expected quota rejections to be incremented")
	}
}
