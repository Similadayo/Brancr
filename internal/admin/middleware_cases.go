package admin

import (
	"context"
	"net/http"
	"strconv"

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
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			id, err := strconv.ParseUint(caseID, 10, 64)
			if err != nil {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			var adminCase storage.AdminCase
			if err := ctx.DB.First(&adminCase, id).Error; err != nil {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if adminCase.Status != "investigating" {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			adminEmail := AdminEmailFromRequest(r)
			if adminCase.AssignedTo == nil || *adminCase.AssignedTo != adminEmail {
				http.Error(w, "forbidden", http.StatusForbidden)
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
				http.Error(w, "audit log failed", http.StatusInternalServerError)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
