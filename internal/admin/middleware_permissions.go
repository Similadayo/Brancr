package admin

import (
	"net/http"

	"github.com/similadayo/brancr/internal/middleware"
	"go.uber.org/zap"
)

// RequirePermission enforces action-based permissions for admin endpoints.
func RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := AdminRoleFromRequest(r)
			adminEmail := AdminEmailFromRequest(r)
			if role == "" {
				logDeniedPermission(r, adminEmail, role, permission, "missing role")
				writeAdminError(w, r, http.StatusForbidden, "missing_role", "forbidden", map[string]any{
					"required_permission": permission,
				})
				return
			}
			perms, ok := RolePermissions[role]
			if !ok {
				logDeniedPermission(r, adminEmail, role, permission, "role not found")
				writeAdminError(w, r, http.StatusForbidden, "role_not_found", "forbidden", map[string]any{
					"role":                role,
					"required_permission": permission,
				})
				return
			}
			if perms["*"] {
				next.ServeHTTP(w, r)
				return
			}
			if !perms[permission] {
				logDeniedPermission(r, adminEmail, role, permission, "permission denied")
				writeAdminError(w, r, http.StatusForbidden, "permission_denied", "forbidden", map[string]any{
					"role":                role,
					"required_permission": permission,
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func logDeniedPermission(r *http.Request, adminEmail, role, permission, reason string) {
	logger := GetAdminLogger(r)
	if logger == nil {
		return
	}
	logger.Warn("admin permission denied",
		zap.String("request_id", middleware.GetRequestID(r.Context())),
		zap.String("admin_email", adminEmail),
		zap.String("admin_role", role),
		zap.String("permission", permission),
		zap.String("reason", reason),
		zap.String("path", r.URL.Path),
		zap.String("method", r.Method),
		zap.String("remote_addr", r.RemoteAddr),
	)
}
