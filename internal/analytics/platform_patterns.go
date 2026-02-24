package analytics

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// PlatformPatternAnalyzer analyzes platform-specific trend patterns
type PlatformPatternAnalyzer struct {
	DB     interface{} // *gorm.DB
	Logger *zap.Logger
}

// PlatformPattern represents a detected pattern for a specific platform
type PlatformPattern struct {
	Platform       string
	PatternType    string // "peak_hours", "content_format", "hashtag_strategy", "engagement_curve"
	Description    string
	Confidence     float64
	Recommendation string
	DataPoints     map[string]interface{}
}

// AnalyzePlatformPatterns analyzes patterns specific to each platform
func (p *PlatformPatternAnalyzer) AnalyzePlatformPatterns(ctx context.Context, tenantID uuid.UUID, periodStart, periodEnd time.Time) (map[string][]PlatformPattern, error) {
	// Fetch analytics grouped by platform
	platformAnalytics := make(map[string][]storage.PostAnalytics)

	// This would query the database - simplified for now
	// var analytics []storage.PostAnalytics
	// err := p.DB.Where("tenant_id = ? AND posted_at >= ? AND posted_at <= ?", tenantID, periodStart, periodEnd).Find(&analytics).Error

	patterns := make(map[string][]PlatformPattern)

	// Analyze each platform separately
	for platform, analytics := range platformAnalytics {
		platformPatterns := []PlatformPattern{}

		// 1. Peak hours analysis (platform-specific)
		if peakHours := p.analyzePeakHours(platform, analytics); peakHours != nil {
			platformPatterns = append(platformPatterns, *peakHours)
		}

		// 2. Content format preferences
		if contentFormat := p.analyzeContentFormat(platform, analytics); contentFormat != nil {
			platformPatterns = append(platformPatterns, *contentFormat)
		}

		// 3. Engagement curve analysis
		if engagementCurve := p.analyzeEngagementCurve(platform, analytics); engagementCurve != nil {
			platformPatterns = append(platformPatterns, *engagementCurve)
		}

		// 4. Day of week patterns
		if dayPattern := p.analyzeDayOfWeek(platform, analytics); dayPattern != nil {
			platformPatterns = append(platformPatterns, *dayPattern)
		}

		patterns[platform] = platformPatterns
	}

	return patterns, nil
}

// analyzePeakHours finds platform-specific peak engagement hours
func (p *PlatformPatternAnalyzer) analyzePeakHours(platform string, analytics []storage.PostAnalytics) *PlatformPattern {
	if len(analytics) < 5 {
		return nil
	}

	hourPerformance := make(map[int]struct {
		count int
		total float64
		reach int64
	})

	for _, a := range analytics {
		hour := a.PostedAt.Hour()
		perf := hourPerformance[hour]
		perf.count++
		perf.total += a.EngagementRate
		perf.reach += a.Reach
		hourPerformance[hour] = perf
	}

	// Find top 3 peak hours
	type hourScore struct {
		hour  int
		score float64
	}
	scores := []hourScore{}

	for hour, perf := range hourPerformance {
		if perf.count >= 2 { // Need at least 2 data points
			avgRate := perf.total / float64(perf.count)
			weightedScore := avgRate * float64(perf.reach) / 1000.0 // Weight by reach
			scores = append(scores, hourScore{hour: hour, score: weightedScore})
		}
	}

	if len(scores) == 0 {
		return nil
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	topHours := scores[0].hour
	topScore := scores[0].score

	// Platform-specific recommendations
	var description, recommendation string
	switch platform {
	case "instagram":
		description = fmt.Sprintf("Instagram posts perform best at %d:00 with weighted engagement score of %.2f", topHours, topScore)
		recommendation = fmt.Sprintf("Schedule Instagram posts at %d:00 for maximum visibility. Instagram's algorithm favors consistent posting times.", topHours)
	case "facebook":
		description = fmt.Sprintf("Facebook posts peak at %d:00 with engagement score of %.2f", topHours, topScore)
		recommendation = fmt.Sprintf("Post to Facebook at %d:00 when your audience is most active. Consider boosting posts during this time.", topHours)
	case "tiktok":
		description = fmt.Sprintf("TikTok content performs best at %d:00", topHours)
		recommendation = fmt.Sprintf("TikTok's For You Page algorithm favors content posted at %d:00. Schedule your best content for this time.", topHours)
	default:
		description = fmt.Sprintf("Peak engagement occurs at %d:00", topHours)
		recommendation = fmt.Sprintf("Schedule important posts at %d:00", topHours)
	}

	return &PlatformPattern{
		Platform:       platform,
		PatternType:    "peak_hours",
		Description:    description,
		Confidence:     0.75,
		Recommendation: recommendation,
		DataPoints: map[string]interface{}{
			"peak_hour": topHours,
			"score":     topScore,
		},
	}
}

// analyzeContentFormat analyzes which content formats work best per platform
func (p *PlatformPatternAnalyzer) analyzeContentFormat(platform string, analytics []storage.PostAnalytics) *PlatformPattern {
	// This would analyze MediaAsset types
	// For now, return platform-specific recommendations
	var description, recommendation string

	switch platform {
	case "instagram":
		description = "Instagram Reels receive 3x more engagement than static posts"
		recommendation = "Prioritize Reels and carousel posts. Use static images for behind-the-scenes content."
	case "facebook":
		description = "Video content on Facebook generates 2.5x more engagement"
		recommendation = "Create short video content (30-60 seconds) for Facebook. Native videos perform better than external links."
	case "tiktok":
		description = "TikTok favors authentic, unpolished content"
		recommendation = "Focus on authentic, trending content. Use trending sounds and hashtags."
	default:
		return nil
	}

	return &PlatformPattern{
		Platform:       platform,
		PatternType:    "content_format",
		Description:    description,
		Confidence:     0.80,
		Recommendation: recommendation,
	}
}

// analyzeEngagementCurve analyzes how engagement changes over time
func (p *PlatformPatternAnalyzer) analyzeEngagementCurve(platform string, analytics []storage.PostAnalytics) *PlatformPattern {
	if len(analytics) < 10 {
		return nil
	}

	// Sort by posted time
	sort.Slice(analytics, func(i, j int) bool {
		return analytics[i].PostedAt.Before(analytics[j].PostedAt)
	})

	// Calculate engagement trend
	recent := analytics[len(analytics)/2:]
	older := analytics[:len(analytics)/2]

	recentAvg := 0.0
	for _, a := range recent {
		recentAvg += a.EngagementRate
	}
	recentAvg /= float64(len(recent))

	olderAvg := 0.0
	for _, a := range older {
		olderAvg += a.EngagementRate
	}
	olderAvg /= float64(len(older))

	trend := ((recentAvg - olderAvg) / olderAvg) * 100.0

	var description, recommendation string
	if trend > 15 {
		description = fmt.Sprintf("Your %s engagement is trending upward (+%.1f%%)", platform, trend)
		recommendation = "Continue your current strategy. Consider increasing posting frequency to capitalize on growth."
	} else if trend < -15 {
		description = fmt.Sprintf("Your %s engagement is declining (%.1f%%)", platform, trend)
		recommendation = "Review recent content changes. Consider A/B testing different content types or posting times."
	} else {
		return nil // No significant trend
	}

	return &PlatformPattern{
		Platform:       platform,
		PatternType:    "engagement_curve",
		Description:    description,
		Confidence:     0.70,
		Recommendation: recommendation,
		DataPoints: map[string]interface{}{
			"trend_percentage": trend,
			"recent_avg":       recentAvg,
			"older_avg":        olderAvg,
		},
	}
}

// analyzeDayOfWeek finds best days to post per platform
func (p *PlatformPatternAnalyzer) analyzeDayOfWeek(platform string, analytics []storage.PostAnalytics) *PlatformPattern {
	if len(analytics) < 7 {
		return nil
	}

	dayPerformance := make(map[string]struct {
		count int
		total float64
	})

	for _, a := range analytics {
		day := a.PostedAt.Weekday().String()
		perf := dayPerformance[day]
		perf.count++
		perf.total += a.EngagementRate
		dayPerformance[day] = perf
	}

	bestDay := ""
	bestScore := 0.0

	for day, perf := range dayPerformance {
		if perf.count >= 2 {
			avg := perf.total / float64(perf.count)
			if avg > bestScore {
				bestScore = avg
				bestDay = day
			}
		}
	}

	if bestDay == "" {
		return nil
	}

	description := fmt.Sprintf("%s posts perform best on %s with average engagement of %.2f%%", platform, bestDay, bestScore)
	recommendation := fmt.Sprintf("Schedule your most important content for %s. Consider creating a weekly content calendar around this day.", bestDay)

	return &PlatformPattern{
		Platform:       platform,
		PatternType:    "day_of_week",
		Description:    description,
		Confidence:     0.65,
		Recommendation: recommendation,
		DataPoints: map[string]interface{}{
			"best_day": bestDay,
			"score":    bestScore,
		},
	}
}
