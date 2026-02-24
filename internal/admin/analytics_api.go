package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// AnalyticsResponse represents analytics dashboard response
type AnalyticsResponse struct {
	TotalPosts      int64                 `json:"total_posts"`
	TotalEngagement int64                 `json:"total_engagement"`
	TopPlatforms    []PlatformAnalytics   `json:"top_platforms"`
	TopTenants      []TenantAnalytics     `json:"top_tenants"`
	RecentPosts     []PostAnalyticsDetail `json:"recent_posts"`
}

// PlatformAnalytics represents platform-level analytics
type PlatformAnalytics struct {
	Platform   string `json:"platform"`
	PostCount  int64  `json:"post_count"`
	Engagement int64  `json:"engagement"`
}

// TenantAnalytics represents tenant-level analytics
type TenantAnalytics struct {
	TenantID   uuid.UUID `json:"tenant_id"`
	TenantName string    `json:"tenant_name"`
	PostCount  int64     `json:"post_count"`
	Engagement int64     `json:"engagement"`
}

// PostAnalyticsDetail represents detailed post analytics
type PostAnalyticsDetail struct {
	ID         uuid.UUID `json:"id"`
	TenantID   uuid.UUID `json:"tenant_id"`
	TenantName string    `json:"tenant_name"`
	Platform   string    `json:"platform"`
	PostedAt   time.Time `json:"posted_at"`
	Likes      int       `json:"likes"`
	Comments   int       `json:"comments"`
	Shares     int       `json:"shares"`
	Reaches    int       `json:"reaches"`
}

// AnalyticsAPIHandler returns analytics dashboard data
func AnalyticsAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		// Get period filter (default: 30 days)
		period := strings.TrimSpace(r.URL.Query().Get("period"))
		startDate := strings.TrimSpace(r.URL.Query().Get("start_date"))
		endDate := strings.TrimSpace(r.URL.Query().Get("end_date"))

		if period == "" {
			period = "30d"
		}

		var since, until time.Time
		until = time.Now()

		switch period {
		case "24h":
			since = time.Now().Add(-24 * time.Hour)
		case "7d":
			since = time.Now().Add(-7 * 24 * time.Hour)
		case "30d":
			since = time.Now().Add(-30 * 24 * time.Hour)
		case "90d":
			since = time.Now().Add(-90 * 24 * time.Hour)
		case "custom":
			since = time.Now().Add(-30 * 24 * time.Hour) // Default
			if startDate != "" {
				if t, err := time.Parse("2006-01-02", startDate); err == nil {
					since = t
				}
			}
			if endDate != "" {
				if t, err := time.Parse("2006-01-02", endDate); err == nil {
					until = t.Add(24 * time.Hour).Add(-time.Nanosecond)
				}
			}
		default:
			since = time.Now().Add(-30 * 24 * time.Hour)
		}

		data, err := BuildAnalyticsData(ctx, since, until, reqCtx)
		if err != nil {
			ctx.Logger.Error("failed to build analytics data", zap.Error(err))
			http.Error(w, "failed to build analytics data", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(data); err != nil {
			ctx.Logger.Error("failed to encode analytics data", zap.Error(err))
			http.Error(w, "encoding error", http.StatusInternalServerError)
			return
		}
	}
}

// BuildAnalyticsData builds analytics dashboard data
func BuildAnalyticsData(ctx *AdminContext, since, until time.Time, reqCtx context.Context) (*AnalyticsResponse, error) {
	// Get all analytics posts in period
	var analytics []storage.PostAnalytics
	if err := ctx.DB.WithContext(reqCtx).
		Where("posted_at >= ? AND posted_at <= ?", since, until).
		Order("posted_at DESC").
		Limit(1000).
		Find(&analytics).Error; err != nil {
		return nil, fmt.Errorf("failed to query analytics: %w", err)
	}

	totalPosts := int64(len(analytics))
	var totalEngagement int64

	// Get tenant names
	tenantMap := make(map[uuid.UUID]string)
	var tenantIDs []uuid.UUID
	platformCounts := make(map[string]int64)
	platformEngagement := make(map[string]int64)
	tenantCounts := make(map[uuid.UUID]int64)
	tenantEngagement := make(map[uuid.UUID]int64)

	for _, a := range analytics {
		if _, ok := tenantMap[a.TenantID]; !ok {
			tenantIDs = append(tenantIDs, a.TenantID)
		}

		// Calculate engagement (likes + comments + shares + reach)
		engagement := a.Likes + a.Comments + a.Shares + a.Reach
		totalEngagement += engagement

		// Platform stats
		platformCounts[a.Platform]++
		platformEngagement[a.Platform] += engagement

		// Tenant stats
		tenantCounts[a.TenantID]++
		tenantEngagement[a.TenantID] += engagement
	}

	if len(tenantIDs) > 0 {
		var tenants []storage.Tenant
		ctx.DB.WithContext(reqCtx).Where("id IN ?", tenantIDs).Find(&tenants)
		for _, t := range tenants {
			tenantMap[t.ID] = t.Name
		}
	}

	// Build top platforms
	topPlatforms := make([]PlatformAnalytics, 0)
	for platform, count := range platformCounts {
		topPlatforms = append(topPlatforms, PlatformAnalytics{
			Platform:   platform,
			PostCount:  count,
			Engagement: platformEngagement[platform],
		})
	}

	// Build top tenants
	topTenants := make([]TenantAnalytics, 0)
	for tenantID, count := range tenantCounts {
		topTenants = append(topTenants, TenantAnalytics{
			TenantID:   tenantID,
			TenantName: tenantMap[tenantID],
			PostCount:  count,
			Engagement: tenantEngagement[tenantID],
		})
	}

	// Build recent posts details
	recentPosts := make([]PostAnalyticsDetail, 0, len(analytics))
	for _, a := range analytics {
		recentPosts = append(recentPosts, PostAnalyticsDetail{
			ID:         a.ID,
			TenantID:   a.TenantID,
			TenantName: tenantMap[a.TenantID],
			Platform:   a.Platform,
			PostedAt:   a.PostedAt,
			Likes:      int(a.Likes),
			Comments:   int(a.Comments),
			Shares:     int(a.Shares),
			Reaches:    int(a.Reach),
		})
	}

	return &AnalyticsResponse{
		TotalPosts:      totalPosts,
		TotalEngagement: totalEngagement,
		TopPlatforms:    topPlatforms,
		TopTenants:      topTenants,
		RecentPosts:     recentPosts,
	}, nil
}

// TenantAnalyticsAPIHandler returns analytics for a specific tenant
func TenantAnalyticsAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		tenantIDStr := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
		if tenantIDStr == "" {
			http.Error(w, "tenant_id required", http.StatusBadRequest)
			return
		}

		tenantID, err := uuid.Parse(tenantIDStr)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}

		// Get period filter
		period := strings.TrimSpace(r.URL.Query().Get("period"))
		startDate := strings.TrimSpace(r.URL.Query().Get("start_date"))
		endDate := strings.TrimSpace(r.URL.Query().Get("end_date"))

		if period == "" {
			period = "30d"
		}

		var since, until time.Time
		until = time.Now()

		switch period {
		case "24h":
			since = time.Now().Add(-24 * time.Hour)
		case "7d":
			since = time.Now().Add(-7 * 24 * time.Hour)
		case "30d":
			since = time.Now().Add(-30 * 24 * time.Hour)
		case "90d":
			since = time.Now().Add(-90 * 24 * time.Hour)
		case "custom":
			since = time.Now().Add(-30 * 24 * time.Hour) // Default
			if startDate != "" {
				if t, err := time.Parse("2006-01-02", startDate); err == nil {
					since = t
				}
			}
			if endDate != "" {
				if t, err := time.Parse("2006-01-02", endDate); err == nil {
					until = t.Add(24 * time.Hour).Add(-time.Nanosecond)
				}
			}
		default:
			since = time.Now().Add(-30 * 24 * time.Hour)
		}

		// Get tenant analytics
		var analytics []storage.PostAnalytics
		if err := ctx.DB.WithContext(reqCtx).
			Where("tenant_id = ? AND posted_at >= ? AND posted_at <= ?", tenantID, since, until).
			Order("posted_at DESC").
			Limit(1000).
			Find(&analytics).Error; err != nil {
			ctx.Logger.Error("failed to fetch tenant analytics", zap.Error(err))
			http.Error(w, "failed to fetch analytics", http.StatusInternalServerError)
			return
		}

		var totalEngagement int64
		platformCounts := make(map[string]int64)

		for _, a := range analytics {
			engagement := a.Likes + a.Comments + a.Shares + a.Reach
			totalEngagement += engagement
			platformCounts[a.Platform]++
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"tenant_id":        tenantID,
			"total_posts":      len(analytics),
			"total_engagement": totalEngagement,
			"platform_counts":  platformCounts,
			"posts":            analytics,
		})
	}
}
