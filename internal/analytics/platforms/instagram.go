package platforms

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"github.com/similadayo/brancr/pkg/interfaces"
)

// InstagramAnalyticsClient implements analytics collection for Instagram
type InstagramAnalyticsClient struct {
	client *http.Client
}

// NewInstagramAnalyticsClient creates a new Instagram analytics client
func NewInstagramAnalyticsClient() *InstagramAnalyticsClient {
	return &InstagramAnalyticsClient{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Platform returns the platform name
func (c *InstagramAnalyticsClient) Platform() string {
	return "instagram"
}

// FetchPostMetrics retrieves engagement metrics for an Instagram post
func (c *InstagramAnalyticsClient) FetchPostMetrics(ctx context.Context, account *storage.SocialAccount, postID string, periodStart, periodEnd time.Time) (*interfaces.PostMetrics, error) {
	if postID == "" {
		return nil, fmt.Errorf("post ID is empty")
	}

	// Decrypt access token if needed
	accessToken := account.AccessToken
	decryptedToken, err := utils.DecryptToken(accessToken)
	if err != nil {
		// If decryption fails, try using as-is (might already be decrypted)
		decryptedToken = accessToken
	}

	if decryptedToken == "" {
		return nil, fmt.Errorf("access token is empty")
	}

	// Determine API base URL based on login method
	var apiBase string
	if account.LoginMethod == "instagram_login" {
		apiBase = "https://graph.instagram.com/v24.0"
	} else {
		apiBase = "https://graph.facebook.com/v18.0"
	}

	// Instagram Graph API endpoint for post insights
	url := fmt.Sprintf("%s/%s/insights", apiBase, postID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add query parameters
	// According to Instagram API docs (v24.0):
	// - "impressions" is deprecated for v22.0+ and will be deprecated for all versions on April 21, 2025
	//   (still available for media created before July 2, 2024)
	// - "views" is the new metric for FEED, STORY, and REELS (replaces impressions for newer media)
	// - "plays" is deprecated for v22.0+ and all versions on April 21, 2025
	// - "saved" is the correct metric name (not "saves")
	// - "shares" may not be available for all post types, but API will return what's available
	// - Metrics available: comments, likes, saved, shares, reach, views, total_interactions
	q := req.URL.Query()
	q.Add("access_token", decryptedToken)
	// Request core metrics that are available for most post types
	// Using "views" instead of deprecated "impressions" for newer media
	// Note: API will return only available metrics for the specific media type
	q.Add("metric", "views,reach,likes,comments,saved,shares,total_interactions")
	q.Add("period", "lifetime") // Period is automatically set to lifetime and cannot be changed
	req.URL.RawQuery = q.Encode()

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		errorMsg := fmt.Sprintf("API returned status %d", resp.StatusCode)
		if len(body) > 0 {
			errorMsg += fmt.Sprintf(": %s", string(body))
		}
		return nil, fmt.Errorf("%s", errorMsg)
	}

	var result struct {
		Data []struct {
			Name   string `json:"name"`
			Period string `json:"period"`
			Values []struct {
				// Value can be a number (for lifetime period) or an object (for time-series data)
				// Using interface{} to handle both cases
				Value interface{} `json:"value"`
			} `json:"values"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Parse metrics from response
	metrics := &interfaces.PostMetrics{
		PlatformPostID: postID,
		PostedAt:       time.Now(), // Would get from post details API
		PeriodStart:    periodStart,
		PeriodEnd:      periodEnd,
		Demographics:   make(map[string]interface{}),
		Metadata:       make(map[string]interface{}),
	}

	// Helper function to extract numeric value from Value field
	extractValue := func(value interface{}) (float64, bool) {
		switch v := value.(type) {
		case float64:
			return v, true
		case int:
			return float64(v), true
		case int64:
			return float64(v), true
		case map[string]interface{}:
			// Handle nested structure (for time-series data)
			if nestedVal, ok := v["value"].(float64); ok {
				return nestedVal, true
			}
		}
		return 0, false
	}

	// Extract metrics from response
	for _, metric := range result.Data {
		if len(metric.Values) == 0 {
			continue
		}
		value := metric.Values[0].Value
		metricValue, ok := extractValue(value)
		if !ok {
			continue
		}

		switch metric.Name {
		case "views":
			// New metric for FEED, STORY, and REELS (replaces deprecated "impressions")
			metrics.Views = int64(metricValue)
			// For backward compatibility, also set Impressions if Views is available
			if metrics.Impressions == 0 {
				metrics.Impressions = int64(metricValue)
			}
		case "impressions":
			// Deprecated for v22.0+ but still available for older media (created before July 2, 2024)
			metrics.Impressions = int64(metricValue)
		case "reach":
			metrics.Reach = int64(metricValue)
		case "likes":
			metrics.Likes = int64(metricValue)
		case "comments":
			metrics.Comments = int64(metricValue)
		case "saved", "saves": // Instagram API returns "saved" but we support both for compatibility
			metrics.Saves = int64(metricValue)
		case "shares":
			metrics.Shares = int64(metricValue)
		case "total_interactions":
			// Total interactions (likes + saves + comments + shares, minus unlikes/unsaves/deleted comments)
			if metrics.Metadata == nil {
				metrics.Metadata = make(map[string]interface{})
			}
			metrics.Metadata["total_interactions"] = int64(metricValue)
		case "engagement": // Composite metric (likes + comments) - parse if API returns it
			// If we don't have individual likes/comments, use engagement as fallback
			if metrics.Likes == 0 && metrics.Comments == 0 {
				// Engagement = likes + comments, so we can't split it
				// But we'll use it to calculate engagement rate if reach is available
				if metrics.Metadata == nil {
					metrics.Metadata = make(map[string]interface{})
				}
				metrics.Metadata["engagement"] = int64(metricValue)
			}
		}
	}

	// Calculate engagement rate
	if metrics.Reach > 0 {
		engagement := float64(metrics.Likes + metrics.Comments + metrics.Shares + metrics.Saves)
		metrics.EngagementRate = (engagement / float64(metrics.Reach)) * 100.0
	}

	return metrics, nil
}

// FetchAccountMetrics retrieves overall Instagram account metrics
func (c *InstagramAnalyticsClient) FetchAccountMetrics(ctx context.Context, account *storage.SocialAccount, periodStart, periodEnd time.Time) (*interfaces.AccountMetrics, error) {
	// Use Instagram Business Account ID
	url := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/insights", account.ExternalID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := req.URL.Query()
	q.Add("access_token", account.AccessToken)
	q.Add("metric", "follower_count,impressions,reach,profile_views")
	q.Add("period", "day")
	req.URL.RawQuery = q.Encode()

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			Name   string `json:"name"`
			Values []struct {
				Value   map[string]interface{} `json:"value"`
				EndTime string                 `json:"end_time"`
			} `json:"values"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	metrics := &interfaces.AccountMetrics{
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
	}

	// Parse metrics
	for _, metric := range result.Data {
		if len(metric.Values) == 0 {
			continue
		}
		value := metric.Values[0].Value

		switch metric.Name {
		case "follower_count":
			if v, ok := value["value"].(float64); ok {
				metrics.TotalFollowers = int64(v)
			}
		case "impressions":
			if v, ok := value["value"].(float64); ok {
				metrics.TotalImpressions = int64(v)
			}
		case "reach":
			if v, ok := value["value"].(float64); ok {
				metrics.TotalReach = int64(v)
			}
		}
	}

	return metrics, nil
}

// FetchAudienceDemographics retrieves audience demographic data
func (c *InstagramAnalyticsClient) FetchAudienceDemographics(ctx context.Context, account *storage.SocialAccount) (*interfaces.AudienceDemographics, error) {
	url := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/insights", account.ExternalID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := req.URL.Query()
	q.Add("access_token", account.AccessToken)
	q.Add("metric", "audience_gender_age,audience_country,audience_locale")
	q.Add("period", "lifetime")
	req.URL.RawQuery = q.Encode()

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	demographics := &interfaces.AudienceDemographics{
		AgeGroups: make(map[string]int64),
		Gender:    make(map[string]int64),
		Locations: make(map[string]int64),
		Metadata:  make(map[string]interface{}),
	}

	// Parse demographics from response
	// Implementation would parse the actual API response structure

	return demographics, nil
}
