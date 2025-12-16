package middleware

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/similadayo/brancr/internal/quota"
	"github.com/similadayo/brancr/pkg/metrics"
)

// APIQuota returns middleware that charges API calls to tenant quota on request entry.
// It expects tenant claims to be present in context (use TenantAuth middleware first).
func APIQuota(qs *quota.QuotaService, m *metrics.Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract tenant from context
			claims, ok := TenantFromContext(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			// Check and increment (charges on entry)
			if err := qs.CheckAndIncrementAPICallQuota(r.Context(), claims.TenantID); err != nil {
				if m != nil {
					m.IncQuotaRejection("api_calls")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "api_quota_exceeded", "message": err.Error()})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// For use in tests
func contextWithTenant(ctx context.Context, tenantID uint) context.Context {
	return WithTenantContext(ctx, &TenantClaims{TenantID: tenantID})
}
