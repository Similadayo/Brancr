package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
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
				// When quota exceeded, return 429 and Retry-After
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "60") // default retry after 60s
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "api_quota_exceeded", "message": err.Error()})
				return
			}

			// Provide quota headers for UX
			if usage, err := qs.GetQuotaUsage(r.Context(), claims.TenantID); err == nil {
				if usage.APICallsLimit > 0 {
					remaining := usage.APICallsLimit - usage.APICallsUsed
					if remaining < 0 {
						remaining = 0
					}
					w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", usage.APICallsLimit))
					w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// For use in tests
func contextWithTenant(ctx context.Context, tenantID uuid.UUID) context.Context {
	return WithTenantContext(ctx, &TenantClaims{TenantID: tenantID})
}
