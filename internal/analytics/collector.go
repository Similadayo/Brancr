package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
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

	// Convert demographics and metadata to JSON strings
	demographicsJSON := "{}"
	if metrics.Demographics != nil && len(metrics.Demographics) > 0 {
		if jsonBytes, err := json.Marshal(metrics.Demographics); err == nil {
			demographicsJSON = string(jsonBytes)
		}
	}

	metadataJSON := "{}"
	if metrics.Metadata != nil && len(metrics.Metadata) > 0 {
		if jsonBytes, err := json.Marshal(metrics.Metadata); err == nil {
			metadataJSON = string(jsonBytes)
		}
	}

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
		Demographics:    demographicsJSON,
		Metadata:        metadataJSON,
	}

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
			// Skip TikTok analytics collection
			if platform == "tiktok" {
				c.Logger.Debug("Skipping TikTok analytics collection",
					zap.Uint("post_id", post.ID),
					zap.String("platform", platform))
				continue
			}

			// Get platform post ID from PostLog or metadata
			platformPostID := c.getPlatformPostID(tenantID, post.ID, platform)
			if platformPostID == "" {
				// Try to retrieve from Instagram API if it's a recent post (within 24 hours)
				if platform == "instagram" && post.PostedAt != nil {
					platformPostID = c.tryRetrieveInstagramPostIDFromAPI(tenantID, post.ID, post.PostedAt)
					if platformPostID != "" {
						// Store it for future use
						c.storePlatformPostIDInPostLog(tenantID, post.ID, platform, platformPostID)
					}
				}
				
				if platformPostID == "" {
					c.Logger.Warn("No platform post ID found, skipping",
						zap.Uint("post_id", post.ID),
						zap.String("platform", platform),
						zap.Time("posted_at", *post.PostedAt))
					continue
				}
			}

			// Collect metrics (with error handling to continue on individual failures)
			if err := c.CollectPostMetrics(ctx, tenantID, post.ID, platform, platformPostID); err != nil {
				c.Logger.Warn("Failed to collect metrics for post",
					zap.Uint("post_id", post.ID),
					zap.String("platform", platform),
					zap.String("platform_post_id", platformPostID),
					zap.Error(err),
					zap.String("error_type", fmt.Sprintf("%T", err)))
				
				// If it's a 400 error, it might be due to invalid post ID or permissions
				if strings.Contains(err.Error(), "status 400") {
					c.Logger.Warn("Instagram API returned 400 - possible causes: invalid post ID, missing permissions, or post too old",
						zap.Uint("post_id", post.ID),
						zap.String("platform_post_id", platformPostID),
						zap.String("platform", platform))
				}
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

// getPlatformPostID retrieves the platform-specific post ID with multiple fallback strategies
func (c *CollectorService) getPlatformPostID(tenantID, scheduledPostID uint, platform string) string {
	// Strategy 1: Check PostLog for platform post ID in metadata (most recent first)
	var postLogs []storage.PostLog
	if err := c.DB.Where("tenant_id = ? AND scheduled_post_id = ? AND platform = ?",
		tenantID, scheduledPostID, platform).
		Order("created_at DESC").
		Find(&postLogs).Error; err == nil {
		for _, postLog := range postLogs {
			// Try to extract from metadata
			if postLog.Metadata != "" && postLog.Metadata != "{}" {
				var metadata map[string]interface{}
				if err := json.Unmarshal([]byte(postLog.Metadata), &metadata); err == nil {
					if platformPostID, ok := metadata["platform_post_id"].(string); ok && platformPostID != "" {
						c.Logger.Debug("Found platform post ID in PostLog metadata",
							zap.Uint("post_id", scheduledPostID),
							zap.String("platform", platform),
							zap.String("platform_post_id", platformPostID))
						return platformPostID
					}
				}
			}
			// Also check ErrorMessage or other fields (some connectors might log it there)
			if postLog.ErrorMessage != "" && strings.Contains(postLog.ErrorMessage, "media_id:") {
				// Extract from error message as fallback
				if idx := strings.Index(postLog.ErrorMessage, "media_id: "); idx >= 0 {
					parts := strings.Fields(postLog.ErrorMessage[idx+10:])
					if len(parts) > 0 {
						id := strings.Trim(parts[0], "()")
						if id != "" {
							c.Logger.Debug("Found platform post ID in PostLog error message",
								zap.Uint("post_id", scheduledPostID),
								zap.String("platform", platform),
								zap.String("platform_post_id", id))
							return id
						}
					}
				}
			}
		}
	}

	// Strategy 2: Check if there's already a PostAnalytics entry with the platform post ID
	var existingAnalytics storage.PostAnalytics
	if err := c.DB.Where("tenant_id = ? AND scheduled_post_id = ? AND platform = ?",
		tenantID, scheduledPostID, platform).
		First(&existingAnalytics).Error; err == nil {
		if existingAnalytics.PlatformPostID != "" {
			c.Logger.Debug("Found platform post ID from existing analytics",
				zap.Uint("post_id", scheduledPostID),
				zap.String("platform", platform),
				zap.String("platform_post_id", existingAnalytics.PlatformPostID))
			return existingAnalytics.PlatformPostID
		}
	}

	// Strategy 3: For Instagram, try to retrieve from Instagram API (requires post to be recent)
	// Note: This strategy requires PostedAt time, so it's handled in CollectAllPostMetrics
	// where we have access to the ScheduledPost with PostedAt field

	c.Logger.Warn("No platform post ID found for scheduled post",
		zap.Uint("tenant_id", tenantID),
		zap.Uint("scheduled_post_id", scheduledPostID),
		zap.String("platform", platform))
	
	return ""
}

// tryRetrieveInstagramPostIDFromAPI attempts to retrieve the Instagram post ID by querying recent posts from Instagram API
func (c *CollectorService) tryRetrieveInstagramPostIDFromAPI(tenantID, scheduledPostID uint, postedAt *time.Time) string {
	if postedAt == nil {
		return ""
	}

	// Only try if post is recent (within last 7 days - Instagram API allows fetching recent media)
	if time.Since(*postedAt) > 7*24*time.Hour {
		return ""
	}

	// Get the social account
	var account storage.SocialAccount
	if err := c.DB.Where("tenant_id = ? AND platform = ? AND connected = ?", tenantID, "instagram", true).
		First(&account).Error; err != nil {
		c.Logger.Debug("No Instagram account found for tenant", zap.Uint("tenant_id", tenantID))
		return ""
	}

	// Decrypt access token if needed
	accessToken := account.AccessToken
	if accessToken == "" {
		return ""
	}
	
	// Try to decrypt token (might be encrypted)
	if decrypted, err := utils.DecryptToken(accessToken); err == nil {
		accessToken = decrypted
	} else {
		// Token might not be encrypted, use as-is
		accessToken = account.AccessToken
	}

	// Get Instagram Business Account ID
	var instagramAccountID string
	if account.PageID != "" && !strings.Contains(account.PageID, "_") {
		instagramAccountID = account.PageID
	} else if account.ExternalID != "" {
		instagramAccountID = account.ExternalID
	} else {
		c.Logger.Debug("No Instagram account ID found", zap.Uint("tenant_id", tenantID))
		return ""
	}

	// Query Instagram API for recent media
	// Get posts from the last 7 days, match by timestamp (within 5 minute window)
	ctx := context.Background()
	client := &http.Client{Timeout: 10 * time.Second}
	
	// Fetch recent media from Instagram
	url := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/media?fields=id,timestamp,caption&limit=25&access_token=%s",
		instagramAccountID, accessToken)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return ""
	}

	resp, err := client.Do(req)
	if err != nil {
		c.Logger.Debug("Failed to query Instagram API for recent posts", zap.Error(err))
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		c.Logger.Debug("Instagram API returned non-200 status", zap.Int("status", resp.StatusCode))
		return ""
	}

	var apiResponse struct {
		Data []struct {
			ID        string `json:"id"`
			Timestamp string `json:"timestamp"`
			Caption   string `json:"caption"`
		} `json:"data"`
		Paging struct {
			Next string `json:"next"`
		} `json:"paging"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
		c.Logger.Debug("Failed to decode Instagram API response", zap.Error(err))
		return ""
	}

	// Match posts by timestamp (within 5 minute window)
	targetTime := postedAt.Unix()
	for _, media := range apiResponse.Data {
		if media.Timestamp == "" {
			continue
		}
		
		// Parse Instagram timestamp (ISO 8601 format)
		postTime, err := time.Parse(time.RFC3339, media.Timestamp)
		if err != nil {
			// Try alternative format
			postTime, err = time.Parse("2006-01-02T15:04:05+0000", media.Timestamp)
			if err != nil {
				continue
			}
		}
		
		// Check if timestamp matches (within 5 minutes)
		timeDiff := abs(postTime.Unix() - targetTime)
		if timeDiff <= 300 { // 5 minutes = 300 seconds
			c.Logger.Info("Matched Instagram post by timestamp",
				zap.Uint("scheduled_post_id", scheduledPostID),
				zap.String("instagram_media_id", media.ID),
				zap.Int64("time_diff_seconds", timeDiff))
			return media.ID
		}
	}

	c.Logger.Debug("No matching Instagram post found for scheduled post",
		zap.Uint("scheduled_post_id", scheduledPostID),
		zap.Time("posted_at", *postedAt))
	
	return ""
}

// abs returns absolute value of an int64
func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// storePlatformPostIDInPostLog stores the platform post ID in PostLog metadata for future use
func (c *CollectorService) storePlatformPostIDInPostLog(tenantID, scheduledPostID uint, platform, platformPostID string) {
	var postLog storage.PostLog
	if err := c.DB.Where("tenant_id = ? AND scheduled_post_id = ? AND platform = ? AND action = ?",
		tenantID, scheduledPostID, platform, "posted").
		Order("created_at DESC").
		First(&postLog).Error; err != nil {
		// No PostLog found - skip
		return
	}

	// Update metadata with platform post ID
	metadata := map[string]interface{}{}
	if postLog.Metadata != "" && postLog.Metadata != "{}" {
		json.Unmarshal([]byte(postLog.Metadata), &metadata)
	}
	metadata["platform_post_id"] = platformPostID
	
	if metadataJSON, err := json.Marshal(metadata); err == nil {
		c.DB.Model(&postLog).Update("metadata", string(metadataJSON))
		c.Logger.Info("Stored platform post ID in PostLog metadata",
			zap.Uint("post_id", scheduledPostID),
			zap.String("platform", platform),
			zap.String("platform_post_id", platformPostID))
	}
}

