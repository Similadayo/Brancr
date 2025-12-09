package admin

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/quota"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// UsageStatsResponse represents tenant usage statistics
type UsageStatsResponse struct {
	TenantID        uint      `json:"tenant_id"`
	TenantName      string    `json:"tenant_name"`
	PlanType        string    `json:"plan_type"`
	AICallsUsed     int       `json:"ai_calls_used"`
	AICallsLimit    int       `json:"ai_calls_limit"`
	AICallsPercent  float64   `json:"ai_calls_percent"`
	PostsUsed       int       `json:"posts_used"`
	PostsLimit      int       `json:"posts_limit"`
	PostsPercent    float64   `json:"posts_percent"`
	StorageUsedMB   int       `json:"storage_used_mb"`
	StorageLimitMB  int       `json:"storage_limit_mb"`
	StoragePercent  float64   `json:"storage_percent"`
	APICallsUsed    int       `json:"api_calls_used"`
	APICallsLimit   int       `json:"api_calls_limit"`
	APICallsPercent float64   `json:"api_calls_percent"`
	LastResetAt     time.Time `json:"last_reset_at"`
}

// UsageStatsPageHandler renders the usage stats HTML page
func UsageStatsPageHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats, totalTenants, nearQuotaLimit, quotaExceeded := getUsageStats(ctx, r)

		data := map[string]interface{}{
			"Stats":          stats,
			"TotalTenants":   totalTenants,
			"NearQuotaLimit": nearQuotaLimit,
			"QuotaExceeded":  quotaExceeded,
			"Page":           "usage",
		}

		renderTemplate(w, r, "usage.html", data, ctx.Logger)
	}
}

// UsageStatsAPIHandler returns tenant resource usage statistics as JSON
func UsageStatsAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats, _, _, _ := getUsageStats(ctx, r)

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(stats); err != nil {
			ctx.Logger.Error("failed to encode usage stats", zap.Error(err))
			http.Error(w, "encoding error", http.StatusInternalServerError)
			return
		}
	}
}

// getUsageStats retrieves usage statistics for all tenants
func getUsageStats(ctx *AdminContext, r *http.Request) ([]UsageStatsResponse, int, int, int) {
	// Get tenant ID from query param (optional - if not provided, return all)
	tenantIDStr := r.URL.Query().Get("tenant_id")

	var tenants []storage.Tenant
	// Query all tenants (not just active) to show usage for all
	query := ctx.DB

	if tenantIDStr != "" {
		query = query.Where("id = ?", tenantIDStr)
	}

	if err := query.Find(&tenants).Error; err != nil {
		ctx.Logger.Error("failed to query tenants", zap.Error(err))
		return []UsageStatsResponse{}, 0, 0, 0
	}

	requestID := middleware.GetRequestID(r.Context())
	logger := ctx.Logger.With(
		zap.String("request_id", requestID),
		zap.Int("tenant_count", len(tenants)),
	)
	logger.Info("fetching usage stats")

	quotaService := quota.NewQuotaService(ctx.DB, ctx.Logger)
	stats := make([]UsageStatsResponse, 0, len(tenants))
	nearQuotaLimit := 0
	quotaExceeded := 0

	for _, tenant := range tenants {
		quotaData, err := quotaService.GetQuotaUsage(r.Context(), tenant.ID)
		if err != nil {
			logger.Error("failed to get quota for tenant",
				zap.Uint("tenant_id", tenant.ID),
				zap.String("tenant_name", tenant.Name),
				zap.Error(err))
			// Still include tenant in stats with zero usage if quota fetch fails
			stats = append(stats, UsageStatsResponse{
				TenantID:        tenant.ID,
				TenantName:      tenant.Name,
				PlanType:        "unknown",
				AICallsUsed:     0,
				AICallsLimit:    0,
				AICallsPercent:  0,
				PostsUsed:       0,
				PostsLimit:      0,
				PostsPercent:    0,
				StorageUsedMB:   0,
				StorageLimitMB:  0,
				StoragePercent:  0,
				APICallsUsed:    0,
				APICallsLimit:   0,
				APICallsPercent: 0,
				LastResetAt:     time.Now(),
			})
			continue
		}

		aiPercent := 0.0
		if quotaData.AICallsLimit > 0 {
			aiPercent = float64(quotaData.AICallsUsed) / float64(quotaData.AICallsLimit) * 100
		}

		postsPercent := 0.0
		if quotaData.PostsLimit > 0 {
			postsPercent = float64(quotaData.PostsUsed) / float64(quotaData.PostsLimit) * 100
		}

		storagePercent := 0.0
		if quotaData.StorageLimitMB > 0 {
			storagePercent = float64(quotaData.StorageUsedMB) / float64(quotaData.StorageLimitMB) * 100
		}

		apiPercent := 0.0
		if quotaData.APICallsLimit > 0 {
			apiPercent = float64(quotaData.APICallsUsed) / float64(quotaData.APICallsLimit) * 100
		}

		// Check if near limit (>= 90%) or exceeded (>= 100%)
		isNearLimit := aiPercent >= 90 || postsPercent >= 90 || storagePercent >= 90 || apiPercent >= 90
		isExceeded := aiPercent >= 100 || postsPercent >= 100 || storagePercent >= 100 || apiPercent >= 100

		if isExceeded {
			quotaExceeded++
		} else if isNearLimit {
			nearQuotaLimit++
		}

		stats = append(stats, UsageStatsResponse{
			TenantID:        tenant.ID,
			TenantName:      tenant.Name,
			PlanType:        quotaData.PlanType,
			AICallsUsed:     quotaData.AICallsUsed,
			AICallsLimit:    quotaData.AICallsLimit,
			AICallsPercent:  aiPercent,
			PostsUsed:       quotaData.PostsUsed,
			PostsLimit:      quotaData.PostsLimit,
			PostsPercent:    postsPercent,
			StorageUsedMB:   quotaData.StorageUsedMB,
			StorageLimitMB:  quotaData.StorageLimitMB,
			StoragePercent:  storagePercent,
			APICallsUsed:    quotaData.APICallsUsed,
			APICallsLimit:   quotaData.APICallsLimit,
			APICallsPercent: apiPercent,
			LastResetAt:     quotaData.LastResetAt,
		})
	}

	return stats, len(tenants), nearQuotaLimit, quotaExceeded
}
