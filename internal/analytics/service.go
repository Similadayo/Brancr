package analytics

import (
	"context"
	"fmt"
	"strings"
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
		TenantID:          tenantID,
		PeriodStart:       periodStart,
		PeriodEnd:         periodEnd,
		TotalPosts:        len(analytics),
		PlatformBreakdown: make(map[string]*interfaces.PlatformSummary),
		TopPosts:          []*interfaces.PostSummary{},
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

// AggregateCampaignAnalytics aggregates post analytics by campaign
func (s *Service) AggregateCampaignAnalytics(ctx context.Context, tenantID uint, startDate, endDate time.Time) error {
	// Fetch all posted posts in the period
	var posts []storage.ScheduledPost
	if err := s.DB.Where("tenant_id = ? AND status = ? AND posted_at >= ? AND posted_at <= ?",
		tenantID, "posted", startDate, endDate).Find(&posts).Error; err != nil {
		return fmt.Errorf("failed to fetch posts: %w", err)
	}

	// Group posts by campaign (from MediaAsset.Campaign)
	campaignMap := make(map[string][]uint) // campaign name -> post IDs

	for _, post := range posts {
		// Get campaign from MediaAsset - parse MediaAssetIDs array
		if post.MediaAssetIDs != "" && post.MediaAssetIDs != "[]" {
			// Parse JSON array format "[1,2,3]" - extract IDs
			idsStr := strings.Trim(post.MediaAssetIDs, "[]")
			idsParts := strings.Split(idsStr, ",")

			for _, idPart := range idsParts {
				idPart = strings.TrimSpace(idPart)
				var assetID uint
				if _, err := fmt.Sscanf(idPart, "%d", &assetID); err == nil {
					// Get campaign from asset
					var asset storage.MediaAsset
					if err := s.DB.Where("id = ? AND tenant_id = ?", assetID, tenantID).
						First(&asset).Error; err == nil && asset.Campaign != "" {
						campaignMap[asset.Campaign] = append(campaignMap[asset.Campaign], post.ID)
						break // Use first campaign found
					}
				}
			}
		}
	}

	// Aggregate analytics for each campaign
	for campaignName, postIDs := range campaignMap {
		if campaignName == "" {
			continue
		}

		// Get analytics for posts in this campaign
		var analytics []storage.PostAnalytics
		if err := s.DB.Where("tenant_id = ? AND scheduled_post_id IN ? AND posted_at >= ? AND posted_at <= ?",
			tenantID, postIDs, startDate, endDate).Find(&analytics).Error; err != nil {
			s.Logger.Warn("failed to fetch analytics for campaign", zap.String("campaign", campaignName), zap.Error(err))
			continue
		}

		if len(analytics) == 0 {
			continue
		}

		// Aggregate metrics
		campaignAnalytics := storage.CampaignAnalytics{
			TenantID:   tenantID,
			CampaignID: fmt.Sprintf("%d-%s", tenantID, campaignName),
			Name:       campaignName,
			TotalPosts: len(analytics),
			StartDate:  startDate,
			EndDate:    endDate,
		}

		var totalEngagement int64
		for _, a := range analytics {
			campaignAnalytics.TotalImpressions += a.Impressions
			campaignAnalytics.TotalReach += a.Reach
			campaignAnalytics.TotalLikes += a.Likes
			campaignAnalytics.TotalComments += a.Comments
			campaignAnalytics.TotalShares += a.Shares
			totalEngagement += a.Likes + a.Comments + a.Shares
			campaignAnalytics.AvgEngagementRate += a.EngagementRate
		}

		campaignAnalytics.TotalEngagement = totalEngagement
		if len(analytics) > 0 {
			campaignAnalytics.AvgEngagementRate = campaignAnalytics.AvgEngagementRate / float64(len(analytics))
		}

		// Upsert
		var existing storage.CampaignAnalytics
		err := s.DB.Where("tenant_id = ? AND campaign_id = ?", tenantID, campaignAnalytics.CampaignID).First(&existing).Error
		if err == gorm.ErrRecordNotFound {
			if err := s.DB.Create(&campaignAnalytics).Error; err != nil {
				s.Logger.Warn("failed to create campaign analytics", zap.String("campaign", campaignName), zap.Error(err))
			}
		} else if err == nil {
			campaignAnalytics.ID = existing.ID
			if err := s.DB.Save(&campaignAnalytics).Error; err != nil {
				s.Logger.Warn("failed to update campaign analytics", zap.String("campaign", campaignName), zap.Error(err))
			}
		}
	}

	return nil
}

// GeneratePlatformMetricsSnapshot creates daily snapshots of platform metrics
func (s *Service) GeneratePlatformMetricsSnapshot(ctx context.Context, tenantID uint, snapshotDate time.Time, periodType string) error {
	// Calculate period boundaries
	var periodStart, periodEnd time.Time
	switch periodType {
	case "daily":
		periodStart = time.Date(snapshotDate.Year(), snapshotDate.Month(), snapshotDate.Day(), 0, 0, 0, 0, snapshotDate.Location())
		periodEnd = periodStart.Add(24 * time.Hour)
	case "weekly":
		// Start of week (Monday)
		daysFromMonday := int(snapshotDate.Weekday()) - 1
		if daysFromMonday < 0 {
			daysFromMonday = 6
		}
		periodStart = time.Date(snapshotDate.Year(), snapshotDate.Month(), snapshotDate.Day(), 0, 0, 0, 0, snapshotDate.Location()).
			AddDate(0, 0, -daysFromMonday)
		periodEnd = periodStart.AddDate(0, 0, 7)
	case "monthly":
		periodStart = time.Date(snapshotDate.Year(), snapshotDate.Month(), 1, 0, 0, 0, 0, snapshotDate.Location())
		periodEnd = periodStart.AddDate(0, 1, 0)
	default:
		return fmt.Errorf("invalid period type: %s", periodType)
	}

	// Get all platforms for this tenant
	var accounts []storage.SocialAccount
	if err := s.DB.Where("tenant_id = ? AND connected = ?", tenantID, true).
		Distinct("platform").Find(&accounts).Error; err != nil {
		return fmt.Errorf("failed to fetch platforms: %w", err)
	}

	platforms := make(map[string]bool)
	for _, acc := range accounts {
		platforms[acc.Platform] = true
	}

	// Generate snapshot for each platform
	for platform := range platforms {
		// Fetch analytics for this platform and period
		var analytics []storage.PostAnalytics
		if err := s.DB.Where("tenant_id = ? AND platform = ? AND posted_at >= ? AND posted_at < ?",
			tenantID, platform, periodStart, periodEnd).Find(&analytics).Error; err != nil {
			s.Logger.Warn("failed to fetch analytics for snapshot",
				zap.String("platform", platform),
				zap.Error(err))
			continue
		}

		if len(analytics) == 0 {
			continue
		}

		// Aggregate metrics
		snapshot := storage.PlatformMetricsSnapshot{
			TenantID:     tenantID,
			Platform:     platform,
			TotalPosts:   len(analytics),
			SnapshotDate: snapshotDate,
			PeriodType:   periodType,
		}

		var bestPostID uint
		var bestEngagement int64

		for _, a := range analytics {
			snapshot.TotalImpressions += a.Impressions
			snapshot.TotalReach += a.Reach
			snapshot.TotalLikes += a.Likes
			snapshot.TotalComments += a.Comments
			snapshot.TotalShares += a.Shares
			snapshot.AvgEngagementRate += a.EngagementRate

			engagement := a.Likes + a.Comments + a.Shares
			if engagement > bestEngagement {
				bestEngagement = engagement
				bestPostID = a.ScheduledPostID
			}
		}

		snapshot.BestPostID = bestPostID
		snapshot.BestPostEngagement = bestEngagement
		if len(analytics) > 0 {
			snapshot.AvgEngagementRate = snapshot.AvgEngagementRate / float64(len(analytics))
		}

		// Upsert snapshot
		var existing storage.PlatformMetricsSnapshot
		err := s.DB.Where("tenant_id = ? AND platform = ? AND snapshot_date = ? AND period_type = ?",
			tenantID, platform, snapshotDate, periodType).First(&existing).Error

		if err == gorm.ErrRecordNotFound {
			// Create new
			if err := s.DB.Create(&snapshot).Error; err != nil {
				s.Logger.Warn("failed to create platform snapshot",
					zap.String("platform", platform),
					zap.Error(err))
			}
		} else if err == nil {
			// Update existing
			snapshot.ID = existing.ID
			if err := s.DB.Save(&snapshot).Error; err != nil {
				s.Logger.Warn("failed to update platform snapshot",
					zap.String("platform", platform),
					zap.Error(err))
			}
		}
	}

	return nil
}

// PopulateAudienceSegments creates audience segment records from post analytics demographics
// This is a placeholder that will need platform API integration for full demographics
func (s *Service) PopulateAudienceSegments(ctx context.Context, tenantID uint, periodStart, periodEnd time.Time) error {
	// Fetch analytics with demographics data
	var analytics []storage.PostAnalytics
	if err := s.DB.Where("tenant_id = ? AND posted_at >= ? AND posted_at <= ? AND demographics != ''",
		tenantID, periodStart, periodEnd).Find(&analytics).Error; err != nil {
		return fmt.Errorf("failed to fetch analytics: %w", err)
	}

	// Group by platform and aggregate demographics
	platformSegments := make(map[string]map[string]*storage.AudienceSegment)

	for _, a := range analytics {
		if a.Demographics == "" {
			continue
		}

		// Parse demographics JSON (placeholder - actual format depends on platform)
		// For now, we'll create a simple segment structure
		// In production, this would parse actual platform demographics JSON

		if platformSegments[a.Platform] == nil {
			platformSegments[a.Platform] = make(map[string]*storage.AudienceSegment)
		}

		// Create or update segment (simplified - would parse actual demographics)
		// This is a placeholder that shows the structure
		segmentKey := "unknown-unknown-unknown"
		if segment, exists := platformSegments[a.Platform][segmentKey]; exists {
			segment.Count++
			segment.Engagement += a.Likes + a.Comments + a.Shares
		} else {
			platformSegments[a.Platform][segmentKey] = &storage.AudienceSegment{
				TenantID:    tenantID,
				Platform:    a.Platform,
				AgeGroup:    "unknown",
				Gender:      "unknown",
				Location:    "unknown",
				Count:       1,
				Engagement:  a.Likes + a.Comments + a.Shares,
				PeriodStart: periodStart,
				PeriodEnd:   periodEnd,
			}
		}
	}

	// Save segments to database
	for _, segments := range platformSegments {
		for _, segment := range segments {
			// Calculate percentage (simplified - would need total audience)
			segment.Percentage = 100.0 // Placeholder

			// Upsert
			var existing storage.AudienceSegment
			err := s.DB.Where("tenant_id = ? AND platform = ? AND age_group = ? AND gender = ? AND location = ? AND period_start = ?",
				segment.TenantID, segment.Platform, segment.AgeGroup, segment.Gender, segment.Location, segment.PeriodStart).
				First(&existing).Error

			if err == gorm.ErrRecordNotFound {
				if err := s.DB.Create(segment).Error; err != nil {
					s.Logger.Warn("failed to create audience segment", zap.Error(err))
				}
			} else if err == nil {
				segment.ID = existing.ID
				if err := s.DB.Save(segment).Error; err != nil {
					s.Logger.Warn("failed to update audience segment", zap.Error(err))
				}
			}
		}
	}

	return nil
}
