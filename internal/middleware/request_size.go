package middleware

import (
	"encoding/json"
	"net/http"
	"strconv"

	"go.uber.org/zap"
)

// RequestSizeLimit middleware limits the size of request bodies
func RequestSizeLimit(maxSize int64, logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only limit POST, PUT, PATCH requests
			if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodPatch {
				next.ServeHTTP(w, r)
				return
			}

			// Check Content-Length header if present
			if cl := r.Header.Get("Content-Length"); cl != "" {
				size, err := strconv.ParseInt(cl, 10, 64)
				if err == nil && size > maxSize {
					logger.Warn("request body too large",
						zap.String("path", r.URL.Path),
						zap.String("method", r.Method),
						zap.Int64("size", size),
						zap.Int64("max_size", maxSize))

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusRequestEntityTooLarge)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"error":   "request_too_large",
						"message": "Request body exceeds maximum size",
						"max_size": maxSize,
						"code":    http.StatusRequestEntityTooLarge,
					})
					return
				}
			}

			// Limit the request body reader
			r.Body = http.MaxBytesReader(w, r.Body, maxSize)

			next.ServeHTTP(w, r)
		})
	}
}

// DefaultRequestSizeLimit returns middleware with default size limit (10MB)
func DefaultRequestSizeLimit(logger *zap.Logger) func(http.Handler) http.Handler {
	return RequestSizeLimit(10*1024*1024, logger) // 10MB
}

