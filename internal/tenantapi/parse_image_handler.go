package tenantapi

import (
	"bytes"
	"io"
	"net/http"

	"github.com/similadayo/brancr/internal/middleware"
	"go.uber.org/zap"
)

// parseImage handles POST /tenant/products/parse/image
func (h *Handler) parseImage(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if h.OCRService == nil {
		h.Logger.Warn("ocr-not-configured", zap.Stringer("tenant_id", claims.TenantID))
		http.Error(w, "ocr service not configured", http.StatusServiceUnavailable)
		return
	}

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		h.Logger.Warn("parse-image-invalid-multipart", zap.Error(err))
		http.Error(w, "invalid multipart request", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "image is required", http.StatusBadRequest)
		return
	}
	defer file.Close()
	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, file); err != nil {
		h.Logger.Warn("image-read-failed", zap.Error(err))
		http.Error(w, "failed to read image", http.StatusInternalServerError)
		return
	}
	text, err := h.OCRService.ExtractText(r.Context(), buf.Bytes())
	if err != nil {
		h.Logger.Warn("ocr-extract-failed", zap.Error(err))
		http.Error(w, "failed to OCR image", http.StatusInternalServerError)
		return
	}
	// parse OCR text
	parser := NewDefaultParser()
	items, err := parser.Parse(text)
	if err != nil {
		h.Logger.Warn("parse-image-failed", zap.Error(err))
		http.Error(w, "failed to parse ocr text", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ocr_text": text, "items": items})
}
