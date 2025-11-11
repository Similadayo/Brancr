package middleware

import (
	"net/http"
	"strings"
)

// CORSMiddleware configures Cross-Origin Resource Sharing headers for incoming requests.
// allowedOrigins is a comma-separated list of origins. Whitespace is trimmed.
func CORSMiddleware(allowedOrigins string) func(next http.Handler) http.Handler {
	origins := parseOrigins(allowedOrigins)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && originAllowed(origin, origins) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token, X-Requested-With")
				w.Header().Set("Access-Control-Expose-Headers", "Link")
			}

			if r.Method == http.MethodOptions {
				if origin == "" || !originAllowed(origin, origins) {
					// If the origin isn't allowed, fall through with 204 to avoid auth leakage.
					w.WriteHeader(http.StatusNoContent)
					return
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func parseOrigins(raw string) []string {
	chunks := strings.Split(raw, ",")
	origins := make([]string, 0, len(chunks))
	for _, c := range chunks {
		trimmed := strings.TrimSpace(c)
		if trimmed == "" {
			continue
		}
		origins = append(origins, trimmed)
	}
	if len(origins) == 0 {
		return []string{"http://localhost:3000"}
	}
	return origins
}

func originAllowed(origin string, allowed []string) bool {
	for _, o := range allowed {
		if o == "*" || o == origin {
			return true
		}
	}
	return false
}
