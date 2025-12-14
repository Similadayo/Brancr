package admin

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/similadayo/brancr/internal/storage"
)

// EscalationContentHandler returns full escalation content if case-gated and permissioned
func EscalationContentHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// RequirePermission("content.read") and RequireActiveAdminCase must be applied at router
		caseObj := AdminCaseFromRequest(r)
		if caseObj == nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		idStr := chi.URLParam(r, "id")
		id64, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil || id64 == 0 {
			http.Error(w, "invalid escalation id", http.StatusBadRequest)
			return
		}
		var interaction storage.Interaction
		if err := ctx.DB.First(&interaction, id64).Error; err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if caseObj.TenantID != interaction.TenantID {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		// Audit enforcement: only on first access per case+escalation
		adminEmail := AdminEmailFromRequest(r)
		role := AdminRoleFromRequest(r)
		ae := AdminAuditEntry{
			AdminEmail: adminEmail,
			AdminRole:  role,
			Action:     "escalation.content.read",
			TenantID:   &interaction.TenantID,
			CaseID:     &caseObj.ID,
			Metadata: map[string]interface{}{
				"case_id":       caseObj.ID,
				"escalation_id": interaction.ID,
				"tenant_id":     interaction.TenantID,
				"admin_email":   adminEmail,
				"role":          role,
				"action":        "escalation.content.read",
			},
		}
		if err := auditWriter(r.Context(), ctx, ae); err != nil {
			http.Error(w, "audit log failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(interaction)
	}
}
