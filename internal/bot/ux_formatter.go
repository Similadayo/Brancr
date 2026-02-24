package bot

import (
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// UXFormatter provides consistent, professional message formatting for the Telegram bot
type UXFormatter struct{}

// FormatSlotPromptWithSuggestions creates a prompt with quick-reply buttons
func (f *UXFormatter) FormatSlotPromptWithSuggestions(prompt string, suggestions []string) (string, tgbotapi.InlineKeyboardMarkup) {
	var rows [][]tgbotapi.InlineKeyboardButton

	for _, suggestion := range suggestions {
		btn := tgbotapi.NewInlineKeyboardButtonData(suggestion, fmt.Sprintf("slot:%s", suggestion))
		rows = append(rows, []tgbotapi.InlineKeyboardButton{btn})
	}

	return prompt, tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// FormatConfirmation creates a Yes/No confirmation message
func (f *UXFormatter) FormatConfirmation(message string, confirmAction string) (string, tgbotapi.InlineKeyboardMarkup) {
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Yes", fmt.Sprintf("confirm:%s", confirmAction)),
			tgbotapi.NewInlineKeyboardButtonData("❌ No", "confirm:cancel"),
		),
	)
	return message, keyboard
}

// FormatErrorWithFallback creates error message with button options
func (f *UXFormatter) FormatErrorWithFallback(message string, options []string) (string, tgbotapi.InlineKeyboardMarkup) {
	msg := fmt.Sprintf("🤔 %s\n\nTry one of these:", message)

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, opt := range options {
		btn := tgbotapi.NewInlineKeyboardButtonData(opt, fmt.Sprintf("fallback:%s", opt))
		rows = append(rows, []tgbotapi.InlineKeyboardButton{btn})
	}

	return msg, tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// FormatMainMenu creates a clean, professional main menu message
func (f *UXFormatter) FormatMainMenu(tenant storage.Tenant, persona storage.Persona, accounts []storage.SocialAccount, stats TodayStats) string {
	var lines []string

	// Header - Clean and minimal
	lines = append(lines, "✨ *Brancr Dashboard*")
	lines = append(lines, "")

	// Quick stats in a clean format
	if stats.HasActivity {
		lines = append(lines, "📊 *Today's Activity*")
		if stats.Posts > 0 {
			lines = append(lines, fmt.Sprintf("  • Posts published: %d", stats.Posts))
		}
		if stats.Messages > 0 {
			lines = append(lines, fmt.Sprintf("  • Messages received: %d", stats.Messages))
		}
		if stats.Escalations > 0 {
			lines = append(lines, fmt.Sprintf("  • Escalations: %d", stats.Escalations))
		}
		lines = append(lines, "")
	}

	// Unread notifications
	if stats.UnreadCount > 0 {
		lines = append(lines, fmt.Sprintf("🔔 *%d unread message(s) need your attention*", stats.UnreadCount))
		lines = append(lines, "")
	}

	// Platform connections - Clean grid format
	lines = append(lines, "🔗 *Connected Platforms*")

	platforms := []struct {
		Name      string
		Icon      string
		Connected bool
	}{
		{"Instagram", "📷", f.isPlatformConnected(accounts, "instagram")},
		{"Facebook", "👥", f.isPlatformConnected(accounts, "facebook")},
		{"WhatsApp", "💬", f.isPlatformConnected(accounts, "whatsapp")},
		{"TikTok", "🎵", f.isPlatformConnected(accounts, "tiktok")},
	}

	for _, p := range platforms {
		status := "Not connected"
		if p.Connected {
			status = "Connected"
		}
		icon := "❌"
		if p.Connected {
			icon = "✅"
		}
		lines = append(lines, fmt.Sprintf("  %s %s: %s", icon, p.Name, status))
	}

	lines = append(lines, "")
	lines = append(lines, "What would you like to do?")

	return strings.Join(lines, "\n")
}

// CreateMainMenuKeyboard creates a clean, organized keyboard layout
func (f *UXFormatter) CreateMainMenuKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		// Primary actions - Most used
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📱 Social Accounts", "menu:connect"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📊 Analytics", "menu:analytics"),
			tgbotapi.NewInlineKeyboardButtonData("💬 Business Info", "menu:business"),
		),
		// Secondary actions
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📦 Products", "products:menu"),
			tgbotapi.NewInlineKeyboardButtonData("📋 Orders", "orders:menu"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💳 Payments", "payments:menu"),
			tgbotapi.NewInlineKeyboardButtonData("🤖 AI Settings", "menu:ai"),
		),
		// Help
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❓ Help & Support", "help:menu"),
		),
	)
}

// FormatConnectionStatus creates a clean connection status message
func (f *UXFormatter) FormatConnectionStatus(accounts []storage.SocialAccount) string {
	var lines []string

	lines = append(lines, "🔗 *Account Connections*")
	lines = append(lines, "")
	lines = append(lines, "Manage your social media platform connections:")
	lines = append(lines, "")

	platforms := map[string]string{
		"instagram": "📷 Instagram",
		"facebook":  "👥 Facebook",
		"whatsapp":  "💬 WhatsApp",
		"tiktok":    "🎵 TikTok",
	}

	// Get account status
	statusMap := make(map[string]*storage.SocialAccount)
	for i := range accounts {
		acc := &accounts[i]
		if _, ok := platforms[acc.Platform]; ok {
			statusMap[acc.Platform] = acc
		}
	}

	for platform, name := range platforms {
		acc := statusMap[platform]
		if acc == nil || !acc.Connected {
			lines = append(lines, fmt.Sprintf("%s ❌ *Not connected*", name))
		} else {
			timestamp := "just now"
			if !acc.UpdatedAt.IsZero() {
				timestamp = acc.UpdatedAt.Format("Jan 2, 3:04 PM")
			}
			method := ""
			if acc.Platform == "instagram" {
				if acc.LoginMethod == "instagram_login" {
					method = " (Instagram Login)"
				} else if acc.LoginMethod == "facebook_login" {
					method = " (Facebook Login)"
				}
			}
			lines = append(lines, fmt.Sprintf("%s ✅ *Connected*%s", name, method))
			lines = append(lines, fmt.Sprintf("   Updated: %s", timestamp))
		}
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}

// CreateConnectionKeyboard creates organized connection buttons
func (f *UXFormatter) CreateConnectionKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		// Primary platforms - One per row for clarity
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📷 Instagram", "connect:instagram"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("👥 Facebook", "connect:facebook"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💬 WhatsApp", "connect:whatsapp"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🎵 TikTok", "connect:tiktok"),
		),
		// Actions
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Refresh Status", "menu:connect"),
		),
		// Navigation
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("← Back to Menu", "menu:main"),
		),
	)
}

// FormatHelpMessage creates a warm, conversational help message (V2 UX)
func (f *UXFormatter) FormatHelpMessage() string {
	lines := []string{
		"Hey! 👋 Just tell me what you need:",
		"",
		"*Try saying:*",
		"• \"I want to post something\"",
		"• \"How are my posts doing?\"",
		"• \"Connect Instagram\"",
		"• \"Check my orders\"",
		"",
		"Or ask me anything! I'm here to help.",
		"",
		"_Need a human? Reach out at support@brancr.com_",
	}

	return strings.Join(lines, "\n")
}

// FormatErrorMessage creates user-friendly error messages
func (f *UXFormatter) FormatErrorMessage(err error, context string) string {
	if err == nil {
		return "Something went wrong. Please try again."
	}

	errMsg := strings.ToLower(err.Error())

	// Map common errors to user-friendly messages
	if strings.Contains(errMsg, "not found") {
		return fmt.Sprintf("❌ %s not found. Please check and try again.", context)
	}
	if strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "deadline") {
		return "⏱️ The request took too long. Please try again in a moment."
	}
	if strings.Contains(errMsg, "unauthorized") || strings.Contains(errMsg, "permission") {
		return "🔒 You don't have permission to do that. Contact support if you need access."
	}
	if strings.Contains(errMsg, "network") || strings.Contains(errMsg, "connection") {
		return "🌐 Connection issue. Please check your internet and try again."
	}

	// Generic error
	return fmt.Sprintf("❌ Unable to %s right now. Please try again shortly.", context)
}

// FormatSuccessMessage creates consistent success messages
func (f *UXFormatter) FormatSuccessMessage(action string, details ...string) string {
	msg := fmt.Sprintf("✅ %s", action)
	if len(details) > 0 {
		msg += "\n\n" + strings.Join(details, "\n")
	}
	return msg
}

// FormatInfoMessage creates clean info messages
func (f *UXFormatter) FormatInfoMessage(title string, lines []string) string {
	parts := []string{fmt.Sprintf("ℹ️ *%s*", title), ""}
	parts = append(parts, lines...)
	return strings.Join(parts, "\n")
}

// Helper methods
func (f *UXFormatter) isPlatformConnected(accounts []storage.SocialAccount, platform string) bool {
	for _, acc := range accounts {
		if acc.Platform == platform && acc.Connected {
			return true
		}
	}
	return false
}

// TodayStats holds today's activity statistics
type TodayStats struct {
	Posts       int64
	Messages    int64
	Escalations int64
	UnreadCount int64
	HasActivity bool
}

// GetTodayStats calculates today's statistics for a tenant
func GetTodayStats(db *gorm.DB, tenantID uuid.UUID) TodayStats {
	today := time.Now()
	todayStart := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)

	var postsToday int64
	db.Model(&storage.ScheduledPost{}).
		Where("tenant_id = ? AND created_at >= ? AND status = ?", tenantID, todayStart, "posted").
		Count(&postsToday)

	var messagesToday int64
	db.Model(&storage.Interaction{}).
		Where("tenant_id = ? AND created_at >= ? AND direction = ?", tenantID, todayStart, "incoming").
		Count(&messagesToday)

	var escalationsToday int64
	db.Model(&storage.Interaction{}).
		Where("tenant_id = ? AND response_type = ? AND response_status = ? AND created_at >= ?", tenantID, "escalated", "pending", todayStart).
		Count(&escalationsToday)

	var unreadCount int64
	db.Model(&storage.Interaction{}).
		Where("tenant_id = ? AND response_status = ? AND response_type = ?", tenantID, "pending", "escalated").
		Count(&unreadCount)

	return TodayStats{
		Posts:       postsToday,
		Messages:    messagesToday,
		Escalations: escalationsToday,
		UnreadCount: unreadCount,
		HasActivity: postsToday > 0 || messagesToday > 0 || escalationsToday > 0,
	}
}
