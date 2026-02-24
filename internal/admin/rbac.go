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
	RoleSuperAdmin        = "SuperAdmin"
	RoleSupportAdmin      = "SupportAdmin"  // Growth & Customer Success
	RolePlatformAdmin     = "PlatformAdmin" // Project Manager
	RoleBillingAdmin      = "BillingAdmin"
	RoleComplianceAdmin   = "ComplianceAdmin"
	RoleOperationsSupport = "OperationsSupport" // Operations/Admin Support
)

// Add missing roles for legacy compatibility
const (
	RoleTechnicalOperator = "TechnicalOperator"
	RoleSupportAgent      = "SupportAgent"
)

// SectionRequiredPermission maps dashboard sections to the permission that grants access.
// SuperAdmin ("*") always has access.
var SectionRequiredPermission = map[string]string{
	"system_health":        "health.view",
	"ai_behavior":          "ai.behavior.view",
	"tenant_overview":      "tenant.read",
	"messaging_activity":   "messaging.view",
	"posting_status":       "posting.view",
	"qualitative_insights": "insights.read",
	// cost_control remains SuperAdmin-only (no role should have it by default).
	"cost_control": "*",
}

// hasSectionAccess checks if a role has access to a dashboard section
func hasSectionAccess(role, section string) bool {
	// SuperAdmin has access to everything
	if role == RoleSuperAdmin {
		return true
	}

	required, ok := SectionRequiredPermission[section]
	if !ok {
		return false
	}
	if required == "*" {
		return false
	}
	perms, ok := RolePermissions[role]
	if !ok {
		return false
	}
	if perms["*"] {
		return true
	}
	return perms[required]
}

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
				writeAdminError(w, r, http.StatusForbidden, "missing_role", "forbidden", map[string]any{
					"required_roles": allowed,
				})
				return
			}
			if _, ok := allowedSet[role]; !ok {
				writeAdminError(w, r, http.StatusForbidden, "role_not_allowed", "forbidden", map[string]any{
					"role":           role,
					"required_roles": allowed,
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
