package admin

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
)

// RequireActiveAdminCase enforces that a valid, assigned, investigating admin case is present for sensitive actions.
func RequireActiveAdminCase(ctx *AdminContext) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			caseID := r.URL.Query().Get("case_id")
			if caseID == "" {
				caseID = r.Header.Get("X-Admin-Case-ID")
			}
			if caseID == "" {
				writeAdminError(w, r, http.StatusForbidden, "case_required", "forbidden", map[string]any{
					"hint": "provide case_id query param or X-Admin-Case-ID header",
				})
				return
			}
			id, err := uuid.Parse(caseID)
			if err != nil {
				writeAdminError(w, r, http.StatusForbidden, "case_invalid", "forbidden", nil)
				return
			}
			var adminCase storage.AdminCase
			if err := ctx.DB.First(&adminCase, id).Error; err != nil {
				writeAdminError(w, r, http.StatusForbidden, "case_not_found", "forbidden", nil)
				return
			}
			if adminCase.Status != "investigating" {
				writeAdminError(w, r, http.StatusForbidden, "case_not_active", "forbidden", map[string]any{
					"case_status": adminCase.Status,
				})
				return
			}
			adminEmail := AdminEmailFromRequest(r)
			if adminCase.AssignedTo == nil || *adminCase.AssignedTo != adminEmail {
				writeAdminError(w, r, http.StatusForbidden, "case_not_assigned", "forbidden", nil)
				return
			}
			// Inject case into context
			r = r.WithContext(context.WithValue(r.Context(), adminCaseKey, &adminCase))

			// Audit log: log every access (fail closed). Use strict writer so audit errors abort request.
			ae := AdminAuditEntry{
				AdminEmail: adminEmail,
				AdminRole:  AdminRoleFromRequest(r),
				Action:     "case.gate.access",
				TenantID:   &adminCase.TenantID,
				CaseID:     &adminCase.ID,
				Metadata: map[string]interface{}{
					"case_id":     adminCase.ID,
					"tenant_id":   adminCase.TenantID,
					"admin_email": adminEmail,
					"role":        AdminRoleFromRequest(r),
					"action":      "case.gate.access",
				},
			}
			if err := auditWriter(r.Context(), ctx, ae); err != nil {
				writeAdminError(w, r, http.StatusInternalServerError, "audit_failed", "audit log failed", nil)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
