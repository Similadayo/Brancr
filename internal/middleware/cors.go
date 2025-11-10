package middleware

import (
	"net/http"
	"strings"

	"github.com/go-chi/cors"
)

// CORSMiddleware configures Cross-Origin Resource Sharing headers for incoming requests.
// allowedOrigins is a comma-separated list of origins. Whitespace is trimmed.
func CORSMiddleware(allowedOrigins string) func(next http.Handler) http.Handler {
	origins := parseOrigins(allowedOrigins)

	return cors.Handler(cors.Options{
		AllowedOrigins:   origins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	})
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
