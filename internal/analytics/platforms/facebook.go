package platforms

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/pkg/interfaces"
)

// FacebookAnalyticsClient implements analytics collection for Facebook
type FacebookAnalyticsClient struct {
	client *http.Client
}

// NewFacebookAnalyticsClient creates a new Facebook analytics client
func NewFacebookAnalyticsClient() *FacebookAnalyticsClient {
	return &FacebookAnalyticsClient{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Platform returns the platform name
func (c *FacebookAnalyticsClient) Platform() string {
	return "facebook"
}

// FetchPostMetrics retrieves engagement metrics for a Facebook post
func (c *FacebookAnalyticsClient) FetchPostMetrics(ctx context.Context, account *storage.SocialAccount, postID string, periodStart, periodEnd time.Time) (*interfaces.PostMetrics, error) {
	// Facebook Graph API endpoint for post insights
	url := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/insights", postID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := req.URL.Query()
	q.Add("access_token", account.AccessToken)
	q.Add("metric", "post_impressions,post_engaged_users,post_reactions_by_type_total,post_clicks")
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

	var result struct {
		Data []struct {
			Name   string `json:"name"`
			Values []struct {
				Value map[string]interface{} `json:"value"`
			} `json:"values"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	metrics := &interfaces.PostMetrics{
		PlatformPostID: postID,
		PostedAt:       time.Now(),
		PeriodStart:    periodStart,
		PeriodEnd:      periodEnd,
		Demographics:   make(map[string]interface{}),
		Metadata:       make(map[string]interface{}),
	}

	// Parse metrics
	for _, metric := range result.Data {
		if len(metric.Values) == 0 {
			continue
		}
		value := metric.Values[0].Value

		switch metric.Name {
		case "post_impressions":
			if v, ok := value["value"].(float64); ok {
				metrics.Impressions = int64(v)
			}
		case "post_engaged_users":
			if v, ok := value["value"].(float64); ok {
				metrics.Reach = int64(v)
			}
		case "post_reactions_by_type_total":
			// Sum all reaction types
			if reactions, ok := value["value"].(map[string]interface{}); ok {
				total := int64(0)
				for _, count := range reactions {
					if v, ok := count.(float64); ok {
						total += int64(v)
					}
				}
				metrics.Reactions = total
			}
		case "post_clicks":
			if v, ok := value["value"].(float64); ok {
				metrics.ClickThrough = int64(v)
			}
		}
	}

	// Calculate engagement rate
	if metrics.Reach > 0 {
		engagement := float64(metrics.Reactions + metrics.ClickThrough)
		metrics.EngagementRate = (engagement / float64(metrics.Reach)) * 100.0
	}

	return metrics, nil
}

// FetchAccountMetrics retrieves overall Facebook page metrics
func (c *FacebookAnalyticsClient) FetchAccountMetrics(ctx context.Context, account *storage.SocialAccount, periodStart, periodEnd time.Time) (*interfaces.AccountMetrics, error) {
	url := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/insights", account.PageID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := req.URL.Query()
	q.Add("access_token", account.AccessToken)
	q.Add("metric", "page_fans,page_impressions,page_reach")
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
				Value map[string]interface{} `json:"value"`
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

	for _, metric := range result.Data {
		if len(metric.Values) == 0 {
			continue
		}
		value := metric.Values[0].Value

		switch metric.Name {
		case "page_fans":
			if v, ok := value["value"].(float64); ok {
				metrics.TotalFollowers = int64(v)
			}
		case "page_impressions":
			if v, ok := value["value"].(float64); ok {
				metrics.TotalImpressions = int64(v)
			}
		case "page_reach":
			if v, ok := value["value"].(float64); ok {
				metrics.TotalReach = int64(v)
			}
		}
	}

	return metrics, nil
}

// FetchAudienceDemographics retrieves audience demographic data
func (c *FacebookAnalyticsClient) FetchAudienceDemographics(ctx context.Context, account *storage.SocialAccount) (*interfaces.AudienceDemographics, error) {
	url := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/insights", account.PageID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := req.URL.Query()
	q.Add("access_token", account.AccessToken)
	q.Add("metric", "page_fans_by_age_gender,page_fans_by_country")
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
