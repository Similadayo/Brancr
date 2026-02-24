package tenantapi

import (
	"encoding/json"
	"net/http"

	"github.com/similadayo/brancr/internal/middleware"
	"go.uber.org/zap"
)

// parseProducts handles POST /tenant/products/parse
// Request: { raw_input: string, source: "paste"|"file"|"image" }
func (h *Handler) parseProducts(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var body struct {
		RawInput string `json:"raw_input"`
		Source   string `json:"source"`
		Industry string `json:"industry"` // Optional context
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.Logger.Warn("parse-products-invalid-body", zap.Error(err))
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	// If source indicates a file, reject here - file parsing handled by /parse/file
	if body.Source == "file" || body.Source == "image" {
		http.Error(w, "use /tenant/products/parse/file or /tenant/products/parse/image for file/image inputs", http.StatusBadRequest)
		return
	}
	// Protect against extremely large inputs
	if len(body.RawInput) > 64*1024 { // 64KB max for paste
		h.Logger.Warn("parse-input-too-large", zap.Stringer("tenant_id", claims.TenantID), zap.Int("length", len(body.RawInput)))
		http.Error(w, "input too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Choose parser: force AI parser for almost all inputs if client is available
	var items []ParsedItem
	var err error
	usedAI := false
	if h.AIClient != nil && len(body.RawInput) > 10 { // lowered threshold to ensure AI handles "scattered" text
		usedAI = true
		aiParser := NewAIParser(h.AIClient)
		items, err = aiParser.Parse(body.RawInput, body.Industry)
		if err != nil {
			h.Logger.Warn("ai-parse-failed", zap.Error(err), zap.Stringer("tenant_id", claims.TenantID))
			// fallback to default parser
			parser := NewDefaultParser()
			items, err = parser.Parse(body.RawInput)
			if err != nil {
				h.Logger.Warn("parse-products-failed", zap.Error(err), zap.Stringer("tenant_id", claims.TenantID))
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to parse input"})
				return
			}
		}
	} else {
		parser := NewDefaultParser()
		items, err = parser.Parse(body.RawInput)
		if err != nil {
			h.Logger.Warn("parse-products-failed", zap.Error(err), zap.Stringer("tenant_id", claims.TenantID))
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to parse input"})
			return
		}
	}

	// Enforce max items limit
	if len(items) > 1000 {
		h.Logger.Warn("parse-too-many-items", zap.Stringer("tenant_id", claims.TenantID), zap.Int("items", len(items)))
		http.Error(w, "too many parsed items; try uploading a file instead", http.StatusRequestEntityTooLarge)
		return
	}

	// Metrics
	if h.Metrics != nil {
		h.Metrics.HTTPRequests.Inc()
		if usedAI {
			h.Metrics.AIRequests.Inc()
		}
	}

	writeJSON(w, http.StatusOK, items)
}

// helper used for tests that need to call handler directly
func (h *Handler) ParseProductsForTest(raw string) ([]ParsedItem, error) {
	p := NewDefaultParser()
	return p.Parse(raw)
}
