package admin

import "net/http"

const adminEmailKey = "admin_email"

func AdminEmailFromRequest(r *http.Request) string {
	if v, ok := r.Context().Value(adminEmailKey).(string); ok {
		return v
	}
	return ""
}

const (
	RoleSuperAdmin      = "SuperAdmin"
	RoleSupportAdmin    = "SupportAdmin"
	RolePlatformAdmin   = "PlatformAdmin"
	RoleBillingAdmin    = "BillingAdmin"
	RoleComplianceAdmin = "ComplianceAdmin"
)

// Add missing roles for legacy compatibility
const (
	RoleTechnicalOperator = "TechnicalOperator"
	RoleSupportAgent      = "SupportAgent"
)

type adminContextKey string

const (
	adminRoleKey adminContextKey = "admin_role"
)

func AdminRoleFromRequest(r *http.Request) string {
	if v, ok := r.Context().Value(adminRoleKey).(string); ok {
		return v
	}
	return ""
}

func requireAnyRole(allowed ...string) func(http.Handler) http.Handler {
	allowedSet := map[string]struct{}{}
	for _, a := range allowed {
		allowedSet[a] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := AdminRoleFromRequest(r)
			if role == "" {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if _, ok := allowedSet[role]; !ok {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
