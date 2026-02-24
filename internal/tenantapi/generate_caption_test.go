package tenantapi

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

func TestGenerateCaption_NoMedia_Facebook(t *testing.T) {
	h, db := setupTestHandler(t)
	defer testutil.CleanupDB(t, db)

	tenant := testutil.CreateTestTenant(t, db, "fb@example.com", "pass", "FB Tenant")

	req := testutil.TestRequestWithTenant(t, "POST", "/tenant/posts/generate-caption", map[string]interface{}{"platform": "facebook"}, tenant.ID, tenant.Email)
	rr := testutil.NewTestResponseRecorder()

	h.generateCaption(rr, req)

	rr.AssertStatus(t, http.StatusOK)
	var resp map[string]interface{}
	rr.BodyJSON(t, &resp)
	if resp["caption"] == nil {
		t.Fatalf("expected caption in response")
	}
	if resp["media_type"] != "text" {
		t.Fatalf("expected media_type text for no media, got %v", resp["media_type"])
	}
}

func TestGenerateCaption_MultipleMedia_Carousel(t *testing.T) {
	h, db := setupTestHandler(t)
	defer testutil.CleanupDB(t, db)

	tenant := testutil.CreateTestTenant(t, db, "multi@example.com", "pass", "Multi Tenant")

	// Create two media assets
	asset1 := storage.MediaAsset{TenantID: tenant.ID, Type: "image", URLs: "[\"https://example.com/img1.jpg\"]"}
	asset2 := storage.MediaAsset{TenantID: tenant.ID, Type: "image", URLs: "[\"https://example.com/img2.jpg\"]"}
	if err := h.DB.Create(&asset1).Error; err != nil {
		t.Fatalf("create asset1: %v", err)
	}
	if err := h.DB.Create(&asset2).Error; err != nil {
		t.Fatalf("create asset2: %v", err)
	}

	req := testutil.TestRequestWithTenant(t, "POST", "/tenant/posts/generate-caption", map[string]interface{}{"media_ids": []uuid.UUID{asset1.ID, asset2.ID}, "platform": "instagram"}, tenant.ID, tenant.Email)
	rr := testutil.NewTestResponseRecorder()

	h.generateCaption(rr, req)

	rr.AssertStatus(t, http.StatusOK)
	var resp map[string]interface{}
	rr.BodyJSON(t, &resp)
	if resp["caption"] == nil {
		t.Fatalf("expected caption in response")
	}
	if resp["media_type"] != "carousel" {
		t.Fatalf("expected media_type carousel for multiple images, got %v", resp["media_type"])
	}
}
