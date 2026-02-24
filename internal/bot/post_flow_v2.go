package bot

// ============================================================
// DEPRECATED: V2 Post Flow
// ============================================================
// Replaced by posting_v3.go (V3 Ultra-Simplified Flow)
// This file is kept for backward compatibility during rollout.
// Scheduled for removal in future release.
// ============================================================

import (
	"context"
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// ============================================================
// POST FLOW V2 - Simplified, Conversational UX
// ============================================================
// Design Principles:
// 1. Max 3 steps for single post
// 2. Natural language first, buttons as shortcuts
// 3. Smart defaults (remember last platforms, auto-spacing)
// 4. Warmer, casual language
// 5. Undo-based confirmation (no explicit confirm step)
// ============================================================

// PostSessionV2 is the simplified session state
type PostSessionV2 struct {
	Step          int         `json:"step"`            // 0=platform, 1=caption, 2=schedule
	MediaAssetIDs []uuid.UUID `json:"media_asset_ids"` // Support multiple for batch
	MediaType     string      `json:"media_type"`      // image, video, carousel
	TenantID      uuid.UUID   `json:"tenant_id"`

	// User choices
	Platforms     []string `json:"platforms"`
	CaptionMode   string   `json:"caption_mode"`   // "user", "ai", "same" (for batch)
	Captions      []string `json:"captions"`       // One per media asset
	CaptionTheme  string   `json:"caption_theme"`  // For AI generation
	ScheduleMode  string   `json:"schedule_mode"`  // "now", "specific", "spaced"
	ScheduleTimes []string `json:"schedule_times"` // Parsed times

	// Batch scheduling
	IsBatch      bool   `json:"is_batch"`
	BatchSpacing string `json:"batch_spacing"` // "24h", "12h", "custom"

	// Remembered preferences (from last post)
	LastPlatforms []string `json:"last_platforms"`
}

// StartPostFlowV2 initiates the new simplified posting flow
func (m *TenantBotManager) StartPostFlowV2(bot *tgbotapi.BotAPI, chatID int64, assetIDs []uuid.UUID) {
	if len(assetIDs) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "Hmm, I can't find that upload. Send it again?"))
		return
	}

	// Load first asset to get tenant ID and type
	var firstAsset storage.MediaAsset
	if err := m.DB.First(&firstAsset, assetIDs[0]).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "Something went wrong finding that upload. Try sending it again?"))
		return
	}

	// Get connected platforms
	var accounts []storage.SocialAccount
	m.DB.Where("tenant_id = ? AND connected = ?", firstAsset.TenantID, true).Find(&accounts)

	connectedPlatforms := []string{}
	for _, acc := range accounts {
		connectedPlatforms = append(connectedPlatforms, acc.Platform)
	}

	if len(connectedPlatforms) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "You don't have any social accounts connected yet. Say \"connect\" to set one up!"))
		return
	}

	// Create session
	sess := &PostSessionV2{
		Step:          0,
		MediaAssetIDs: assetIDs,
		MediaType:     firstAsset.Type,
		TenantID:      firstAsset.TenantID,
		IsBatch:       len(assetIDs) > 1,
	}

	// Get last used platforms (memory)
	sess.LastPlatforms = m.getLastUsedPlatforms(firstAsset.TenantID)

	// Save session
	ctx := context.Background()
	if m.SessionManager != nil {
		if err := m.SessionManager.SavePostSessionV2(ctx, chatID, sess); err != nil {
			m.Logger.Error("❌ Failed to save post session v2 - session will expire immediately",
				zap.Error(err),
				zap.Int64("chat_id", chatID),
				zap.Stringer("tenant_id", firstAsset.TenantID))
			bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Session save failed. Please try sending your media again."))
			return
		}
	} else {
		m.Logger.Error("❌ SessionManager is nil - cannot save session",
			zap.Int64("chat_id", chatID))
		bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Bot not fully initialized. Please try again in a moment."))
		return
	}

	// Determine prompt based on batch vs single
	if sess.IsBatch {
		m.promptBatchStart(bot, chatID, sess, len(assetIDs), connectedPlatforms)
	} else {
		m.promptSingleStart(bot, chatID, sess, connectedPlatforms)
	}
}

// promptSingleStart - First message for single image/video
func (m *TenantBotManager) promptSingleStart(bot *tgbotapi.BotAPI, chatID int64, sess *PostSessionV2, connected []string) {
	emoji := "📸"
	if sess.MediaType == "video" {
		emoji = "🎬"
	}

	// Build platform list
	platformList := formatPlatformList(connected)

	var msg tgbotapi.MessageConfig

	// If they have last platforms, offer quick repost option
	if len(sess.LastPlatforms) > 0 {
		lastList := formatPlatformList(sess.LastPlatforms)
		msg = tgbotapi.NewMessage(chatID, fmt.Sprintf(
			"%s Got it! Where should this go?\n\nConnected: %s\n\n(Say \"all\" or list them, e.g., \"IG and FB\")",
			emoji, platformList,
		))
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("Same as last (%s)", lastList), "postv2:platforms:last"),
				tgbotapi.NewInlineKeyboardButtonData("All", "postv2:platforms:all"),
			),
		)
	} else {
		msg = tgbotapi.NewMessage(chatID, fmt.Sprintf(
			"%s Got it! Where should this go?\n\nConnected: %s\n\n(Say \"all\" or list them)",
			emoji, platformList,
		))
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("All platforms", "postv2:platforms:all"),
			),
		)
	}

	bot.Send(msg)
}

// promptBatchStart - First message for multiple images
func (m *TenantBotManager) promptBatchStart(bot *tgbotapi.BotAPI, chatID int64, sess *PostSessionV2, count int, connected []string) {
	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"📷 Got %d images! How should I handle these?\n\n(Carousel = 1 post, Separate = multiple posts)",
		count,
	))
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Carousel (1 post)", "postv2:batch:carousel"),
			tgbotapi.NewInlineKeyboardButtonData("Separate posts", "postv2:batch:separate"),
		),
	)
	bot.Send(msg)
}

// ContinuePostFlowV2 handles ongoing conversation
func (m *TenantBotManager) ContinuePostFlowV2(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, text string) {
	ctx := context.Background()

	// Get session
	var sess *PostSessionV2
	if m.SessionManager != nil {
		var ok bool
		sess, ok = m.SessionManager.GetPostSessionV2(ctx, chatID)
		if !ok {
			return // No active session
		}
	} else {
		return
	}

	lower := strings.ToLower(strings.TrimSpace(text))

	// Handle abandon phrases
	if isAbandonPhrase(lower) {
		m.SessionManager.DeletePostSessionV2(ctx, chatID)
		bot.Send(tgbotapi.NewMessage(chatID, "No worries! Just ping me when you're ready. 👍"))
		return
	}

	switch sess.Step {
	case 0: // Platform selection
		m.handlePlatformInputV2(bot, chatID, sess, text)

	case 1: // Caption input
		m.handleCaptionInputV2(bot, chatID, sess, text)

	case 2: // Schedule input
		m.handleScheduleInputV2(bot, chatID, sess, text)
	}
}

// handlePlatformInputV2 processes platform selection
func (m *TenantBotManager) handlePlatformInputV2(bot *tgbotapi.BotAPI, chatID int64, sess *PostSessionV2, text string) {
	ctx := context.Background()
	platforms := m.parsePlatforms(text)

	if len(platforms) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "I didn't catch that. Which platforms? (Say \"all\" or list them like \"IG and FB\")"))
		return
	}

	sess.Platforms = platforms
	sess.Step = 1
	m.SessionManager.SavePostSessionV2(ctx, chatID, sess)

	// Move to caption step
	m.promptCaptionV2(bot, chatID, sess)
}

// promptCaptionV2 asks for caption
func (m *TenantBotManager) promptCaptionV2(bot *tgbotapi.BotAPI, chatID int64, sess *PostSessionV2) {
	if sess.IsBatch && len(sess.MediaAssetIDs) > 1 {
		// Batch: ask about caption strategy
		msg := tgbotapi.NewMessage(chatID, "What about captions?\n\n(Same for all, different for each, or I'll write them)")
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Same for all", "postv2:caption:same"),
				tgbotapi.NewInlineKeyboardButtonData("You write them", "postv2:caption:ai"),
			),
		)
		bot.Send(msg)
	} else {
		// Single: just ask for caption
		msg := tgbotapi.NewMessage(chatID, "What should the caption say?\n\n(Type one, or say \"you pick\" and I'll draft something)")
		bot.Send(msg)
	}
}

// handleCaptionInputV2 processes caption input
func (m *TenantBotManager) handleCaptionInputV2(bot *tgbotapi.BotAPI, chatID int64, sess *PostSessionV2, text string) {
	ctx := context.Background()
	lower := strings.ToLower(strings.TrimSpace(text))

	// Check for AI request
	if lower == "you pick" || lower == "ai" || lower == "generate" || lower == "write one" {
		sess.CaptionMode = "ai"
		sess.Step = 2
		m.SessionManager.SavePostSessionV2(ctx, chatID, sess)

		// Generate AI caption using vision API + tenant context
		// Determine platform (use first selected, or default to instagram)
		platform := "instagram"
		if len(sess.Platforms) > 0 {
			platform = sess.Platforms[0]
		}

		// Get first asset ID for vision analysis
		var assetID uuid.UUID
		if len(sess.MediaAssetIDs) > 0 {
			assetID = sess.MediaAssetIDs[0]
		}

		// Show loading indicator
		bot.Send(tgbotapi.NewMessage(chatID, "✍️ Writing a caption..."))

		caption := m.generateAICaption(sess.TenantID, assetID, platform)
		sess.Captions = []string{caption}
		m.SessionManager.SavePostSessionV2(ctx, chatID, sess)

		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("How about:\n\n\"%s\"\n\nGood?", caption))
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Use this ✓", "postv2:caption:use"),
				tgbotapi.NewInlineKeyboardButtonData("Another one", "postv2:caption:regenerate"),
			),
		)
		bot.Send(msg)
		return
	}

	// User provided their own caption
	sess.CaptionMode = "user"
	sess.Captions = []string{text}
	sess.Step = 2
	m.SessionManager.SavePostSessionV2(ctx, chatID, sess)

	// Move to schedule step
	m.promptScheduleV2(bot, chatID, sess)
}

// promptScheduleV2 asks for schedule
func (m *TenantBotManager) promptScheduleV2(bot *tgbotapi.BotAPI, chatID int64, sess *PostSessionV2) {
	// Calculate smart time suggestions based on timezone
	now := time.Now()
	tonight := time.Date(now.Year(), now.Month(), now.Day(), 19, 0, 0, 0, now.Location())
	tomorrow := now.AddDate(0, 0, 1)
	tomorrowMorning := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 9, 0, 0, 0, tomorrow.Location())

	tonightLabel := "Tonight 7pm"
	tomorrowLabel := "Tomorrow 9am"

	// If it's already past 7pm, change tonight to "Tomorrow 7pm"
	if now.Hour() >= 19 {
		tonight = tonight.AddDate(0, 0, 1)
		tonightLabel = "Tomorrow 7pm"
		tomorrowLabel = fmt.Sprintf("%s 9am", tomorrow.Weekday().String()[:3])
	}

	msg := tgbotapi.NewMessage(chatID, "When should this go live?\n\n(Or tell me, e.g., \"Friday at 2pm\")")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Now", "postv2:schedule:now"),
			tgbotapi.NewInlineKeyboardButtonData(tonightLabel, fmt.Sprintf("postv2:schedule:%d", tonight.Unix())),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(tomorrowLabel, fmt.Sprintf("postv2:schedule:%d", tomorrowMorning.Unix())),
		),
	)
	bot.Send(msg)

	// Store suggested times for callback handling
	sess.ScheduleTimes = []string{
		fmt.Sprintf("%d", tonight.Unix()),
		fmt.Sprintf("%d", tomorrowMorning.Unix()),
	}
	m.SessionManager.SavePostSessionV2(context.Background(), chatID, sess)
}

// handleScheduleInputV2 processes schedule input
func (m *TenantBotManager) handleScheduleInputV2(bot *tgbotapi.BotAPI, chatID int64, sess *PostSessionV2, text string) {
	ctx := context.Background()
	lower := strings.ToLower(strings.TrimSpace(text))

	var scheduleTime time.Time
	var isNow bool

	// Parse natural language time
	if lower == "now" {
		isNow = true
		scheduleTime = time.Now()
	} else {
		// Use existing time parser
		parsed, err := parseScheduleTime(text)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "I didn't catch that time. Try something like \"tomorrow at 9am\" or \"Friday noon\"."))
			return
		}
		scheduleTime = parsed
	}

	// Finalize the post
	m.finalizePostV2(bot, chatID, sess, scheduleTime, isNow)

	// Clean up session
	if m.SessionManager != nil {
		m.SessionManager.DeletePostSessionV2(ctx, chatID)
	}
}

// finalizePostV2 creates the scheduled post and confirms
func (m *TenantBotManager) finalizePostV2(bot *tgbotapi.BotAPI, chatID int64, sess *PostSessionV2, scheduleTime time.Time, isNow bool) {
	if len(sess.MediaAssetIDs) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "Something went wrong. Try uploading the media again."))
		return
	}

	caption := ""
	if len(sess.Captions) > 0 {
		caption = sess.Captions[0]
	}

	// Serialize media asset IDs to JSON string
	mediaAssetIDsJSON := "["
	for i, id := range sess.MediaAssetIDs {
		if i > 0 {
			mediaAssetIDsJSON += ","
		}
		mediaAssetIDsJSON += fmt.Sprintf(`"%s"`, id.String())
	}
	mediaAssetIDsJSON += "]"

	// Serialize platforms to JSON string
	platformsJSON := "["
	for i, p := range sess.Platforms {
		if i > 0 {
			platformsJSON += ","
		}
		platformsJSON += fmt.Sprintf(`"%s"`, p)
	}
	platformsJSON += "]"

	// Create single scheduled post with all platforms
	post := storage.ScheduledPost{
		TenantID:      sess.TenantID,
		MediaAssetIDs: mediaAssetIDsJSON,
		Platforms:     platformsJSON,
		ScheduledAt:   &scheduleTime,
		Caption:       caption,
		Status:        "scheduled",
	}

	if isNow {
		post.Status = "processing"
	}

	if err := m.DB.Create(&post).Error; err != nil {
		m.Logger.Error("failed to create scheduled post", zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "Oops, something went wrong saving that. Try again?"))
		return
	}

	// If posting now, trigger immediate execution
	if isNow && m.Scheduler != nil {
		go func(postID uuid.UUID) {
			if err := m.Scheduler.ExecutePostImmediately(postID); err != nil {
				m.Logger.Error("failed to execute immediate post", zap.Error(err))
			}
		}(post.ID)
	}

	// Remember platforms for next time
	m.saveLastUsedPlatforms(sess.TenantID, sess.Platforms)

	// Send confirmation
	platformList := formatPlatformList(sess.Platforms)
	var confirmMsg string

	if isNow {
		confirmMsg = fmt.Sprintf("Posting now to %s! 🚀\n\n\"%s\"", platformList, truncate(caption, 100))
	} else {
		timeStr := scheduleTime.Format("Mon Jan 2 at 3:04pm")
		confirmMsg = fmt.Sprintf("Scheduled for %s on %s ✅\n\n\"%s\"", timeStr, platformList, truncate(caption, 100))
	}

	msg := tgbotapi.NewMessage(chatID, confirmMsg)
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Undo", fmt.Sprintf("postv2:undo:%s", post.ID)),
		),
	)
	bot.Send(msg)
}

// ============================================================
// HELPER FUNCTIONS
// ============================================================

func formatPlatformList(platforms []string) string {
	if len(platforms) == 0 {
		return "None"
	}

	names := make([]string, len(platforms))
	for i, p := range platforms {
		switch p {
		case "instagram":
			names[i] = "IG"
		case "facebook":
			names[i] = "FB"
		case "tiktok":
			names[i] = "TikTok"
		case "youtube":
			names[i] = "YT"
		case "whatsapp":
			names[i] = "WhatsApp"
		default:
			names[i] = strings.Title(p)
		}
	}

	if len(names) == 1 {
		return names[0]
	}
	if len(names) == 2 {
		return names[0] + " & " + names[1]
	}
	return strings.Join(names[:len(names)-1], ", ") + " & " + names[len(names)-1]
}

func isAbandonPhrase(text string) bool {
	phrases := []string{"cancel", "never mind", "nevermind", "forget it", "stop", "skip this", "not now"}
	for _, p := range phrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func (m *TenantBotManager) getLastUsedPlatforms(tenantID uuid.UUID) []string {
	// Get most recent scheduled post to see what platforms were used
	var lastPost storage.ScheduledPost
	if err := m.DB.Where("tenant_id = ?", tenantID).Order("created_at DESC").First(&lastPost).Error; err != nil {
		return nil
	}
	// Parse platforms from JSON string
	var platforms []string
	if lastPost.Platforms != "" {
		// Simple JSON parse (handle both quoted and unquoted)
		platformsStr := strings.Trim(lastPost.Platforms, "[]")
		if platformsStr != "" {
			parts := strings.Split(platformsStr, ",")
			for _, p := range parts {
				p = strings.Trim(p, `" `)
				if p != "" {
					platforms = append(platforms, p)
				}
			}
		}
	}
	return platforms
}

func (m *TenantBotManager) saveLastUsedPlatforms(tenantID uuid.UUID, platforms []string) {
	// This is implicitly done by creating posts
	// Could extend to store in tenant metadata if needed
}

// generateAICaption uses vision AI to analyze the image and generate a contextual caption
// It combines visual understanding with the tenant's business profile
func (m *TenantBotManager) generateAICaption(tenantID uuid.UUID, assetID uuid.UUID, platform string) string {
	ctx := context.Background()

	// Check if AI client is available
	if m.AIClient == nil {
		return m.generateFallbackCaption()
	}

	// Get media URLs from asset
	var asset storage.MediaAsset
	if err := m.DB.First(&asset, assetID).Error; err != nil {
		return m.generateFallbackCaption()
	}

	// Parse URLs from the asset
	var imageURLs []string
	if asset.URLs != "" {
		// Parse JSON array of URLs
		urlsStr := strings.Trim(asset.URLs, "[]")
		if urlsStr != "" {
			parts := strings.Split(urlsStr, ",")
			for _, p := range parts {
				p = strings.Trim(p, `" `)
				if p != "" {
					imageURLs = append(imageURLs, p)
				}
			}
		}
	}

	// Create caption optimizer with vision capabilities
	optimizer := &ai.CaptionOptimizer{
		Client: m.AIClient,
		DB:     m.DB,
	}

	// Generate caption using vision API + tenant context
	// This analyzes the actual image content and combines it with:
	// - Tenant's persona (tone, brand voice)
	// - Business profile (industry, description, keywords)
	// - Products/services (menu items, offerings)
	result, err := optimizer.OptimizeCaption(ctx, ai.OptimizeCaptionRequest{
		OriginalCaption: "", // Empty = generate from scratch
		Platform:        platform,
		MediaType:       asset.Type,
		TenantID:        tenantID,
		ImageURLs:       imageURLs,
	})

	if err != nil || result == nil {
		m.Logger.Warn("AI caption generation failed, using fallback",
			zap.Stringer("tenant_id", tenantID),
			zap.Error(err))
		return m.generateFallbackCaption()
	}

	return result.OptimizedCaption
}

// generateFallbackCaption returns a simple caption when AI is unavailable
func (m *TenantBotManager) generateFallbackCaption() string {
	defaults := []string{
		"New drop! 🔥",
		"Check this out 👀",
		"What do you think? 💭",
		"Fresh content incoming ✨",
		"Something special for you 💫",
	}
	idx := int(time.Now().Unix()) % len(defaults)
	return defaults[idx]
}

// parseScheduleTime parses natural language time input
func parseScheduleTime(input string) (time.Time, error) {
	input = strings.ToLower(strings.TrimSpace(input))
	now := time.Now()

	// Handle "now"
	if input == "now" {
		return now, nil
	}

	// Handle "tonight"
	if strings.Contains(input, "tonight") {
		return time.Date(now.Year(), now.Month(), now.Day(), 19, 0, 0, 0, now.Location()), nil
	}

	// Handle "tomorrow"
	if strings.Contains(input, "tomorrow") {
		tomorrow := now.AddDate(0, 0, 1)
		hour := 9 // Default morning
		if strings.Contains(input, "evening") || strings.Contains(input, "night") {
			hour = 19
		} else if strings.Contains(input, "noon") || strings.Contains(input, "afternoon") {
			hour = 14
		}
		// Try to extract specific hour
		if h := extractHour(input); h > 0 {
			hour = h
		}
		return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), hour, 0, 0, 0, tomorrow.Location()), nil
	}

	// Handle day names (Monday, Tuesday, etc.)
	for i := 0; i < 7; i++ {
		checkDay := now.AddDate(0, 0, i)
		dayName := strings.ToLower(checkDay.Weekday().String())
		if strings.Contains(input, dayName) || strings.Contains(input, dayName[:3]) {
			hour := 10 // Default
			if h := extractHour(input); h > 0 {
				hour = h
			}
			return time.Date(checkDay.Year(), checkDay.Month(), checkDay.Day(), hour, 0, 0, 0, checkDay.Location()), nil
		}
	}

	// Handle relative times ("in 2 hours")
	if strings.HasPrefix(input, "in ") {
		// Parse duration
		// Simple implementation - can be enhanced
		if strings.Contains(input, "hour") {
			hours := 1
			fmt.Sscanf(input, "in %d hour", &hours)
			return now.Add(time.Duration(hours) * time.Hour), nil
		}
	}

	return time.Time{}, fmt.Errorf("could not parse time: %s", input)
}

func extractHour(input string) int {
	// Try common patterns
	var hour int

	// "at 9" or "at 9am" or "at 9pm"
	if _, err := fmt.Sscanf(input, "%*s %dam", &hour); err == nil {
		return hour
	}
	if _, err := fmt.Sscanf(input, "%*s %dpm", &hour); err == nil {
		if hour != 12 {
			hour += 12
		}
		return hour
	}
	if _, err := fmt.Sscanf(input, "%*s %d am", &hour); err == nil {
		return hour
	}
	if _, err := fmt.Sscanf(input, "%*s %d pm", &hour); err == nil {
		if hour != 12 {
			hour += 12
		}
		return hour
	}

	// Just a number at the end
	words := strings.Fields(input)
	for _, w := range words {
		if n, err := fmt.Sscanf(w, "%d", &hour); n > 0 && err == nil && hour >= 1 && hour <= 23 {
			// Assume PM if afternoon hour mentioned
			if hour < 12 && (strings.Contains(input, "afternoon") || strings.Contains(input, "evening")) {
				hour += 12
			}
			return hour
		}
	}

	return 0
}
