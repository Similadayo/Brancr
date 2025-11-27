package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/pkg/interfaces"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// CollectorService handles collection of analytics data from all platforms
type CollectorService struct {
	DB          *gorm.DB
	Logger      *zap.Logger
	Clients     map[string]interfaces.PlatformAnalyticsClient // platform -> client
}

// NewCollectorService creates a new analytics collector
func NewCollectorService(db *gorm.DB, logger *zap.Logger) *CollectorService {
	return &CollectorService{
		DB:      db,
		Logger:  logger,
		Clients: make(map[string]interfaces.PlatformAnalyticsClient),
	}
}

// RegisterClient registers a platform analytics client
func (c *CollectorService) RegisterClient(client interfaces.PlatformAnalyticsClient) {
	c.Clients[client.Platform()] = client
	c.Logger.Info("Registered analytics client", zap.String("platform", client.Platform()))
}

// CollectPostMetrics fetches and stores metrics for a specific post
func (c *CollectorService) CollectPostMetrics(ctx context.Context, tenantID uint, scheduledPostID uint, platform string, platformPostID string) error {
	// Get the social account for this tenant and platform
	var account storage.SocialAccount
	if err := c.DB.Where("tenant_id = ? AND platform = ? AND connected = ?", tenantID, platform, true).First(&account).Error; err != nil {
		return fmt.Errorf("no connected account found for platform %s: %w", platform, err)
	}

	// Get the analytics client for this platform
	client, ok := c.Clients[platform]
	if !ok {
		return fmt.Errorf("no analytics client registered for platform %s", platform)
	}

	// Fetch metrics from platform API
	periodEnd := time.Now()
	periodStart := periodEnd.AddDate(0, 0, -30) // Last 30 days
	
	metrics, err := client.FetchPostMetrics(ctx, &account, platformPostID, periodStart, periodEnd)
	if err != nil {
		return fmt.Errorf("failed to fetch metrics from %s: %w", platform, err)
	}

	// Calculate engagement rate
	engagementRate := c.calculateEngagementRate(metrics)

	// Store or update analytics
	analytics := storage.PostAnalytics{
		TenantID:        tenantID,
		ScheduledPostID: scheduledPostID,
		Platform:        platform,
		PlatformPostID:  platformPostID,
		Impressions:     metrics.Impressions,
		Reach:           metrics.Reach,
		Likes:           metrics.Likes,
		Comments:        metrics.Comments,
		Saves:           metrics.Saves,
		Shares:          metrics.Shares,
		Reactions:       metrics.Reactions,
		WatchTime:       metrics.WatchTime,
		Views:           metrics.Views,
		ClickThrough:    metrics.ClickThrough,
		EngagementRate:  engagementRate,
		PostedAt:        metrics.PostedAt,
		CollectedAt:     time.Now(),
		PeriodStart:     metrics.PeriodStart,
		PeriodEnd:       metrics.PeriodEnd,
	}

	// Convert demographics and metadata to JSON strings (simplified - would use proper JSON encoding)
	// For now, we'll store empty strings and implement proper JSON encoding later

	// Upsert analytics (update if exists, create if not)
	var existing storage.PostAnalytics
	err = c.DB.Where("tenant_id = ? AND scheduled_post_id = ? AND platform = ? AND platform_post_id = ?",
		tenantID, scheduledPostID, platform, platformPostID).First(&existing).Error
	
	if err == gorm.ErrRecordNotFound {
		// Create new
		if err := c.DB.Create(&analytics).Error; err != nil {
			return fmt.Errorf("failed to create analytics: %w", err)
		}
		c.Logger.Info("Created post analytics",
			zap.Uint("tenant_id", tenantID),
			zap.Uint("post_id", scheduledPostID),
			zap.String("platform", platform))
	} else if err == nil {
		// Update existing
		analytics.ID = existing.ID
		if err := c.DB.Save(&analytics).Error; err != nil {
			return fmt.Errorf("failed to update analytics: %w", err)
		}
		c.Logger.Info("Updated post analytics",
			zap.Uint("tenant_id", tenantID),
			zap.Uint("post_id", scheduledPostID),
			zap.String("platform", platform))
	} else {
		return fmt.Errorf("database error: %w", err)
	}

	return nil
}

// SetTrendAnalyzer sets the trend analyzer for automatic insights generation
func (c *CollectorService) SetTrendAnalyzer(trendAnalyzer interface{}) {
	// This will be used for automatic insights generation
	// Implementation will trigger insights after collecting enough data
}

// CollectAllPostMetrics collects metrics for all posted content for a tenant
func (c *CollectorService) CollectAllPostMetrics(ctx context.Context, tenantID uint, daysBack int) error {
	// Get all posted scheduled posts for this tenant
	var posts []storage.ScheduledPost
	cutoffDate := time.Now().AddDate(0, 0, -daysBack)
	
	if err := c.DB.Where("tenant_id = ? AND status = ? AND posted_at >= ?", tenantID, "posted", cutoffDate).
		Find(&posts).Error; err != nil {
		return fmt.Errorf("failed to fetch posts: %w", err)
	}

	c.Logger.Info("Collecting metrics for posts",
		zap.Uint("tenant_id", tenantID),
		zap.Int("post_count", len(posts)))

	// Collect metrics for each post
	for _, post := range posts {
		// Parse platforms from JSON string
		platforms := c.parsePlatforms(post.Platforms)
		
		for _, platform := range platforms {
			// Get platform post ID from PostLog or metadata
			platformPostID := c.getPlatformPostID(tenantID, post.ID, platform)
			if platformPostID == "" {
				c.Logger.Warn("No platform post ID found, skipping",
					zap.Uint("post_id", post.ID),
					zap.String("platform", platform))
				continue
			}

			// Collect metrics (with error handling to continue on individual failures)
			if err := c.CollectPostMetrics(ctx, tenantID, post.ID, platform, platformPostID); err != nil {
				c.Logger.Warn("Failed to collect metrics for post",
					zap.Uint("post_id", post.ID),
					zap.String("platform", platform),
					zap.Error(err))
				continue
			}
		}
	}

	return nil
}

// calculateEngagementRate calculates engagement rate from metrics
func (c *CollectorService) calculateEngagementRate(metrics *interfaces.PostMetrics) float64 {
	if metrics.Reach == 0 {
		return 0.0
	}
	engagement := float64(metrics.Likes + metrics.Comments + metrics.Shares + metrics.Saves)
	return (engagement / float64(metrics.Reach)) * 100.0
}

// parsePlatforms parses platforms JSON string to slice
func (c *CollectorService) parsePlatforms(platformsJSON string) []string {
	if platformsJSON == "" {
		return []string{}
	}
	var platforms []string
	if err := json.Unmarshal([]byte(platformsJSON), &platforms); err != nil {
		c.Logger.Warn("failed to parse platforms JSON", zap.Error(err))
		return []string{}
	}
	return platforms
}

// getPlatformPostID retrieves the platform-specific post ID
func (c *CollectorService) getPlatformPostID(tenantID, scheduledPostID uint, platform string) string {
	// Check PostLog for platform post ID in metadata
	var postLog storage.PostLog
	if err := c.DB.Where("tenant_id = ? AND scheduled_post_id = ? AND platform = ? AND action = ?",
		tenantID, scheduledPostID, platform, "posted").
		Order("created_at DESC").First(&postLog).Error; err == nil {
		// Extract platform post ID from metadata
		if postLog.Metadata != "" {
			var metadata map[string]interface{}
			if err := json.Unmarshal([]byte(postLog.Metadata), &metadata); err == nil {
				if platformPostID, ok := metadata["platform_post_id"].(string); ok && platformPostID != "" {
					return platformPostID
				}
			}
		}
	}
	return ""
}

