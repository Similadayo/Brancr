package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/pkg/metrics"
	"go.uber.org/zap"
)

// AdminLoggingMiddleware provides comprehensive request logging for admin routes
func AdminLoggingMiddleware(ctx *AdminContext, m *metrics.Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Get request ID from context
			requestID := middleware.GetRequestID(r.Context())

			// Create logger with request context
			logger := ctx.Logger.With(
				zap.String("request_id", requestID),
				zap.String("path", r.URL.Path),
				zap.String("method", r.Method),
				zap.String("remote_addr", r.RemoteAddr),
			)

			// Get admin user from session if available
			adminUser := getAdminUserFromRequest(r)
			if adminUser != "" {
				logger = logger.With(zap.String("admin_user", adminUser))
			}

			// Wrap response writer to capture status code
			wrapped := &adminResponseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			// Add logger to request context for handlers to use
			ctxWithLogger := context.WithValue(r.Context(), adminLoggerKey{}, logger)
			r = r.WithContext(ctxWithLogger)

			next.ServeHTTP(wrapped, r)

			duration := time.Since(start)

			// Build log fields
			fields := []zap.Field{
				zap.Int("status", wrapped.statusCode),
				zap.Duration("duration_ms", duration),
				zap.String("query", r.URL.RawQuery),
				zap.String("user_agent", r.UserAgent()),
			}

			// Add tenant ID if in query params (for filtered views)
			if tenantID := r.URL.Query().Get("tenant_id"); tenantID != "" {
				fields = append(fields, zap.String("filter_tenant_id", tenantID))
			}

			// Record metrics
			if m != nil {
				m.HTTPRequests.Inc()
				m.HTTPRequestDuration.RecordDuration(duration)
				if wrapped.statusCode >= 400 {
					m.Errors.Inc()
				}
			}

			// Log based on status code
			if wrapped.statusCode >= 500 {
				logger.Error("admin request failed", fields...)
			} else if wrapped.statusCode >= 400 {
				logger.Warn("admin request client error", fields...)
			} else {
				// Only log info for non-health-check endpoints to reduce noise
				if r.URL.Path != "/admin/_health" && r.URL.Path != "/admin/_health_partial" {
					logger.Info("admin request", fields...)
				}
			}

			// Persist audit logs for state-changing requests.
			if r.Method != http.MethodGet && r.URL.Path != "/admin/_health" && r.URL.Path != "/admin/_health_partial" {
				persistAdminAuditLog(ctx, r, wrapped.statusCode)
			}
		})
	}
}

func persistAdminAuditLog(ctx *AdminContext, r *http.Request, status int) {
	if ctx == nil || ctx.DB == nil {
		return
	}

	adminEmail := getAdminUserFromRequest(r)
	adminRole := AdminRoleFromRequest(r)

	var tenantID *uint
	if tid := r.URL.Query().Get("tenant_id"); tid != "" {
		if v, err := strconv.ParseUint(tid, 10, 64); err == nil {
			t := uint(v)
			tenantID = &t
		}
	}

	metaBytes, _ := json.Marshal(map[string]any{
		"query": r.URL.RawQuery,
	})

	entry := storage.AdminAuditLog{
		AdminEmail: adminEmail,
		AdminRole:  adminRole,
		Action:     "admin.request",
		Method:     r.Method,
		Path:       r.URL.Path,
		TenantID:   tenantID,
		Resource:   "admin",
		ResourceID: "",
		Success:    status < 400,
		StatusCode: status,
		IP:         r.RemoteAddr,
		UserAgent:  r.UserAgent(),
		Metadata:   string(metaBytes),
	}

	_ = ctx.DB.WithContext(r.Context()).Create(&entry).Error
}

// adminResponseWriter wraps http.ResponseWriter to capture status code
type adminResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *adminResponseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// adminLoggerKey is used to store logger in request context
type adminLoggerKey struct{}

// GetAdminLogger retrieves the admin logger from request context
func GetAdminLogger(r *http.Request) *zap.Logger {
	if logger, ok := r.Context().Value(adminLoggerKey{}).(*zap.Logger); ok {
		return logger
	}
	return nil
}

// getAdminUserFromRequest extracts admin user email from request context
func getAdminUserFromRequest(r *http.Request) string {
	// Get from context (set by AdminAuthMiddleware after token validation)
	if email, ok := r.Context().Value("admin_email").(string); ok {
		return email
	}
	return ""
}

// LogAdminAction logs an admin action with full context
func LogAdminAction(r *http.Request, action string, fields ...zap.Field) {
	logger := GetAdminLogger(r)
	if logger == nil {
		return
	}

	allFields := append([]zap.Field{
		zap.String("action", action),
	}, fields...)

	logger.Info("admin action", allFields...)
}

// LogAdminError logs an admin error with full context
func LogAdminError(r *http.Request, action string, err error, fields ...zap.Field) {
	logger := GetAdminLogger(r)
	if logger == nil {
		return
	}

	allFields := append([]zap.Field{
		zap.String("action", action),
		zap.Error(err),
	}, fields...)

	logger.Error("admin action failed", allFields...)
}
