package middleware

import (
	"html"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

// SanitizeInput middleware sanitizes user input to prevent XSS
func SanitizeInput(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Sanitize query parameters
			query := r.URL.Query()
			for key, values := range query {
				for i, val := range values {
					// HTML escape to prevent XSS
					query[key][i] = html.EscapeString(strings.TrimSpace(val))
				}
			}
			r.URL.RawQuery = query.Encode()

			// Note: Request body sanitization should be done in handlers
			// as it depends on content type and structure
			next.ServeHTTP(w, r)
		})
	}
}

// SanitizeString sanitizes a string by HTML escaping
func SanitizeString(s string) string {
	return html.EscapeString(strings.TrimSpace(s))
}

// SanitizeStrings sanitizes a slice of strings
func SanitizeStrings(strs []string) []string {
	result := make([]string, len(strs))
	for i, s := range strs {
		result[i] = SanitizeString(s)
	}
	return result
}
