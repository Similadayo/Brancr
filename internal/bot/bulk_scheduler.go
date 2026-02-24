package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// BulkScheduler handles bulk scheduling operations
type BulkScheduler struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

// NewBulkScheduler creates a new bulk scheduler
func NewBulkScheduler(db *gorm.DB, logger *zap.Logger) *BulkScheduler {
	return &BulkScheduler{
		DB:     db,
		Logger: logger,
	}
}

// ScheduleRequest represents a single scheduling request
type ScheduleRequest struct {
	MediaAssetIDs []uuid.UUID
	Platforms     []string
	ScheduledAt   time.Time
	Caption       string
	Name          string
}

// ParseBulkSchedule parses natural language bulk scheduling commands
// Examples:
// - "Schedule 4 carousels: Mon 10am, Wed 2pm, Fri 9am, Sun 3pm"
// - "Spread 20 posts over next month, 2 per day at 10am and 2pm"
// - "Schedule video1 at 9am and pic1 at 3pm today"
func (s *BulkScheduler) ParseBulkSchedule(
	ctx context.Context,
	tenantID uuid.UUID,
	input string,
	assets []storage.MediaAsset,
) ([]ScheduleRequest, error) {
	input = strings.ToLower(strings.TrimSpace(input))
	var requests []ScheduleRequest

	// Get tenant timezone
	var tenant storage.Tenant
	if err := s.DB.First(&tenant, tenantID).Error; err != nil {
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

	now := time.Now().In(loc)

	// Pattern 1: "Schedule X carousels: Mon 10am, Wed 2pm, ..."
	if strings.Contains(input, "schedule") && strings.Contains(input, ":") {
		parts := strings.Split(input, ":")
		if len(parts) == 2 {
			timesStr := strings.TrimSpace(parts[1])
			timeList := strings.Split(timesStr, ",")

			for i, timeStr := range timeList {
				if i >= len(assets) {
					break
				}

				scheduledAt, err := s.parseTimeString(timeStr, now, loc)
				if err != nil {
					s.Logger.Warn("failed to parse time",
						zap.String("time_str", timeStr),
						zap.Error(err))
					continue
				}

				requests = append(requests, ScheduleRequest{
					MediaAssetIDs: []uuid.UUID{assets[i].ID},
					Platforms:     []string{"instagram"}, // Default
					ScheduledAt:   scheduledAt,
					Name:          assets[i].Name,
				})
			}
		}
	}

	// Pattern 2: "Spread X posts over Y period, Z per day"
	if strings.Contains(input, "spread") || strings.Contains(input, "over") {
		requests = s.parseSpreadCommand(input, assets, now, loc)
	}

	// Pattern 3: "Schedule asset1 at 9am and asset2 at 3pm today"
	if strings.Contains(input, "at") && strings.Contains(input, "and") {
		requests = s.parseMultipleTimesCommand(input, assets, now, loc)
	}

	return requests, nil
}

// parseTimeString parses time strings like "Mon 10am", "tomorrow 2pm", "next week at 10am"
func (s *BulkScheduler) parseTimeString(timeStr string, baseTime time.Time, loc *time.Location) (time.Time, error) {
	timeStr = strings.TrimSpace(strings.ToLower(timeStr))

	// Handle "today"
	if strings.Contains(timeStr, "today") {
		timeStr = strings.ReplaceAll(timeStr, "today", "")
		timeStr = strings.TrimSpace(timeStr)
		baseTime = time.Date(baseTime.Year(), baseTime.Month(), baseTime.Day(), 0, 0, 0, 0, loc)
	}

	// Handle "tomorrow"
	if strings.Contains(timeStr, "tomorrow") {
		baseTime = baseTime.AddDate(0, 0, 1)
		timeStr = strings.ReplaceAll(timeStr, "tomorrow", "")
		timeStr = strings.TrimSpace(timeStr)
		baseTime = time.Date(baseTime.Year(), baseTime.Month(), baseTime.Day(), 0, 0, 0, 0, loc)
	}

	// Handle day names (Mon, Tue, Wed, etc.)
	dayMap := map[string]int{
		"mon": 1, "monday": 1,
		"tue": 2, "tuesday": 2,
		"wed": 3, "wednesday": 3,
		"thu": 4, "thursday": 4,
		"fri": 5, "friday": 5,
		"sat": 6, "saturday": 6,
		"sun": 7, "sunday": 7,
	}

	for dayName, dayNum := range dayMap {
		if strings.Contains(timeStr, dayName) {
			// Find next occurrence of this day
			daysUntil := (dayNum - int(baseTime.Weekday()) + 7) % 7
			if daysUntil == 0 {
				daysUntil = 7 // Next week
			}
			baseTime = baseTime.AddDate(0, 0, daysUntil)
			timeStr = strings.ReplaceAll(timeStr, dayName, "")
			timeStr = strings.TrimSpace(timeStr)
			baseTime = time.Date(baseTime.Year(), baseTime.Month(), baseTime.Day(), 0, 0, 0, 0, loc)
			break
		}
	}

	// Parse time (10am, 2pm, 14:00, etc.)
	timeRegex := regexp.MustCompile(`(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)
	matches := timeRegex.FindStringSubmatch(timeStr)
	if len(matches) > 0 {
		hour := 0
		minute := 0
		fmt.Sscanf(matches[1], "%d", &hour)

		if len(matches) > 2 && matches[2] != "" {
			fmt.Sscanf(matches[2], "%d", &minute)
		}

		if len(matches) > 3 {
			ampm := strings.ToLower(matches[3])
			if ampm == "pm" && hour != 12 {
				hour += 12
			} else if ampm == "am" && hour == 12 {
				hour = 0
			}
		}

		return time.Date(
			baseTime.Year(),
			baseTime.Month(),
			baseTime.Day(),
			hour, minute, 0, 0,
			loc,
		), nil
	}

	// Default to 10am if no time specified
	return time.Date(
		baseTime.Year(),
		baseTime.Month(),
		baseTime.Day(),
		10, 0, 0, 0,
		loc,
	), nil
}

// parseSpreadCommand parses "Spread X posts over Y period, Z per day"
func (s *BulkScheduler) parseSpreadCommand(
	input string,
	assets []storage.MediaAsset,
	baseTime time.Time,
	loc *time.Location,
) []ScheduleRequest {
	var requests []ScheduleRequest

	// Extract number of posts
	postCount := len(assets)
	if matches := regexp.MustCompile(`(\d+)\s*posts?`).FindStringSubmatch(input); len(matches) > 0 {
		fmt.Sscanf(matches[1], "%d", &postCount)
		if postCount > len(assets) {
			postCount = len(assets)
		}
	}

	// Extract posts per day
	postsPerDay := 1
	if matches := regexp.MustCompile(`(\d+)\s*per\s*day`).FindStringSubmatch(input); len(matches) > 0 {
		fmt.Sscanf(matches[1], "%d", &postsPerDay)
	}

	// Extract times per day
	times := []int{10} // Default 10am
	if matches := regexp.MustCompile(`(\d+)(am|pm)`).FindAllStringSubmatch(input, -1); len(matches) > 0 {
		times = []int{}
		for _, match := range matches {
			hour := 0
			fmt.Sscanf(match[1], "%d", &hour)
			if match[2] == "pm" && hour != 12 {
				hour += 12
			} else if match[2] == "am" && hour == 12 {
				hour = 0
			}
			times = append(times, hour)
		}
	}

	// Generate schedule
	dayOffset := 0
	timeIndex := 0
	for i := 0; i < postCount && i < len(assets); i++ {
		if timeIndex >= len(times) {
			timeIndex = 0
			dayOffset++
		}

		scheduledTime := baseTime.AddDate(0, 0, dayOffset)
		scheduledTime = time.Date(
			scheduledTime.Year(),
			scheduledTime.Month(),
			scheduledTime.Day(),
			times[timeIndex], 0, 0, 0,
			loc,
		)

		requests = append(requests, ScheduleRequest{
			MediaAssetIDs: []uuid.UUID{assets[i].ID},
			Platforms:     []string{"instagram"},
			ScheduledAt:   scheduledTime,
			Name:          assets[i].Name,
		})

		timeIndex++
		if timeIndex >= len(times) {
			dayOffset++
			timeIndex = 0
		}
	}

	return requests
}

// parseMultipleTimesCommand parses "Schedule asset1 at 9am and asset2 at 3pm"
func (s *BulkScheduler) parseMultipleTimesCommand(
	input string,
	assets []storage.MediaAsset,
	baseTime time.Time,
	loc *time.Location,
) []ScheduleRequest {
	var requests []ScheduleRequest

	// Split by "and"
	parts := strings.Split(input, "and")
	for i, part := range parts {
		if i >= len(assets) {
			break
		}

		part = strings.TrimSpace(part)
		scheduledAt, err := s.parseTimeString(part, baseTime, loc)
		if err != nil {
			continue
		}

		requests = append(requests, ScheduleRequest{
			MediaAssetIDs: []uuid.UUID{assets[i].ID},
			Platforms:     []string{"instagram"},
			ScheduledAt:   scheduledAt,
			Name:          assets[i].Name,
		})
	}

	return requests
}

// CreateBulkSchedule creates multiple scheduled posts from requests
func (s *BulkScheduler) CreateBulkSchedule(
	ctx context.Context,
	tenantID uuid.UUID,
	requests []ScheduleRequest,
	platforms []string,
) error {
	// Get tenant timezone
	var tenant storage.Tenant
	if err := s.DB.First(&tenant, tenantID).Error; err != nil {
		return fmt.Errorf("tenant not found: %w", err)
	}

	timezone := tenant.Timezone
	if timezone == "" {
		timezone = "UTC"
	}

	platformsJSON, _ := json.Marshal(platforms)

	for _, req := range requests {
		assetIDsJSON, _ := json.Marshal(req.MediaAssetIDs)

		sp := storage.ScheduledPost{
			TenantID:      tenantID,
			Name:          req.Name,
			Caption:       req.Caption,
			MediaAssetIDs: string(assetIDsJSON),
			Platforms:     string(platformsJSON),
			ScheduledAt:   &req.ScheduledAt,
			Status:        "scheduled",
			Timezone:      timezone,
		}

		if err := s.DB.Create(&sp).Error; err != nil {
			s.Logger.Error("failed to create scheduled post",
				zap.Error(err),
				zap.String("name", req.Name))
			continue
		}
	}

	s.Logger.Info("✅ Created bulk schedule",
		zap.Stringer("tenant_id", tenantID),
		zap.Int("count", len(requests)))

	return nil
}
