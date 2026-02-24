package tenantapi

import (
	"strings"
	"testing"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

func setupHandlerWithDB(t *testing.T) *Handler {
	db := testutil.TestDB(t)
	if err := db.AutoMigrate(&storage.Draft{}, &storage.Tenant{}); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	logger := testutil.TestLogger(t)
	h := &Handler{DB: db, Logger: logger}
	return h
}

func TestDrafts_CreateAndList(t *testing.T) {
	h := setupHandlerWithDB(t)
	tenant := &storage.Tenant{Email: "a@b.com", Name: "Tenant1"}
	if err := h.DB.Create(tenant).Error; err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	reqBody := map[string]interface{}{"key": "compose.post", "content": map[string]interface{}{"text": "hello"}}
	req := testutil.TestRequestWithTenant(t, "POST", "/tenant/drafts", reqBody, tenant.ID, tenant.Email)
	rr := testutil.NewTestResponseRecorder()

	h.createDraft(rr, req)
	rr.AssertStatus(t, 201)
	var created storage.Draft
	rr.BodyJSON(t, &created)
	if created.Key != "compose.post" {
		t.Fatalf("unexpected key: %v", created.Key)
	}

	// Update by POSTing same key (should upsert)
	req2 := testutil.TestRequestWithTenant(t, "POST", "/tenant/drafts", map[string]interface{}{"key": "compose.post", "content": map[string]interface{}{"text": "updated"}}, tenant.ID, tenant.Email)
	rr2 := testutil.NewTestResponseRecorder()
	h.createDraft(rr2, req2)
	rr2.AssertStatus(t, 200) // upsert returns 200
	var updated storage.Draft
	rr2.BodyJSON(t, &updated)
	if updated.Content == created.Content {
		t.Fatalf("expected content updated, got same content")
	}

	// List
	listReq := testutil.TestRequestWithTenant(t, "GET", "/tenant/drafts?key=compose.post", nil, tenant.ID, tenant.Email)
	lr := testutil.NewTestResponseRecorder()
	h.listDrafts(lr, listReq)
	lr.AssertStatus(t, 200)
	var ds []storage.Draft
	lr.BodyJSON(t, &ds)
	if len(ds) == 0 {
		t.Fatalf("expected drafts, got none")
	}
}

func TestDrafts_TableMissing_ReturnsHelpfulError(t *testing.T) {
	// Create DB but do not migrate drafts table
	db := testutil.TestDB(t)
	// Migrate tenant only
	if err := db.AutoMigrate(&storage.Tenant{}); err != nil {
		t.Fatalf("migrate tenant failed: %v", err)
	}
	logger := testutil.TestLogger(t)
	h := &Handler{DB: db, Logger: logger}

	tenant := &storage.Tenant{Email: "missing@d.com", Name: "NoDrafts"}
	if err := h.DB.Create(tenant).Error; err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	reqBody := map[string]interface{}{"key": "compose.post", "content": map[string]interface{}{"text": "hello"}}
	req := testutil.TestRequestWithTenant(t, "POST", "/tenant/drafts", reqBody, tenant.ID, tenant.Email)
	rr := testutil.NewTestResponseRecorder()

	h.createDraft(rr, req)
	rr.AssertStatus(t, 500)
	if !strings.Contains(rr.BodyString(), "migrations") {
		t.Fatalf("expected error message to mention migrations, got: %s", rr.BodyString())
	}

	// List should also return 500
	lr := testutil.NewTestResponseRecorder()
	listReq := testutil.TestRequestWithTenant(t, "GET", "/tenant/drafts", nil, tenant.ID, tenant.Email)
	h.listDrafts(lr, listReq)
	lr.AssertStatus(t, 500)
	if !strings.Contains(lr.BodyString(), "migrations") {
		t.Fatalf("expected error message to mention migrations, got: %s", lr.BodyString())
	}
}
