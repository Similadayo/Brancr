package admin

import (
	"net/http"
)

// AdminRootRedirectHandler redirects /admin to /admin/login or /admin/app based on authentication
func AdminRootRedirectHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Try to validate the admin session cookie
		cookie, err := r.Cookie("brancr_admin")
		if err != nil || cookie.Value == "" {
			// Not authenticated, redirect to login
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		// Check if token is valid
		token, _, err := validateToken(ctx, r.Context(), cookie.Value)
		if err != nil || token == nil || !token.Valid {
			// Invalid token, redirect to login
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		// Authenticated, redirect to the new SPA dashboard
		http.Redirect(w, r, "/admin/app", http.StatusSeeOther)
	}
}
