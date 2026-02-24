package admin

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func adminWriteJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// @Summary      Get tenant KYB
// @Description  Retrieve KYB submission for a tenant
// @Tags         admin kyb
// @Produce      json
// @Param        id   path  int  true  "Tenant ID"
// @Success      200 {object} object
// @Security     BearerAuth
// @Router       /admin/api/tenants/{id}/kyb [get]
func AdminGetTenantKYBHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := uuid.Parse(idStr)
		if err != nil || id == uuid.Nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		var t storage.Tenant
		if err := ctx.DB.First(&t, id).Error; err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tenant_id":                t.ID,
			"kyb_status":               t.KYBStatus,
			"kyb_data":                 t.KYBData,
			"kyb_submitted_at":         t.KYBSubmittedAt,
			"kyb_verified_at":          t.KYBVerifiedAt,
			"kyb_verified_by_admin_id": t.KYBVerifiedByAdminID,
		})
	}
}

// @Summary      Verify tenant KYB
// @Description  Mark a tenant's KYB as verified
// @Tags         admin kyb
// @Produce      json
// @Param        id   path  int  true  "Tenant ID"
// @Success      200 {object} map[string]bool
// @Security     BearerAuth
// @Router       /admin/api/tenants/{id}/kyb/verify [post]
func AdminVerifyTenantKYBHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := uuid.Parse(idStr)
		if err != nil || id == uuid.Nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		adminEmail := AdminEmailFromRequest(r)
		if err := ctx.DB.Model(&storage.Tenant{}).Where("id = ?", id).Updates(map[string]interface{}{
			"kyb_status":               "verified",
			"kyb_verified_at":          gorm.Expr("now()"),
			"kyb_verified_by_admin_id": nil,
		}).Error; err != nil {
			ctx.Logger.Error("failed to verify kyb", zap.Error(err))
			http.Error(w, "failed to verify", http.StatusInternalServerError)
			return
		}
		ae := AdminAuditEntry{
			AdminEmail: adminEmail,
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.kyb.verify",
			TenantID:   &id,
		}
		if err := auditWriter(r.Context(), ctx, ae); err != nil {
			ctx.Logger.Warn("audit failed", zap.Error(err))
		}
		adminWriteJSON(w, http.StatusOK, map[string]any{"success": true})
	}
}

// @Summary      Reject tenant KYB
// @Description  Mark a tenant's KYB as rejected
// @Tags         admin kyb
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Tenant ID"
// @Param        payload body object true "Reject payload"
// @Success      200 {object} map[string]bool
// @Security     BearerAuth
// @Router       /admin/api/tenants/{id}/kyb/reject [post]
func AdminRejectTenantKYBHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := uuid.Parse(idStr)
		if err != nil || id == uuid.Nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		var req struct {
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := ctx.DB.Model(&storage.Tenant{}).Where("id = ?", id).Updates(map[string]interface{}{
			"kyb_status": "rejected",
			"kyb_data":   gorm.Expr("jsonb_set(kyb_data, '{rejection_reason}', to_jsonb(?::text))", req.Reason),
		}).Error; err != nil {
			ctx.Logger.Error("failed to reject kyb", zap.Error(err))
			http.Error(w, "failed to reject", http.StatusInternalServerError)
			return
		}
		ae := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.kyb.reject",
			TenantID:   &id,
			Metadata:   map[string]any{"reason": req.Reason},
		}
		if err := auditWriter(r.Context(), ctx, ae); err != nil {
			ctx.Logger.Warn("audit failed", zap.Error(err))
		}
		adminWriteJSON(w, http.StatusOK, map[string]any{"success": true})
	}
}
