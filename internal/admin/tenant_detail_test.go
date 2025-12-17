package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

func TestTenantDetail_ScheduledPosts_NoContent(t *testing.T) {
	db := testutil.TestDB(t)
	ctx := &AdminContext{DB: db}

	tenant := storage.Tenant{Name: "T", Email: "t@example.com"}
	_ = db.Create(&tenant).Error

	now := time.Now().Add(24 * time.Hour)
	sp := storage.ScheduledPost{TenantID: tenant.ID, Caption: "SECRET_CAPTION_42", Platforms: "[\"instagram\"]", ScheduledAt: &now, Status: "scheduled"}
	_ = db.Create(&sp).Error

	req := httptest.NewRequest("GET", "/admin/tenants/1", nil)
	w := httptest.NewRecorder()

	// Put admin role/email into context so handler won't reject
	req = req.WithContext(context.WithValue(req.Context(), adminEmailKey, "admin@example.com"))
	req = req.WithContext(context.WithValue(req.Context(), adminRoleKey, RoleSuperAdmin))

	handler := TenantDetailHandler(ctx)
	handler(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}

	body := w.Body.String()
	if strings.Contains(body, "SECRET_CAPTION_42") {
		t.Fatalf("expected caption NOT to be present in tenant page HTML")
	}
}
