package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// CustomGroupingSessions tracks custom grouping sessions
var CustomGroupingSessions sync.Map // chatID → bulkSessionID

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
		// Custom grouping - prompt user to specify groups
		h.promptCustomGrouping(bot, tenantID, chatID, bulkSessionID)
		return

	case "ai":
		// AI organization - analyze images and group them
		h.handleAIGrouping(bot, tenantID, chatID, bulkSessionID, bulkManager)
		return

	case "apply_custom_groups":
		// User submitted custom grouping instructions
		if len(parts) >= 3 {
			// The grouping instructions are in the callback data or we need to prompt for text input
			// For now, we'll use a session-based approach and wait for text input
			h.startCustomGroupingSession(bot, tenantID, chatID, bulkSessionID)
		}
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

// promptCustomGrouping prompts user to specify custom groups
func (h *TenantWebhookHandler) promptCustomGrouping(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, bulkSessionID string) {
	// Get bulk session
	var bulkSession storage.BulkUploadSession
	if err := h.Manager.DB.Where("session_id = ? AND tenant_id = ?", bulkSessionID, tenantID).
		First(&bulkSession).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Couldn't find bulk upload session."))
		return
	}

	// Get original session ID (remove "bulk_" prefix)
	originalSessionID := strings.TrimPrefix(bulkSession.SessionID, "bulk_")

	// Get all items
	var items []storage.MediaItem
	if err := h.Manager.DB.Where("tenant_id = ? AND session_id = ?", tenantID, originalSessionID).
		Order("id ASC").Find(&items).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Couldn't find media items."))
		return
	}

	// Build numbered list of images
	var imageList []string
	imageList = append(imageList, fmt.Sprintf("📸 *I have %d images:*\n", len(items)))
	
	maxShow := 30 // Show first 30 for brevity
	for i, item := range items {
		if i >= maxShow {
			imageList = append(imageList, fmt.Sprintf("\n... and %d more images", len(items)-maxShow))
			break
		}
		itemType := "🖼️"
		if item.ResourceType == "video" {
			itemType = "🎥"
		}
		imageList = append(imageList, fmt.Sprintf("%d. %s Image %d", i+1, itemType, i+1))
	}

	msg := strings.Join(imageList, "\n") + 
		"\n\n*How would you like to group them?*\n\n" +
		"Format examples:\n" +
		"• `Group 1: 1-3, 5, 7`\n" +
		"• `Group 1: 1-5; Group 2: 6-10`\n" +
		"• `1-4; 5-8; 9-12`\n\n" +
		"Just type your grouping instructions:"

	reply := tgbotapi.NewMessage(chatID, msg)
	reply.ParseMode = "Markdown"
	bot.Send(reply)

	// Store session to wait for user input
	CustomGroupingSessions.Store(chatID, bulkSessionID)
}

// startCustomGroupingSession handles user's custom grouping text input
func (h *TenantWebhookHandler) handleCustomGroupingInput(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, text string) {
	// Check if user is in custom grouping mode
	raw, ok := CustomGroupingSessions.Load(chatID)
	if !ok {
		return // Not in custom grouping mode
	}

	bulkSessionID := raw.(string)
	defer CustomGroupingSessions.Delete(chatID)

	// Get bulk session
	var bulkSession storage.BulkUploadSession
	if err := h.Manager.DB.Where("session_id = ? AND tenant_id = ?", bulkSessionID, tenantID).
		First(&bulkSession).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Couldn't find bulk upload session."))
		return
	}

	// Get original session ID
	originalSessionID := strings.TrimPrefix(bulkSession.SessionID, "bulk_")

	// Get all items
	var items []storage.MediaItem
	if err := h.Manager.DB.Where("tenant_id = ? AND session_id = ?", tenantID, originalSessionID).
		Order("id ASC").Find(&items).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Couldn't find media items."))
		return
	}

	// Parse grouping instructions
	groups, err := parseGroupingInstructions(text, len(items))
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Invalid format: %v\n\nTry: `Group 1: 1-3, 5; Group 2: 4, 6-10`", err)))
		CustomGroupingSessions.Store(chatID, bulkSessionID) // Keep session active
		return
	}

	// Create carousels from groups
	bulkManager := NewBulkUploadManager(h.Manager.DB, h.Manager.Logger)
	
	var assets []storage.MediaAsset
	for groupIdx, group := range groups {
		if len(group) == 0 {
			continue
		}

		var urls []string
		seenURLs := make(map[string]bool)
		for _, itemIdx := range group {
			if itemIdx >= 0 && itemIdx < len(items) {
				url := items[itemIdx].URL
				// Only add unique URLs to prevent duplicates
				if url != "" && !seenURLs[url] {
					urls = append(urls, url)
					seenURLs[url] = true
				}
			}
		}

		if len(urls) == 0 {
			h.Manager.Logger.Warn("skipping empty custom group",
				zap.Int("group_idx", groupIdx))
			continue
		}

		// Log for debugging
		h.Manager.Logger.Info("creating carousel from custom group",
			zap.Int("group_idx", groupIdx),
			zap.Int("url_count", len(urls)),
			zap.Int("indices_count", len(group)))

		urlsJSON, _ := json.Marshal(urls)
		asset := storage.MediaAsset{
			TenantID:     tenantID,
			Type:         "carousel",
			Name:         fmt.Sprintf("Custom Group %d", groupIdx+1),
			CarouselName: fmt.Sprintf("bulk_%s_group_%d", bulkSessionID, groupIdx+1),
			URLs:         string(urlsJSON),
			Status:       "uploaded",
			Platforms:    "[]",
			BulkUploadID: bulkSessionID,
		}

		if err := bulkManager.DB.Create(&asset).Error; err != nil {
			h.Manager.Logger.Error("failed to create custom group asset", zap.Error(err))
			continue
		}

		assets = append(assets, asset)
	}

	if len(assets) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ No valid groups created. Please check your grouping instructions."))
		return
	}

	// Update bulk session
	bulkSession.Status = "completed"
	bulkSession.ProcessedItems = len(items)
	bulkSession.CarouselCount = len(assets)
	bulkSession.SplitStrategy = "custom"
	h.Manager.DB.Save(&bulkSession)

	// Prompt for scheduling
	msg := fmt.Sprintf(
		"✅ Created %d custom groups from %d images!\n\n"+
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

// parseGroupingInstructions parses text like "Group 1: 1-3, 5; Group 2: 4, 6-10" into item indices
func parseGroupingInstructions(text string, totalItems int) ([][]int, error) {
	text = strings.TrimSpace(text)
	
	// Split by semicolon or newline to get groups
	groupTexts := regexp.MustCompile(`[;\n]`).Split(text, -1)
	
	var groups [][]int
	
	for _, groupText := range groupTexts {
		groupText = strings.TrimSpace(groupText)
		if groupText == "" {
			continue
		}

		// Remove "Group X:" prefix if present
		groupText = regexp.MustCompile(`(?i)^group\s+\d+\s*:`).ReplaceAllString(groupText, "")
		groupText = strings.TrimSpace(groupText)

		// Parse individual items and ranges (e.g., "1-3, 5, 7-9")
		var itemIndices []int
		
		// Split by comma
		parts := regexp.MustCompile(`\s*,\s*`).Split(groupText, -1)
		
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}

			// Check if it's a range (e.g., "1-3" or "1-5")
			if strings.Contains(part, "-") {
				rangeParts := strings.Split(part, "-")
				if len(rangeParts) != 2 {
					return nil, fmt.Errorf("invalid range format: %s", part)
				}
				
				start, err1 := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
				end, err2 := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
				
				if err1 != nil || err2 != nil {
					return nil, fmt.Errorf("invalid range numbers: %s", part)
				}

				// Convert to 0-based indices
				startIdx := start - 1
				endIdx := end - 1

				if startIdx < 0 || endIdx >= totalItems || startIdx > endIdx {
					return nil, fmt.Errorf("range out of bounds: %s (total: %d)", part, totalItems)
				}

				for i := startIdx; i <= endIdx; i++ {
					itemIndices = append(itemIndices, i)
				}
			} else {
				// Single number
				num, err := strconv.Atoi(part)
				if err != nil {
					return nil, fmt.Errorf("invalid number: %s", part)
				}

				// Convert to 0-based index
				idx := num - 1
				if idx < 0 || idx >= totalItems {
					return nil, fmt.Errorf("item number out of bounds: %d (total: %d)", num, totalItems)
				}

				itemIndices = append(itemIndices, idx)
			}
		}

		if len(itemIndices) > 0 {
			groups = append(groups, itemIndices)
		}
	}

	if len(groups) == 0 {
		return nil, fmt.Errorf("no valid groups found")
	}

	return groups, nil
}

// handleAIGrouping uses AI to analyze images and group them by content
func (h *TenantWebhookHandler) handleAIGrouping(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, bulkSessionID string, bulkManager *BulkUploadManager) {
	ctx := context.Background()

	// Get bulk session
	var bulkSession storage.BulkUploadSession
	if err := h.Manager.DB.Where("session_id = ? AND tenant_id = ?", bulkSessionID, tenantID).
		First(&bulkSession).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Couldn't find bulk upload session."))
		return
	}

	// Get original session ID
	originalSessionID := strings.TrimPrefix(bulkSession.SessionID, "bulk_")

	// Get all items
	var items []storage.MediaItem
	if err := bulkManager.DB.Where("tenant_id = ? AND session_id = ?", tenantID, originalSessionID).
		Order("id ASC").Find(&items).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Couldn't find media items."))
		return
	}

	// Send processing message
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🤖 Analyzing %d images with AI... This may take a moment.", len(items))))

	// Use AI to analyze and group images
	groups, err := h.analyzeImagesWithAI(ctx, tenantID, items)
	if err != nil {
		h.Manager.Logger.Error("AI grouping failed", zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ AI analysis failed: %v\n\nTry custom grouping instead.", err)))
		return
	}

	if len(groups) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ AI couldn't find meaningful groups. Try custom grouping instead."))
		return
	}

	// Create carousels from AI groups
	var assets []storage.MediaAsset
	for groupIdx, group := range groups {
		if len(group.Indices) == 0 {
			continue
		}

		var urls []string
		seenURLs := make(map[string]bool)
		for _, itemIdx := range group.Indices {
			if itemIdx >= 0 && itemIdx < len(items) {
				url := items[itemIdx].URL
				// Only add unique URLs to prevent duplicates
				if url != "" && !seenURLs[url] {
					urls = append(urls, url)
					seenURLs[url] = true
				}
			}
		}

		if len(urls) == 0 {
			h.Manager.Logger.Warn("skipping empty group",
				zap.Int("group_idx", groupIdx),
				zap.String("theme", group.Theme))
			continue
		}

		// Log for debugging
		h.Manager.Logger.Info("creating carousel from AI group",
			zap.Int("group_idx", groupIdx),
			zap.String("theme", group.Theme),
			zap.Int("url_count", len(urls)),
			zap.Int("indices_count", len(group.Indices)))

		urlsJSON, _ := json.Marshal(urls)
		asset := storage.MediaAsset{
			TenantID:     tenantID,
			Type:         "carousel",
			Name:         fmt.Sprintf("AI Group %d: %s", groupIdx+1, group.Theme),
			CarouselName: fmt.Sprintf("bulk_%s_ai_%d", bulkSessionID, groupIdx+1),
			URLs:         string(urlsJSON),
			Status:       "uploaded",
			Platforms:    "[]",
			BulkUploadID: bulkSessionID,
		}

		if err := bulkManager.DB.Create(&asset).Error; err != nil {
			h.Manager.Logger.Error("failed to create AI group asset", zap.Error(err))
			continue
		}

		assets = append(assets, asset)
	}

	if len(assets) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ No valid groups created. Please try again."))
		return
	}

	// Update bulk session
	bulkSession.Status = "completed"
	bulkSession.ProcessedItems = len(items)
	bulkSession.CarouselCount = len(assets)
	bulkSession.SplitStrategy = "ai_organized"
	h.Manager.DB.Save(&bulkSession)

	// Build summary message
	var summaryParts []string
	summaryParts = append(summaryParts, fmt.Sprintf("✅ AI organized %d images into %d groups:\n", len(items), len(assets)))
	
	for i, group := range groups {
		if i < len(assets) {
			summaryParts = append(summaryParts, fmt.Sprintf("%d. %s (%d images)", i+1, group.Theme, len(group.Indices)))
		}
	}

	// Prompt for scheduling
	msg := strings.Join(summaryParts, "\n") +
		"\n\nWhen should I schedule them?\n\n" +
		"Option A: Spread over a month (one per week)\n" +
		"Option B: Custom dates\n" +
		"Option C: Optimal times (AI-suggested)"

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

// AIGroup represents an AI-identified group of images
type AIGroup struct {
	Theme   string
	Indices []int
}

// analyzeImagesWithAI uses AI to analyze images and group them by content
func (h *TenantWebhookHandler) analyzeImagesWithAI(ctx context.Context, tenantID uint, items []storage.MediaItem) ([]AIGroup, error) {
	// Limit to first 20 images for performance (can be increased)
	maxImages := 20
	if len(items) > maxImages {
		items = items[:maxImages]
	}

	// Build image URLs list for AI prompt
	var imageURLs []string
	for _, item := range items {
		imageURLs = append(imageURLs, item.URL)
	}

	// Create AI client
	client := ai.NewAIClient()

	// Build prompt for AI to analyze and group images
	prompt := fmt.Sprintf(`Analyze these %d image URLs and group them by similar content, theme, or subject.

Image URLs:
%s

Your task:
1. Identify groups of similar images (by content, theme, color, subject, etc.)
2. Group images that belong together logically
3. Aim for 3-6 groups (depending on diversity)
4. Each group should have 2-10 images

Return ONLY a JSON array like this:
[
  {
    "theme": "Product photos",
    "indices": [0, 1, 2, 5]
  },
  {
    "theme": "Lifestyle shots",
    "indices": [3, 4, 6, 7]
  }
]

Important:
- "indices" are 0-based (first image is 0, second is 1, etc.)
- Use descriptive themes (e.g., "Nature photos", "Product close-ups", "Team photos")
- Don't include images that don't fit any group
- Return valid JSON only, no extra text`, len(items), strings.Join(imageURLs, "\n"))

	messages := []ai.Message{
		{Role: "system", Content: "You are an expert at analyzing and organizing images by content and theme. Always return valid JSON only."},
		{Role: "user", Content: prompt},
	}

	response, err := client.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("AI generation failed: %w", err)
	}

	// Parse JSON response
	var groups []struct {
		Theme   string `json:"theme"`
		Indices []int  `json:"indices"`
	}

	// Extract JSON from response (in case there's extra text)
	jsonStart := strings.Index(response, "[")
	jsonEnd := strings.LastIndex(response, "]")
	if jsonStart == -1 || jsonEnd == -1 || jsonEnd <= jsonStart {
		return nil, fmt.Errorf("invalid JSON format in AI response")
	}

	jsonStr := response[jsonStart : jsonEnd+1]
	if err := json.Unmarshal([]byte(jsonStr), &groups); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}

	// Convert to AIGroup format
	var result []AIGroup
	for _, g := range groups {
		if len(g.Indices) > 0 && g.Theme != "" {
			// Validate indices
			validIndices := []int{}
			for _, idx := range g.Indices {
				if idx >= 0 && idx < len(items) {
					validIndices = append(validIndices, idx)
				}
			}
			if len(validIndices) > 0 {
				result = append(result, AIGroup{
					Theme:   g.Theme,
					Indices: validIndices,
				})
			}
		}
	}

	return result, nil
}

// startCustomGroupingSession is an alias for handleCustomGroupingInput (callback wrapper)
func (h *TenantWebhookHandler) startCustomGroupingSession(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, bulkSessionID string) {
	CustomGroupingSessions.Store(chatID, bulkSessionID)
	h.promptCustomGrouping(bot, tenantID, chatID, bulkSessionID)
}

