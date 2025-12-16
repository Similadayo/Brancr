package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// RecurringTemplateManager handles recurring post templates
type RecurringTemplateManager struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

// NewRecurringTemplateManager creates a new recurring template manager
func NewRecurringTemplateManager(db *gorm.DB, logger *zap.Logger) *RecurringTemplateManager {
	return &RecurringTemplateManager{
		DB:     db,
		Logger: logger,
	}
}

// ScheduleConfig represents the schedule configuration for a recurring template
type ScheduleConfig struct {
	Days     []int  `json:"days"`     // 0=Sunday, 1=Monday, ..., 6=Saturday
	Time     string `json:"time"`     // "10:00", "14:30", etc.
	Timezone string `json:"timezone"` // "America/New_York"
}

// CreateTemplate creates a new recurring template
func (m *RecurringTemplateManager) CreateTemplate(
	ctx context.Context,
	tenantID uint,
	name string,
	frequency string, // daily, weekly, monthly, custom
	schedule ScheduleConfig,
	captionTemplate string,
	platforms []string,
) (*storage.RecurringTemplate, error) {
	scheduleJSON, _ := json.Marshal(schedule)
	platformsJSON, _ := json.Marshal(platforms)

	template := storage.RecurringTemplate{
		TenantID:        tenantID,
		Name:            name,
		Frequency:       frequency,
		Schedule:        string(scheduleJSON),
		CaptionTemplate: captionTemplate,
		Platforms:       string(platformsJSON),
		Active:          true,
	}

	// Calculate next run time
	nextRun, err := m.calculateNextRun(schedule, frequency)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate next run: %w", err)
	}
	template.NextRun = nextRun

	if err := m.DB.Create(&template).Error; err != nil {
		return nil, fmt.Errorf("failed to create template: %w", err)
	}

	m.Logger.Info("✅ Created recurring template",
		zap.Uint("tenant_id", tenantID),
		zap.String("name", name),
		zap.String("frequency", frequency))

	return &template, nil
}

// calculateNextRun calculates the next execution time for a template
func (m *RecurringTemplateManager) calculateNextRun(
	schedule ScheduleConfig,
	frequency string,
) (*time.Time, error) {
	// Get timezone
	loc := time.UTC
	if schedule.Timezone != "" {
		var err error
		loc, err = time.LoadLocation(schedule.Timezone)
		if err != nil {
			loc = time.UTC
		}
	}

	now := time.Now().In(loc)

	// Parse time
	timeParts := strings.Split(schedule.Time, ":")
	if len(timeParts) != 2 {
		return nil, fmt.Errorf("invalid time format: %s", schedule.Time)
	}

	var hour, minute int
	fmt.Sscanf(timeParts[0], "%d", &hour)
	fmt.Sscanf(timeParts[1], "%d", &minute)

	switch frequency {
	case "daily":
		// Next occurrence of the time today or tomorrow
		nextRun := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, loc)
		if nextRun.Before(now) {
			nextRun = nextRun.AddDate(0, 0, 1)
		}
		return &nextRun, nil

	case "weekly":
		// Next occurrence on one of the specified days
		if len(schedule.Days) == 0 {
			return nil, fmt.Errorf("weekly frequency requires at least one day")
		}

		// Find next matching day
		for i := 0; i < 7; i++ {
			candidate := now.AddDate(0, 0, i)
			dayOfWeek := int(candidate.Weekday())
			if dayOfWeek == 0 {
				dayOfWeek = 7 // Convert Sunday from 0 to 7
			}

			for _, targetDay := range schedule.Days {
				if dayOfWeek == targetDay {
					nextRun := time.Date(candidate.Year(), candidate.Month(), candidate.Day(), hour, minute, 0, 0, loc)
					if nextRun.After(now) {
						return &nextRun, nil
					}
				}
			}
		}
		// If no match found in next 7 days, use first day of next week
		firstDay := schedule.Days[0]
		daysUntil := (firstDay - int(now.Weekday()) + 7) % 7
		if daysUntil == 0 {
			daysUntil = 7
		}
		nextRun := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, loc).AddDate(0, 0, daysUntil)
		return &nextRun, nil

	case "monthly":
		// Next occurrence on the same day of next month
		nextRun := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, loc).AddDate(0, 1, 0)
		if nextRun.Before(now) {
			nextRun = nextRun.AddDate(0, 1, 0)
		}
		return &nextRun, nil

	default:
		return nil, fmt.Errorf("unsupported frequency: %s", frequency)
	}
}

// ProcessDueTemplates processes all templates that are due to run
func (m *RecurringTemplateManager) ProcessDueTemplates(ctx context.Context) error {
	now := time.Now()

	var templates []storage.RecurringTemplate
	if err := m.DB.Where("active = ? AND next_run <= ?", true, now).
		Find(&templates).Error; err != nil {
		return fmt.Errorf("failed to fetch due templates: %w", err)
	}

	for _, template := range templates {
		if err := m.executeTemplate(ctx, template); err != nil {
			m.Logger.Error("failed to execute template",
				zap.Uint("template_id", template.ID),
				zap.String("name", template.Name),
				zap.Error(err))
			continue
		}

		// Update last run and calculate next run
		now := time.Now()
		template.LastRun = &now

		// Parse schedule to calculate next run
		var schedule ScheduleConfig
		if err := json.Unmarshal([]byte(template.Schedule), &schedule); err == nil {
			nextRun, err := m.calculateNextRun(schedule, template.Frequency)
			if err == nil {
				template.NextRun = nextRun
			}
		}

		m.DB.Save(&template)
	}

	return nil
}

// executeTemplate executes a recurring template by creating a scheduled post
func (m *RecurringTemplateManager) executeTemplate(
	ctx context.Context,
	template storage.RecurringTemplate,
) error {
	// Parse schedule
	var schedule ScheduleConfig
	if err := json.Unmarshal([]byte(template.Schedule), &schedule); err != nil {
		return fmt.Errorf("invalid schedule format: %w", err)
	}

	// Get next run time (should be now or very soon)
	if template.NextRun == nil {
		return fmt.Errorf("template has no next run time")
	}

	// Parse platforms
	var platforms []string
	if template.Platforms != "" {
		json.Unmarshal([]byte(template.Platforms), &platforms)
	}
	if len(platforms) == 0 {
		platforms = []string{"instagram"}
	}

	// For now, create a scheduled post with the template caption
	// In a full implementation, you'd select media based on MediaTemplate
	sp := storage.ScheduledPost{
		TenantID:      template.TenantID,
		Name:          fmt.Sprintf("Recurring: %s", template.Name),
		Caption:       template.CaptionTemplate,
		MediaAssetIDs: "[]", // Would be populated from MediaTemplate in full implementation
		Platforms:     template.Platforms,
		ScheduledAt:   template.NextRun,
		Status:        "scheduled",
	}

	// Get tenant timezone
	var tenant storage.Tenant
	timezone := "UTC"
	if err := m.DB.First(&tenant, template.TenantID).Error; err == nil && tenant.Timezone != "" {
		timezone = tenant.Timezone
	}
	sp.Timezone = timezone

	if err := m.DB.Create(&sp).Error; err != nil {
		return fmt.Errorf("failed to create scheduled post: %w", err)
	}

	m.Logger.Info("✅ Executed recurring template",
		zap.Uint("template_id", template.ID),
		zap.Uint("tenant_id", template.TenantID),
		zap.Uint("post_id", sp.ID))

	return nil
}

// ListTemplates lists all recurring templates for a tenant
func (m *RecurringTemplateManager) ListTemplates(
	ctx context.Context,
	tenantID uint,
) ([]storage.RecurringTemplate, error) {
	var templates []storage.RecurringTemplate
	if err := m.DB.Where("tenant_id = ?", tenantID).
		Order("created_at DESC").
		Find(&templates).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch templates: %w", err)
	}

	return templates, nil
}

// SendTemplatesList sends a formatted list of templates to Telegram
func (m *RecurringTemplateManager) SendTemplatesList(
	ctx context.Context,
	bot *tgbotapi.BotAPI,
	tenantID uint,
	chatID int64,
) error {
	templates, err := m.ListTemplates(ctx, tenantID)
	if err != nil {
		return err
	}

	if len(templates) == 0 {
		msg := "📋 *Recurring Templates*\n\nNo recurring templates yet.\n\n💡 Create one to automate your posting schedule!"
		bot.Send(tgbotapi.NewMessage(chatID, msg))
		return nil
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("📋 *Recurring Templates* (%d)", len(templates)))
	lines = append(lines, "")

	for i, template := range templates {
		status := "✅ Active"
		if !template.Active {
			status = "⏸️ Paused"
		}

		nextRun := "Not scheduled"
		if template.NextRun != nil {
			nextRun = template.NextRun.Format("Jan 2, 3:04 PM")
		}

		lines = append(lines, fmt.Sprintf("%d. *%s* - %s", i+1, template.Name, status))
		lines = append(lines, fmt.Sprintf("   📅 Frequency: %s", strings.Title(template.Frequency)))
		lines = append(lines, fmt.Sprintf("   ⏰ Next run: %s", nextRun))
		lines = append(lines, "")
	}

	msg := tgbotapi.NewMessage(chatID, strings.Join(lines, "\n"))
	msg.ParseMode = "Markdown"
	bot.Send(msg)

	return nil
}
