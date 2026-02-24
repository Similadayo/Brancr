package tenantapi

import (
	"encoding/json"
	"net/http"

	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// @Summary      Submit KYB
// @Description  Submit KYB data for tenant onboarding
// @Tags         onboarding kyb
// @Accept       json
// @Produce      json
// @Param        payload body object true "KYB payload"
// @Success      200 {object} map[string]bool
// @Security     BearerAuth
// @Router       /tenant/onboarding/kyb [post]
func (h *Handler) submitKYB(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	// Save to tenants table
	if err := h.DB.Model(&storage.Tenant{}).Where("id = ?", claims.TenantID).Updates(map[string]interface{}{
		"kyb_status":       "pending",
		"kyb_data":         string(req.Data),
		"kyb_submitted_at": gorm.Expr("now()"),
	}).Error; err != nil {
		h.Logger.Error("failed to submit kyb", zap.Error(err))
		http.Error(w, "failed to submit kyb", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
