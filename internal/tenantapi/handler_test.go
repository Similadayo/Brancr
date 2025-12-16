package tenantapi

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"fmt"

	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/quota"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
	"gorm.io/gorm"
)

// setupTestHandler creates a test handler with test dependencies
func setupTestHandler(t *testing.T) (*Handler, *gorm.DB) {
	db := testutil.TestDB(t)
	logger := testutil.TestLogger(t)
	cfg := testutil.TestConfigWithJWT()
	redis := testutil.TestRedis(t) // May be nil

	// Create minimal handler for testing
	handler := &Handler{
		DB:     db,
		Logger: logger,
		Config: cfg,
		Redis:  redis,
	}

	// Run migrations for test models
	if err := db.AutoMigrate(
		&storage.Tenant{},
		&storage.MediaAsset{},
		&storage.ScheduledPost{},
		&storage.SocialAccount{},
	); err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}

	return handler, db
}

func TestHandler_Signup(t *testing.T) {
	handler, db := setupTestHandler(t)
	defer testutil.CleanupDB(t, db)

	tests := []struct {
		name           string
		requestBody    map[string]interface{}
		expectedStatus int
		checkResponse  func(t *testing.T, rr *testutil.TestResponseRecorder)
	}{
		{
			name: "successful signup",
			requestBody: map[string]interface{}{
				"email":        "newuser@example.com",
				"password":     "securePassword123",
				"name":         "Test User",
				"company_name": "Test Company",
			},
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, rr *testutil.TestResponseRecorder) {
				var response map[string]interface{}
				rr.BodyJSON(t, &response)

				if response["tenant_id"] == nil {
					t.Error("Expected tenant_id in response")
				}
				if response["message"] == nil {
					t.Error("Expected message in response")
				}
			},
		},
		{
			name: "missing required fields",
			requestBody: map[string]interface{}{
				"email": "test@example.com",
				// Missing password and company_name
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "duplicate email",
			requestBody: map[string]interface{}{
				"email":        "duplicate@example.com",
				"password":     "password123",
				"company_name": "Company",
			},
			expectedStatus: http.StatusConflict,
			checkResponse: func(t *testing.T, rr *testutil.TestResponseRecorder) {
				// Create first user
				req1 := testutil.TestRequest(t, "POST", "/auth/signup", map[string]interface{}{
					"email":        "duplicate@example.com",
					"password":     "password123",
					"company_name": "Company",
				})
				rr1 := testutil.NewTestResponseRecorder()
				handler.signup(rr1, req1)

				// Try to create duplicate
				req2 := testutil.TestRequest(t, "POST", "/auth/signup", map[string]interface{}{
					"email":        "duplicate@example.com",
					"password":     "password123",
					"company_name": "Company",
				})
				rr2 := testutil.NewTestResponseRecorder()
				handler.signup(rr2, req2)

				rr2.AssertStatus(t, http.StatusConflict)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := testutil.TestRequest(t, "POST", "/auth/signup", tt.requestBody)
			rr := testutil.NewTestResponseRecorder()

			handler.signup(rr, req)

			rr.AssertStatus(t, tt.expectedStatus)

			if tt.checkResponse != nil {
				tt.checkResponse(t, rr)
			}
		})
	}
}

func TestHandler_Login(t *testing.T) {
	handler, db := setupTestHandler(t)
	defer testutil.CleanupDB(t, db)

	// Create a test tenant
	_ = testutil.CreateTestTenant(t, db, "test@example.com", "password123", "Test User")

	tests := []struct {
		name           string
		requestBody    map[string]interface{}
		expectedStatus int
		checkResponse  func(t *testing.T, rr *testutil.TestResponseRecorder)
	}{
		{
			name: "successful login",
			requestBody: map[string]interface{}{
				"email":    "test@example.com",
				"password": "password123",
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, rr *testutil.TestResponseRecorder) {
				var response map[string]interface{}
				rr.BodyJSON(t, &response)

				if response["tenant_id"] == nil {
					t.Error("Expected tenant_id in response")
				}
				if got := response["email"]; got != "test@example.com" {
					t.Errorf("Expected email 'test@example.com', got %v", got)
				}

				// Check for session cookie
				cookies := rr.Header().Values("Set-Cookie")
				if len(cookies) == 0 {
					t.Error("Expected Set-Cookie header in response")
				}
			},
		},
		{
			name: "invalid credentials",
			requestBody: map[string]interface{}{
				"email":    "test@example.com",
				"password": "wrongpassword",
			},
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name: "non-existent user",
			requestBody: map[string]interface{}{
				"email":    "nonexistent@example.com",
				"password": "password123",
			},
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name: "missing email",
			requestBody: map[string]interface{}{
				"password": "password123",
			},
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := testutil.TestRequest(t, "POST", "/auth/login", tt.requestBody)
			rr := testutil.NewTestResponseRecorder()

			handler.login(rr, req)

			rr.AssertStatus(t, tt.expectedStatus)

			if tt.checkResponse != nil {
				tt.checkResponse(t, rr)
			}
		})
	}
}

func TestHandler_CreatePost(t *testing.T) {
	handler, db := setupTestHandler(t)
	defer testutil.CleanupDB(t, db)

	// Create test tenant
	tenant := testutil.CreateTestTenant(t, db, "test@example.com", "password123", "Test User")

	// Create test media assets
	media1 := testutil.CreateTestMediaAsset(t, db, tenant.ID, "Image 1", "https://example.com/image1.jpg")
	media2 := testutil.CreateTestMediaAsset(t, db, tenant.ID, "Image 2", "https://example.com/image2.jpg")

	tests := []struct {
		name           string
		requestBody    map[string]interface{}
		expectedStatus int
		checkResponse  func(t *testing.T, rr *testutil.TestResponseRecorder, db *gorm.DB)
	}{
		{
			name: "successful post creation",
			requestBody: map[string]interface{}{
				"media_ids":       []uint{media1.ID, media2.ID},
				"caption":         "Test post caption",
				"platforms":       []string{"instagram", "facebook"},
				"scheduled_at":    time.Now().Add(24 * time.Hour).Format(time.RFC3339),
				"name":            "Test Post",
				"enhance_caption": false,
			},
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, rr *testutil.TestResponseRecorder, db *gorm.DB) {
				var response map[string]interface{}
				rr.BodyJSON(t, &response)

				if response["id"] == nil {
					t.Error("Expected id in response")
				}
				if response["status"] == nil {
					t.Error("Expected status in response")
				}

				// Verify post was created in database
				var post storage.ScheduledPost
				if err := db.First(&post, response["id"]).Error; err != nil {
					t.Errorf("Post not found in database: %v", err)
				}

				if post.TenantID != tenant.ID {
					t.Errorf("Expected tenant_id %d, got %d", tenant.ID, post.TenantID)
				}

				if post.Caption != "Test post caption" {
					t.Errorf("Expected caption 'Test post caption', got '%s'", post.Caption)
				}

				if post.EnhanceCaption {
					t.Error("Expected EnhanceCaption to be false")
				}
			},
		},
		{
			name: "missing media_ids",
			requestBody: map[string]interface{}{
				"caption":   "Test post",
				"platforms": []string{"instagram"},
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "missing platforms",
			requestBody: map[string]interface{}{
				"media_ids": []uint{media1.ID},
				"caption":   "Test post",
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "invalid scheduled_at format",
			requestBody: map[string]interface{}{
				"media_ids":    []uint{media1.ID},
				"caption":      "Test post",
				"platforms":    []string{"instagram"},
				"scheduled_at": "invalid-date",
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "publish immediately (now)",
			requestBody: map[string]interface{}{
				"media_ids":       []uint{media1.ID},
				"caption":         "Test post",
				"platforms":       []string{"instagram"},
				"scheduled_at":    "now",
				"enhance_caption": true,
			},
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, rr *testutil.TestResponseRecorder, db *gorm.DB) {
				var response map[string]interface{}
				rr.BodyJSON(t, &response)

				if response["publishing_now"] != true {
					t.Error("Expected publishing_now to be true")
				}

				// Verify EnhanceCaption is true
				var post storage.ScheduledPost
				if err := db.First(&post, response["id"]).Error; err != nil {
					t.Errorf("Post not found in database: %v", err)
				}

				if !post.EnhanceCaption {
					t.Error("Expected EnhanceCaption to be true")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create request with tenant context
			req := testutil.TestRequestWithTenant(t, "POST", "/api/tenant/posts", tt.requestBody, tenant.ID, tenant.Email)
			rr := testutil.NewTestResponseRecorder()

			handler.createPost(rr, req)

			rr.AssertStatus(t, tt.expectedStatus)

			if tt.checkResponse != nil {
				tt.checkResponse(t, rr, db)
			}
		})
	}
}

func TestHandler_CreatePost_Unauthorized(t *testing.T) {
	handler, db := setupTestHandler(t)
	defer testutil.CleanupDB(t, db)

	req := testutil.TestRequest(t, "POST", "/api/tenant/posts", map[string]interface{}{
		"media_ids": []uint{1},
		"platforms": []string{"instagram"},
	})
	rr := testutil.NewTestResponseRecorder()

	handler.createPost(rr, req)

	rr.AssertStatus(t, http.StatusUnauthorized)
}

func TestHandler_UpdateAIModeAndBlockAI(t *testing.T) {
	handler, db := setupTestHandler(t)
	defer testutil.CleanupDB(t, db)

	tenant := testutil.CreateTestTenant(t, db, "aiuser@example.com", "password123", "AI User")

	// Update AI mode to 'human'
	req := testutil.TestRequestWithTenant(t, "PUT", "/api/tenant/settings/ai-mode", map[string]interface{}{"mode": "human"}, tenant.ID, tenant.Email)
	rr := testutil.NewTestResponseRecorder()
	handler.updateAIModeSettings(rr, req)
	rr.AssertStatus(t, http.StatusOK)

	// Verify in DB
	var updated storage.Tenant
	if err := db.First(&updated, tenant.ID).Error; err != nil {
		t.Fatalf("failed to fetch tenant: %v", err)
	}
	if updated.AIHumanMode != "human" {
		t.Fatalf("expected AIHumanMode 'human', got '%s'", updated.AIHumanMode)
	}

	// Now attempt to generate a caption - should be rejected with 403 (ai_disabled)
	genReq := testutil.TestRequestWithTenant(t, "POST", "/api/tenant/posts/generate-caption", map[string]interface{}{"platform": "instagram"}, tenant.ID, tenant.Email)
	genRR := testutil.NewTestResponseRecorder()
	handler.generateCaption(genRR, genReq)
	genRR.AssertStatus(t, http.StatusForbidden)

	var resp map[string]interface{}
	genRR.BodyJSON(t, &resp)
	if resp["error"] != "ai_disabled" {
		t.Fatalf("expected ai_disabled error, got %v", resp)
	}
}

func TestHandler_UpdateInboxAIMode(t *testing.T) {
	handler, db := setupTestHandler(t)
	defer testutil.CleanupDB(t, db)

	tenant := testutil.CreateTestTenant(t, db, "inboxai@example.com", "password123", "Inbox AI Tenant")

	// Create a social account (inbox)
	acct := storage.SocialAccount{TenantID: tenant.ID, Platform: "whatsapp", Username: "inbox1", Connected: true}
	if err := db.Create(&acct).Error; err != nil {
		t.Fatalf("failed to create social account: %v", err)
	}

	// Update per-inbox AI mode to 'human'
	path := fmt.Sprintf("/api/tenant/settings/inboxes/%d/ai-mode", acct.ID)
	req := testutil.TestRequestWithTenant(t, "PUT", path, map[string]interface{}{"mode": "human"}, tenant.ID, tenant.Email)
	rr := testutil.NewTestResponseRecorder()
	handler.updateInboxAIMode(rr, req)
	rr.AssertStatus(t, http.StatusOK)

	var updated storage.SocialAccount
	if err := db.First(&updated, acct.ID).Error; err != nil {
		t.Fatalf("failed to fetch social account: %v", err)
	}
	if updated.AiMode != "human" {
		t.Fatalf("expected AiMode 'human', got '%s'", updated.AiMode)
	}
}

func TestStorageQuota_EnforcedOnUpload_NoSideEffects(t *testing.T) {
	handler, db := setupTestHandler(t)
	defer testutil.CleanupDB(t, db)

	tenant := testutil.CreateTestTenant(t, db, "storage@example.com", "password123", "Storage Tenant")

	// Set storage limit to 1 MB
	qs := quota.NewQuotaService(db, testutil.TestLogger(t), nil)
	if err := qs.UpdateQuotaLimits(context.Background(), tenant.ID, map[string]int{"storage_limit_mb": 1}, ""); err != nil {
		t.Fatalf("failed to set storage limit: %v", err)
	}

	// Build 2MB multipart upload
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "video.mp4")
	if err != nil {
		t.Fatalf("create form file failed: %v", err)
	}
	// 2MB of zeros
	payload := bytes.Repeat([]byte{0}, 2*1024*1024)
	if _, err := fw.Write(payload); err != nil {
		t.Fatalf("write payload failed: %v", err)
	}
	mw.Close()

	req := httptest.NewRequest("POST", "/api/tenant/media", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	// Attach tenant claims
	req = req.WithContext(middleware.WithTenantContext(req.Context(), &middleware.TenantClaims{TenantID: tenant.ID, Email: tenant.Email}))

	rr := testutil.NewTestResponseRecorder()
	handler.createMedia(rr, req)
	rr.AssertStatus(t, http.StatusTooManyRequests)

	// Ensure no media asset was created
	var assets []storage.MediaAsset
	if err := db.Where("tenant_id = ?", tenant.ID).Find(&assets).Error; err != nil {
		t.Fatalf("db query failed: %v", err)
	}
	if len(assets) != 0 {
		t.Fatalf("expected no media assets created, found %d", len(assets))
	}

	// Metric increment check
	if handler.Metrics != nil && handler.Metrics.QuotaRejections.Value() == 0 {
		t.Fatalf("expected quota rejections to be incremented on storage rejection")
	}
}
