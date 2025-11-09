package interfaces

import (
	"context"
	"time"

	"github.com/similadayo/brancr/internal/storage"
)

// PlatformAnalyticsClient defines the interface for fetching analytics from social platforms
type PlatformAnalyticsClient interface {
	// FetchPostMetrics retrieves engagement metrics for a specific post
	FetchPostMetrics(ctx context.Context, account *storage.SocialAccount, postID string, periodStart, periodEnd time.Time) (*PostMetrics, error)
	
	// FetchAccountMetrics retrieves overall account/page metrics
	FetchAccountMetrics(ctx context.Context, account *storage.SocialAccount, periodStart, periodEnd time.Time) (*AccountMetrics, error)
	
	// FetchAudienceDemographics retrieves audience demographic data
	FetchAudienceDemographics(ctx context.Context, account *storage.SocialAccount) (*AudienceDemographics, error)
	
	// Platform returns the platform name (instagram, facebook, youtube, tiktok, whatsapp)
	Platform() string
}

// PostMetrics represents normalized engagement metrics for a post
type PostMetrics struct {
	PlatformPostID string
	Impressions    int64
	Reach          int64
	Likes          int64
	Comments       int64
	Saves          int64
	Shares         int64
	Reactions      int64
	WatchTime      int64 // seconds
	Views          int64
	ClickThrough   int64
	EngagementRate float64
	PostedAt       time.Time
	PeriodStart    time.Time
	PeriodEnd      time.Time
	Demographics   map[string]interface{}
	Metadata       map[string]interface{}
}

// AccountMetrics represents overall account/page performance
type AccountMetrics struct {
	TotalFollowers   int64
	TotalPosts       int
	TotalImpressions int64
	TotalReach       int64
	TotalEngagement  int64
	AvgEngagementRate float64
	PeriodStart      time.Time
	PeriodEnd        time.Time
}

// AudienceDemographics represents audience demographic breakdown
type AudienceDemographics struct {
	AgeGroups map[string]int64    // "18-24": 1000, "25-34": 2000, etc.
	Gender    map[string]int64    // "male": 5000, "female": 6000, etc.
	Locations map[string]int64    // "US": 3000, "UK": 2000, etc.
	Interests []string            // Top interests
	Metadata  map[string]interface{}
}

// AnalyticsService defines the interface for analytics operations
type AnalyticsService interface {
	// CollectPostMetrics fetches and stores metrics for a specific post
	CollectPostMetrics(ctx context.Context, tenantID uint, scheduledPostID uint, platform string, platformPostID string) error
	
	// CollectAllPostMetrics collects metrics for all posted content for a tenant
	CollectAllPostMetrics(ctx context.Context, tenantID uint, daysBack int) error
	
	// GetPostAnalytics retrieves stored analytics for a post
	GetPostAnalytics(ctx context.Context, tenantID uint, scheduledPostID uint, platform string) (*storage.PostAnalytics, error)
	
	// GetTenantAnalytics retrieves aggregated analytics for a tenant
	GetTenantAnalytics(ctx context.Context, tenantID uint, periodStart, periodEnd time.Time) (*TenantAnalyticsSummary, error)
	
	// GenerateTrendInsights analyzes data and generates AI insights
	GenerateTrendInsights(ctx context.Context, tenantID uint, periodStart, periodEnd time.Time) ([]*storage.TrendInsight, error)
}

// TenantAnalyticsSummary represents aggregated analytics for a tenant
type TenantAnalyticsSummary struct {
	TenantID           uint
	PeriodStart        time.Time
	PeriodEnd          time.Time
	TotalPosts         int
	TotalImpressions   int64
	TotalReach         int64
	TotalLikes         int64
	TotalComments      int64
	TotalShares        int64
	AvgEngagementRate  float64
	PlatformBreakdown  map[string]*PlatformSummary
	TopPosts           []*PostSummary
	TrendInsights      []*storage.TrendInsight
}

// PlatformSummary represents metrics for a specific platform
type PlatformSummary struct {
	Platform          string
	TotalPosts        int
	TotalImpressions  int64
	TotalReach        int64
	TotalLikes        int64
	TotalComments     int64
	TotalShares       int64
	AvgEngagementRate float64
	BestPostID        uint
	BestPostEngagement int64
}

// PostSummary represents a summary of a post's performance
type PostSummary struct {
	ScheduledPostID uint
	Platform        string
	PlatformPostID  string
	Impressions     int64
	Reach           int64
	Likes           int64
	Comments        int64
	EngagementRate  float64
	PostedAt        time.Time
}

