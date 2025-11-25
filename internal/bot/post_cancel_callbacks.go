package bot

import (
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// handlePostCancelCallback processes post cancellation callbacks
// Callback format: "post:cancel:<post_id>" or "post:cancel_all"
func (h *TenantWebhookHandler) handlePostCancelCallback(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, arg string) {
	if arg == "cancel_all" {
		// Cancel all scheduled posts
		result := h.Manager.DB.Model(&storage.ScheduledPost{}).
			Where("tenant_id = ? AND status IN ?", tenantID, []string{"scheduled", "posting"}).
			Update("status", "cancelled")

		if result.Error != nil {
			h.Manager.Logger.Error("failed to cancel all posts", zap.Error(result.Error))
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Unable to cancel all posts. Please try again later."))
			return
		}

		if result.RowsAffected == 0 {
			bot.Send(tgbotapi.NewMessage(chatID, "✅ No scheduled posts to cancel."))
			return
		}

		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Cancelled %d post(s).", result.RowsAffected)))
		return
	}

	// Cancel individual post
	postIDStr := strings.TrimPrefix(arg, "cancel:")
	if postIDStr == "" {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Invalid post ID."))
		return
	}

	postID, err := strconv.ParseUint(postIDStr, 10, 32)
	if err != nil {
		h.Manager.Logger.Warn("invalid post ID in cancel callback", zap.String("id", postIDStr), zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Invalid post ID."))
		return
	}

	var post storage.ScheduledPost
	if err := h.Manager.DB.Where("id = ? AND tenant_id = ? AND status IN ?", postID, tenantID, []string{"scheduled", "posting"}).
		First(&post).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Couldn't find scheduled post (ID: %d). It may have already been cancelled or posted.", postID)))
		return
	}

	if err := h.Manager.DB.Model(&post).Update("status", "cancelled").Error; err != nil {
		h.Manager.Logger.Error("failed to cancel post", zap.Uint64("post_id", postID), zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Unable to cancel the post. Please try again later."))
		return
	}

	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Cancelled: *%s*", post.Name)))
}

