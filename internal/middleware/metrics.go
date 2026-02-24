package middleware

import (
	"net/http"
	"time"

	"github.com/similadayo/brancr/pkg/metrics"
)

// MetricsMiddleware records HTTP request metrics
func MetricsMiddleware(m *metrics.Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap response writer to capture status code
			wrapped := &metricsResponseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			next.ServeHTTP(wrapped, r)

			// Record metrics
			duration := time.Since(start)
			m.HTTPRequests.Inc()
			m.HTTPRequestDuration.RecordDuration(duration)

			// Record errors
			if wrapped.statusCode >= 400 {
				m.Errors.Inc()
			}
		})
	}
}

// metricsResponseWriter wraps http.ResponseWriter to capture status code
type metricsResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *metricsResponseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
