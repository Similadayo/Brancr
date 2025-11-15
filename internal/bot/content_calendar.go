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

// ContentCalendarManager handles content calendar operations
type ContentCalendarManager struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

// NewContentCalendarManager creates a new content calendar manager
func NewContentCalendarManager(db *gorm.DB, logger *zap.Logger) *ContentCalendarManager {
	return &ContentCalendarManager{
		DB:     db,
		Logger: logger,
	}
}

// GetCalendarForMonth returns calendar entries for a specific month
func (m *ContentCalendarManager) GetCalendarForMonth(
	ctx context.Context,
	tenantID uint,
	year int,
	month time.Month,
) ([]storage.ContentCalendar, error) {
	startDate := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	endDate := startDate.AddDate(0, 1, 0).Add(-time.Second)

	var entries []storage.ContentCalendar
	if err := m.DB.Where("tenant_id = ? AND date >= ? AND date <= ?", tenantID, startDate, endDate).
		Order("date ASC").
		Find(&entries).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch calendar: %w", err)
	}

	return entries, nil
}

// GetCalendarForWeek returns calendar entries for a specific week
func (m *ContentCalendarManager) GetCalendarForWeek(
	ctx context.Context,
	tenantID uint,
	startDate time.Time,
) ([]storage.ContentCalendar, error) {
	endDate := startDate.AddDate(0, 0, 7)

	var entries []storage.ContentCalendar
	if err := m.DB.Where("tenant_id = ? AND date >= ? AND date < ?", tenantID, startDate, endDate).
		Order("date ASC").
		Find(&entries).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch calendar: %w", err)
	}

	return entries, nil
}

// CreateOrUpdateCalendarEntry creates or updates a calendar entry
func (m *ContentCalendarManager) CreateOrUpdateCalendarEntry(
	ctx context.Context,
	tenantID uint,
	date time.Time,
	theme string,
	notes string,
) (*storage.ContentCalendar, error) {
	// Normalize date to start of day in UTC
	normalizedDate := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)

	var entry storage.ContentCalendar
	err := m.DB.Where("tenant_id = ? AND date = ?", tenantID, normalizedDate).
		First(&entry).Error

	if err == nil {
		// Update existing entry
		if theme != "" {
			entry.Theme = theme
		}
		if notes != "" {
			entry.Notes = notes
		}
		entry.Status = "planned"
		if err := m.DB.Save(&entry).Error; err != nil {
			return nil, fmt.Errorf("failed to update calendar entry: %w", err)
		}
		return &entry, nil
	}

	// Create new entry
	entry = storage.ContentCalendar{
		TenantID: tenantID,
		Date:     normalizedDate,
		Theme:    theme,
		Notes:    notes,
		Status:   "planned",
		Posts:    "[]", // Empty JSON array
	}

	if err := m.DB.Create(&entry).Error; err != nil {
		return nil, fmt.Errorf("failed to create calendar entry: %w", err)
	}

	m.Logger.Info("✅ Created calendar entry",
		zap.Uint("tenant_id", tenantID),
		zap.String("date", normalizedDate.Format("2006-01-02")),
		zap.String("theme", theme))

	return &entry, nil
}

// AddPostToCalendar adds a scheduled post to a calendar entry
func (m *ContentCalendarManager) AddPostToCalendar(
	ctx context.Context,
	tenantID uint,
	postID uint,
	postDate time.Time,
) error {
	// Normalize date to start of day
	normalizedDate := time.Date(postDate.Year(), postDate.Month(), postDate.Day(), 0, 0, 0, 0, time.UTC)

	var entry storage.ContentCalendar
	err := m.DB.Where("tenant_id = ? AND date = ?", tenantID, normalizedDate).
		First(&entry).Error

	if err != nil {
		// Create entry if it doesn't exist
		entry = storage.ContentCalendar{
			TenantID: tenantID,
			Date:     normalizedDate,
			Status:   "scheduled",
			Posts:    "[]",
		}
		if err := m.DB.Create(&entry).Error; err != nil {
			return fmt.Errorf("failed to create calendar entry: %w", err)
		}
	}

	// Parse existing posts
	var postIDs []uint
	if entry.Posts != "" && entry.Posts != "[]" {
		if err := json.Unmarshal([]byte(entry.Posts), &postIDs); err != nil {
			postIDs = []uint{}
		}
	}

	// Check if post already exists
	for _, id := range postIDs {
		if id == postID {
			return nil // Already added
		}
	}

	// Add new post ID
	postIDs = append(postIDs, postID)
	postsJSON, _ := json.Marshal(postIDs)
	entry.Posts = string(postsJSON)
	entry.Status = "scheduled"

	if err := m.DB.Save(&entry).Error; err != nil {
		return fmt.Errorf("failed to update calendar entry: %w", err)
	}

	return nil
}

// GetCalendarView generates a formatted calendar view for display
func (m *ContentCalendarManager) GetCalendarView(
	ctx context.Context,
	tenantID uint,
	year int,
	month time.Month,
) (string, error) {
	entries, err := m.GetCalendarForMonth(ctx, tenantID, year, month)
	if err != nil {
		return "", err
	}

	// Get tenant timezone
	var tenant storage.Tenant
	timezone := "UTC"
	if err := m.DB.First(&tenant, tenantID).Error; err == nil && tenant.Timezone != "" {
		timezone = tenant.Timezone
	}

	loc, _ := time.LoadLocation(timezone)
	if loc == nil {
		loc = time.UTC
	}

	// Build calendar view
	var lines []string
	lines = append(lines, fmt.Sprintf("📅 *Content Calendar - %s %d*", month.String(), year))
	lines = append(lines, "")

	// Group entries by date
	entriesByDate := make(map[string][]storage.ContentCalendar)
	for _, entry := range entries {
		dateKey := entry.Date.In(loc).Format("Jan 2")
		entriesByDate[dateKey] = append(entriesByDate[dateKey], entry)
	}

	// Display entries
	if len(entries) == 0 {
		lines = append(lines, "No scheduled posts for this month.")
		lines = append(lines, "")
		lines = append(lines, "💡 Tip: Schedule posts and they'll appear here automatically!")
	} else {
		// Sort dates
		dates := make([]string, 0, len(entriesByDate))
		for date := range entriesByDate {
			dates = append(dates, date)
		}

		// Simple date sorting (for display)
		for _, entry := range entries {
			dateStr := entry.Date.In(loc).Format("Jan 2 (Mon)")
			lines = append(lines, fmt.Sprintf("📌 *%s*", dateStr))

			// Count posts
			var postIDs []uint
			if entry.Posts != "" && entry.Posts != "[]" {
				json.Unmarshal([]byte(entry.Posts), &postIDs)
			}

			if len(postIDs) > 0 {
				lines = append(lines, fmt.Sprintf("   📝 %d post(s) scheduled", len(postIDs)))
			}

			if entry.Theme != "" {
				lines = append(lines, fmt.Sprintf("   🎨 Theme: %s", entry.Theme))
			}

			if entry.Notes != "" {
				notes := entry.Notes
				if len(notes) > 100 {
					notes = notes[:100] + "..."
				}
				lines = append(lines, fmt.Sprintf("   💬 %s", notes))
			}

			lines = append(lines, "")
		}
	}

	return strings.Join(lines, "\n"), nil
}

// SendCalendarView sends calendar view to Telegram chat
func (m *ContentCalendarManager) SendCalendarView(
	ctx context.Context,
	bot *tgbotapi.BotAPI,
	tenantID uint,
	chatID int64,
	year int,
	month time.Month,
) error {
	view, err := m.GetCalendarView(ctx, tenantID, year, month)
	if err != nil {
		return err
	}

	// Get current month/year for navigation
	now := time.Now()
	currentYear := now.Year()
	currentMonth := now.Month()

	// Build navigation keyboard
	var keyboardRows [][]tgbotapi.InlineKeyboardButton

	// Previous/Next month buttons
	prevMonth := month - 1
	prevYear := year
	if prevMonth < 1 {
		prevMonth = 12
		prevYear--
	}

	nextMonth := month + 1
	nextYear := year
	if nextMonth > 12 {
		nextMonth = 1
		nextYear++
	}

	keyboardRows = append(keyboardRows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Previous", fmt.Sprintf("calendar:month:%d:%d", prevYear, prevMonth)),
		tgbotapi.NewInlineKeyboardButtonData("Today", fmt.Sprintf("calendar:month:%d:%d", currentYear, currentMonth)),
		tgbotapi.NewInlineKeyboardButtonData("Next ➡️", fmt.Sprintf("calendar:month:%d:%d", nextYear, nextMonth)),
	))

	// Quick actions
	keyboardRows = append(keyboardRows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📅 This Week", fmt.Sprintf("calendar:week:%d:%d:%d", currentYear, int(currentMonth), now.Day())),
		tgbotapi.NewInlineKeyboardButtonData("➕ Add Theme", fmt.Sprintf("calendar:add_theme:%d:%d:%d", year, int(month), 1)),
	))

	msg := tgbotapi.NewMessage(chatID, view)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(keyboardRows...)
	bot.Send(msg)

	return nil
}

// AutoUpdateCalendarFromPosts automatically updates calendar when posts are scheduled
func (m *ContentCalendarManager) AutoUpdateCalendarFromPosts(
	ctx context.Context,
	tenantID uint,
	postID uint,
	scheduledAt *time.Time,
) error {
	if scheduledAt == nil {
		return nil
	}

	return m.AddPostToCalendar(ctx, tenantID, postID, *scheduledAt)
}

