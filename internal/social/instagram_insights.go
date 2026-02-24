package social

import (
	"github.com/google/uuid"
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

// BusinessAllowedMetrics are valid metrics for Facebook Login API (graph.facebook.com)
// Based on actual Instagram API error messages - these are the valid account-level metrics
// NOTE: "impressions" is NOT valid for account insights (only for media insights)
var BusinessAllowedMetrics = map[string]bool{
	"reach":              true,
	"follower_count":     true,
	"website_clicks":     true,
	"profile_views":      true,
	"online_followers":   true,
	"accounts_engaged":   true,
	"total_interactions": true,
	"likes":              true,
	"comments":           true,
	"shares":             true,
	"saves":              true,
	"replies":            true,
	// "impressions": false, // NOT valid for account insights (only for media)
}

// InstagramLoginMetrics are metrics allowed for Instagram Login API (graph.instagram.com)
// NOTE: "impressions" is NOT valid for account insights (only for media insights)
// Based on actual Instagram API error messages, these are the valid account-level metrics
var InstagramLoginMetrics = map[string]bool{
	"reach":              true,
	"follower_count":     true,
	"website_clicks":     true,
	"profile_views":      true,
	"online_followers":   true,
	"accounts_engaged":   true,
	"total_interactions": true,
	"likes":              true,
	"comments":           true,
	"shares":             true,
	"saves":              true,
	"replies":            true,
	// "impressions": false, // NOT valid for account insights (only for media)
}

// MediaAllowedMetrics are valid metrics for media-level insights (posts, reels)
var MediaAllowedMetrics = map[string]bool{
	"likes":              true,
	"comments":           true,
	"shares":             true,
	"saves":              true,
	"profile_links_taps": true,
	"views":              true,
	"impressions":        true,
	"reach":              true,
	// Threads metrics are separate and should not be mixed with Instagram metrics
}

// validateAccountMetrics validates metrics for account-level insights
// Returns error if any metric is invalid for the given login method
func validateAccountMetrics(metrics []string, loginMethod string) error {
	var allowedMetrics map[string]bool

	if loginMethod == "instagram_login" {
		allowedMetrics = InstagramLoginMetrics
	} else {
		allowedMetrics = BusinessAllowedMetrics
	}

	for _, m := range metrics {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}

		// Filter out Threads metrics (they should be separate)
		if strings.HasPrefix(m, "threads_") {
			return fmt.Errorf("metric %s is a Threads metric and cannot be used with Instagram account insights", m)
		}

		if !allowedMetrics[m] {
			// Check if it's an invalid metric for account insights
			if m == "impressions" {
				return fmt.Errorf("metric 'impressions' is not valid for account insights (only valid for media insights). Use 'reach', 'follower_count', 'profile_views', etc. for account insights")
			}
			return fmt.Errorf("metric %s is not a valid business account metric", m)
		}
	}

	return nil
}

// validateMediaMetrics validates metrics for media-level insights
func validateMediaMetrics(metrics []string) error {
	for _, m := range metrics {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}

		// Filter out Threads metrics
		if strings.HasPrefix(m, "threads_") {
			return fmt.Errorf("metric %s is a Threads metric and cannot be used with Instagram media insights", m)
		}

		// Filter out invalid metrics
		if m == "engagement" {
			return fmt.Errorf("metric 'engagement' is not valid. Use 'likes', 'comments', 'shares', 'saves' instead")
		}

		if !MediaAllowedMetrics[m] {
			return fmt.Errorf("metric %s is not a valid media metric", m)
		}
	}

	return nil
}

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
	Name        string `json:"name"`
	Period      string `json:"period"`
	Title       string `json:"title"`
	Description string `json:"description"`
	ID          string `json:"id"`
	Values      []struct {
		Value   int64  `json:"value"`
		EndTime string `json:"end_time"`
	} `json:"values"`
}

// MediaInsightMetric represents a single media insight metric
type MediaInsightMetric struct {
	Name        string `json:"name"`
	Period      string `json:"period"`
	Title       string `json:"title"`
	Description string `json:"description"`
	ID          string `json:"id"`
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
// Valid metrics depend on login method:
// - Instagram Login (graph.instagram.com): ONLY "impressions", "reach", "profile_views" (3 metrics)
// - Facebook Login (graph.facebook.com): reach, follower_count, profile_views, total_interactions, etc.
// NOTE: Do NOT mix account-level and media-level metrics
// NOTE: Threads metrics must be filtered out (they're separate)
// period: day, week, days_28, lifetime
func (s *InstagramInsightsService) GetAccountInsights(ctx context.Context, account *storage.SocialAccount, metrics []string, period string) (*AccountInsightsResponse, error) {
	if account.AccessToken == "" {
		return nil, fmt.Errorf("access token missing")
	}

	// Validate metrics before making API call
	if err := validateAccountMetrics(metrics, account.LoginMethod); err != nil {
		return nil, fmt.Errorf("invalid metrics: %w", err)
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
		errorMsg := fmt.Sprintf("Instagram API error %d: %s", resp.StatusCode, string(body))

		// Provide helpful error message for permission issues
		if resp.StatusCode == http.StatusForbidden {
			if account.LoginMethod == "instagram_login" {
				errorMsg += "\n\n💡 Solution: Your Instagram account needs the 'instagram_business_manage_insights' permission. " +
					"Please disconnect and reconnect your Instagram account to grant this permission."
			} else {
				errorMsg += "\n\n💡 Solution: Your Facebook account needs the 'instagram_manage_insights' and 'pages_read_engagement' permissions. " +
					"Please disconnect and reconnect your Instagram account via Facebook Login to grant these permissions."
			}
		}

		return nil, fmt.Errorf("%s", errorMsg)
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

	// Validate metrics before making API call
	if err := validateMediaMetrics(metrics); err != nil {
		return nil, fmt.Errorf("invalid metrics: %w", err)
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
		errorMsg := fmt.Sprintf("Instagram API error %d: %s", resp.StatusCode, string(body))

		// Provide helpful error message for permission issues
		if resp.StatusCode == http.StatusForbidden {
			errorMsg += "\n\n💡 Solution: Your Instagram account needs the 'instagram_business_manage_insights' permission for media insights. " +
				"Please disconnect and reconnect your Instagram account to grant this permission."
		}

		return nil, fmt.Errorf("%s", errorMsg)
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
// Valid account-level metrics vary by login method:
// - Instagram Login: impressions, reach, profile_views (ONLY 3 metrics)
// - Facebook Login: reach, follower_count, profile_views, total_interactions, etc.
// NOTE: Do NOT mix account-level and media-level metrics
func (s *InstagramInsightsService) SaveAccountInsights(ctx context.Context, tenantID uuid.UUID, accountID string, insights *AccountInsightsResponse) error {
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
				// CRITICAL: Initialize Metadata to valid empty JSON - PostgreSQL JSONB rejects empty strings
				snapshot = storage.PlatformMetricsSnapshot{
					TenantID:     tenantID,
					Platform:     "instagram",
					SnapshotDate: endTime,
					PeriodType:   metric.Period,
					Metadata:     "{}", // Valid empty JSON for PostgreSQL JSONB
				}
			}

			// Update metrics based on metric name
			// Valid account-level metrics vary by login method (updated for 2025):
			// - Instagram Login: impressions, reach (profile_views deprecated)
			// - Facebook Login: reach, follower_count, impressions, total_interactions, etc.
			// NOTE: Do NOT mix account-level and media-level metrics
			switch metric.Name {
			case "impressions":
				// Valid for both Instagram Login and Facebook Login account insights
				snapshot.TotalImpressions = value.Value
			case "reach":
				snapshot.TotalReach = value.Value
			case "profile_views":
				// Deprecated in 2025, but handle if API still returns it for backward compatibility
				// Store in metadata with deprecation flag
				if snapshot.Metadata == "" {
					snapshot.Metadata = "{}"
				}
				var metadata map[string]interface{}
				json.Unmarshal([]byte(snapshot.Metadata), &metadata)
				if metadata == nil {
					metadata = make(map[string]interface{})
				}
				metadata["profile_views"] = value.Value
				metadata["profile_views_deprecated"] = true // Mark as deprecated
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
func (s *InstagramInsightsService) SaveMediaInsights(ctx context.Context, tenantID uuid.UUID, mediaID string, insights *MediaInsightsResponse) error {
	// Find existing PostAnalytics record
	var analytics storage.PostAnalytics
	err := s.DB.WithContext(ctx).
		Where("tenant_id = ? AND platform = ? AND platform_post_id = ?",
			tenantID, "instagram", mediaID).
		First(&analytics).Error

	if err == gorm.ErrRecordNotFound {
		// Create new analytics record
		// CRITICAL: Initialize JSONB fields to valid empty JSON - PostgreSQL rejects empty strings
		analytics = storage.PostAnalytics{
			TenantID:       tenantID,
			Platform:       "instagram",
			PlatformPostID: mediaID,
			CollectedAt:    time.Now(),
			Metadata:       "{}",   // Valid empty JSON for PostgreSQL JSONB
			Demographics:   "{}",   // Valid empty JSON for PostgreSQL JSONB
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
		case "saves":
			// Fixed: metric name is "saves" not "saved"
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
