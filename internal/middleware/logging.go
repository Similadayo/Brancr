package middleware

import (
	"net/http"
	"time"

	"go.uber.org/zap"
)

// LoggingMiddleware logs HTTP requests with correlation ID
func LoggingMiddleware(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Get request ID from context for correlation
			requestID := GetRequestID(r.Context())

			// Wrap response writer to capture status code
			wrapped := &responseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			// Add request ID to logger context
			logger := logger.With(zap.String("request_id", requestID))

			next.ServeHTTP(wrapped, r)

			duration := time.Since(start)

			// Log with structured fields including correlation ID
			fields := []zap.Field{
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.String("query", r.URL.RawQuery),
				zap.Int("status", wrapped.statusCode),
				zap.Duration("duration", duration),
				zap.String("remote_addr", r.RemoteAddr),
				zap.String("user_agent", r.UserAgent()),
			}

			// Add tenant ID if available
			if claims, ok := TenantFromContext(r.Context()); ok {
				fields = append(fields, zap.Uint("tenant_id", claims.TenantID))
			}

			// Log level based on status code
			if wrapped.statusCode >= 500 {
				logger.Error("HTTP request", fields...)
			} else if wrapped.statusCode >= 400 {
				logger.Warn("HTTP request", fields...)
			} else {
				logger.Info("HTTP request", fields...)
			}
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
