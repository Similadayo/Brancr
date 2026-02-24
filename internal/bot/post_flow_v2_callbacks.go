package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// ============================================================
// POST FLOW V2 CALLBACKS
// Handles inline keyboard button presses for the V2 posting flow
// ============================================================

// HandlePostFlowV2Callback handles all postv2: callbacks
func (h *TenantWebhookHandler) HandlePostFlowV2Callback(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, arg string) {
	ctx := context.Background()

	// Parse callback: postv2:<action>:<params>
	parts := strings.SplitN(arg, ":", 2)
	action := parts[0]
	param := ""
	if len(parts) > 1 {
		param = parts[1]
	}

	h.Manager.Logger.Debug("V2 post flow callback",
		zap.String("action", action),
		zap.String("param", param),
		zap.Stringer("tenant_id", tenantID))

	switch action {

	// === PLATFORM SELECTION ===
	case "platforms":
		h.handleV2PlatformCallback(bot, tenantID, chatID, param)

	// === BATCH MODE ===
	case "batch":
		h.handleV2BatchCallback(bot, tenantID, chatID, param)

	// === CAPTION ===
	case "caption":
		h.handleV2CaptionCallback(bot, tenantID, chatID, param)

	// === SCHEDULE ===
	case "schedule":
		h.handleV2ScheduleCallback(bot, tenantID, chatID, param, ctx)

	// === UNDO ===
	case "undo":
		h.handleV2UndoCallback(bot, tenantID, chatID, param)

	default:
		h.Manager.Logger.Debug("unhandled V2 callback", zap.String("action", action))
	}
}

// handleV2PlatformCallback handles platform selection
func (h *TenantWebhookHandler) handleV2PlatformCallback(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, param string) {
	ctx := context.Background()
	sess, ok := h.Manager.SessionManager.GetPostSessionV2(ctx, chatID)
	if !ok {
		bot.Send(tgbotapi.NewMessage(chatID, "Session expired. Send the media again?"))
		return
	}

	switch param {
	case "all":
		// Get all connected platforms
		var accounts []storage.SocialAccount
		h.Manager.DB.Where("tenant_id = ? AND connected = ?", sess.TenantID, true).Find(&accounts)
		for _, acc := range accounts {
			sess.Platforms = append(sess.Platforms, acc.Platform)
		}

	case "last":
		// Use last used platforms
		sess.Platforms = sess.LastPlatforms

	default:
		// Single platform toggle
		if strings.HasPrefix(param, "toggle:") {
			platform := strings.TrimPrefix(param, "toggle:")
			// Toggle platform in list
			found := false
			for i, p := range sess.Platforms {
				if p == platform {
					sess.Platforms = append(sess.Platforms[:i], sess.Platforms[i+1:]...)
					found = true
					break
				}
			}
			if !found {
				sess.Platforms = append(sess.Platforms, platform)
			}
			// Re-display platform selection
			h.Manager.promptSingleStart(bot, chatID, sess, sess.Platforms)
			h.Manager.SessionManager.SavePostSessionV2(ctx, chatID, sess)
			return
		}
	}

	// Move to caption step
	sess.Step = 1
	h.Manager.SessionManager.SavePostSessionV2(ctx, chatID, sess)
	h.Manager.promptCaptionV2(bot, chatID, sess)
}

// handleV2BatchCallback handles batch mode selection (carousel vs separate)
func (h *TenantWebhookHandler) handleV2BatchCallback(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, param string) {
	ctx := context.Background()
	sess, ok := h.Manager.SessionManager.GetPostSessionV2(ctx, chatID)
	if !ok {
		bot.Send(tgbotapi.NewMessage(chatID, "Session expired. Send the media again?"))
		return
	}

	switch param {
	case "carousel":
		// Treat as single carousel post
		sess.IsBatch = false
		h.Manager.SessionManager.SavePostSessionV2(ctx, chatID, sess)

		// Get connected platforms and prompt
		var accounts []storage.SocialAccount
		h.Manager.DB.Where("tenant_id = ? AND connected = ?", sess.TenantID, true).Find(&accounts)
		connected := []string{}
		for _, acc := range accounts {
			connected = append(connected, acc.Platform)
		}
		h.Manager.promptSingleStart(bot, chatID, sess, connected)

	case "separate":
		// Keep as separate posts
		sess.IsBatch = true
		h.Manager.SessionManager.SavePostSessionV2(ctx, chatID, sess)

		// Ask about spacing
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf(
			"How should I space out these %d posts?\n\n(Or tell me, e.g., \"post one each day\")",
			len(sess.MediaAssetIDs),
		))
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Every 24 hours", "postv2:spacing:24h"),
				tgbotapi.NewInlineKeyboardButtonData("Every 12 hours", "postv2:spacing:12h"),
			),
		)
		bot.Send(msg)
	}
}

// handleV2CaptionCallback handles caption-related callbacks
func (h *TenantWebhookHandler) handleV2CaptionCallback(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, param string) {
	ctx := context.Background()
	sess, ok := h.Manager.SessionManager.GetPostSessionV2(ctx, chatID)
	if !ok {
		bot.Send(tgbotapi.NewMessage(chatID, "Session expired. Send the media again?"))
		return
	}

	switch param {
	case "same":
		// Same caption for all batch items
		sess.CaptionMode = "same"
		h.Manager.SessionManager.SavePostSessionV2(ctx, chatID, sess)
		bot.Send(tgbotapi.NewMessage(chatID, "What should the caption be?\n\n(Type one, or say \"you pick\")"))

	case "ai":
		// AI writes captions
		sess.CaptionMode = "ai"
		h.Manager.SessionManager.SavePostSessionV2(ctx, chatID, sess)
		bot.Send(tgbotapi.NewMessage(chatID, "What's the vibe or theme?\n\n(e.g., \"summer sale\", \"behind the scenes\")"))

	case "use":
		// User approved AI caption, move to schedule
		sess.Step = 2
		h.Manager.SessionManager.SavePostSessionV2(ctx, chatID, sess)
		h.Manager.promptScheduleV2(bot, chatID, sess)

	case "regenerate":
		// Generate another caption using AI with vision
		platform := "instagram"
		if len(sess.Platforms) > 0 {
			platform = sess.Platforms[0]
		}
		var assetID uuid.UUID
		if len(sess.MediaAssetIDs) > 0 {
			assetID = sess.MediaAssetIDs[0]
		}

		bot.Send(tgbotapi.NewMessage(chatID, "✍️ Trying again..."))
		caption := h.Manager.generateAICaption(sess.TenantID, assetID, platform)
		sess.Captions = []string{caption}
		h.Manager.SessionManager.SavePostSessionV2(ctx, chatID, sess)

		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("How about:\n\n\"%s\"\n\nBetter?", caption))
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Use this ✓", "postv2:caption:use"),
				tgbotapi.NewInlineKeyboardButtonData("Another one", "postv2:caption:regenerate"),
			),
		)
		bot.Send(msg)
	}
}

// handleV2ScheduleCallback handles schedule selection
func (h *TenantWebhookHandler) handleV2ScheduleCallback(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, param string, ctx context.Context) {
	sess, ok := h.Manager.SessionManager.GetPostSessionV2(ctx, chatID)
	if !ok {
		bot.Send(tgbotapi.NewMessage(chatID, "Session expired. Send the media again?"))
		return
	}

	var scheduleTime time.Time
	var isNow bool

	switch param {
	case "now":
		isNow = true
		scheduleTime = time.Now()

	default:
		// Try to parse as Unix timestamp
		if unix, err := strconv.ParseInt(param, 10, 64); err == nil {
			scheduleTime = time.Unix(unix, 0)
		} else {
			// Try natural language
			parsed, err := parseScheduleTime(param)
			if err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "I didn't catch that time. Try \"tomorrow at 9am\" or tap a button."))
				return
			}
			scheduleTime = parsed
		}
	}

	// Finalize the post
	h.Manager.finalizePostV2(bot, chatID, sess, scheduleTime, isNow)

	// Clean up session
	h.Manager.SessionManager.DeletePostSessionV2(ctx, chatID)
}

// handleV2UndoCallback handles undo/cancel actions
func (h *TenantWebhookHandler) handleV2UndoCallback(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, postIDStr string) {
	postID, err := uuid.Parse(postIDStr)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "Couldn't find that post to undo."))
		return
	}

	// Delete the scheduled post
	if err := h.Manager.DB.Where("id = ? AND tenant_id = ?", postID, tenantID).Delete(&storage.ScheduledPost{}).Error; err != nil {
		h.Manager.Logger.Error("failed to undo scheduled post", zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "Couldn't undo that post. It may have already been posted."))
		return
	}

	bot.Send(tgbotapi.NewMessage(chatID, "Done! That post has been cancelled. 👍"))
}
