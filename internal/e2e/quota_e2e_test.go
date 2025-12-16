package e2e

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/similadayo/brancr/internal/config"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/quota"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/tenantapi"
	"github.com/similadayo/brancr/internal/testutil"
	"github.com/similadayo/brancr/pkg/metrics"
)

// TestE2E_APIQuota verifies API quota enforced across real router + middleware
func TestE2E_APIQuota(t *testing.T) {
	db := testutil.TestDB(t)
	logger := testutil.TestLogger(t)
	cfg := testutil.TestConfigWithJWT()
	defer testutil.CleanupDB(t, db)

	// Create handler and register routes
	h := tenantapi.NewHandler(db, logger, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, metrics.NewMetrics())

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	// Create tenant and set API limit to 1
	tenant := testutil.CreateTestTenant(t, db, "e2e-api@example.com", "password123", "E2E API Tenant")
	qs := quota.NewQuotaService(db, logger, nil)
	if err := qs.UpdateQuotaLimits(context.Background(), tenant.ID, map[string]int{"api_calls_limit": 1}, ""); err != nil {
		t.Fatalf("failed to set quota: %v", err)
	}

	// Create signed tenant token
	token, err := middlewareCreateToken(cfg, tenant.ID, tenant.Email)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	// First request should succeed
	req1 := httptest.NewRequest("GET", "/auth/me", nil)
	req1.AddCookie(&http.Cookie{Name: tenantapi.SessionCookie, Value: token})
	rr1 := httptest.NewRecorder()
	r.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("expected first request 200, got %d", rr1.Code)
	}

	// Second request should be blocked
	req2 := httptest.NewRequest("GET", "/auth/me", nil)
	req2.AddCookie(&http.Cookie{Name: tenantapi.SessionCookie, Value: token})
	rr2 := httptest.NewRecorder()
	r.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second request 429, got %d", rr2.Code)
	}

	// Ensure quota usage recorded once
	qData, err := qs.GetQuotaUsage(context.Background(), tenant.ID)
	if err != nil {
		t.Fatalf("failed to get quota: %v", err)
	}
	if qData.APICallsUsed != 1 {
		t.Fatalf("expected api_calls_used == 1, got %d", qData.APICallsUsed)
	}
}

// TestE2E_StorageQuota verifies storage quota enforcement using real router + handlers
func TestE2E_StorageQuota(t *testing.T) {
	db := testutil.TestDB(t)
	logger := testutil.TestLogger(t)
	cfg := testutil.TestConfigWithJWT()
	defer testutil.CleanupDB(t, db)

	h := tenantapi.NewHandler(db, logger, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, metrics.NewMetrics())

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	// Create tenant and set storage limit 1MB
	tenant := testutil.CreateTestTenant(t, db, "e2e-storage@example.com", "password123", "E2E Storage Tenant")
	qs := quota.NewQuotaService(db, logger, nil)
	if err := qs.UpdateQuotaLimits(context.Background(), tenant.ID, map[string]int{"storage_limit_mb": 1}, ""); err != nil {
		t.Fatalf("failed to set quota: %v", err)
	}

	token, err := middlewareCreateToken(cfg, tenant.ID, tenant.Email)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	// Build 2MB multipart payload
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "video.mp4")
	if err != nil {
		t.Fatalf("CreateFormFile failed: %v", err)
	}
	payload := bytes.Repeat([]byte{0}, 2*1024*1024)
	if _, err := fw.Write(payload); err != nil {
		t.Fatalf("write payload failed: %v", err)
	}
	mw.Close()

	req := httptest.NewRequest("POST", "/tenant/media", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: tenantapi.SessionCookie, Value: token})
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on storage exceed, got %d", rr.Code)
	}

	// Ensure no media record created
	var assets []storage.MediaAsset
	if err := db.Where("tenant_id = ?", tenant.ID).Find(&assets).Error; err != nil {
		t.Fatalf("db query failed: %v", err)
	}
	if len(assets) != 0 {
		t.Fatalf("expected no assets created, found %d", len(assets))
	}

	// Ensure storage usage not increased beyond limit
	qData, err := qs.GetQuotaUsage(context.Background(), tenant.ID)
	if err != nil {
		t.Fatalf("failed to get quota: %v", err)
	}
	if qData.StorageUsedMB > qData.StorageLimitMB {
		t.Fatalf("storage used exceeds limit: used=%d limit=%d", qData.StorageUsedMB, qData.StorageLimitMB)
	}
}

// Helper: create tenant JWT token
func middlewareCreateToken(cfg *config.Config, tenantID uint, email string) (string, error) {
	// Use the same secret as TenantAuth
	secret := []byte(cfg.TenantJWTSecret)
	return middleware.CreateTenantToken(secret, tenantID, email, time.Hour)
}
