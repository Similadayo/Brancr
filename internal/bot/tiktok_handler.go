package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/social"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
)

// handleTikTokCommand handles TikTok-specific commands from Telegram
func (r *IntentRouter) handleTikTokCommand(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, intent *TenantIntent) (bool, error) {
	action := intent.Parameters["tiktok_action"]
	if action == "" {
		// Try to infer from message
		lower := strings.ToLower(intent.RawMessage)
		if strings.Contains(lower, "list") || strings.Contains(lower, "videos") {
			action = "list_videos"
		} else if strings.Contains(lower, "analytics") || strings.Contains(lower, "stats") {
			action = "analytics"
		} else if strings.Contains(lower, "comments") {
			action = "comments"
		} else if strings.Contains(lower, "status") {
			action = "status"
		} else {
			// Show TikTok menu
			r.showTikTokMenu(bot, chatID, tenantID)
			return true, nil
		}
	}

	switch action {
	case "list_videos", "videos":
		return r.handleTikTokListVideos(bot, tenantID, chatID)
	case "analytics", "stats":
		// TikTok analytics removed
		bot.Send(tgbotapi.NewMessage(chatID, "❌ TikTok analytics functionality has been removed. Analytics collection for TikTok is no longer supported."))
		return true, nil
	case "comments":
		videoID := intent.Parameters["video_id"]
		return r.handleTikTokComments(bot, tenantID, chatID, videoID)
	case "status":
		publishID := intent.Parameters["publish_id"]
		return r.handleTikTokStatus(bot, tenantID, chatID, publishID)
	case "reply":
		videoID := intent.Parameters["video_id"]
		commentID := intent.Parameters["comment_id"]
		replyText := intent.Parameters["reply_text"]
		return r.handleTikTokReply(bot, tenantID, chatID, videoID, commentID, replyText)
	case "delete":
		videoID := intent.Parameters["video_id"]
		return r.handleTikTokDelete(bot, tenantID, chatID, videoID)
	default:
		r.showTikTokMenu(bot, chatID, tenantID)
		return true, nil
	}
}

// showTikTokMenu shows the TikTok management menu
func (r *IntentRouter) showTikTokMenu(bot *tgbotapi.BotAPI, chatID int64, tenantID uint) {
	// Check if TikTok is connected
	var acc storage.SocialAccount
	connected := r.Manager.DB.Where("tenant_id = ? AND platform = ?", tenantID, "tiktok").First(&acc).Error == nil && acc.Connected

	if !connected {
		msg := tgbotapi.NewMessage(chatID, "⚠️ TikTok account not connected.\n\nConnect your TikTok account first to manage videos and reply to comments.")
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🔗 Connect TikTok", "connect:tiktok"),
			),
		)
		msg.ReplyMarkup = keyboard
		bot.Send(msg)
		return
	}

	text := `🎵 *TikTok Management*

What would you like to do?

• List videos: "tiktok videos" or "show my tiktok videos"
• Video management: List, view, and delete videos
• View comments: "tiktok comments [video_id]"
• Check status: "tiktok status [publish_id]"
• Reply to comment: "tiktok reply [video_id] [comment_id] [text]"
• Delete video: "tiktok delete [video_id]"

Or just say "tiktok" for quick stats!`

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📹 List Videos", "tiktok:list"),
			// Analytics button removed - TikTok analytics disabled
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💬 Comments", "tiktok:comments"),
			tgbotapi.NewInlineKeyboardButtonData("🔍 Check Status", "tiktok:status"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

// getTikTokConnector gets the TikTok connector and account
func (r *IntentRouter) getTikTokConnector(tenantID uint) (*social.TikTokConnector, *storage.SocialAccount, error) {
	// Get TikTok account
	var acc storage.SocialAccount
	if err := r.Manager.DB.Where("tenant_id = ? AND platform = ?", tenantID, "tiktok").First(&acc).Error; err != nil {
		return nil, nil, fmt.Errorf("TikTok account not connected")
	}

	if !acc.Connected {
		return nil, nil, fmt.Errorf("TikTok account not connected")
	}

	// Get TikTok connector
	connector, ok := social.Connectors["tiktok"]
	if !ok {
		return nil, nil, fmt.Errorf("TikTok connector not available")
	}

	tikTokConnector, ok := connector.(*social.TikTokConnector)
	if !ok {
		return nil, nil, fmt.Errorf("TikTok connector type assertion failed")
	}

	// Decrypt token
	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		decryptedToken = acc.AccessToken
	}
	acc.AccessToken = decryptedToken

	return tikTokConnector, &acc, nil
}

// handleTikTokListVideos lists all TikTok videos
func (r *IntentRouter) handleTikTokListVideos(bot *tgbotapi.BotAPI, tenantID uint, chatID int64) (bool, error) {
	connector, acc, err := r.getTikTokConnector(tenantID)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ %s", err.Error())))
		return true, nil
	}

	result, err := connector.ListVideos(context.Background(), acc, 20, "")
	if err != nil {
		r.Manager.Logger.Error("failed to list TikTok videos", zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Failed to fetch videos. Please try again later."))
		return true, nil
	}

	videos, _ := result["videos"].([]interface{})
	count, _ := result["count"].(float64)

	if len(videos) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "📹 No videos found. Start posting to TikTok to see them here!"))
		return true, nil
	}

	text := fmt.Sprintf("📹 *Your TikTok Videos* (%d)\n\n", int(count))
	for i, v := range videos {
		if i >= 10 { // Limit to 10 videos
			text += fmt.Sprintf("\n... and %d more videos", len(videos)-10)
			break
		}
		video, ok := v.(map[string]interface{})
		if !ok {
			continue
		}

		videoID, _ := video["video_id"].(string)
		createTime, _ := video["create_time"].(float64)
		viewCount, _ := video["view_count"].(float64)

		text += fmt.Sprintf("🎬 *Video %d*\n", i+1)
		if videoID != "" {
			text += fmt.Sprintf("ID: `%s`\n", videoID)
		}
		if createTime > 0 {
			t := time.Unix(int64(createTime), 0)
			text += fmt.Sprintf("Posted: %s\n", t.Format("Jan 2, 2006"))
		}
		if viewCount > 0 {
			text += fmt.Sprintf("Views: %.0f\n", viewCount)
		}
		text += "\n"
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	bot.Send(msg)
	return true, nil
}

// handleTikTokAnalytics - REMOVED: TikTok analytics functionality disabled
// This function has been removed. TikTok analytics collection is no longer supported.
func (r *IntentRouter) handleTikTokAnalytics(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, videoID string) (bool, error) {
	bot.Send(tgbotapi.NewMessage(chatID, "❌ TikTok analytics functionality has been removed. Analytics collection for TikTok is no longer supported."))
	return true, nil
}

// handleTikTokComments lists comments for a video
func (r *IntentRouter) handleTikTokComments(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, videoID string) (bool, error) {
	if videoID == "" {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Please specify a video ID.\n\nExample: `tiktok comments video_123`"))
		return true, nil
	}

	connector, acc, err := r.getTikTokConnector(tenantID)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ %s", err.Error())))
		return true, nil
	}

	result, err := connector.ListComments(context.Background(), acc, videoID, 20, "")
	if err != nil {
		r.Manager.Logger.Error("failed to list comments", zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Failed to fetch comments."))
		return true, nil
	}

	comments, _ := result["comments"].([]interface{})
	count, _ := result["count"].(float64)

	if len(comments) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "💬 No comments yet. Keep creating great content!"))
		return true, nil
	}

	text := fmt.Sprintf("💬 *Comments* (%d)\n\n", int(count))
	for i, c := range comments {
		if i >= 10 {
			text += fmt.Sprintf("\n... and %d more comments", len(comments)-10)
			break
		}
		comment, ok := c.(map[string]interface{})
		if !ok {
			continue
		}

		commentID, _ := comment["comment_id"].(string)
		commentText, _ := comment["text"].(string)
		username, _ := comment["username"].(string)

		text += fmt.Sprintf("💬 *Comment %d*\n", i+1)
		if username != "" {
			text += fmt.Sprintf("From: %s\n", username)
		}
		if commentText != "" {
			text += fmt.Sprintf("Text: %s\n", commentText)
		}
		if commentID != "" {
			text += fmt.Sprintf("ID: `%s`\n", commentID)
		}
		text += "\n"
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	bot.Send(msg)
	return true, nil
}

// handleTikTokStatus checks video publish status
func (r *IntentRouter) handleTikTokStatus(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, publishID string) (bool, error) {
	if publishID == "" {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Please specify a publish ID.\n\nExample: `tiktok status publish_123`"))
		return true, nil
	}

	connector, acc, err := r.getTikTokConnector(tenantID)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ %s", err.Error())))
		return true, nil
	}

	result, err := connector.CheckVideoStatus(context.Background(), acc, publishID)
	if err != nil {
		r.Manager.Logger.Error("failed to check status", zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Failed to check status."))
		return true, nil
	}

	status, _ := result["status"].(string)
	videoID, _ := result["video_id"].(string)

	statusEmoji := "⏳"
	if status == "PUBLISHED" {
		statusEmoji = "✅"
	} else if status == "FAILED" {
		statusEmoji = "❌"
	}

	text := fmt.Sprintf("%s *Video Status*\n\nStatus: %s\n", statusEmoji, status)
	if videoID != "" {
		text += fmt.Sprintf("Video ID: `%s`\n", videoID)
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	bot.Send(msg)
	return true, nil
}

// handleTikTokReply replies to a comment
func (r *IntentRouter) handleTikTokReply(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, videoID, commentID, replyText string) (bool, error) {
	if videoID == "" || commentID == "" || replyText == "" {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Please provide video ID, comment ID, and reply text.\n\nExample: `tiktok reply video_123 comment_456 Thanks for watching!`"))
		return true, nil
	}

	connector, acc, err := r.getTikTokConnector(tenantID)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ %s", err.Error())))
		return true, nil
	}

	_, err = connector.ReplyToComment(context.Background(), acc, videoID, commentID, replyText)
	if err != nil {
		r.Manager.Logger.Error("failed to reply to comment", zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Failed to send reply. Please try again."))
		return true, nil
	}

	bot.Send(tgbotapi.NewMessage(chatID, "✅ Reply sent successfully!"))
	return true, nil
}

// handleTikTokDelete deletes a video
func (r *IntentRouter) handleTikTokDelete(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, videoID string) (bool, error) {
	if videoID == "" {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Please specify a video ID.\n\nExample: `tiktok delete video_123`"))
		return true, nil
	}

	connector, acc, err := r.getTikTokConnector(tenantID)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ %s", err.Error())))
		return true, nil
	}

	err = connector.DeleteVideo(context.Background(), acc, videoID)
	if err != nil {
		r.Manager.Logger.Error("failed to delete video", zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Failed to delete video."))
		return true, nil
	}

	bot.Send(tgbotapi.NewMessage(chatID, "✅ Video deleted successfully!"))
	return true, nil
}

