package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// handleBulkUploadCallback handles callback queries for bulk upload operations
func (h *TenantWebhookHandler) handleBulkUploadCallback(bot *tgbotapi.BotAPI, tenantID uint, cq *tgbotapi.CallbackQuery) {
	data := cq.Data
	chatID := cq.Message.Chat.ID

	// Acknowledge callback
	bot.Request(tgbotapi.NewCallback(cq.ID, "Processing..."))

	parts := strings.Split(data, ":")
	if len(parts) < 3 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Invalid bulk upload command."))
		return
	}

	action := parts[1]
	bulkSessionID := parts[len(parts)-1] // Last part is always session ID

	bulkManager := NewBulkUploadManager(h.Manager.DB, h.Manager.Logger)

	switch action {
	case "carousels":
		// User wants to create carousels - ask for count
		h.promptCarouselCount(bot, tenantID, chatID, bulkSessionID)
		return

	case "individual":
		// Create individual posts (not implemented yet - would be complex)
		bot.Send(tgbotapi.NewMessage(chatID, "Individual post creation from bulk upload is coming soon! For now, use carousels."))
		return

	case "custom":
		// Custom grouping (not implemented yet)
		bot.Send(tgbotapi.NewMessage(chatID, "Custom grouping is coming soon! For now, use carousels."))
		return

	case "ai":
		// AI organization (not implemented yet)
		bot.Send(tgbotapi.NewMessage(chatID, "AI organization is coming soon! For now, use carousels."))
		return

	case "carousel_count":
		// User selected carousel count
		if len(parts) >= 4 {
			count, err := strconv.Atoi(parts[2])
			if err == nil && count > 0 {
				h.handleCarouselCountSelection(bot, tenantID, chatID, bulkSessionID, count, bulkManager)
				return
			}
		}
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Invalid carousel count."))
		return

	case "schedule":
		// User selected scheduling strategy
		if len(parts) >= 3 {
			strategy := parts[2] // spread, custom, optimal
			h.handleBulkScheduleStrategy(bot, tenantID, chatID, bulkSessionID, strategy, bulkManager)
			return
		}
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Invalid schedule strategy."))
		return

	default:
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Unknown bulk upload action."))
		return
	}
}

// promptCarouselCount asks user how many carousels to create
func (h *TenantWebhookHandler) promptCarouselCount(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, bulkSessionID string) {
	// Get bulk session to know total items
	var bulkSession storage.BulkUploadSession
	if err := h.Manager.DB.Where("session_id = ? AND tenant_id = ?", bulkSessionID, tenantID).
		First(&bulkSession).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Couldn't find bulk upload session."))
		return
	}

	// Calculate suggested counts
	totalItems := bulkSession.TotalItems
	suggestedCounts := []int{2, 3, 4, 5}
	if totalItems > 50 {
		suggestedCounts = []int{4, 5, 6, 8}
	}

	msg := fmt.Sprintf(
		"📦 *How many carousels?*\n\n"+
			"I have %d images. How would you like to split them?\n\n"+
			"Recommended: %d carousels (~%d images each)",
		totalItems,
		suggestedCounts[1],
		totalItems/suggestedCounts[1],
	)

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, count := range suggestedCounts {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("%d carousels", count),
				fmt.Sprintf("bulk:carousel_count:%d:%s", count, bulkSessionID),
			),
		))
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
	reply := tgbotapi.NewMessage(chatID, msg)
	reply.ParseMode = "Markdown"
	reply.ReplyMarkup = keyboard
	bot.Send(reply)
}

// handleCarouselCountSelection processes user's carousel count selection
func (h *TenantWebhookHandler) handleCarouselCountSelection(
	bot *tgbotapi.BotAPI,
	tenantID uint,
	chatID int64,
	bulkSessionID string,
	carouselCount int,
	bulkManager *BulkUploadManager,
) {
	ctx := context.Background()

	// Get original session ID (remove "bulk_" prefix)
	originalSessionID := strings.TrimPrefix(bulkSessionID, "bulk_")

	// Get all items
	var items []storage.MediaItem
	if err := h.Manager.DB.Where("tenant_id = ? AND session_id = ?", tenantID, originalSessionID).
		Find(&items).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Couldn't find media items."))
		return
	}

	// Split into carousels
	assets, err := bulkManager.SplitIntoCarousels(ctx, tenantID, items, carouselCount, bulkSessionID)
	if err != nil {
		h.Manager.Logger.Error("failed to split into carousels", zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Failed to create carousels. Please try again."))
		return
	}

	// Update bulk session
	var bulkSession storage.BulkUploadSession
	if err := h.Manager.DB.Where("session_id = ? AND tenant_id = ?", bulkSessionID, tenantID).
		First(&bulkSession).Error; err == nil {
		bulkSession.Status = "completed"
		bulkSession.ProcessedItems = len(items)
		bulkSession.CarouselCount = len(assets)
		bulkSession.ItemsPerCarousel = len(items) / len(assets)
		h.Manager.DB.Save(&bulkSession)
	}

	// Prompt for scheduling
	msg := fmt.Sprintf(
		"✅ Created %d carousels from %d images!\n\n"+
			"When should I schedule them?\n\n"+
			"Option A: Spread over a month (one per week)\n"+
			"Option B: Custom dates\n"+
			"Option C: Optimal times (AI-suggested)",
		len(assets),
		len(items),
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("A: Spread", fmt.Sprintf("bulk:schedule:spread:%s", bulkSessionID)),
			tgbotapi.NewInlineKeyboardButtonData("B: Custom", fmt.Sprintf("bulk:schedule:custom:%s", bulkSessionID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("C: Optimal", fmt.Sprintf("bulk:schedule:optimal:%s", bulkSessionID)),
		),
	)

	reply := tgbotapi.NewMessage(chatID, msg)
	reply.ReplyMarkup = keyboard
	bot.Send(reply)
}

// handleBulkScheduleStrategy handles scheduling strategy selection
func (h *TenantWebhookHandler) handleBulkScheduleStrategy(
	bot *tgbotapi.BotAPI,
	tenantID uint,
	chatID int64,
	bulkSessionID string,
	strategy string,
	bulkManager *BulkUploadManager,
) {
	ctx := context.Background()

	// Get bulk session
	var bulkSession storage.BulkUploadSession
	if err := h.Manager.DB.Where("session_id = ? AND tenant_id = ?", bulkSessionID, tenantID).
		First(&bulkSession).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Couldn't find bulk upload session."))
		return
	}

	// Get all assets created from this bulk upload
	var assets []storage.MediaAsset
	if err := h.Manager.DB.Where("tenant_id = ? AND bulk_upload_id = ?", tenantID, bulkSessionID).
		Find(&assets).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Couldn't find carousels. Please try again."))
		return
	}

	// Default platforms
	platforms := []string{"instagram", "facebook"}

	// Auto-schedule
	if err := bulkManager.AutoScheduleCarousels(ctx, tenantID, assets, strategy, platforms); err != nil {
		h.Manager.Logger.Error("failed to auto-schedule", zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Failed to schedule: %v", err)))
		return
	}

	// Generate summary
	var summaryParts []string
	summaryParts = append(summaryParts, fmt.Sprintf("✅ Scheduled %d carousels!", len(assets)))

	// Get scheduled posts to show times
	var scheduledPosts []storage.ScheduledPost
	h.Manager.DB.Where("tenant_id = ? AND status = ?", tenantID, "scheduled").
		Order("scheduled_at ASC").
		Limit(len(assets)).
		Find(&scheduledPosts)

	if len(scheduledPosts) > 0 {
		summaryParts = append(summaryParts, "\n📅 Schedule:")
		for i, post := range scheduledPosts {
			if i >= len(assets) {
				break
			}
			// Convert UTC back to tenant timezone for display
			var tenant storage.Tenant
			h.Manager.DB.First(&tenant, tenantID)
			timezone := tenant.Timezone
			if timezone == "" {
				timezone = "UTC"
			}

			loc, _ := time.LoadLocation(timezone)
			if loc == nil {
				loc = time.UTC
			}

			displayTime := post.ScheduledAt.In(loc)
			summaryParts = append(summaryParts,
				fmt.Sprintf("%d. %s - %s", i+1, post.Name, displayTime.Format("Jan 2, 3:04 PM")))
		}
	}

	summaryParts = append(summaryParts, "\n🚀 All scheduled for Instagram & Facebook!")

	bot.Send(tgbotapi.NewMessage(chatID, strings.Join(summaryParts, "\n")))
}

