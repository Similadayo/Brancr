package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// handleCalendarCallback handles calendar-related callback queries
func (h *TenantWebhookHandler) handleCalendarCallback(bot *tgbotapi.BotAPI, tenantID uint, cq *tgbotapi.CallbackQuery) {
	// Acknowledge callback
	bot.Request(tgbotapi.NewCallback(cq.ID, "Loading calendar..."))

	data := cq.Data
	chatID := cq.Message.Chat.ID

	parts := strings.Split(data, ":")
	if len(parts) < 2 {
		return
	}

	action := parts[1]

	switch action {
	case "view", "month":
		// Show current month or specified month
		year := time.Now().Year()
		month := time.Now().Month()

		if len(parts) >= 4 {
			// Parse year and month from callback
			if y, err := strconv.Atoi(parts[2]); err == nil {
				year = y
			}
			if m, err := strconv.Atoi(parts[3]); err == nil && m >= 1 && m <= 12 {
				month = time.Month(m)
			}
		}

		calendarManager := NewContentCalendarManager(h.Manager.DB, h.Manager.Logger)
		calendarManager.SendCalendarView(context.Background(), bot, tenantID, chatID, year, month)

	case "week":
		// Show current week
		now := time.Now()
		calendarManager := NewContentCalendarManager(h.Manager.DB, h.Manager.Logger)
		entries, err := calendarManager.GetCalendarForWeek(context.Background(), tenantID, now)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Couldn't load calendar. Please try again."))
			return
		}

		var lines []string
		lines = append(lines, "📅 *This Week's Schedule*")
		lines = append(lines, "")

		if len(entries) == 0 {
			lines = append(lines, "No posts scheduled for this week.")
		} else {
			for _, entry := range entries {
				dateStr := entry.Date.Format("Mon, Jan 2")
				lines = append(lines, fmt.Sprintf("📌 *%s*", dateStr))
				if entry.Theme != "" {
					lines = append(lines, fmt.Sprintf("   🎨 Theme: %s", entry.Theme))
				}
				lines = append(lines, "")
			}
		}

		msg := tgbotapi.NewMessage(chatID, strings.Join(lines, "\n"))
		msg.ParseMode = "Markdown"
		bot.Send(msg)

	case "add_theme":
		// Start theme addition flow
		bot.Send(tgbotapi.NewMessage(chatID, "📝 Send me the theme for this date (e.g., 'Product Launch', 'Behind the Scenes'). Type 'cancel' to stop."))
		// Would need to store state for this flow
	}
}

// handleCalendarCommand handles text commands for calendar
func (h *TenantWebhookHandler) handleCalendarCommand(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, arg string) {
	calendarManager := NewContentCalendarManager(h.Manager.DB, h.Manager.Logger)
	ctx := context.Background()

	switch strings.ToLower(arg) {
	case "view", "":
		now := time.Now()
		calendarManager.SendCalendarView(ctx, bot, tenantID, chatID, now.Year(), now.Month())
	case "week":
		now := time.Now()
		entries, err := calendarManager.GetCalendarForWeek(ctx, tenantID, now)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Couldn't load calendar."))
			return
		}

		var lines []string
		lines = append(lines, "📅 *This Week's Schedule*")
		lines = append(lines, "")

		if len(entries) == 0 {
			lines = append(lines, "No posts scheduled for this week.")
		} else {
			for _, entry := range entries {
				dateStr := entry.Date.Format("Mon, Jan 2")
				lines = append(lines, fmt.Sprintf("📌 *%s*", dateStr))
				if entry.Theme != "" {
					lines = append(lines, fmt.Sprintf("   🎨 Theme: %s", entry.Theme))
				}
				lines = append(lines, "")
			}
		}

		msg := tgbotapi.NewMessage(chatID, strings.Join(lines, "\n"))
		msg.ParseMode = "Markdown"
		bot.Send(msg)
	default:
		now := time.Now()
		calendarManager.SendCalendarView(ctx, bot, tenantID, chatID, now.Year(), now.Month())
	}
}

