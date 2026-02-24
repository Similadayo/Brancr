package storage

import (
	"time"

	"github.com/google/uuid"
)

// =======================
//  ANALYTICS & METRICS
// =======================

// PostAnalytics stores normalized engagement metrics for individual posts across platforms
type PostAnalytics struct {
	ID              uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TenantID        uuid.UUID `gorm:"index:idx_tenant_post"`
	ScheduledPostID uuid.UUID `gorm:"index:idx_tenant_post"`         // Links to ScheduledPost
	Platform        string    `gorm:"size:50;index:idx_tenant_post"` // instagram, facebook, youtube, tiktok, whatsapp
	PlatformPostID  string    `gorm:"size:255;index"`                // Platform-specific post ID

	// Core Engagement Metrics
	Impressions    int64   `gorm:"default:0"` // Views/impressions
	Reach          int64   `gorm:"default:0"` // Unique users reached
	Likes          int64   `gorm:"default:0"`
	Comments       int64   `gorm:"default:0"`
	Saves          int64   `gorm:"default:0"`          // Instagram saves, Facebook saves
	Shares         int64   `gorm:"default:0"`          // Shares/reposts
	Reactions      int64   `gorm:"default:0"`          // Facebook reactions (all types)
	WatchTime      int64   `gorm:"default:0"`          // Total watch time in seconds (YouTube, TikTok)
	Views          int64   `gorm:"default:0"`          // Video views (YouTube, TikTok)
	ClickThrough   int64   `gorm:"default:0"`          // Link clicks, CTA clicks
	EngagementRate float64 `gorm:"type:decimal(10,4)"` // Calculated: (likes+comments+shares)/reach

	// Audience Demographics (JSON)
	Demographics string `gorm:"type:jsonb"` // Age groups, gender, locations, etc.

	// Timing & Performance
	PostedAt    time.Time `gorm:"index"`
	CollectedAt time.Time `gorm:"index"` // When metrics were last fetched
	PeriodStart time.Time // Metrics period start
	PeriodEnd   time.Time // Metrics period end

	// Metadata
	Metadata  string `gorm:"type:jsonb"` // Platform-specific additional data
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CampaignAnalytics aggregates metrics across multiple posts for campaigns
type CampaignAnalytics struct {
	ID         uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TenantID   uuid.UUID `gorm:"index"`
	CampaignID string    `gorm:"size:100;index"` // Optional campaign identifier
	Name       string    `gorm:"size:255"`

	// Aggregated Metrics
	TotalPosts        int
	TotalImpressions  int64
	TotalReach        int64
	TotalLikes        int64
	TotalComments     int64
	TotalShares       int64
	TotalEngagement   int64
	AvgEngagementRate float64 `gorm:"type:decimal(10,4)"`

	// Period
	StartDate time.Time `gorm:"index"`
	EndDate   time.Time `gorm:"index"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TrendInsight stores AI-generated insights and recommendations
type TrendInsight struct {
	ID       uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TenantID uuid.UUID `gorm:"index"`

	// Insight Type
	Type     string `gorm:"size:50;index"` // best_posting_time, caption_tone, content_type, audience_peak, engagement_pattern
	Category string `gorm:"size:50"`       // scheduling, content, audience, engagement

	// Insight Data
	Title      string  `gorm:"size:255"`
	Summary    string  `gorm:"type:text"`         // Human-readable summary
	Details    string  `gorm:"type:jsonb"`        // Structured insight data
	Confidence float64 `gorm:"type:decimal(5,4)"` // 0-1 confidence score

	// Recommendations
	Recommendation string `gorm:"type:text"` // Actionable recommendation
	Impact         string `gorm:"size:50"`   // high, medium, low

	// Period Analyzed
	PeriodStart time.Time `gorm:"index"`
	PeriodEnd   time.Time `gorm:"index"`

	// Status
	Status    string `gorm:"size:20"` // active, dismissed, applied
	ViewedAt  *time.Time
	AppliedAt *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// PlatformMetricsSnapshot stores daily/hourly aggregated metrics per platform
type PlatformMetricsSnapshot struct {
	ID       uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TenantID uuid.UUID `gorm:"index:idx_tenant_platform_time"`
	Platform string    `gorm:"size:50;index:idx_tenant_platform_time"`

	// Aggregated Metrics
	TotalPosts        int
	TotalImpressions  int64
	TotalReach        int64
	TotalLikes        int64
	TotalComments     int64
	TotalShares       int64
	AvgEngagementRate float64 `gorm:"type:decimal(10,4)"`

	// Best Performing Post
	BestPostID         uuid.UUID
	BestPostEngagement int64

	// Snapshot Period
	SnapshotDate time.Time `gorm:"index:idx_tenant_platform_time"`
	PeriodType   string    `gorm:"size:20"` // daily, weekly, monthly

	// Metadata for additional metrics (profile_views, etc.)
	Metadata string `gorm:"type:jsonb"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// AudienceSegment stores audience demographic data
type AudienceSegment struct {
	ID       uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TenantID uuid.UUID `gorm:"index"`
	Platform string    `gorm:"size:50;index"`

	// Demographics
	AgeGroup string `gorm:"size:50"`  // 18-24, 25-34, 35-44, etc.
	Gender   string `gorm:"size:20"`  // male, female, other, unknown
	Location string `gorm:"size:100"` // Country, city, etc.

	// Metrics
	Count      int64   // Number of users in segment
	Engagement int64   // Total engagement from segment
	Percentage float64 `gorm:"type:decimal(5,2)"` // Percentage of total audience

	// Period
	PeriodStart time.Time `gorm:"index"`
	PeriodEnd   time.Time `gorm:"index"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName ensures proper table naming
func (PostAnalytics) TableName() string {
	return "post_analytics"
}

func (CampaignAnalytics) TableName() string {
	return "campaign_analytics"
}

func (TrendInsight) TableName() string {
	return "trend_insights"
}

func (PlatformMetricsSnapshot) TableName() string {
	return "platform_metrics_snapshots"
}

func (AudienceSegment) TableName() string {
	return "audience_segments"
}
