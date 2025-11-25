package bot

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// showPostPreview displays a visual preview of the post before finalizing
func (m *TenantBotManager) showPostPreview(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, sess *PostSession) {
	// Load media asset
	var ma storage.MediaAsset
	if err := m.DB.First(&ma, sess.MediaAssetID).Error; err != nil {
		m.Logger.Error("failed to load media asset for preview", zap.Uint("asset_id", sess.MediaAssetID), zap.Uint("tenant_id", tenantID), zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Couldn't find that media. Please try uploading again."))
		return
	}

	// Parse scheduled time
	var tenant storage.Tenant
	if err := m.DB.First(&tenant, tenantID).Error; err != nil {
		m.Logger.Error("failed to load tenant for preview", zap.Uint("tenant_id", tenantID), zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Couldn't find your account. Please try again."))
		return
	}
	if tenant.Timezone == "" {
		tenant.Timezone = "UTC"
	}

	scheduledAt, err := m.parseNaturalScheduleWithTenant(sess.ScheduleInput, tenantID)
	if err != nil {
		m.Logger.Warn("failed to parse schedule in preview", zap.String("schedule_input", sess.ScheduleInput), zap.Uint("tenant_id", tenantID), zap.Error(err))
		m.showSchedulingErrorWithSuggestions(bot, chatID, err)
		return
	}
	
	m.Logger.Info("showing post preview", 
		zap.Uint("tenant_id", tenantID),
		zap.Uint("asset_id", sess.MediaAssetID),
		zap.String("schedule_input", sess.ScheduleInput),
		zap.Time("scheduled_at", scheduledAt),
		zap.Strings("platforms", sess.Platforms))

	// Build preview text
	var previewParts []string
	previewParts = append(previewParts, "📋 *Post Preview*")
	previewParts = append(previewParts, "")
	
	// Media info
	name := sess.Name
	if name == "" {
		name = ma.Name
	}
	if name == "" {
		name = ma.Type
	}
	
	if ma.Type == "carousel" {
		var urls []string
		_ = json.Unmarshal([]byte(ma.URLs), &urls)
		previewParts = append(previewParts, fmt.Sprintf("📸 *Media:* %d-image carousel", len(urls)))
	} else {
		previewParts = append(previewParts, fmt.Sprintf("📸 *Media:* %s", strings.Title(ma.Type)))
	}
	
	if name != "" {
		previewParts = append(previewParts, fmt.Sprintf("📝 *Name:* %s", name))
	}
	
	// Caption with platform-specific length warnings
	caption := sess.Caption
	if sess.UseAICaption {
		caption = "✨ AI-generated caption (will be optimized for each platform)"
	}
	
	// Get platforms first for caption length validation
	platforms := sess.Platforms
	if len(platforms) == 0 {
		platforms = []string{"instagram"} // Default
	}
	
	if caption != "" {
		captionPreview := caption
		if len(captionPreview) > 100 {
			captionPreview = captionPreview[:100] + "..."
		}
		previewParts = append(previewParts, fmt.Sprintf("💬 *Caption:* %s", captionPreview))
		
		// Check platform-specific caption length limits
		for _, platform := range platforms {
			length := len(caption)
			var warning string
			switch platform {
			case "instagram":
				if length > 2200 {
					warning = fmt.Sprintf("⚠️ Instagram caption exceeds 2200 chars (%d). It will be truncated.", length)
				} else if length > 125 {
					warning = fmt.Sprintf("💡 Instagram caption is %d chars (optimal is <125, max 2200).", length)
				}
			case "facebook":
				if length > 63206 {
					warning = fmt.Sprintf("⚠️ Facebook caption exceeds 63,206 chars (%d). It will be truncated.", length)
				} else if length > 250 {
					warning = fmt.Sprintf("💡 Facebook caption is %d chars (optimal is <250).", length)
				}
			case "tiktok":
				if length > 2200 {
					warning = fmt.Sprintf("⚠️ TikTok caption exceeds 2200 chars (%d). It will be truncated.", length)
				} else if length > 150 {
					warning = fmt.Sprintf("💡 TikTok caption is %d chars (optimal is <150).", length)
				}
			}
			if warning != "" {
				previewParts = append(previewParts, warning)
			}
		}
	} else {
		previewParts = append(previewParts, "💬 *Caption:* None")
	}
	
	// Platforms (already defined above)
	platformStr := ""
	for i, p := range platforms {
		if i > 0 {
			platformStr += ", "
		}
		platformStr += strings.Title(p)
	}
	previewParts = append(previewParts, fmt.Sprintf("📱 *Platforms:* %s", platformStr))
	
	// Schedule
	now := time.Now()
	timeUntil := scheduledAt.Sub(now)
	var scheduleStr string
	if timeUntil < 5*time.Minute {
		scheduleStr = "⏰ *Schedule:* Posting now"
	} else if scheduledAt.Year() == now.Year() && scheduledAt.YearDay() == now.YearDay() {
		scheduleStr = fmt.Sprintf("⏰ *Schedule:* Today at %s", scheduledAt.Format("3:04 PM"))
	} else if scheduledAt.Year() == now.Year() && scheduledAt.YearDay() == now.YearDay()+1 {
		scheduleStr = fmt.Sprintf("⏰ *Schedule:* Tomorrow at %s", scheduledAt.Format("3:04 PM"))
	} else {
		scheduleStr = fmt.Sprintf("⏰ *Schedule:* %s", scheduledAt.Format("Jan 2 at 3:04 PM"))
	}
	previewParts = append(previewParts, scheduleStr)
	
	previewParts = append(previewParts, "")
	previewParts = append(previewParts, "Does this look good?")
	
	previewText := strings.Join(previewParts, "\n")
	
	// Create keyboard with action buttons
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Confirm & Schedule", "postflow:confirm"),
			tgbotapi.NewInlineKeyboardButtonData("✏️ Edit Caption", "postflow:edit_caption"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🕐 Change Time", "postflow:edit_schedule"),
			tgbotapi.NewInlineKeyboardButtonData("📱 Change Platforms", "postflow:edit_platforms"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🚀 Post Now", "postflow:post_now"),
			tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "postflow:cancel"),
		),
	)
	
	msg := tgbotapi.NewMessage(chatID, previewText)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
	
	// Store schedule info for later use
	sess.Step = 6 // Preview step
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

