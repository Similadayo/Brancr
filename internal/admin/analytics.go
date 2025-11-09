package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// AnalyticsHandler handles analytics-related admin endpoints
type AnalyticsHandler struct {
	DB      *gorm.DB
	Logger  *zap.Logger
	Service interface{} // *analytics.Service
}

// NewAnalyticsHandler creates a new analytics handler
func NewAnalyticsHandler(db *gorm.DB, logger *zap.Logger, service interface{}) *AnalyticsHandler {
	return &AnalyticsHandler{
		DB:      db,
		Logger:  logger,
		Service: service,
	}
}

// GetTenantAnalytics returns analytics for a specific tenant
func (h *AnalyticsHandler) GetTenantAnalytics(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantIDStr := chi.URLParam(r, "id")
		tenantID, err := strconv.ParseUint(tenantIDStr, 10, 32)
		if err != nil {
			http.Error(w, "invalid tenant ID", http.StatusBadRequest)
			return
		}
		
		// Parse query parameters
		daysBack := 30
		if daysStr := r.URL.Query().Get("days"); daysStr != "" {
			if d, err := strconv.Atoi(daysStr); err == nil {
				daysBack = d
			}
		}
		
		periodEnd := time.Now()
		periodStart := periodEnd.AddDate(0, 0, -daysBack)
		
		// Fetch analytics
		var analytics []storage.PostAnalytics
		if err := ctx.DB.Where("tenant_id = ? AND posted_at >= ? AND posted_at <= ?",
			uint(tenantID), periodStart, periodEnd).
			Order("posted_at DESC").
			Find(&analytics).Error; err != nil {
			ctx.Logger.Error("failed to fetch analytics", zap.Error(err))
			http.Error(w, "failed to fetch analytics", http.StatusInternalServerError)
			return
		}
		
		// Aggregate metrics
		summary := map[string]interface{}{
			"tenant_id":        tenantID,
			"period_start":     periodStart,
			"period_end":       periodEnd,
			"total_posts":      len(analytics),
			"total_impressions": int64(0),
			"total_reach":      int64(0),
			"total_likes":      int64(0),
			"total_comments":   int64(0),
			"total_shares":     int64(0),
			"avg_engagement_rate": 0.0,
			"platform_breakdown": map[string]interface{}{},
		}
		
		platformStats := make(map[string]map[string]interface{})
		
		for _, a := range analytics {
			// Update totals
			summary["total_impressions"] = summary["total_impressions"].(int64) + a.Impressions
			summary["total_reach"] = summary["total_reach"].(int64) + a.Reach
			summary["total_likes"] = summary["total_likes"].(int64) + a.Likes
			summary["total_comments"] = summary["total_comments"].(int64) + a.Comments
			summary["total_shares"] = summary["total_shares"].(int64) + a.Shares
			
			// Platform breakdown
			if platformStats[a.Platform] == nil {
				platformStats[a.Platform] = map[string]interface{}{
					"platform":           a.Platform,
					"total_posts":        0,
					"total_impressions":  int64(0),
					"total_reach":        int64(0),
					"total_likes":        int64(0),
					"total_comments":     int64(0),
					"avg_engagement_rate": 0.0,
				}
			}
			
			stats := platformStats[a.Platform]
			stats["total_posts"] = stats["total_posts"].(int) + 1
			stats["total_impressions"] = stats["total_impressions"].(int64) + a.Impressions
			stats["total_reach"] = stats["total_reach"].(int64) + a.Reach
			stats["total_likes"] = stats["total_likes"].(int64) + a.Likes
			stats["total_comments"] = stats["total_comments"].(int64) + a.Comments
			stats["avg_engagement_rate"] = stats["avg_engagement_rate"].(float64) + a.EngagementRate
		}
		
		// Calculate averages
		if summary["total_reach"].(int64) > 0 {
			totalEngagement := float64(summary["total_likes"].(int64) + summary["total_comments"].(int64) + summary["total_shares"].(int64))
			summary["avg_engagement_rate"] = (totalEngagement / float64(summary["total_reach"].(int64))) * 100.0
		}
		
		// Calculate platform averages
		for platform, stats := range platformStats {
			if stats["total_posts"].(int) > 0 {
				stats["avg_engagement_rate"] = stats["avg_engagement_rate"].(float64) / float64(stats["total_posts"].(int))
			}
			summary["platform_breakdown"].(map[string]interface{})[platform] = stats
		}
		
		// Fetch trend insights
		var insights []storage.TrendInsight
		ctx.DB.Where("tenant_id = ? AND period_start >= ? AND period_end <= ? AND status = ?",
			uint(tenantID), periodStart, periodEnd, "active").
			Order("created_at DESC").
			Limit(10).
			Find(&insights)
		
		summary["insights"] = insights
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(summary)
	}
}

// GetAnalyticsDashboard returns analytics dashboard data
func (h *AnalyticsHandler) GetAnalyticsDashboard(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get all tenants with analytics
		var tenants []storage.Tenant
		ctx.DB.Find(&tenants)
		
		dashboard := map[string]interface{}{
			"total_tenants":      len(tenants),
			"tenants_with_data":  0,
			"total_posts":        0,
			"total_impressions":  int64(0),
			"total_engagement":   int64(0),
			"top_performers":     []map[string]interface{}{},
		}
		
		// Aggregate across all tenants
		var totalAnalytics []storage.PostAnalytics
		periodStart := time.Now().AddDate(0, 0, -30)
		
		ctx.DB.Where("posted_at >= ?", periodStart).Find(&totalAnalytics)
		
		dashboard["total_posts"] = len(totalAnalytics)
		
		tenantsWithData := make(map[uint]bool)
		for _, a := range totalAnalytics {
			tenantsWithData[a.TenantID] = true
			dashboard["total_impressions"] = dashboard["total_impressions"].(int64) + a.Impressions
			dashboard["total_engagement"] = dashboard["total_engagement"].(int64) + a.Likes + a.Comments + a.Shares
		}
		
		dashboard["tenants_with_data"] = len(tenantsWithData)
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(dashboard)
	}
}

