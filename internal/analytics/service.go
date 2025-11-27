package analytics

import (
	"context"
	"fmt"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/pkg/interfaces"
	"go.uber.org/zap"
	"gorm.io/gorm"
)


// Service provides analytics operations
type Service struct {
	DB            *gorm.DB
	Logger        *zap.Logger
	Collector     *CollectorService
	TrendAnalyzer *TrendAnalyzer
	GrowthAdvisor *GrowthAdvisor
}

// NewService creates a new analytics service
func NewService(db *gorm.DB, logger *zap.Logger, collector *CollectorService, trendAnalyzer *TrendAnalyzer, growthAdvisor *GrowthAdvisor) *Service {
	return &Service{
		DB:            db,
		Logger:        logger,
		Collector:     collector,
		TrendAnalyzer: trendAnalyzer,
		GrowthAdvisor: growthAdvisor,
	}
}

// CollectPostMetrics fetches and stores metrics for a specific post
func (s *Service) CollectPostMetrics(ctx context.Context, tenantID uint, scheduledPostID uint, platform string, platformPostID string) error {
	return s.Collector.CollectPostMetrics(ctx, tenantID, scheduledPostID, platform, platformPostID)
}

// GetPostAnalytics retrieves stored analytics for a post
func (s *Service) GetPostAnalytics(ctx context.Context, tenantID uint, scheduledPostID uint, platform string) (*storage.PostAnalytics, error) {
	var analytics storage.PostAnalytics
	err := s.DB.Where("tenant_id = ? AND scheduled_post_id = ? AND platform = ?",
		tenantID, scheduledPostID, platform).First(&analytics).Error
	
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to fetch analytics: %w", err)
	}
	
	return &analytics, nil
}

// GetTenantAnalytics retrieves aggregated analytics for a tenant
func (s *Service) GetTenantAnalytics(ctx context.Context, tenantID uint, periodStart, periodEnd time.Time) (*interfaces.TenantAnalyticsSummary, error) {
	// Fetch all post analytics for the period
	var analytics []storage.PostAnalytics
	if err := s.DB.Where("tenant_id = ? AND posted_at >= ? AND posted_at <= ?",
		tenantID, periodStart, periodEnd).Find(&analytics).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch analytics: %w", err)
	}

	summary := &interfaces.TenantAnalyticsSummary{
		TenantID:      tenantID,
		PeriodStart:   periodStart,
		PeriodEnd:     periodEnd,
		TotalPosts:    len(analytics),
		PlatformBreakdown: make(map[string]*interfaces.PlatformSummary),
		TopPosts:      []*interfaces.PostSummary{},
	}

	// Aggregate metrics
	platformStats := make(map[string]*interfaces.PlatformSummary)
	
	for _, a := range analytics {
		// Update totals
		summary.TotalImpressions += a.Impressions
		summary.TotalReach += a.Reach
		summary.TotalLikes += a.Likes
		summary.TotalComments += a.Comments
		summary.TotalShares += a.Shares

		// Platform breakdown
		if platformStats[a.Platform] == nil {
			platformStats[a.Platform] = &interfaces.PlatformSummary{
				Platform: a.Platform,
			}
		}
		ps := platformStats[a.Platform]
		ps.TotalPosts++
		ps.TotalImpressions += a.Impressions
		ps.TotalReach += a.Reach
		ps.TotalLikes += a.Likes
		ps.TotalComments += a.Comments
		ps.TotalShares += a.Shares
		ps.AvgEngagementRate += a.EngagementRate

		// Track best post
		engagement := a.Likes + a.Comments + a.Shares
		if engagement > ps.BestPostEngagement {
			ps.BestPostEngagement = engagement
			ps.BestPostID = a.ScheduledPostID
		}
	}

	// Calculate averages
	if summary.TotalReach > 0 {
		totalEngagement := float64(summary.TotalLikes + summary.TotalComments + summary.TotalShares)
		summary.AvgEngagementRate = (totalEngagement / float64(summary.TotalReach)) * 100.0
	}

	// Calculate platform averages
	for platform, ps := range platformStats {
		if ps.TotalPosts > 0 {
			ps.AvgEngagementRate = ps.AvgEngagementRate / float64(ps.TotalPosts)
		}
		summary.PlatformBreakdown[platform] = ps
	}

	// Get top posts (by engagement rate)
	topPosts := make([]*interfaces.PostSummary, 0, 10)
	for _, a := range analytics {
		postSummary := &interfaces.PostSummary{
			ScheduledPostID: a.ScheduledPostID,
			Platform:        a.Platform,
			PlatformPostID:  a.PlatformPostID,
			Impressions:     a.Impressions,
			Reach:           a.Reach,
			Likes:           a.Likes,
			Comments:        a.Comments,
			EngagementRate:  a.EngagementRate,
			PostedAt:        a.PostedAt,
		}
		topPosts = append(topPosts, postSummary)
	}

	// Sort by engagement rate (simplified - would use proper sorting)
	if len(topPosts) > 10 {
		topPosts = topPosts[:10]
	}
	summary.TopPosts = topPosts

	// Fetch trend insights
	var insights []storage.TrendInsight
	if err := s.DB.Where("tenant_id = ? AND period_start >= ? AND period_end <= ? AND status = ?",
		tenantID, periodStart, periodEnd, "active").Find(&insights).Error; err == nil {
		// Convert to pointers
		insightPtrs := make([]*storage.TrendInsight, len(insights))
		for i := range insights {
			insightPtrs[i] = &insights[i]
		}
		summary.TrendInsights = insightPtrs
	}

	return summary, nil
}

// GenerateInsights generates trend insights for a tenant
func (s *Service) GenerateInsights(ctx context.Context, tenantID uint, periodStart, periodEnd time.Time) ([]*storage.TrendInsight, error) {
	return s.TrendAnalyzer.GenerateTrendInsights(ctx, tenantID, periodStart, periodEnd)
}

// CollectMetricsForTenant collects metrics for all posts for a tenant
func (s *Service) CollectMetricsForTenant(ctx context.Context, tenantID uint, daysBack int) error {
	return s.Collector.CollectAllPostMetrics(ctx, tenantID, daysBack)
}

// GenerateGrowthSuggestions generates business growth suggestions
func (s *Service) GenerateGrowthSuggestions(ctx context.Context, tenantID uint, periodStart, periodEnd time.Time) ([]*BusinessGrowthSuggestion, error) {
	if s.GrowthAdvisor == nil {
		return nil, fmt.Errorf("growth advisor not initialized")
	}
	return s.GrowthAdvisor.GenerateGrowthSuggestions(ctx, tenantID, periodStart, periodEnd)
}

