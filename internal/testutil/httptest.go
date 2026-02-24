package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// TestRequest creates an HTTP request for testing
func TestRequest(t *testing.T, method, url string, body interface{}) *http.Request {
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("Failed to marshal request body: %v", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req := httptest.NewRequest(method, url, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

// TestRequestWithContext creates an HTTP request with context
func TestRequestWithContext(t *testing.T, method, url string, body interface{}, ctx context.Context) *http.Request {
	req := TestRequest(t, method, url, body)
	return req.WithContext(ctx)
}

// TestRequestWithTenant creates an HTTP request with tenant context
func TestRequestWithTenant(t *testing.T, method, url string, body interface{}, tenantID uuid.UUID, email string) *http.Request {
	req := TestRequest(t, method, url, body)
	claims := &middleware.TenantClaims{
		TenantID: tenantID,
		Email:    email,
	}
	ctx := middleware.WithTenantContext(req.Context(), claims)
	return req.WithContext(ctx)
}

// TestResponseRecorder wraps httptest.ResponseRecorder with helper methods
type TestResponseRecorder struct {
	*httptest.ResponseRecorder
}

// NewTestResponseRecorder creates a new test response recorder
func NewTestResponseRecorder() *TestResponseRecorder {
	return &TestResponseRecorder{
		ResponseRecorder: httptest.NewRecorder(),
	}
}

// BodyString returns the response body as a string
func (rr *TestResponseRecorder) BodyString() string {
	return rr.Body.String()
}

// BodyJSON unmarshals the response body as JSON
func (rr *TestResponseRecorder) BodyJSON(t *testing.T, v interface{}) {
	if err := json.Unmarshal(rr.Body.Bytes(), v); err != nil {
		t.Fatalf("Failed to unmarshal response body: %v\nBody: %s", err, rr.Body.String())
	}
}

// AssertStatus asserts the response status code
func (rr *TestResponseRecorder) AssertStatus(t *testing.T, expected int) {
	if rr.Code != expected {
		t.Errorf("Expected status %d, got %d\nBody: %s", expected, rr.Code, rr.Body.String())
	}
}

// AssertJSON asserts the response is JSON and matches expected structure
func (rr *TestResponseRecorder) AssertJSON(t *testing.T, expected interface{}) {
	var actual map[string]interface{}
	var expectedMap map[string]interface{}

	rr.BodyJSON(t, &actual)

	expectedBytes, err := json.Marshal(expected)
	if err != nil {
		t.Fatalf("Failed to marshal expected: %v", err)
	}
	if err := json.Unmarshal(expectedBytes, &expectedMap); err != nil {
		t.Fatalf("Failed to unmarshal expected: %v", err)
	}

	// Simple comparison - can be enhanced
	if len(actual) != len(expectedMap) {
		t.Errorf("JSON structure mismatch. Expected %d keys, got %d", len(expectedMap), len(actual))
	}
}

// CreateTestTenant creates a test tenant in the database
func CreateTestTenant(t *testing.T, db *gorm.DB, email, password, name string) *storage.Tenant {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	tenant := &storage.Tenant{
		Email:        email,
		PasswordHash: string(hash),
		Name:         name,
		Status:       "active",
	}

	if err := db.Create(tenant).Error; err != nil {
		t.Fatalf("Failed to create test tenant: %v", err)
	}

	return tenant
}

// CreateTestMediaAsset creates a test media asset
func CreateTestMediaAsset(t *testing.T, db *gorm.DB, tenantID uuid.UUID, name, url string) *storage.MediaAsset {
	urlsJSON, _ := json.Marshal([]string{url})
	asset := &storage.MediaAsset{
		TenantID: tenantID,
		Name:     name,
		URLs:     string(urlsJSON),
		Type:     "image",
		Status:   "uploaded",
	}

	if err := db.Create(asset).Error; err != nil {
		t.Fatalf("Failed to create test media asset: %v", err)
	}

	return asset
}

// CreateTestScheduledPost creates a test scheduled post
func CreateTestScheduledPost(t *testing.T, db *gorm.DB, tenantID uuid.UUID, caption string, platforms []string, mediaIDs []uuid.UUID) *storage.ScheduledPost {
	platformsJSON, _ := json.Marshal(platforms)
	mediaIDsJSON, _ := json.Marshal(mediaIDs)

	post := &storage.ScheduledPost{
		TenantID:       tenantID,
		Caption:        caption,
		Platforms:      string(platformsJSON),
		MediaAssetIDs:  string(mediaIDsJSON),
		Status:         "scheduled",
		EnhanceCaption: false,
	}

	if err := db.Create(post).Error; err != nil {
		t.Fatalf("Failed to create test scheduled post: %v", err)
	}

	return post
}

// SetupTestRouter creates a chi router for testing
func SetupTestRouter() *chi.Mux {
	return chi.NewRouter()
}
