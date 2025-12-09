package middleware

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"
)

// ValidationError represents a validation error
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationErrorResponse is the response format for validation errors
type ValidationErrorResponse struct {
	Error   string            `json:"error"`
	Message string            `json:"message"`
	Details []ValidationError `json:"details,omitempty"`
	Code    int               `json:"code"`
}

// ValidateContentType ensures Content-Type header is set for POST/PUT/PATCH
// This is a lightweight check - full validation should be done in handlers
func ValidateContentType(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
				contentType := r.Header.Get("Content-Type")
				// Allow empty for form data or if explicitly set to form-urlencoded
				if contentType == "" && r.ContentLength > 0 {
					logger.Warn("missing Content-Type header",
						zap.String("path", r.URL.Path),
						zap.String("method", r.Method))

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					json.NewEncoder(w).Encode(ValidationErrorResponse{
						Error:   "validation_error",
						Message: "Content-Type header is required for requests with body",
						Code:    http.StatusBadRequest,
					})
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
