package bot

import (
	"context"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
)

// handlePostPreviewCallback handles preview action callbacks
func (h *TenantWebhookHandler) handlePostPreviewCallback(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, arg string) {
	var sess *PostSession
	var ok bool
	if h.Manager != nil && h.Manager.SessionManager != nil {
		sess, ok = h.Manager.SessionManager.GetPostSession(context.Background(), chatID)
	}
	if !ok || sess == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Session expired. Please start over."))
		return
	}

	switch arg {
	case "confirm":
		// Confirm and schedule
		h.Manager.finalizePostFlow(bot, tenantID, chatID, sess)
		if h.Manager != nil && h.Manager.SessionManager != nil {
			h.Manager.SessionManager.DeletePostSession(context.Background(), chatID)
		}

	case "post_now":
		// Post immediately
		sess.ScheduleInput = "now"
		h.Manager.finalizePostFlow(bot, tenantID, chatID, sess)
		if h.Manager != nil && h.Manager.SessionManager != nil {
			h.Manager.SessionManager.DeletePostSession(context.Background(), chatID)
		}

	case "edit_caption":
		// Return to caption editing
		sess.Step = 3
		if h.Manager != nil && h.Manager.SessionManager != nil {
			h.Manager.SessionManager.SavePostSession(context.Background(), chatID, sess)
		}
		bot.Send(tgbotapi.NewMessage(chatID, "✏️ Type your new caption, or say 'skip' to leave it blank."))

	case "edit_schedule":
		// Return to schedule editing
		sess.Step = 5
		if h.Manager != nil && h.Manager.SessionManager != nil {
			h.Manager.SessionManager.SavePostSession(context.Background(), chatID, sess)
		}
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🚀 Post Now", "postflow:post_now"),
			),
		)
		msg := tgbotapi.NewMessage(chatID, "When should I publish it? Type a time (e.g., \"tomorrow at 9am\") or use the button to post now.")
		msg.ReplyMarkup = keyboard
		bot.Send(msg)

	case "schedule":
		// User wants to schedule (wait for text input) - stay on Step 5
		bot.Send(tgbotapi.NewMessage(chatID, "Type when to publish it, e.g. \"tomorrow at 9am\" or \"next week at 10am\"."))

	case "edit_platforms":
		// Return to platform selection
		sess.Step = 4
		if h.Manager != nil && h.Manager.SessionManager != nil {
			h.Manager.SessionManager.SavePostSession(context.Background(), chatID, sess)
		}
		h.Manager.showPlatformSelection(bot, chatID, sess)

	case "cancel":
		// Cancel the post
		if h.Manager != nil && h.Manager.SessionManager != nil {
			h.Manager.SessionManager.DeletePostSession(context.Background(), chatID)
		}
		bot.Send(tgbotapi.NewMessage(chatID, "👌 Cancelled. Your media is still saved - you can schedule it later."))
	}
}
