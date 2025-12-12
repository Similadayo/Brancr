package admin

import "net/http"

const (
	RoleSuperAdmin        = "SuperAdmin"
	RoleSupportAgent      = "SupportAgent"
	RoleTechnicalOperator = "TechnicalOperator"
	RoleBillingAdmin      = "BillingAdmin"
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
