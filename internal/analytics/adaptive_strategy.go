package analytics

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// AdaptiveStrategyService uses analytics to optimize scheduling and engagement strategies
type AdaptiveStrategyService struct {
	DB            *gorm.DB
	Logger        *zap.Logger
	TrendAnalyzer *TrendAnalyzer
}

// NewAdaptiveStrategyService creates a new adaptive strategy service
func NewAdaptiveStrategyService(db *gorm.DB, logger *zap.Logger, trendAnalyzer *TrendAnalyzer) *AdaptiveStrategyService {
	return &AdaptiveStrategyService{
		DB:            db,
		Logger:        logger,
		TrendAnalyzer: trendAnalyzer,
	}
}

// OptimalPostingTime calculates the best time to post based on historical analytics
func (a *AdaptiveStrategyService) OptimalPostingTime(ctx context.Context, tenantID uuid.UUID, platform string) (time.Time, error) {
	// Fetch recent analytics for this platform
	var analytics []storage.PostAnalytics
	periodStart := time.Now().AddDate(0, 0, -30) // Last 30 days

	err := a.DB.Where("tenant_id = ? AND platform = ? AND posted_at >= ?",
		tenantID, platform, periodStart).
		Order("engagement_rate DESC").
		Limit(50).
		Find(&analytics).Error

	if err != nil {
		return time.Time{}, fmt.Errorf("failed to fetch analytics: %w", err)
	}

	if len(analytics) == 0 {
		// Default to current time + 1 hour if no data
		return time.Now().Add(1 * time.Hour), nil
	}

	// Find the hour with best average engagement
	hourPerformance := make(map[int]struct {
		count int
		total float64
	})

	for _, a := range analytics {
		hour := a.PostedAt.Hour()
		perf := hourPerformance[hour]
		perf.count++
		perf.total += a.EngagementRate
		hourPerformance[hour] = perf
	}

	bestHour := -1
	bestAvg := 0.0

	for hour, perf := range hourPerformance {
		if perf.count >= 2 {
			avg := perf.total / float64(perf.count)
			if avg > bestAvg {
				bestAvg = avg
				bestHour = hour
			}
		}
	}

	if bestHour == -1 {
		// Default to 2 PM if no clear pattern
		bestHour = 14
	}

	// Calculate next occurrence of this hour
	now := time.Now()
	optimalTime := time.Date(now.Year(), now.Month(), now.Day(), bestHour, 0, 0, 0, now.Location())
	if optimalTime.Before(now) {
		optimalTime = optimalTime.Add(24 * time.Hour)
	}

	return optimalTime, nil
}

// RecommendContentType suggests the best content type based on performance
func (a *AdaptiveStrategyService) RecommendContentType(ctx context.Context, tenantID uuid.UUID, platform string) (string, error) {
	// Fetch analytics grouped by content type (would need MediaAsset data)
	// For now, return platform-specific recommendations

	switch platform {
	case "instagram":
		return "reel", nil // Reels perform best on Instagram
	case "facebook":
		return "video", nil // Native video performs best on Facebook
	case "tiktok":
		return "video", nil // Video is the only format on TikTok
	default:
		return "image", nil
	}
}

// OptimizeCaptionStrategy suggests caption improvements based on performance
func (a *AdaptiveStrategyService) OptimizeCaptionStrategy(ctx context.Context, tenantID uuid.UUID) (map[string]interface{}, error) {
	// Fetch insights about caption performance
	var insights []storage.TrendInsight
	err := a.DB.Where("tenant_id = ? AND type = ? AND status = ?",
		tenantID, "caption_tone", "active").
		Order("created_at DESC").
		Limit(1).
		Find(&insights).Error

	if err != nil {
		return nil, fmt.Errorf("failed to fetch insights: %w", err)
	}

	strategy := map[string]interface{}{
		"recommended_tone": "friendly",
		"hashtag_count":    5,
		"call_to_action":   true,
	}

	if len(insights) > 0 {
		// Extract recommendations from insights
		insight := insights[0]
		strategy["recommendation"] = insight.Recommendation
		strategy["confidence"] = insight.Confidence
	}

	return strategy, nil
}

// GetPlatformPriority returns recommended platform priority based on performance
func (a *AdaptiveStrategyService) GetPlatformPriority(ctx context.Context, tenantID uuid.UUID) ([]string, error) {
	// Fetch analytics grouped by platform
	var analytics []storage.PostAnalytics
	periodStart := time.Now().AddDate(0, 0, -30)

	err := a.DB.Where("tenant_id = ? AND posted_at >= ?",
		tenantID, periodStart).
		Find(&analytics).Error

	if err != nil {
		return nil, fmt.Errorf("failed to fetch analytics: %w", err)
	}

	platformScores := make(map[string]struct {
		count int
		total float64
	})

	for _, a := range analytics {
		score := platformScores[a.Platform]
		score.count++
		score.total += a.EngagementRate
		platformScores[a.Platform] = score
	}

	// Sort platforms by average engagement
	type platformScore struct {
		platform string
		avg      float64
	}

	scores := []platformScore{}
	for platform, data := range platformScores {
		if data.count > 0 {
			avg := data.total / float64(data.count)
			scores = append(scores, platformScore{platform: platform, avg: avg})
		}
	}

	// Sort by average (descending)
	for i := 0; i < len(scores); i++ {
		for j := i + 1; j < len(scores); j++ {
			if scores[i].avg < scores[j].avg {
				scores[i], scores[j] = scores[j], scores[i]
			}
		}
	}

	priority := make([]string, len(scores))
	for i, s := range scores {
		priority[i] = s.platform
	}

	return priority, nil
}

// ShouldPostNow determines if now is a good time to post based on analytics
func (a *AdaptiveStrategyService) ShouldPostNow(ctx context.Context, tenantID uuid.UUID, platform string) (bool, float64, error) {
	optimalTime, err := a.OptimalPostingTime(ctx, tenantID, platform)
	if err != nil {
		return false, 0.0, err
	}

	now := time.Now()
	timeDiff := optimalTime.Sub(now).Hours()

	// Consider it a good time if within 2 hours of optimal time
	if timeDiff >= 0 && timeDiff <= 2 {
		// Calculate confidence (higher if closer to optimal time)
		confidence := 1.0 - (timeDiff / 2.0)
		return true, confidence, nil
	}

	return false, 0.0, nil
}
