package middleware

import (
	"net/http"
	"runtime/debug"

	"go.uber.org/zap"
)

// RecoveryMiddleware recovers from panics and logs them
func RecoveryMiddleware(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					stack := string(debug.Stack())
					logger.Error("panic recovered",
						zap.Any("panic", rec),
						zap.String("stack", stack),
						zap.String("path", r.URL.Path),
						zap.String("method", r.Method))
					http.Error(w, "internal server error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
