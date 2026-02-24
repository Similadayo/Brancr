package middleware

import (
	"net/http"

	"github.com/google/uuid"
)

// TenantStatusCheck middleware verifies that the tenant is not blocked or suspended.
// This should be used AFTER TenantAuth middleware.
// Returns 403 if tenant is blocked or suspended.
func TenantStatusCheck(statusChecker func(tenantID uuid.UUID) (blocked bool, suspended bool), cookieName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := TenantFromContext(r.Context())
			if !ok {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			// Check if tenant is blocked or suspended
			blocked, suspended := statusChecker(claims.TenantID)

			if blocked {
				ClearSessionCookieForRequest(w, r, cookieName)
				w.Header().Set("Content-Type", "application/json")
				http.Error(w, `{"error":"account_blocked","message":"Your account has been blocked. Please contact support."}`, http.StatusForbidden)
				return
			}

			if suspended {
				ClearSessionCookieForRequest(w, r, cookieName)
				w.Header().Set("Content-Type", "application/json")
				http.Error(w, `{"error":"account_suspended","message":"Your account has been suspended. Please contact support."}`, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
