package bot

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// ============================================================
// POST FLOW V3 - Ultra-Simplified Posting Experience
// ============================================================
// Design Principles:
// 1. Photo + Caption (optional in message) → Ask Time → Done
// 2. Platform auto-defaults to last used or all connected
// 3. AI caption offered ONLY if no caption provided
// 4. Max 2 buttons per message
// 5. 60-second undo window instead of confirm step
// ============================================================

// PostSessionV3 is the minimal session state
type PostSessionV3 struct {
	Step          string      `json:"step"` // "time", "caption_choice", "done"
	MediaAssetIDs []uuid.UUID `json:"media_asset_ids"`
	MediaType     string      `json:"media_type"`
	TenantID      uuid.UUID   `json:"tenant_id"`

	// Content
	Caption   string   `json:"caption"`
	Platforms []string `json:"platforms"`

	// Finalized
	ScheduledPostID uuid.UUID `json:"scheduled_post_id"`
	ScheduleTime    time.Time `json:"schedule_time"`

	// For undo
	CreatedAt time.Time `json:"created_at"`
}

// UndoManager tracks posts that can be undone (60s window)
type UndoManager struct {
	mu    sync.RWMutex
	posts map[int64]*UndoablePost // chatID -> post
}

type UndoablePost struct {
	PostID    uuid.UUID
	ExpiresAt time.Time
	Platforms []string
	Caption   string
}

var globalUndoManager = &UndoManager{
	posts: make(map[int64]*UndoablePost),
}

// ============================================================
// MAIN ENTRY POINT
// ============================================================

// StartPostFlowV3 initiates the ultra-simplified posting flow
func (m *TenantBotManager) StartPostFlowV3(bot *tgbotapi.BotAPI, chatID int64, assetIDs []uuid.UUID, captionFromMessage string) {
	if len(assetIDs) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "Hmm, I couldn't find that. Send it again?"))
		return
	}

	// Load first asset to get tenant ID
	var firstAsset storage.MediaAsset
	if err := m.DB.First(&firstAsset, assetIDs[0]).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "Something went wrong. Try again?"))
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
		bot.Send(tgbotapi.NewMessage(chatID, "No social accounts connected. Say \"connect\" to set one up!"))
		return
	}

	// Create session
	sess := &PostSessionV3{
		MediaAssetIDs: assetIDs,
		MediaType:     firstAsset.Type,
		TenantID:      firstAsset.TenantID,
		CreatedAt:     time.Now(),
	}

	// Smart platform default: last used or all
	sess.Platforms = m.getLastUsedPlatforms(firstAsset.TenantID)
	if len(sess.Platforms) == 0 {
		sess.Platforms = connectedPlatforms
	}

	// Check if caption was provided with the message
	captionFromMessage = strings.TrimSpace(captionFromMessage)
	if captionFromMessage != "" {
		// User provided caption with photo - go straight to time selection
		sess.Caption = captionFromMessage
		sess.Step = "time"
		m.savePostSessionV3(chatID, sess)
		m.promptTimeV3(bot, chatID, sess)
	} else {
		// No caption - offer AI generation or skip
		sess.Step = "caption_choice"
		m.savePostSessionV3(chatID, sess)
		m.promptCaptionChoiceV3(bot, chatID, sess)
	}
}

// ============================================================
// STEP 1: CAPTION CHOICE (only if not provided)
// ============================================================

func (m *TenantBotManager) promptCaptionChoiceV3(bot *tgbotapi.BotAPI, chatID int64, sess *PostSessionV3) {
	emoji := "📸"
	if sess.MediaType == "video" {
		emoji = "🎬"
	}

	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"%s Got it! Need a caption?",
		emoji,
	))
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✨ Generate One", "v3:caption:ai"),
			tgbotapi.NewInlineKeyboardButtonData("Skip", "v3:caption:skip"),
		),
	)
	bot.Send(msg)
}

// ============================================================
// STEP 2: TIME SELECTION
// ============================================================

func (m *TenantBotManager) promptTimeV3(bot *tgbotapi.BotAPI, chatID int64, sess *PostSessionV3) {
	// Get best posting time (simple heuristic: 7 PM local)
	bestTime := m.getBestPostingTime(sess.TenantID)

	platformsStr := formatPlatformListV3(sess.Platforms)

	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"Posting to %s\nWhen?",
		platformsStr,
	))
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Now", "v3:time:now"),
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%s ⭐", bestTime.Format("3PM")), "v3:time:best"),
			tgbotapi.NewInlineKeyboardButtonData("Pick", "v3:time:pick"),
		),
	)
	bot.Send(msg)
}

// ============================================================
// STEP 3: FINALIZE & UNDO
// ============================================================

func (m *TenantBotManager) finalizePostV3(bot *tgbotapi.BotAPI, chatID int64, sess *PostSessionV3, scheduleTime time.Time, isNow bool) {
	// Convert asset IDs to JSON string for storage
	assetIDStrs := make([]string, len(sess.MediaAssetIDs))
	for i, id := range sess.MediaAssetIDs {
		assetIDStrs[i] = id.String()
	}
	mediaAssetIDsJSON := "[" + strings.Join(assetIDStrs, ",") + "]"

	// Convert platforms to JSON string
	platformsJSON := "[\"" + strings.Join(sess.Platforms, "\",\"") + "\"]"

	// Calculate post time
	postTime := scheduleTime
	if isNow {
		postTime = time.Now()
	}

	// Create the scheduled post
	post := storage.ScheduledPost{
		TenantID:      sess.TenantID,
		MediaAssetIDs: mediaAssetIDsJSON,
		Caption:       sess.Caption,
		ScheduledAt:   &postTime,
		Status:        "pending",
		Platforms:     platformsJSON,
	}

	if isNow {
		post.Status = "queued"
	}

	if err := m.DB.Create(&post).Error; err != nil {
		m.Logger.Error("failed to create scheduled post", zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "Something went wrong. Try again?"))
		return
	}

	sess.ScheduledPostID = post.ID

	// Save last used platforms
	m.saveLastUsedPlatforms(sess.TenantID, sess.Platforms)

	// Clear session
	m.clearPostSessionV3(chatID)

	// Register for undo
	globalUndoManager.mu.Lock()
	globalUndoManager.posts[chatID] = &UndoablePost{
		PostID:    sess.ScheduledPostID,
		ExpiresAt: time.Now().Add(60 * time.Second),
		Platforms: sess.Platforms,
		Caption:   sess.Caption,
	}
	globalUndoManager.mu.Unlock()

	// Start undo expiry goroutine
	go func() {
		time.Sleep(60 * time.Second)
		globalUndoManager.mu.Lock()
		delete(globalUndoManager.posts, chatID)
		globalUndoManager.mu.Unlock()
	}()

	// Send confirmation with undo
	var timeText string
	if isNow {
		timeText = "now"
	} else {
		timeText = scheduleTime.Format("Mon 3:04 PM")
	}

	platformsStr := formatPlatformListV3(sess.Platforms)

	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"✅ %s → %s",
		platformsStr, timeText,
	))
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("↩️ Undo", "v3:undo"),
			tgbotapi.NewInlineKeyboardButtonData("📅 Calendar", "v3:calendar"),
		),
	)
	bot.Send(msg)
}

// ============================================================
// CALLBACK HANDLERS
// ============================================================

func (m *TenantBotManager) HandlePostV3Callback(bot *tgbotapi.BotAPI, chatID int64, action string) {

	// Get session
	sess, ok := m.getPostSessionV3(chatID)
	if !ok && action != "undo" {
		return
	}

	switch action {
	case "caption:ai":
		// Show typing indicator and placeholder
		bot.Send(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping))
		placeholder := tgbotapi.NewMessage(chatID, "✨ Writing your caption...")
		sentMsg, _ := bot.Send(placeholder)

		// Generate AI caption
		caption := m.generateAICaptionV3(sess)

		// Edit the placeholder message with the actual caption
		m.showGeneratedCaptionV3Edit(bot, chatID, sentMsg.MessageID, sess, caption)

	case "caption:skip":
		// No caption, go to time
		sess.Caption = ""
		sess.Step = "time"
		m.savePostSessionV3(chatID, sess)
		m.promptTimeV3(bot, chatID, sess)

	case "caption:use":
		// Use the generated caption, go to time
		sess.Step = "time"
		m.savePostSessionV3(chatID, sess)
		m.promptTimeV3(bot, chatID, sess)

	case "caption:another":
		// Show typing indicator and placeholder
		bot.Send(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping))
		placeholder := tgbotapi.NewMessage(chatID, "✨ Trying another one...")
		sentMsg, _ := bot.Send(placeholder)

		// Generate another caption
		caption := m.generateAICaptionV3(sess)

		// Edit the placeholder message with the actual caption
		m.showGeneratedCaptionV3Edit(bot, chatID, sentMsg.MessageID, sess, caption)

	case "time:now":
		m.finalizePostV3(bot, chatID, sess, time.Now(), true)

	case "time:best":
		bestTime := m.getBestPostingTime(sess.TenantID)
		m.finalizePostV3(bot, chatID, sess, bestTime, false)

	case "time:pick":
		msg := tgbotapi.NewMessage(chatID, "When? (e.g., \"tomorrow 3pm\", \"friday 7pm\")")
		bot.Send(msg)
		sess.Step = "time_input"
		m.savePostSessionV3(chatID, sess)

	case "undo":
		m.handleUndoV3(bot, chatID)

	case "calendar":
		// Show calendar view
		m.showCalendarPreview(bot, chatID)
	}
}

func (m *TenantBotManager) showGeneratedCaptionV3(bot *tgbotapi.BotAPI, chatID int64, sess *PostSessionV3, caption string) {
	sess.Caption = caption
	m.savePostSessionV3(chatID, sess)

	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"\"%s\"\n\nUse this?",
		truncateCaption(caption, 150),
	))
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✓ Yes", "v3:caption:use"),
			tgbotapi.NewInlineKeyboardButtonData("↻ Another", "v3:caption:another"),
		),
	)
	bot.Send(msg)
}

// showGeneratedCaptionV3Edit edits an existing message with the generated caption
// This creates a smooth "loading -> result" transition effect in Telegram
func (m *TenantBotManager) showGeneratedCaptionV3Edit(bot *tgbotapi.BotAPI, chatID int64, messageID int, sess *PostSessionV3, caption string) {
	sess.Caption = caption
	m.savePostSessionV3(chatID, sess)

	// Edit the placeholder message with the actual caption
	editMsg := tgbotapi.NewEditMessageText(chatID, messageID, fmt.Sprintf(
		"\"%s\"\n\nUse this?",
		truncateCaption(caption, 150),
	))
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✓ Yes", "v3:caption:use"),
			tgbotapi.NewInlineKeyboardButtonData("↻ Another", "v3:caption:another"),
		),
	)
	editMsg.ReplyMarkup = &keyboard
	bot.Send(editMsg)
}

func (m *TenantBotManager) handleUndoV3(bot *tgbotapi.BotAPI, chatID int64) {
	globalUndoManager.mu.Lock()
	defer globalUndoManager.mu.Unlock()

	undoable, exists := globalUndoManager.posts[chatID]
	if !exists || time.Now().After(undoable.ExpiresAt) {
		bot.Send(tgbotapi.NewMessage(chatID, "⏰ Undo window expired."))
		return
	}

	// Delete the scheduled post(s)
	if err := m.DB.Where("id = ?", undoable.PostID).Delete(&storage.ScheduledPost{}).Error; err != nil {
		m.Logger.Error("failed to undo post", zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "Something went wrong. Check your calendar."))
		return
	}

	delete(globalUndoManager.posts, chatID)

	bot.Send(tgbotapi.NewMessage(chatID, "↩️ Post cancelled."))
}

// ContinuePostFlowV3 handles text input during flow
func (m *TenantBotManager) ContinuePostFlowV3(bot *tgbotapi.BotAPI, chatID int64, text string) bool {
	sess, ok := m.getPostSessionV3(chatID)
	if !ok {
		return false // No active session
	}

	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)

	// Handle cancel/abort
	if lower == "cancel" || lower == "nevermind" || lower == "stop" {
		m.clearPostSessionV3(chatID)
		bot.Send(tgbotapi.NewMessage(chatID, "👍 Cancelled."))
		return true
	}

	switch sess.Step {
	case "caption_choice":
		// User typed a caption instead of using buttons
		sess.Caption = text
		sess.Step = "time"
		m.savePostSessionV3(chatID, sess)
		m.promptTimeV3(bot, chatID, sess)
		return true

	case "time_input":
		// Parse natural language time
		scheduleTime, err := parseScheduleTime(text)
		if err != nil {
			msg := tgbotapi.NewMessage(chatID, "Hmm, I didn't get that. Try \"tomorrow 3pm\" or \"friday evening\"")
			bot.Send(msg)
			return true
		}
		m.finalizePostV3(bot, chatID, sess, scheduleTime, false)
		return true
	}

	return false
}

// ============================================================
// HELPER FUNCTIONS
// ============================================================

func (m *TenantBotManager) generateAICaptionV3(sess *PostSessionV3) string {
	if len(sess.MediaAssetIDs) == 0 {
		return m.generateFallbackCaption()
	}

	// Use existing AI caption generation
	return m.generateAICaption(sess.TenantID, sess.MediaAssetIDs[0], sess.Platforms[0])
}

func (m *TenantBotManager) getBestPostingTime(tenantID uuid.UUID) time.Time {
	// Simple heuristic: today at 7 PM, or tomorrow at 7 PM if already past 7
	now := time.Now()
	best := time.Date(now.Year(), now.Month(), now.Day(), 19, 0, 0, 0, now.Location())
	if now.After(best) {
		best = best.Add(24 * time.Hour)
	}
	return best
}

func formatPlatformListV3(platforms []string) string {
	if len(platforms) == 0 {
		return ""
	}

	names := make([]string, len(platforms))
	for i, p := range platforms {
		switch strings.ToLower(p) {
		case "instagram":
			names[i] = "IG"
		case "facebook":
			names[i] = "FB"
		case "twitter", "x":
			names[i] = "X"
		case "tiktok":
			names[i] = "TikTok"
		default:
			names[i] = p
		}
	}

	if len(names) == 1 {
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " + " + names[len(names)-1]
}

func truncateCaption(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// Session management helpers (using existing SessionManager)
func (m *TenantBotManager) savePostSessionV3(chatID int64, sess *PostSessionV3) {
	ctx := context.Background()
	if m.SessionManager != nil {
		// Use generic session storage
		m.SessionManager.SetGenericSession(ctx, fmt.Sprintf("postv3:%d", chatID), sess, 30*time.Minute)
	}
}

func (m *TenantBotManager) getPostSessionV3(chatID int64) (*PostSessionV3, bool) {
	ctx := context.Background()
	if m.SessionManager == nil {
		return nil, false
	}

	var sess PostSessionV3
	err := m.SessionManager.GetGenericSession(ctx, fmt.Sprintf("postv3:%d", chatID), &sess)
	if err != nil {
		return nil, false
	}
	return &sess, true
}

func (m *TenantBotManager) clearPostSessionV3(chatID int64) {
	ctx := context.Background()
	if m.SessionManager != nil {
		m.SessionManager.DeleteSession(ctx, fmt.Sprintf("postv3:%d", chatID))
	}
}

func (m *TenantBotManager) showCalendarPreview(bot *tgbotapi.BotAPI, chatID int64) {
	// Get upcoming posts for message
	var posts []storage.ScheduledPost
	now := time.Now()
	endOfWeek := now.AddDate(0, 0, 7)

	m.DB.Where("scheduled_at BETWEEN ? AND ? AND status = ?", now, endOfWeek, "pending").
		Order("scheduled_at ASC").
		Limit(5).
		Find(&posts)

	if len(posts) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "📅 No upcoming posts this week."))
		return
	}

	var lines []string
	for _, p := range posts {
		if p.ScheduledAt != nil {
			lines = append(lines, fmt.Sprintf("• %s - %s", p.ScheduledAt.Format("Mon 3PM"), truncateCaption(p.Caption, 30)))
		}
	}

	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("📅 Upcoming:\n%s", strings.Join(lines, "\n")))
	bot.Send(msg)
}
