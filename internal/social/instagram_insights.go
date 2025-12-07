package social

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// InstagramInsightsService handles fetching and storing Instagram insights
type InstagramInsightsService struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

// NewInstagramInsightsService creates a new Instagram insights service
func NewInstagramInsightsService(db *gorm.DB, logger *zap.Logger) *InstagramInsightsService {
	return &InstagramInsightsService{
		DB:     db,
		Logger: logger,
	}
}

// AccountInsightMetric represents a single account insight metric
type AccountInsightMetric struct {
	Name        string    `json:"name"`
	Period      string    `json:"period"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	ID          string    `json:"id"`
	Values      []struct {
		Value   int64  `json:"value"`
		EndTime string `json:"end_time"`
	} `json:"values"`
}

// MediaInsightMetric represents a single media insight metric
type MediaInsightMetric struct {
	Name        string    `json:"name"`
	Period      string    `json:"period"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	ID          string    `json:"id"`
	Values      []struct {
		Value int64 `json:"value"`
	} `json:"values"`
}

// AccountInsightsResponse represents the response from Instagram account insights API
type AccountInsightsResponse struct {
	Data []AccountInsightMetric `json:"data"`
}

// MediaInsightsResponse represents the response from Instagram media insights API
type MediaInsightsResponse struct {
	Data []MediaInsightMetric `json:"data"`
}

// GetAccountInsights fetches account-level insights for an Instagram account
// Valid account-level metrics (for BOTH Instagram Login and Facebook Login):
// reach, follower_count, website_clicks, profile_views, online_followers,
// accounts_engaged, total_interactions, likes, comments, shares, saves, replies
// NOTE: "impressions" was REMOVED from account insights (only available for media insights)
// NOTE: Do NOT mix account-level and media-level metrics
// period: day, week, days_28, lifetime
func (s *InstagramInsightsService) GetAccountInsights(ctx context.Context, account *storage.SocialAccount, metrics []string, period string) (*AccountInsightsResponse, error) {
	if account.AccessToken == "" {
		return nil, fmt.Errorf("access token missing")
	}

	// Decrypt token
	decryptedToken, err := utils.DecryptToken(account.AccessToken)
	if err != nil {
		decryptedToken = account.AccessToken // Fallback to plain text
	}

	// Get Instagram account ID
	igAccountID := account.ExternalID
	if igAccountID == "" {
		igAccountID = account.PageID
	}
	if igAccountID == "" {
		return nil, fmt.Errorf("Instagram account ID not found")
	}

	// Determine API base URL based on login method
	var apiBase string
	if account.LoginMethod == "instagram_login" {
		apiBase = "https://graph.instagram.com/v24.0"
	} else {
		apiBase = "https://graph.facebook.com/v18.0"
	}

	// Build URL
	endpoint := fmt.Sprintf("%s/%s/insights", apiBase, igAccountID)
	params := url.Values{}
	params.Set("metric", strings.Join(metrics, ","))
	params.Set("period", period)
	params.Set("access_token", decryptedToken)

	fullURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())

	// Make request
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch account insights: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Instagram API error %d: %s", resp.StatusCode, string(body))
	}

	var insights AccountInsightsResponse
	if err := json.NewDecoder(resp.Body).Decode(&insights); err != nil {
		return nil, fmt.Errorf("failed to parse insights response: %w", err)
	}

	s.Logger.Info("✅ Fetched Instagram account insights",
		zap.String("account_id", igAccountID),
		zap.Strings("metrics", metrics),
		zap.String("period", period),
		zap.Int("metrics_count", len(insights.Data)))

	return &insights, nil
}

// GetMediaInsights fetches media-level insights for a specific Instagram post
// metrics: comma-separated list (e.g., "engagement,impressions,reach")
// period: day, week, days_28, lifetime (for media, lifetime is most common)
func (s *InstagramInsightsService) GetMediaInsights(ctx context.Context, account *storage.SocialAccount, mediaID string, metrics []string, period string) (*MediaInsightsResponse, error) {
	if account.AccessToken == "" {
		return nil, fmt.Errorf("access token missing")
	}

	// Decrypt token
	decryptedToken, err := utils.DecryptToken(account.AccessToken)
	if err != nil {
		decryptedToken = account.AccessToken // Fallback to plain text
	}

	// Determine API base URL based on login method
	var apiBase string
	if account.LoginMethod == "instagram_login" {
		apiBase = "https://graph.instagram.com/v24.0"
	} else {
		apiBase = "https://graph.facebook.com/v18.0"
	}

	// Build URL
	endpoint := fmt.Sprintf("%s/%s/insights", apiBase, mediaID)
	params := url.Values{}
	params.Set("metric", strings.Join(metrics, ","))
	if period != "" {
		params.Set("period", period)
	}
	params.Set("access_token", decryptedToken)

	fullURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())

	// Make request
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch media insights: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Instagram API error %d: %s", resp.StatusCode, string(body))
	}

	var insights MediaInsightsResponse
	if err := json.NewDecoder(resp.Body).Decode(&insights); err != nil {
		return nil, fmt.Errorf("failed to parse insights response: %w", err)
	}

	s.Logger.Info("✅ Fetched Instagram media insights",
		zap.String("media_id", mediaID),
		zap.Strings("metrics", metrics),
		zap.String("period", period),
		zap.Int("metrics_count", len(insights.Data)))

	return &insights, nil
}

// SaveAccountInsights saves account insights to the database
// Valid account-level metrics: reach, follower_count, profile_views, total_interactions, etc.
// NOTE: "impressions" was REMOVED from account insights (only available for media insights)
// NOTE: Do NOT mix account-level and media-level metrics
func (s *InstagramInsightsService) SaveAccountInsights(ctx context.Context, tenantID uint, accountID string, insights *AccountInsightsResponse) error {
	// Store insights in PlatformMetricsSnapshot
	for _, metric := range insights.Data {
		// Process each value in the metric
		for _, value := range metric.Values {
			var endTime time.Time
			if value.EndTime != "" {
				parsed, err := time.Parse("2006-01-02T15:04:05-0700", value.EndTime)
				if err != nil {
					// Try alternative format
					parsed, err = time.Parse(time.RFC3339, value.EndTime)
					if err != nil {
						s.Logger.Warn("failed to parse end_time", zap.String("end_time", value.EndTime), zap.Error(err))
						endTime = time.Now()
					} else {
						endTime = parsed
					}
				} else {
					endTime = parsed
				}
			} else {
				endTime = time.Now()
			}

			// Find or create snapshot
			var snapshot storage.PlatformMetricsSnapshot
			err := s.DB.WithContext(ctx).
				Where("tenant_id = ? AND platform = ? AND snapshot_date = ? AND period_type = ?",
					tenantID, "instagram", endTime.Format("2006-01-02"), metric.Period).
				First(&snapshot).Error

			if err == gorm.ErrRecordNotFound {
				// Create new snapshot
				snapshot = storage.PlatformMetricsSnapshot{
					TenantID:   tenantID,
					Platform:   "instagram",
					SnapshotDate: endTime,
					PeriodType: metric.Period,
				}
			}

			// Update metrics based on metric name
			// Valid account-level metrics: reach, follower_count, profile_views, total_interactions, etc.
			// NOTE: "impressions" was REMOVED from account insights (only available for media insights)
			// NOTE: Do NOT mix account-level and media-level metrics
			switch metric.Name {
			case "reach":
				snapshot.TotalReach = value.Value
			case "profile_views":
				// Store in metadata as it's not a standard field
				if snapshot.Metadata == "" {
					snapshot.Metadata = "{}"
				}
				var metadata map[string]interface{}
				json.Unmarshal([]byte(snapshot.Metadata), &metadata)
				if metadata == nil {
					metadata = make(map[string]interface{})
				}
				metadata["profile_views"] = value.Value
				metadataBytes, _ := json.Marshal(metadata)
				snapshot.Metadata = string(metadataBytes)
			case "follower_count":
				// Store follower count in metadata
				if snapshot.Metadata == "" {
					snapshot.Metadata = "{}"
				}
				var metadata map[string]interface{}
				json.Unmarshal([]byte(snapshot.Metadata), &metadata)
				if metadata == nil {
					metadata = make(map[string]interface{})
				}
				metadata["follower_count"] = value.Value
				metadataBytes, _ := json.Marshal(metadata)
				snapshot.Metadata = string(metadataBytes)
			case "total_interactions":
				// Store total interactions in metadata
				if snapshot.Metadata == "" {
					snapshot.Metadata = "{}"
				}
				var metadata map[string]interface{}
				json.Unmarshal([]byte(snapshot.Metadata), &metadata)
				if metadata == nil {
					metadata = make(map[string]interface{})
				}
				metadata["total_interactions"] = value.Value
				metadataBytes, _ := json.Marshal(metadata)
				snapshot.Metadata = string(metadataBytes)
			}

			if err == gorm.ErrRecordNotFound {
				if err := s.DB.WithContext(ctx).Create(&snapshot).Error; err != nil {
					s.Logger.Error("failed to create platform metrics snapshot", zap.Error(err))
				}
			} else {
				if err := s.DB.WithContext(ctx).Save(&snapshot).Error; err != nil {
					s.Logger.Error("failed to update platform metrics snapshot", zap.Error(err))
				}
			}
		}
	}

	return nil
}

// SaveMediaInsights saves media insights to PostAnalytics
func (s *InstagramInsightsService) SaveMediaInsights(ctx context.Context, tenantID uint, mediaID string, insights *MediaInsightsResponse) error {
	// Find existing PostAnalytics record
	var analytics storage.PostAnalytics
	err := s.DB.WithContext(ctx).
		Where("tenant_id = ? AND platform = ? AND platform_post_id = ?",
			tenantID, "instagram", mediaID).
		First(&analytics).Error

	if err == gorm.ErrRecordNotFound {
		// Create new analytics record
		analytics = storage.PostAnalytics{
			TenantID:      tenantID,
			Platform:      "instagram",
			PlatformPostID: mediaID,
			CollectedAt:   time.Now(),
		}
	} else {
		analytics.CollectedAt = time.Now()
	}

	// Update metrics based on insight data
	for _, metric := range insights.Data {
		switch metric.Name {
		case "engagement":
			// Engagement = likes + comments
			// We'll need to parse this or get likes/comments separately
			// For now, store in metadata
			if analytics.Metadata == "" {
				analytics.Metadata = "{}"
			}
			var metadata map[string]interface{}
			json.Unmarshal([]byte(analytics.Metadata), &metadata)
			if metadata == nil {
				metadata = make(map[string]interface{})
			}
			if len(metric.Values) > 0 {
				metadata["engagement"] = metric.Values[0].Value
			}
			metadataBytes, _ := json.Marshal(metadata)
			analytics.Metadata = string(metadataBytes)
		case "impressions":
			if len(metric.Values) > 0 {
				analytics.Impressions = metric.Values[0].Value
			}
		case "reach":
			if len(metric.Values) > 0 {
				analytics.Reach = metric.Values[0].Value
			}
		case "likes":
			if len(metric.Values) > 0 {
				analytics.Likes = metric.Values[0].Value
			}
		case "comments":
			if len(metric.Values) > 0 {
				analytics.Comments = metric.Values[0].Value
			}
		case "saved":
			if len(metric.Values) > 0 {
				analytics.Saves = metric.Values[0].Value
			}
		}
	}

	// Calculate engagement rate if we have reach
	if analytics.Reach > 0 {
		totalEngagement := analytics.Likes + analytics.Comments + analytics.Shares
		analytics.EngagementRate = float64(totalEngagement) / float64(analytics.Reach) * 100
	}

	if err == gorm.ErrRecordNotFound {
		if err := s.DB.WithContext(ctx).Create(&analytics).Error; err != nil {
			return fmt.Errorf("failed to create post analytics: %w", err)
		}
	} else {
		if err := s.DB.WithContext(ctx).Save(&analytics).Error; err != nil {
			return fmt.Errorf("failed to update post analytics: %w", err)
		}
	}

	return nil
}

