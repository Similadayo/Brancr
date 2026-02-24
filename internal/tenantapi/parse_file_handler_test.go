package tenantapi

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/similadayo/brancr/internal/testutil"
)

func TestParseFile_CSV(t *testing.T) {
	h, db := setupTestHandler(t)
	defer testutil.CleanupDB(t, db)

	tenant := testutil.CreateTestTenant(t, db, "csv@example.com", "pass", "CSV Tenant")

	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	fw, _ := w.CreateFormFile("file", "menu.csv")
	fw.Write([]byte("Jollof Rice,₦3500\nFried Rice,4000"))
	w.Close()

	req := testutil.TestRequestWithTenant(t, "POST", "/tenant/products/parse/file", nil, tenant.ID, tenant.Email)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Body = io.NopCloser(bytes.NewReader(b.Bytes()))
	rr := testutil.NewTestResponseRecorder()

	h.parseFile(rr, req)

	rr.AssertStatus(t, http.StatusOK)
	var resp []ParsedItem
	rr.BodyJSON(t, &resp)
	if len(resp) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resp))
	}
}
