package middleware

import (
	"net/http"
	"strings"
)

// EnforceJSONContentType ensures that state-changing requests use proper Content-Type.
// This prevents simple CSRF attacks via text/plain or x-www-form-urlencoded.
func EnforceJSONContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip safe methods
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		ct := r.Header.Get("Content-Type")

		// Allow JSON
		if strings.Contains(ct, "application/json") {
			next.ServeHTTP(w, r)
			return
		}

		// Allow Multipart (for file uploads)
		// Note: CSRF protection for multipart relies on Strict CORS Origin check
		if strings.Contains(ct, "multipart/form-data") {
			next.ServeHTTP(w, r)
			return
		}

		// Reject others (x-www-form-urlencoded, text/plain, etc.)
		http.Error(w, "Unsupported Content-Type. Expected application/json or multipart/form-data", http.StatusUnsupportedMediaType)
	})
}
