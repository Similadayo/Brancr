package middleware

import (
	"log"
	"net/http"
	"strings"
)

// CORSMiddleware configures Cross-Origin Resource Sharing headers for incoming requests.
// allowedOrigins is a comma-separated list of origins. Whitespace is trimmed.
func CORSMiddleware(allowedOrigins string) func(next http.Handler) http.Handler {
	origins := parseOrigins(allowedOrigins)

	// Log allowed origins at startup for debugging
	log.Printf("🌐 CORS Middleware initialized with allowed origins: %v", origins)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			// Allow all specific static asset paths regardless of origin
			// This fixes issues where module scripts (requiring CORS) are blocked if origin is not in whitelist
			path := r.URL.Path
			if strings.Contains(path, "/assets/") || strings.HasPrefix(path, "/static/") {
				if origin != "" {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				}
				next.ServeHTTP(w, r)
				return
			}

			if origin != "" {
				if !originAllowed(origin, origins) {
					log.Printf("⛔ CORS: Request blocked from disallowed origin: %s", origin)
					http.Error(w, "Origin not allowed", http.StatusForbidden)
					return
				}

				// Set CORS headers for allowed origin
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS,HEAD")
				w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token, X-Requested-With, X-Request-ID")
				w.Header().Set("Access-Control-Expose-Headers", "Link, X-Request-ID")
			} else if r.Method == http.MethodOptions {
				// OPTIONS request without Origin? Allowed, but unusual.
				// We don't set CORS headers if no origin.
			}

			// Handle preflight OPTIONS requests
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Max-Age", "3600") // Cache preflight for 1 hour
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
