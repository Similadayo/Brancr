package bot

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
)

// Expected natural phrases:
// "schedule this for Oct 30 10:00 to Instagram, Facebook"
// "post this now to instagram"
// "post now"
// "schedule carousel auto_123 on 2025-10-30 10:00 GMT+1 to instagram"
// This function assumes the last created MediaAsset (or a referenced carousel name) is the target.
func (m *TenantBotManager) HandleSchedulingMessage(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, text string) {
	logger := m.Logger.With(zap.Uint("tenant", tenantID))
	lower := strings.ToLower(strings.TrimSpace(text))

	// Quick commands:
	if strings.HasPrefix(lower, "post this now") || lower == "post now" {
		// Find last MediaAsset for tenant
		var ma storage.MediaAsset
		if err := m.DB.Where("tenant_id = ?", tenantID).Order("created_at desc").First(&ma).Error; err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "⚠️ No media asset found to post."))
			return
		}
		// schedule immediate post
		now := time.Now()
		sp := storage.ScheduledPost{
			TenantID:      tenantID,
			Name:          ma.Name,
			Caption:       ma.Caption,
			MediaAssetIDs: fmt.Sprintf("[%d]", ma.ID),
			Platforms:     "[]",
			ScheduledAt:   &now,
			Status:        "scheduled",
		}
		if err := m.DB.Create(&sp).Error; err != nil {
			logger.Error("create scheduled post failed", zap.Error(err))
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Unable to schedule the post. Please try again later."))
			return
		}
		bot.Send(tgbotapi.NewMessage(chatID, "✅ Post scheduled and will be published now."))
		return
	}

	// schedule pattern: extract date and platforms
	// crude parser: find "schedule" and "to" keywords
	if strings.HasPrefix(lower, "schedule") {
		// find "for" or "on"
		var dtStr, platformInput string
		// naive: split on " to "
		parts := strings.Split(lower, " to ")
		if len(parts) > 1 {
			platformInput = parts[1]
		}
		// find the time phrase after "for" or "on"
		if idx := strings.Index(parts[0], " for "); idx != -1 {
			dtStr = strings.TrimSpace(parts[0][idx+5:])
		} else if idx := strings.Index(parts[0], " on "); idx != -1 {
			dtStr = strings.TrimSpace(parts[0][idx+4:])
		}

		// fallback: if dtStr empty, ask for clarification
		if dtStr == "" {
			bot.Send(tgbotapi.NewMessage(chatID, "When should I schedule this? e.g. `schedule this for Oct 30 10:00 to Instagram`"))
			return
		}

		// Parse datetime using dateparse; use tenant timezone if available
		loc := time.UTC
		if tz := os.Getenv("DEFAULT_TIMEZONE"); tz != "" {
			if l, err := time.LoadLocation(tz); err == nil {
				loc = l
			}
		}
		tm, err := utils.ParseSchedule(dtStr, loc)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Could not parse the schedule time. Try a different format like 'Oct 30 10:00' or 'tomorrow 10am'."))
			return
		}

		platforms := utils.NormalizePlatforms(platformInput)
		// find last media asset
		var ma storage.MediaAsset
		if err := m.DB.Where("tenant_id = ?", tenantID).Order("created_at desc").First(&ma).Error; err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "⚠️ No media asset found to schedule. Upload an image or carousel first."))
			return
		}

		mediaIDs := fmt.Sprintf("[%d]", ma.ID)
		sp := storage.ScheduledPost{
			TenantID:      tenantID,
			Name:          fmt.Sprintf("scheduled_%d", time.Now().Unix()),
			Caption:       ma.Caption,
			MediaAssetIDs: mediaIDs,
			Platforms:     toJSONString(platforms),
			ScheduledAt:   &tm,
			Status:        "scheduled",
		}
		if err := m.DB.Create(&sp).Error; err != nil {
			logger.Error("create scheduled post failed", zap.Error(err))
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Unable to schedule the post. Please try again later."))
			return
		}
		var platformDisplay string
		if len(platforms) > 0 {
			platformDisplay = strings.Join(platforms, ", ")
		} else {
			platformDisplay = "All connected platforms"
		}
		msg := fmt.Sprintf("✅ *Post Scheduled*\n\n📅 Scheduled for: %s\n📱 Platforms: %s\n\nYour post will be published automatically.", tm.Format("Jan 2, 2006 at 3:04 PM"), platformDisplay)
		response := tgbotapi.NewMessage(chatID, msg)
		response.ParseMode = "Markdown"
		bot.Send(response)
		return
	}

	// fallback: no recognized scheduling intent
	bot.Send(tgbotapi.NewMessage(chatID, "I didn't understand. Try: `schedule this for Oct 30 10:00 to Instagram` or `post now`"))
}

func toJSONString(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
