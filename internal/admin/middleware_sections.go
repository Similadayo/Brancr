package admin

import (
	"net/http"
)

// RequireSectionAccess restricts dashboard section access based on role
func RequireSectionAccess(section string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := AdminRoleFromRequest(r)

			// Check if role has access to this section
			if !hasSectionAccess(role, section) {
				writeAdminError(w, r, http.StatusForbidden, "section_access_denied", "forbidden", map[string]any{
					"role":    role,
					"section": section,
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
