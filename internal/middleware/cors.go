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
			allowed := origin != "" && originAllowed(origin, origins)
			
			// Log CORS preflight requests for debugging
			if r.Method == http.MethodOptions {
				if origin == "" {
					log.Printf("⚠️ CORS: OPTIONS request with no Origin header from %s", r.RemoteAddr)
				} else if !allowed {
					log.Printf("⚠️ CORS: OPTIONS request from disallowed origin: %s (allowed: %v)", origin, origins)
				} else {
					log.Printf("✅ CORS: OPTIONS request from allowed origin: %s", origin)
			}
			}

			// Handle preflight OPTIONS requests
			if r.Method == http.MethodOptions {
				if allowed {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
					w.Header().Set("Access-Control-Allow-Credentials", "true")
					w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS,HEAD")
					w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token, X-Requested-With, X-Request-ID")
					w.Header().Set("Access-Control-Max-Age", "3600") // Cache preflight for 1 hour
					w.WriteHeader(http.StatusNoContent)
				} else {
					// For disallowed origins, return 403 without CORS headers (security best practice)
					w.WriteHeader(http.StatusForbidden)
				}
				return
			}

			// Set CORS headers for actual requests if origin is allowed
			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS,HEAD")
				w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token, X-Requested-With, X-Request-ID")
				w.Header().Set("Access-Control-Expose-Headers", "Link, X-Request-ID")
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
