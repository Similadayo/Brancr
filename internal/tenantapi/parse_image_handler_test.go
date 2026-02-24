package tenantapi

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/similadayo/brancr/internal/testutil"
)

// fake OCR for tests
type fakeOCR struct{}

func (f fakeOCR) ExtractText(ctx context.Context, imageBytes []byte) (string, error) {
	return "Jollof Rice - ₦3,500", nil
}

func TestParseImage_Handler(t *testing.T) {
	h, db := setupTestHandler(t)
	defer testutil.CleanupDB(t, db)

	tenant := testutil.CreateTestTenant(t, db, "ocr@example.com", "pass", "OCR Tenant")

	// Attach fake OCR
	h.OCRService = fakeOCR{}

	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	fw, _ := w.CreateFormFile("image", "menu.png")
	fw.Write([]byte("fake-image-bytes"))
	w.Close()

	req := testutil.TestRequestWithTenant(t, "POST", "/tenant/products/parse/image", nil, tenant.ID, tenant.Email)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Body = io.NopCloser(bytes.NewReader(b.Bytes()))
	rr := testutil.NewTestResponseRecorder()

	h.parseImage(rr, req)

	rr.AssertStatus(t, http.StatusOK)
	var resp map[string]interface{}
	rr.BodyJSON(t, &resp)
	if resp["ocr_text"] == nil {
		t.Fatalf("expected ocr_text in response")
	}
}
