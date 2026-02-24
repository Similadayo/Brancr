package bot

import (
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// showPostPreview displays a visual preview of the post before finalizing
func (m *TenantBotManager) showPostPreview(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, sess *PostSession) {
	// Load media asset
	var ma storage.MediaAsset
	if err := m.DB.First(&ma, sess.MediaAssetID).Error; err != nil {
		m.Logger.Error("failed to load media asset for preview", zap.Stringer("asset_id", sess.MediaAssetID), zap.Stringer("tenant_id", tenantID), zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Couldn't find that media. Please try uploading again."))
		return
	}

	// Parse scheduled time
	var tenant storage.Tenant
	if err := m.DB.First(&tenant, tenantID).Error; err != nil {
		m.Logger.Error("failed to load tenant for preview", zap.Stringer("tenant_id", tenantID), zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Couldn't find your account. Please try again."))
		return
	}
	if tenant.Timezone == "" {
		tenant.Timezone = "UTC"
	}

	scheduledAt, err := m.parseNaturalScheduleWithTenant(sess.ScheduleInput, tenantID)
	if err != nil {
		m.showSchedulingErrorWithSuggestions(bot, chatID, err)
		return
	}

	// Helper for concise visual formatting
	mediaTypeEmoji := "📸"
	if ma.Type == "video" {
		mediaTypeEmoji = "📹"
	} else if ma.Type == "carousel" {
		mediaTypeEmoji = "🎠"
	}

	// Caption logic
	caption := sess.Caption
	if sess.UseAICaption {
		caption = "✨ (AI will write this for you)"
	} else if caption == "" {
		caption = "(No caption)"
	} else if len(caption) > 100 {
		caption = caption[:97] + "..."
	}

	// Platforms abbreviations
	var platformShorts []string
	for _, p := range sess.Platforms {
		switch p {
		case "instagram":
			platformShorts = append(platformShorts, "IG")
		case "facebook":
			platformShorts = append(platformShorts, "FB")
		case "tiktok":
			platformShorts = append(platformShorts, "TikTok")
		case "youtube":
			platformShorts = append(platformShorts, "YT")
		case "whatsapp":
			platformShorts = append(platformShorts, "WA")
		}
	}
	platformsStr := strings.Join(platformShorts, ", ")
	if platformsStr == "" {
		platformsStr = "IG"
	}

	// Schedule check
	now := time.Now()
	isNow := scheduledAt.Sub(now) < 5*time.Minute
	scheduleStr := scheduledAt.Format("Jan 2, 3:04 PM")
	if isNow {
		scheduleStr = "Now"
	}

	// Build the "Card"
	previewText := fmt.Sprintf(
		"📝 *Proof Your Post*\n\n"+
			"%s *Media:* %s\n"+
			"💬 *Caption:* %s\n"+
			"🚀 *Post to:* %s\n"+
			"⏰ *When:* %s\n\n"+
			"Look good?",
		mediaTypeEmoji, strings.Title(ma.Type),
		caption,
		platformsStr,
		scheduleStr,
	)

	// Cleaner Keyboard
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🚀 Ship It", "postflow:confirm"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✏️ Edit", "postflow:edit_caption"),
			tgbotapi.NewInlineKeyboardButtonData("⏰ Reschedule", "postflow:edit_schedule"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "postflow:cancel"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, previewText)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)

	sess.Step = 6
}

// showSchedulingErrorWithSuggestions shows friendly error with format suggestions
func (m *TenantBotManager) showSchedulingErrorWithSuggestions(bot *tgbotapi.BotAPI, chatID int64, err error) {
	errorMsg := "❌ *Couldn't understand that time.*\n\n"
	errorMsg += "💡 *Try these formats:*\n"
	errorMsg += "• `now` - Post immediately\n"
	errorMsg += "• `tomorrow morning` - 9 AM tomorrow\n"
	errorMsg += "• `tomorrow afternoon` - 2 PM tomorrow\n"
	errorMsg += "• `tomorrow at 3pm` - Specific time tomorrow\n"
	errorMsg += "• `next week at 10am` - Next week at 10 AM\n"
	errorMsg += "• `in 2 hours` - Relative time\n"
	errorMsg += "\nOr use exact format: `Jan 15 at 3:30 PM`"

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🚀 Post Now", "postflow:post_now"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, errorMsg)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}
