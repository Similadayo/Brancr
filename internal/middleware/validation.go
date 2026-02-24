package middleware

import (
	"encoding/json"
	"net/http"
	"strings"

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
			if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete {
				// Only validate if there is a body
				if r.ContentLength > 0 {
					contentType := r.Header.Get("Content-Type")
					if !strings.Contains(contentType, "application/json") && !strings.Contains(contentType, "multipart/form-data") {
						logger.Warn("invalid or missing Content-Type header",
							zap.String("path", r.URL.Path),
							zap.String("method", r.Method),
							zap.String("content_type", contentType))

						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusUnsupportedMediaType)
						json.NewEncoder(w).Encode(ValidationErrorResponse{
							Error:   "validation_error",
							Message: "Content-Type must be application/json or multipart/form-data",
							Code:    http.StatusUnsupportedMediaType,
						})
						return
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
