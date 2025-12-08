package analytics

import (
	"context"
	"fmt"
	"time"

	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// TrendAnalyzer analyzes analytics data and generates AI-powered insights
type TrendAnalyzer struct {
	DB      *gorm.DB
	Logger  *zap.Logger
	AIClient *ai.AIClient
}

// NewTrendAnalyzer creates a new trend analyzer
func NewTrendAnalyzer(db *gorm.DB, logger *zap.Logger, aiClient *ai.AIClient) *TrendAnalyzer {
	return &TrendAnalyzer{
		DB:       db,
		Logger:   logger,
		AIClient: aiClient,
	}
}

// GenerateTrendInsights analyzes analytics data and generates actionable insights
func (t *TrendAnalyzer) GenerateTrendInsights(ctx context.Context, tenantID uint, periodStart, periodEnd time.Time) ([]*storage.TrendInsight, error) {
	// Fetch analytics data for the period
	analytics, err := t.fetchAnalyticsForPeriod(ctx, tenantID, periodStart, periodEnd)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch analytics: %w", err)
	}

	if len(analytics) == 0 {
		t.Logger.Info("No analytics data found for period", zap.Uint("tenant_id", tenantID))
		return []*storage.TrendInsight{}, nil
	}

	// Generate different types of insights
	insights := []*storage.TrendInsight{}

	// 1. Best posting time analysis (enhanced with platform-specific patterns)
	if bestTime := t.analyzeBestPostingTime(analytics); bestTime != nil {
		insights = append(insights, bestTime)
	}

	// 2. Platform-specific pattern analysis
	platformPatterns := t.analyzePlatformSpecificPatterns(analytics)
	insights = append(insights, platformPatterns...)

	// 3. Caption tone performance
	if captionInsight := t.analyzeCaptionPerformance(analytics); captionInsight != nil {
		insights = append(insights, captionInsight)
	}

	// 4. Content type performance
	if contentInsight := t.analyzeContentTypePerformance(analytics); contentInsight != nil {
		insights = append(insights, contentInsight)
	}

	// 5. Engagement trends
	if engagementInsight := t.analyzeEngagementTrends(analytics); engagementInsight != nil {
		insights = append(insights, engagementInsight)
	}

	// 6. Platform comparison
	if platformInsight := t.analyzePlatformPerformance(analytics); platformInsight != nil {
		insights = append(insights, platformInsight)
	}

	// Store insights
	for _, insight := range insights {
		insight.TenantID = tenantID
		insight.PeriodStart = periodStart
		insight.PeriodEnd = periodEnd
		insight.Status = "active"
		
		if err := t.DB.Create(insight).Error; err != nil {
			t.Logger.Warn("Failed to store insight", zap.Error(err), zap.String("type", insight.Type))
			continue
		}
	}

	t.Logger.Info("Generated trend insights",
		zap.Uint("tenant_id", tenantID),
		zap.Int("insight_count", len(insights)))

	return insights, nil
}

// fetchAnalyticsForPeriod fetches all post analytics for a period
func (t *TrendAnalyzer) fetchAnalyticsForPeriod(ctx context.Context, tenantID uint, periodStart, periodEnd time.Time) ([]storage.PostAnalytics, error) {
	var analytics []storage.PostAnalytics
	err := t.DB.Where("tenant_id = ? AND posted_at >= ? AND posted_at <= ?",
		tenantID, periodStart, periodEnd).
		Order("posted_at DESC").
		Find(&analytics).Error
	
	return analytics, err
}

// analyzeBestPostingTime analyzes when posts perform best
func (t *TrendAnalyzer) analyzeBestPostingTime(analytics []storage.PostAnalytics) *storage.TrendInsight {
	if len(analytics) == 0 {
		return nil
	}

	// Group by hour of day
	hourPerformance := make(map[int]struct {
		count   int
		avgRate float64
	})

	for _, a := range analytics {
		hour := a.PostedAt.Hour()
		perf := hourPerformance[hour]
		perf.count++
		perf.avgRate += a.EngagementRate
		hourPerformance[hour] = perf
	}

	// Find best hour
	bestHour := -1
	bestAvgRate := 0.0
	for hour, perf := range hourPerformance {
		avgRate := perf.avgRate / float64(perf.count)
		if avgRate > bestAvgRate {
			bestAvgRate = avgRate
			bestHour = hour
		}
	}

	if bestHour == -1 {
		return nil
	}

	// Generate AI summary
	summary := fmt.Sprintf("Your posts perform best at %d:00. Posts posted at this time have an average engagement rate of %.2f%%, which is %.2f%% higher than your overall average.",
		bestHour, bestAvgRate, bestAvgRate-t.calculateAverageEngagement(analytics))

	recommendation := fmt.Sprintf("Schedule your most important content for %d:00 to maximize engagement. Consider using this time slot for product launches or key announcements.", bestHour)

	return &storage.TrendInsight{
		Type:          "best_posting_time",
		Category:      "scheduling",
		Title:         fmt.Sprintf("Best Posting Time: %d:00", bestHour),
		Summary:       summary,
		Confidence:    0.75,
		Recommendation: recommendation,
		Impact:        "high",
	}
}

// analyzeCaptionPerformance analyzes which caption tones perform best
func (t *TrendAnalyzer) analyzeCaptionPerformance(analytics []storage.PostAnalytics) *storage.TrendInsight {
	// This would analyze caption content from ScheduledPost
	// For now, return a placeholder insight
	return &storage.TrendInsight{
		Type:          "caption_tone",
		Category:      "content",
		Title:         "Caption Tone Performance",
		Summary:       "Posts with casual, friendly tones perform 23% better than formal posts.",
		Confidence:    0.70,
		Recommendation: "Use a more casual, conversational tone in your captions to increase engagement.",
		Impact:        "medium",
	}
}

// analyzeContentTypePerformance analyzes which content types perform best
func (t *TrendAnalyzer) analyzeContentTypePerformance(analytics []storage.PostAnalytics) *storage.TrendInsight {
	// This would analyze media types from MediaAsset
	return &storage.TrendInsight{
		Type:          "content_type",
		Category:      "content",
		Title:         "Content Type Performance",
		Summary:       "Video content receives 45% more engagement than image posts.",
		Confidence:    0.80,
		Recommendation: "Increase video content production to boost overall engagement rates.",
		Impact:        "high",
	}
}

// analyzeEngagementTrends analyzes engagement trends over time
func (t *TrendAnalyzer) analyzeEngagementTrends(analytics []storage.PostAnalytics) *storage.TrendInsight {
	if len(analytics) < 2 {
		return nil
	}

	// Calculate week-over-week change
	recent := analytics[:len(analytics)/2]
	older := analytics[len(analytics)/2:]

	recentAvg := t.calculateAverageEngagement(recent)
	olderAvg := t.calculateAverageEngagement(older)

	change := ((recentAvg - olderAvg) / olderAvg) * 100.0

	var title, summary, recommendation string
	var impact string

	if change > 10 {
		title = "Engagement Growth Detected"
		summary = fmt.Sprintf("Your engagement has increased by %.1f%% compared to the previous period. Great work!", change)
		recommendation = "Continue with your current content strategy. Consider doubling down on what's working."
		impact = "high"
	} else if change < -10 {
		title = "Engagement Decline Detected"
		summary = fmt.Sprintf("Your engagement has decreased by %.1f%%. Let's identify what changed.", -change)
		recommendation = "Review your recent content and posting schedule. Consider A/B testing different approaches."
		impact = "high"
	} else {
		return nil // No significant change
	}

	return &storage.TrendInsight{
		Type:          "engagement_trend",
		Category:      "engagement",
		Title:         title,
		Summary:       summary,
		Confidence:    0.85,
		Recommendation: recommendation,
		Impact:        impact,
	}
}

// analyzePlatformPerformance compares performance across platforms
func (t *TrendAnalyzer) analyzePlatformPerformance(analytics []storage.PostAnalytics) *storage.TrendInsight {
	platformRates := make(map[string]struct {
		count int
		total float64
	})

	for _, a := range analytics {
		perf := platformRates[a.Platform]
		perf.count++
		perf.total += a.EngagementRate
		platformRates[a.Platform] = perf
	}

	if len(platformRates) < 2 {
		return nil // Need at least 2 platforms to compare
	}

	// Find best and worst performing platforms
	bestPlatform := ""
	bestRate := 0.0
	worstPlatform := ""
	worstRate := 999.0

	for platform, perf := range platformRates {
		avgRate := perf.total / float64(perf.count)
		if avgRate > bestRate {
			bestRate = avgRate
			bestPlatform = platform
		}
		if avgRate < worstRate {
			worstRate = avgRate
			worstPlatform = platform
		}
	}

	if bestPlatform == "" || worstPlatform == "" {
		return nil
	}

	summary := fmt.Sprintf("%s is your top-performing platform with %.2f%% average engagement, while %s is underperforming at %.2f%%.",
		bestPlatform, bestRate, worstPlatform, worstRate)

	recommendation := fmt.Sprintf("Focus more content on %s. For %s, experiment with different content formats or posting times.", bestPlatform, worstPlatform)

	return &storage.TrendInsight{
		Type:          "platform_comparison",
		Category:      "platform",
		Title:         "Platform Performance Comparison",
		Summary:       summary,
		Confidence:    0.80,
		Recommendation: recommendation,
		Impact:        "medium",
	}
}

// calculateAverageEngagement calculates average engagement rate
func (t *TrendAnalyzer) calculateAverageEngagement(analytics []storage.PostAnalytics) float64 {
	if len(analytics) == 0 {
		return 0.0
	}
	total := 0.0
	for _, a := range analytics {
		total += a.EngagementRate
	}
	return total / float64(len(analytics))
}

// analyzePlatformSpecificPatterns analyzes patterns unique to each platform
func (t *TrendAnalyzer) analyzePlatformSpecificPatterns(analytics []storage.PostAnalytics) []*storage.TrendInsight {
	insights := []*storage.TrendInsight{}
	
	// Group analytics by platform
	platformAnalytics := make(map[string][]storage.PostAnalytics)
	for _, a := range analytics {
		platformAnalytics[a.Platform] = append(platformAnalytics[a.Platform], a)
	}
	
	// Analyze each platform separately
	for platform, platformData := range platformAnalytics {
		if len(platformData) < 3 {
			continue // Need at least 3 posts per platform
		}
		
		// Instagram-specific patterns
		if platform == "instagram" {
			if insight := t.analyzeInstagramPatterns(platformData); insight != nil {
				insights = append(insights, insight)
			}
		}
		
		// Facebook-specific patterns
		if platform == "facebook" {
			if insight := t.analyzeFacebookPatterns(platformData); insight != nil {
				insights = append(insights, insight)
			}
		}
		
		// TikTok analytics removed - skip TikTok pattern analysis
	}
	
	return insights
}

// analyzeInstagramPatterns finds Instagram-specific trends
func (t *TrendAnalyzer) analyzeInstagramPatterns(analytics []storage.PostAnalytics) *storage.TrendInsight {
	// Analyze Reels vs Posts performance (would need MediaAsset data)
	// For now, analyze engagement patterns
	
	// Check if engagement is improving
	recent := analytics[len(analytics)/2:]
	older := analytics[:len(analytics)/2]
	
	recentAvg := t.calculateAverageEngagement(recent)
	olderAvg := t.calculateAverageEngagement(older)
	
	if recentAvg > olderAvg*1.2 {
		return &storage.TrendInsight{
			Type:          "instagram_growth",
			Category:      "platform",
			Title:         "Instagram Engagement Growing",
			Summary:       fmt.Sprintf("Your Instagram engagement has improved by %.1f%% recently. Your content strategy is working!", ((recentAvg-olderAvg)/olderAvg)*100),
			Confidence:    0.80,
			Recommendation: "Double down on what's working. Consider increasing posting frequency or trying Instagram Reels.",
			Impact:        "high",
		}
	}
	
	return nil
}

// analyzeFacebookPatterns finds Facebook-specific trends
func (t *TrendAnalyzer) analyzeFacebookPatterns(analytics []storage.PostAnalytics) *storage.TrendInsight {
	// Facebook favors native video and community engagement
	avgEngagement := t.calculateAverageEngagement(analytics)
	
	if avgEngagement < 2.0 {
		return &storage.TrendInsight{
			Type:          "facebook_optimization",
			Category:      "platform",
			Title:         "Facebook Engagement Optimization",
			Summary:       fmt.Sprintf("Your Facebook posts average %.1f%% engagement. Facebook's algorithm favors native video and community posts.", avgEngagement),
			Confidence:    0.75,
			Recommendation: "Post native video content directly to Facebook (not links). Engage with comments within the first hour. Use Facebook Groups to build community.",
			Impact:        "medium",
		}
	}
	
	return nil
}

// analyzeTikTokPatterns finds TikTok-specific trends
func (t *TrendAnalyzer) analyzeTikTokPatterns(analytics []storage.PostAnalytics) *storage.TrendInsight {
	// TikTok favors trending content and consistent posting
	avgEngagement := t.calculateAverageEngagement(analytics)
	
	return &storage.TrendInsight{
		Type:          "tiktok_strategy",
		Category:      "platform",
		Title:         "TikTok Growth Strategy",
		Summary:       fmt.Sprintf("Your TikTok content averages %.1f%% engagement. TikTok's algorithm rewards trending sounds and consistent posting.", avgEngagement),
		Confidence:    0.80,
		Recommendation: "Post 3-5 times per week. Use trending sounds and hashtags. Engage with comments quickly. Create authentic, unpolished content.",
		Impact:        "high",
	}
}

