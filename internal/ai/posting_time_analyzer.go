package ai

import (
	"github.com/google/uuid"
	"context"
	"fmt"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// OptimalTimeAnalyzer analyzes historical data to suggest optimal posting times
type OptimalTimeAnalyzer struct {
	DB       *gorm.DB
	Logger   *zap.Logger
	AIClient *AIClient
}

// NewOptimalTimeAnalyzer creates a new optimal time analyzer
func NewOptimalTimeAnalyzer(db *gorm.DB, logger *zap.Logger, client *AIClient) *OptimalTimeAnalyzer {
	return &OptimalTimeAnalyzer{
		DB:       db,
		Logger:   logger,
		AIClient: client,
	}
}

// SuggestOptimalTime suggests the best posting time for a tenant/platform/content type
func (a *OptimalTimeAnalyzer) SuggestOptimalTime(
	ctx context.Context,
	tenantID uuid.UUID,
	platform string,
	contentType string, // image, video, reel, carousel
) (*time.Time, error) {
	// Get tenant timezone
	var tenant storage.Tenant
	if err := a.DB.First(&tenant, tenantID).Error; err != nil {
		return nil, fmt.Errorf("tenant not found: %w", err)
	}

	timezone := tenant.Timezone
	if timezone == "" {
		timezone = "UTC"
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}

	// Get historical engagement data (from analytics if available)
	// For now, use default best practices per platform
	optimalHour := a.getDefaultOptimalHour(platform, contentType)

	now := time.Now().In(loc)
	optimalTime := time.Date(
		now.Year(),
		now.Month(),
		now.Day(),
		optimalHour, 0, 0, 0,
		loc,
	)

	// If optimal time has passed today, suggest tomorrow
	if optimalTime.Before(now) {
		optimalTime = optimalTime.AddDate(0, 0, 1)
	}

	// Adjust to next weekday if it's weekend (configurable)
	if optimalTime.Weekday() == time.Saturday {
		optimalTime = optimalTime.AddDate(0, 0, 2) // Move to Monday
	} else if optimalTime.Weekday() == time.Sunday {
		optimalTime = optimalTime.AddDate(0, 0, 1) // Move to Monday
	}

	return &optimalTime, nil
}

// getDefaultOptimalHour returns default optimal hour based on platform and content type
func (a *OptimalTimeAnalyzer) getDefaultOptimalHour(platform, contentType string) int {
	// Platform-specific best practices
	switch platform {
	case "instagram":
		// Instagram: 10am-11am, 2pm-3pm, 7pm-9pm are generally best
		if contentType == "reel" {
			return 9 // Reels perform best in morning
		}
		return 10 // Regular posts: 10am

	case "facebook":
		// Facebook: 1pm-3pm on weekdays
		return 14 // 2pm

	case "tiktok":
		// TikTok: 6am-10am, 7pm-9pm
		return 9 // 9am

	case "youtube":
		// YouTube: 2pm-4pm on weekdays
		return 15 // 3pm

	default:
		return 10 // Default: 10am
	}
}

// AnalyzeHistoricalEngagement analyzes past posts to find optimal times
func (a *OptimalTimeAnalyzer) AnalyzeHistoricalEngagement(
	ctx context.Context,
	tenantID uuid.UUID,
	platform string,
	daysBack int,
) (map[int]float64, error) {
	// This would analyze PostLog and analytics data
	// For now, return empty map (will be implemented with analytics integration)
	hourlyEngagement := make(map[int]float64)

	// TODO: Query analytics/PostLog to get engagement by hour
	// For now, return default distribution
	for hour := 6; hour < 22; hour++ {
		// Simulate engagement curve (higher in morning and evening)
		if hour >= 9 && hour <= 11 {
			hourlyEngagement[hour] = 0.8 // High engagement
		} else if hour >= 14 && hour <= 16 {
			hourlyEngagement[hour] = 0.7 // Medium-high
		} else if hour >= 19 && hour <= 21 {
			hourlyEngagement[hour] = 0.75 // Evening peak
		} else {
			hourlyEngagement[hour] = 0.4 // Lower engagement
		}
	}

	return hourlyEngagement, nil
}

// SuggestMultipleOptimalTimes suggests multiple optimal times for bulk scheduling
func (a *OptimalTimeAnalyzer) SuggestMultipleOptimalTimes(
	ctx context.Context,
	tenantID uuid.UUID,
	platform string,
	count int,
	startDate time.Time,
	endDate time.Time,
) ([]time.Time, error) {
	var times []time.Time

	// Get optimal hours for this platform
	optimalHours := a.getOptimalHoursForPlatform(platform)

	// Distribute posts across optimal hours and dates
	currentDate := startDate
	hourIndex := 0

	for i := 0; i < count; i++ {
		if currentDate.After(endDate) {
			break
		}

		hour := optimalHours[hourIndex%len(optimalHours)]
		optimalTime := time.Date(
			currentDate.Year(),
			currentDate.Month(),
			currentDate.Day(),
			hour, 0, 0, 0,
			currentDate.Location(),
		)

		times = append(times, optimalTime)

		hourIndex++
		if hourIndex%len(optimalHours) == 0 {
			currentDate = currentDate.AddDate(0, 0, 1)
		}
	}

	return times, nil
}

// getOptimalHoursForPlatform returns multiple optimal hours for a platform
func (a *OptimalTimeAnalyzer) getOptimalHoursForPlatform(platform string) []int {
	switch platform {
	case "instagram":
		return []int{9, 14, 19} // 9am, 2pm, 7pm
	case "facebook":
		return []int{13, 15} // 1pm, 3pm
	case "tiktok":
		return []int{8, 20} // 8am, 8pm
	case "youtube":
		return []int{14, 16} // 2pm, 4pm
	default:
		return []int{10, 14} // Default: 10am, 2pm
	}
}
