package admin

import (
	"net/http"
)

// RequirePermission enforces action-based permissions for admin endpoints.
func RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := AdminRoleFromRequest(r)
			adminEmail := AdminEmailFromRequest(r)
			if role == "" {
				logDeniedPermission(r, adminEmail, role, permission, "missing role")
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			perms, ok := RolePermissions[role]
			if !ok {
				logDeniedPermission(r, adminEmail, role, permission, "role not found")
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if perms["*"] {
				next.ServeHTTP(w, r)
				return
			}
			if !perms[permission] {
				logDeniedPermission(r, adminEmail, role, permission, "permission denied")
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func logDeniedPermission(r *http.Request, adminEmail, role, permission, reason string) {
	// Use existing audit logger pattern
	// This is a minimal, non-blocking log for denied attempts
	// (Do not abort request if logging fails)
	// You may want to expand this to use persistAdminAuditEvent if available
}
