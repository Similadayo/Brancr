package tenantapi

import (
	"net/http"
	"testing"
	"time"

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
