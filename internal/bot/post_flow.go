package bot

import (
	"context"
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

type PostSession struct {
	Step           int
	MediaAssetID   uint
	MediaType      string // image, video, carousel
	IsCarousel     bool   // user confirmed carousel
	Name           string
	Caption        string
	UseAICaption   bool
	Platforms      []string // Selected platforms
	ScheduleInput  string
	BatchSchedule  bool     // Batch scheduling multiple posts
	BatchSpacing   string   // e.g., "24 hours apart"
	OriginalInputs []string // For batch scheduling
}

var tenantSessions = make(map[int64]*PostSession) // chatID → session

func (m *TenantBotManager) StartPostFlow(bot *tgbotapi.BotAPI, chatID int64, assetID uint) {
	var ma storage.MediaAsset
	if err := m.DB.First(&ma, assetID).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "I couldn’t find that upload. Mind sending it again?"))
		return
	}

	sess := &PostSession{
		Step:         0,
		MediaAssetID: assetID,
		MediaType:    ma.Type,
		IsCarousel:   ma.Type == "carousel",
	}
	tenantSessions[chatID] = sess

	prompt := fmt.Sprintf("✅ %s uploaded and ready. Want me to turn it into a post?", strings.Title(ma.Type))
	if ma.Type == "carousel" {
		var urls []string
		_ = json.Unmarshal([]byte(ma.URLs), &urls)
		prompt = fmt.Sprintf("✅ Got %d shots queued. Want me to turn them into a post?", len(urls))
	}

	msg := tgbotapi.NewMessage(chatID, prompt)
	msg.ReplyMarkup = yesNoKeyboard()
	bot.Send(msg)
}

func yesNoKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Yes", "postflow:yes"),
			tgbotapi.NewInlineKeyboardButtonData("Not now", "postflow:skip"),
		),
	)
}

func yesNoKeyboardWithCancel() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Yes", "postflow:yes"),
			tgbotapi.NewInlineKeyboardButtonData("❌ No", "postflow:no"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "postflow:cancel"),
			tgbotapi.NewInlineKeyboardButtonData("❓ Help", "help:postflow"),
		),
	)
}

func cancelKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "postflow:cancel"),
			tgbotapi.NewInlineKeyboardButtonData("❓ Help", "help:postflow"),
		),
	)
}

func platformsKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📸 Instagram", "postflow:platform:instagram"),
			tgbotapi.NewInlineKeyboardButtonData("👥 Facebook", "postflow:platform:facebook"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🎵 TikTok", "postflow:platform:tiktok"),
			tgbotapi.NewInlineKeyboardButtonData("📺 YouTube", "postflow:platform:youtube"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💬 WhatsApp", "postflow:platform:whatsapp"),
			tgbotapi.NewInlineKeyboardButtonData("🌐 All", "postflow:platform:all"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "postflow:cancel"),
			tgbotapi.NewInlineKeyboardButtonData("❓ Help", "help:postflow"),
		),
	)
}

func (m *TenantBotManager) promptFirstStep(bot *tgbotapi.BotAPI, chatID int64, sess *PostSession) {
	var ma storage.MediaAsset
	if err := m.DB.First(&ma, sess.MediaAssetID).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "Couldn’t find that media anymore—try re-uploading."))
		delete(tenantSessions, chatID)
		return
	}

	if sess.MediaType == "carousel" {
		sess.Step = 1
		msgText := "📸 *Step 1/5: Carousel Setup*\n\n" +
			"A carousel is multiple images in one post. Keep these together as one carousel?\n\n" +
			"• **Yes** - Post all images together\n" +
			"• **No** - Create separate posts"
		msg := tgbotapi.NewMessage(chatID, msgText)
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = yesNoKeyboardWithCancel()
		bot.Send(msg)
		return
	}

	sess.Step = 2
	msgText := "📝 *Step 2/5: Post Name* (optional)\n\n" +
		"Give this post a name to find it later.\n\n" +
		"*Examples:*\n" +
		"• `Summer Sale 2024`\n" +
		"• `Product Launch`\n" +
		"• `Weekly Update`\n\n" +
		"Type a name or say `skip` to continue."
	msg := tgbotapi.NewMessage(chatID, msgText)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = cancelKeyboard()
	bot.Send(msg)
}

func (m *TenantBotManager) ContinuePostFlow(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, text string) {
	sess, ok := tenantSessions[chatID]
	if !ok {
		return
	}

	lower := strings.ToLower(strings.TrimSpace(text))

	switch sess.Step {
	case 0:
		if lower == "cancel" || lower == "stop" {
			delete(tenantSessions, chatID)
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Post creation cancelled. You can start again anytime!"))
			return
		}
		if lower == "yes" || lower == "y" {
			m.promptFirstStep(bot, chatID, sess)
			return
		}
		if lower == "no" || lower == "not now" || lower == "skip" {
			delete(tenantSessions, chatID)
			bot.Send(tgbotapi.NewMessage(chatID, "✅ No problem! Just upload media again when you want to schedule a post."))
			return
		}
		errorMsg := "❓ Please choose an option:\n\n" +
			"• **Yes** - Start creating post now\n" +
			"• **Not now** - Save for later"
		msg := tgbotapi.NewMessage(chatID, errorMsg)
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = yesNoKeyboard()
		bot.Send(msg)

	case 1:
		if lower == "yes" || lower == "y" {
			sess.IsCarousel = true
			sess.Step = 2
			msgText := "✅ *Step 2/5: Post Name* (optional)\n\n" +
				"Give this post a name to find it later.\n\n" +
				"*Examples:*\n" +
				"• `Summer Sale 2024`\n" +
				"• `Product Launch`\n\n" +
				"Type a name or say `skip` to continue."
			msg := tgbotapi.NewMessage(chatID, msgText)
			msg.ParseMode = "Markdown"
			msg.ReplyMarkup = cancelKeyboard()
			bot.Send(msg)
			return
		}
		if lower == "no" || lower == "n" {
			sess.IsCarousel = false
			sess.Step = 2
			msgText := "✅ *Step 2/5: Post Name* (optional)\n\n" +
				"Give this post a name to find it later.\n\n" +
				"*Examples:*\n" +
				"• `Summer Sale 2024`\n" +
				"• `Product Launch`\n\n" +
				"Type a name or say `skip` to continue."
			msg := tgbotapi.NewMessage(chatID, msgText)
			msg.ParseMode = "Markdown"
			msg.ReplyMarkup = cancelKeyboard()
			bot.Send(msg)
			return
		}
		errorMsg := "❓ Please answer with **Yes** or **No**.\n\n" +
			"• **Yes** = Keep images together\n" +
			"• **No** = Create separate posts"
		msg := tgbotapi.NewMessage(chatID, errorMsg)
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = yesNoKeyboardWithCancel()
		bot.Send(msg)

	case 2:
		if lower == "cancel" || lower == "stop" {
			delete(tenantSessions, chatID)
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Post creation cancelled. You can start again anytime!"))
			return
		}
		if lower != "skip" && text != "" {
			sess.Name = text
		}
		sess.Step = 3
		msgText := "📝 *Step 3/5: Caption*\n\n" +
			"*Caption options:*\n" +
			"• Type `AI` to generate a caption automatically\n" +
			"• Type your own caption\n" +
			"• Type `skip` for no caption\n\n" +
			"*Example:* `Check out our new collection! #Summer2024`"
		msg := tgbotapi.NewMessage(chatID, msgText)
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = cancelKeyboard()
		bot.Send(msg)

	case 3:
		if lower == "cancel" || lower == "stop" {
			delete(tenantSessions, chatID)
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Post creation cancelled. You can start again anytime!"))
			return
		}
		if strings.EqualFold(lower, "ai caption") || strings.EqualFold(lower, "ai") || strings.EqualFold(lower, "generate") {
			sess.UseAICaption = true
			sess.Step = 4
			msgText := "✅ *Step 4/5: Platforms*\n\n" +
				"Where should we post this?\n\n" +
				"*Examples:*\n" +
				"• `Instagram` - Post to Instagram only\n" +
				"• `Instagram and Facebook` - Post to both\n" +
				"• `all` - Post to all platforms\n\n" +
				"*Available platforms:* Instagram, Facebook, TikTok, YouTube, WhatsApp"
			msg := tgbotapi.NewMessage(chatID, msgText)
			msg.ParseMode = "Markdown"
			msg.ReplyMarkup = platformsKeyboard()
			bot.Send(msg)
			return
		}
		if lower == "skip" || text == "" {
			sess.Caption = ""
			sess.Step = 4
			msgText := "✅ *Step 4/5: Platforms*\n\n" +
				"Where should we post this?\n\n" +
				"*Examples:*\n" +
				"• `Instagram` - Post to Instagram only\n" +
				"• `Instagram and Facebook` - Post to both\n" +
				"• `all` - Post to all platforms\n\n" +
				"*Available platforms:* Instagram, Facebook, TikTok, YouTube, WhatsApp"
			msg := tgbotapi.NewMessage(chatID, msgText)
			msg.ParseMode = "Markdown"
			msg.ReplyMarkup = platformsKeyboard()
			bot.Send(msg)
			return
		}
		sess.Caption = text
		sess.Step = 4
		msgText := "✅ *Step 4/5: Platforms*\n\n" +
			"Where should we post this?\n\n" +
			"*Examples:*\n" +
			"• `Instagram` - Post to Instagram only\n" +
			"• `Instagram and Facebook` - Post to both\n" +
			"• `all` - Post to all platforms\n\n" +
			"*Available platforms:* Instagram, Facebook, TikTok, YouTube, WhatsApp"
		msg := tgbotapi.NewMessage(chatID, msgText)
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = platformsKeyboard()
		bot.Send(msg)

	case 4:
		if lower == "cancel" || lower == "stop" {
			delete(tenantSessions, chatID)
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Post creation cancelled. You can start again anytime!"))
			return
		}
		platforms := m.parsePlatforms(text)
		if len(platforms) == 0 {
			errorMsg := "❌ I didn't recognize those platforms.\n\n" +
				"*Try one of these:*\n" +
				"• `Instagram` - Post to Instagram\n" +
				"• `Instagram and Facebook` - Post to both\n" +
				"• `all` - Post everywhere\n\n" +
				"*Available:* Instagram, Facebook, TikTok, YouTube, WhatsApp"
			msg := tgbotapi.NewMessage(chatID, errorMsg)
			msg.ParseMode = "Markdown"
			msg.ReplyMarkup = platformsKeyboard()
			bot.Send(msg)
			return
		}
		sess.Platforms = platforms
		sess.Step = 5
		platformStr := strings.Join(platforms, ", ")
		msgText := fmt.Sprintf("✅ *Step 5/5: Schedule*\n\n"+
			"Platforms: *%s*\n\n"+
			"When should I publish this?\n\n"+
			"*Examples:*\n"+
			"• `now` - Post immediately\n"+
			"• `tomorrow 9am` - Tomorrow at 9 AM\n"+
			"• `tomorrow morning` - Tomorrow at 9 AM\n"+
			"• `next week monday 10am` - Next Monday at 10 AM\n\n"+
			"Type your preferred time:", platformStr)
		msg := tgbotapi.NewMessage(chatID, msgText)
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = cancelKeyboard()
		bot.Send(msg)

	case 5:
		if lower == "cancel" || lower == "stop" {
			delete(tenantSessions, chatID)
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Post creation cancelled. You can start again anytime!"))
			return
		}
		sess.ScheduleInput = text
		m.finalizePostFlow(bot, tenantID, chatID, sess)
		delete(tenantSessions, chatID)
	}
}

// parsePlatforms extracts platform names from natural language input
func (m *TenantBotManager) parsePlatforms(input string) []string {
	input = strings.ToLower(strings.TrimSpace(input))

	if input == "all" || input == "everywhere" {
		return []string{"instagram", "facebook", "tiktok", "youtube", "whatsapp"}
	}

	var platforms []string
	platformMap := map[string]string{
		"instagram": "instagram",
		"ig":        "instagram",
		"facebook":  "facebook",
		"fb":        "facebook",
		"tiktok":    "tiktok",
		"youtube":   "youtube",
		"yt":        "youtube",
		"whatsapp":  "whatsapp",
		"wa":        "whatsapp",
	}

	// Split by common separators: comma, "and", "&"
	input = strings.ReplaceAll(input, " and ", ",")
	input = strings.ReplaceAll(input, " & ", ",")
	input = strings.ReplaceAll(input, ", ", ",")

	// Split by comma and process each part
	parts := strings.Split(input, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Check if this part matches a platform
		partLower := strings.ToLower(part)
		if platform, ok := platformMap[partLower]; ok {
			// Avoid duplicates
			found := false
			for _, p := range platforms {
				if p == platform {
					found = true
					break
				}
			}
			if !found {
				platforms = append(platforms, platform)
			}
		} else {
			// If not found as a single word, try splitting by spaces
			words := strings.Fields(partLower)
			for _, word := range words {
				word = strings.Trim(word, ",.")
				if platform, ok := platformMap[word]; ok {
					// Avoid duplicates
					found := false
					for _, p := range platforms {
						if p == platform {
							found = true
							break
						}
					}
					if !found {
						platforms = append(platforms, platform)
					}
				}
			}
		}
	}

	return platforms
}

func (m *TenantBotManager) finalizePostFlow(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, sess *PostSession) {
	logger := m.Logger.With(zap.Uint("tenant", tenantID))

	// load media asset
	var ma storage.MediaAsset
	if err := m.DB.First(&ma, sess.MediaAssetID).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "Couldn't find that media."))
		return
	}

	// update name if given
	if sess.Name != "" {
		m.DB.Model(&ma).Update("name", sess.Name)
		ma.Name = sess.Name
	}

	// Use selected platforms or fallback
	platforms := sess.Platforms
	if len(platforms) == 0 {
		// Fallback: try to parse from schedule input
		platforms = utils.NormalizePlatforms(sess.ScheduleInput)
		if len(platforms) == 0 {
			platforms = []string{"instagram"}
		}
	}

	// Get tenant timezone first
	var tenant storage.Tenant
	if err := m.DB.First(&tenant, tenantID).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "Couldn't find your account. Please try again."))
		return
	}
	if tenant.Timezone == "" {
		tenant.Timezone = "UTC"
	}

	// Parse schedule with enhanced natural language support (using tenant timezone)
	scheduledAt, err := m.parseNaturalScheduleWithTenant(sess.ScheduleInput, tenantID)
	if err != nil {
		errorMsg := fmt.Sprintf("❌ *I didn't understand that time format.*\n\n"+
			"*Try one of these examples:*\n"+
			"• `now` - Post immediately\n"+
			"• `tomorrow 9am` - Tomorrow at 9 AM\n"+
			"• `tomorrow morning` - Tomorrow at 9 AM\n"+
			"• `tomorrow afternoon` - Tomorrow at 2 PM\n"+
			"• `next week monday 10am` - Next Monday at 10 AM\n\n"+
			"*Error:* %v\n\nType a time using one of the examples above:", err)
		msg := tgbotapi.NewMessage(chatID, errorMsg)
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = cancelKeyboard()
		bot.Send(msg)
		// Keep session active so user can retry
		return
	}

	// Handle batch scheduling if enabled
	if sess.BatchSchedule && len(sess.Platforms) > 1 {
		// Create multiple posts with spacing
		m.createBatchPosts(bot, tenantID, chatID, sess, ma, scheduledAt, platforms)
		return
	}

	// For AI captions, use placeholder (will be generated by scheduler)
	caption := sess.Caption
	if sess.UseAICaption && caption == "" {
		caption = fmt.Sprintf("AI-generated caption for %s", ma.Name)
	}

	idsJSON, _ := json.Marshal([]uint{ma.ID})
	platformsJSON, _ := json.Marshal(platforms)

	// Convert scheduled time to UTC for storage (scheduled_at is always stored in UTC)
	scheduledAtUTC := scheduledAt.UTC()

	sp := storage.ScheduledPost{
		TenantID:      tenantID,
		Name:          ma.Name,
		Caption:       caption,
		MediaAssetIDs: string(idsJSON),
		Platforms:     string(platformsJSON),
		ScheduledAt:   &scheduledAtUTC,
		Status:        "scheduled",
		Timezone:      tenant.Timezone,
	}

	if err := m.DB.Create(&sp).Error; err != nil {
		logger.Error("failed to create scheduled post", zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "Couldn't schedule the post."))
		return
	}

	// Auto-update content calendar
	calendarManager := NewContentCalendarManager(m.DB, m.Logger)
	calendarCtx := context.Background()
	if err := calendarManager.AutoUpdateCalendarFromPosts(calendarCtx, tenantID, sp.ID, sp.ScheduledAt); err != nil {
		logger.Warn("failed to update calendar", zap.Error(err))
	}

	// Generate friendly confirmation message
	summary := m.generateConfirmationMessage(ma, platforms, scheduledAt, sess.UseAICaption)
	bot.Send(tgbotapi.NewMessage(chatID, summary))
}

// parseNaturalSchedule parses natural language scheduling input with enhanced parsing
// Now accepts tenantID to use tenant's timezone
func (m *TenantBotManager) parseNaturalScheduleWithTenant(input string, tenantID uint) (time.Time, error) {
	input = strings.ToLower(strings.TrimSpace(input))

	// Get tenant timezone
	var tenant storage.Tenant
	loc := time.UTC
	if err := m.DB.First(&tenant, tenantID).Error; err == nil && tenant.Timezone != "" {
		if l, e := time.LoadLocation(tenant.Timezone); e == nil {
			loc = l
		}
	} else {
		// Fallback to DEFAULT_TIMEZONE env var
		if tz := os.Getenv("DEFAULT_TIMEZONE"); tz != "" {
			if l, e := time.LoadLocation(tz); e == nil {
				loc = l
			}
		}
	}

	now := time.Now().In(loc)

	// Handle immediate posts
	if strings.Contains(input, "now") || input == "" {
		return now, nil
	}

	// Handle "tomorrow morning" / "tomorrow afternoon" / "tomorrow evening"
	if strings.Contains(input, "tomorrow") {
		tomorrow := now.AddDate(0, 0, 1)

		if strings.Contains(input, "morning") {
			return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 9, 0, 0, 0, loc), nil
		}
		if strings.Contains(input, "afternoon") {
			return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 14, 0, 0, 0, loc), nil
		}
		if strings.Contains(input, "evening") {
			return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 18, 0, 0, 0, loc), nil
		}
		if strings.Contains(input, "night") {
			return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 20, 0, 0, 0, loc), nil
		}
		// Default tomorrow to 10am
		return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 10, 0, 0, 0, loc), nil
	}

	// Handle "next week" variations
	if strings.Contains(input, "next week") {
		nextWeek := now.AddDate(0, 0, 7)
		// Try to extract time
		if strings.Contains(input, "10am") || strings.Contains(input, "10 am") {
			return time.Date(nextWeek.Year(), nextWeek.Month(), nextWeek.Day(), 10, 0, 0, 0, loc), nil
		}
		if strings.Contains(input, "2pm") || strings.Contains(input, "2 pm") || strings.Contains(input, "14:00") {
			return time.Date(nextWeek.Year(), nextWeek.Month(), nextWeek.Day(), 14, 0, 0, 0, loc), nil
		}
		// Default to 10am next week
		return time.Date(nextWeek.Year(), nextWeek.Month(), nextWeek.Day(), 10, 0, 0, 0, loc), nil
	}

	// Try the existing parser
	parsed, err := utils.ParseSchedule(input, loc)
	if err == nil {
		return parsed, nil
	}

	return time.Time{}, fmt.Errorf("could not parse schedule")
}

// parseNaturalSchedule is kept for backward compatibility
func (m *TenantBotManager) parseNaturalSchedule(input string) (time.Time, error) {
	// Default to UTC if no tenant context
	loc := time.UTC
	if tz := os.Getenv("DEFAULT_TIMEZONE"); tz != "" {
		if l, e := time.LoadLocation(tz); e == nil {
			loc = l
		}
	}
	now := time.Now().In(loc)

	input = strings.ToLower(strings.TrimSpace(input))

	// Handle immediate posts
	if strings.Contains(input, "now") || input == "" {
		return now, nil
	}

	// Handle "tomorrow"
	if strings.Contains(input, "tomorrow") {
		tomorrow := now.AddDate(0, 0, 1)
		if strings.Contains(input, "morning") {
			return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 9, 0, 0, 0, loc), nil
		}
		if strings.Contains(input, "afternoon") {
			return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 14, 0, 0, 0, loc), nil
		}
		if strings.Contains(input, "evening") {
			return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 18, 0, 0, 0, loc), nil
		}
		return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 10, 0, 0, 0, loc), nil
	}

	// Try the existing parser
	parsed, err := utils.ParseSchedule(input, loc)
	if err == nil {
		return parsed, nil
	}

	return time.Time{}, fmt.Errorf("could not parse schedule")
}

// extractSpacing extracts hours from phrases like "24 hours apart", "each 12 hours", etc.
func (m *TenantBotManager) extractSpacing(input string) int {
	lower := strings.ToLower(input)

	// Look for patterns like "24 hours", "12 hours", etc.
	parts := strings.Fields(lower)
	for i, part := range parts {
		if part == "hours" || part == "hour" && i > 0 {
			// Try to parse the number before "hours"
			var hours int
			fmt.Sscanf(parts[i-1], "%d", &hours)
			if hours > 0 {
				return hours
			}
		}
	}

	// Default spacing: 24 hours
	return 24
}

// createBatchPosts creates multiple scheduled posts with spacing between them
func (m *TenantBotManager) createBatchPosts(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, sess *PostSession, ma storage.MediaAsset, startTime time.Time, platforms []string) {
	logger := m.Logger.With(zap.Uint("tenant", tenantID))
	spacing := m.extractSpacing(sess.BatchSpacing)

	var createdPosts []string

	for i, platform := range platforms {
		scheduledAt := startTime.Add(time.Duration(i*spacing) * time.Hour)

		idsJSON, _ := json.Marshal([]uint{ma.ID})
		platformsJSON, _ := json.Marshal([]string{platform})

		caption := sess.Caption
		if sess.UseAICaption && caption == "" {
			caption = fmt.Sprintf("AI-generated caption for %s", ma.Name)
		}

		// Get tenant timezone
		var tenant storage.Tenant
		timezone := "UTC"
		if err := m.DB.First(&tenant, tenantID).Error; err == nil && tenant.Timezone != "" {
			timezone = tenant.Timezone
		}

		// Convert to UTC for storage
		scheduledAtUTC := scheduledAt.UTC()

		sp := storage.ScheduledPost{
			TenantID:      tenantID,
			Name:          ma.Name,
			Caption:       caption,
			MediaAssetIDs: string(idsJSON),
			Platforms:     string(platformsJSON),
			ScheduledAt:   &scheduledAtUTC,
			Status:        "scheduled",
			Timezone:      timezone,
		}

		if err := m.DB.Create(&sp).Error; err != nil {
			logger.Error("failed to create batch post", zap.Error(err), zap.String("platform", platform))
			continue
		}

		createdPosts = append(createdPosts, fmt.Sprintf("%s at %s", strings.Title(platform), scheduledAt.Format("Jan 2, 3:04 PM")))
	}

	if len(createdPosts) > 0 {
		summary := fmt.Sprintf("✅ Scheduled %d posts:\n\n%s\n\nEach %d hours apart. I'll handle them! 🚀",
			len(createdPosts), strings.Join(createdPosts, "\n"), spacing)
		bot.Send(tgbotapi.NewMessage(chatID, summary))
	} else {
		bot.Send(tgbotapi.NewMessage(chatID, "Couldn't create batch posts. Please try again."))
	}
}

// generateConfirmationMessage creates a friendly, conversational confirmation message
func (m *TenantBotManager) generateConfirmationMessage(ma storage.MediaAsset, platforms []string, scheduledAt time.Time, useAI bool) string {
	var parts []string

	// Post name
	name := ma.Name
	if name == "" {
		name = ma.Type
	}

	// Start with friendly confirmation
	parts = append(parts, fmt.Sprintf("✅ Scheduled '%s'", name))

	// Media type description
	if ma.Type == "carousel" {
		var urls []string
		json.Unmarshal([]byte(ma.URLs), &urls)
		parts = append(parts, fmt.Sprintf("📸 %d-image carousel", len(urls)))
	} else {
		parts = append(parts, fmt.Sprintf("📸 %s", strings.Title(ma.Type)))
	}

	// Natural time description
	now := time.Now()
	timeUntil := scheduledAt.Sub(now)
	if timeUntil < 5*time.Minute {
		parts = append(parts, "⏰ posting now")
	} else if scheduledAt.Year() == now.Year() && scheduledAt.YearDay() == now.YearDay() {
		parts = append(parts, fmt.Sprintf("⏰ today at %s", scheduledAt.Format("3:04 PM")))
	} else if scheduledAt.Year() == now.Year() && scheduledAt.YearDay() == now.YearDay()+1 {
		parts = append(parts, fmt.Sprintf("⏰ tomorrow at %s", scheduledAt.Format("3:04 PM")))
	} else {
		parts = append(parts, fmt.Sprintf("⏰ %s", scheduledAt.Format("Jan 2 at 3:04 PM")))
	}

	// Platform list with natural language
	platformStr := ""
	if len(platforms) == 1 {
		platformStr = strings.Title(platforms[0])
	} else if len(platforms) == 2 {
		platformStr = fmt.Sprintf("%s & %s", strings.Title(platforms[0]), strings.Title(platforms[1]))
	} else {
		for i, p := range platforms {
			if i > 0 {
				if i == len(platforms)-1 {
					platformStr += " & "
				} else {
					platformStr += ", "
				}
			}
			platformStr += strings.Title(p)
		}
	}
	parts = append(parts, fmt.Sprintf("📱 to %s", platformStr))

	// AI caption note
	if useAI {
		parts = append(parts, "✨ AI will optimize the caption for each platform")
	}

	return strings.Join(parts, "\n") + "\n\nI'll handle it from here! 🚀"
}
