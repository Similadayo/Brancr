package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/ai"
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
	CaptionMode    string // "write", "keywords", "ai", "" (empty for backward compat)
	Platforms      []string // Selected platforms
	ScheduleInput  string
	BatchSchedule  bool     // Batch scheduling multiple posts
	BatchSpacing   string   // e.g., "24 hours apart"
	OriginalInputs []string // For batch scheduling
	TenantID       uint     // Added to check connected platforms
	// TikTok posting options
	TikTokDisableDuet    bool
	TikTokDisableStitch  bool
	TikTokDisableComment bool
	TikTokScheduleTime   *time.Time
	TikTokOptionsAsked   bool // Track if we've asked for TikTok options
}

var tenantSessions = make(map[int64]*PostSession) // chatID → session

// getTenantSession retrieves a post session (exported for use in tenant_handler.go)
func getTenantSession(chatID int64) (*PostSession, bool) {
	sess, ok := tenantSessions[chatID]
	return sess, ok
}

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
	// Extract tenant ID from media asset
	sess.TenantID = ma.TenantID
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

func (m *TenantBotManager) promptFirstStep(bot *tgbotapi.BotAPI, chatID int64, sess *PostSession) {
	var ma storage.MediaAsset
	if err := m.DB.First(&ma, sess.MediaAssetID).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "Couldn’t find that media anymore—try re-uploading."))
		delete(tenantSessions, chatID)
		return
	}

	if sess.MediaType == "carousel" {
		sess.Step = 1
		msg := tgbotapi.NewMessage(chatID, "Keep this together as one carousel?")
		msg.ReplyMarkup = yesNoKeyboard()
		bot.Send(msg)
		return
	}

	sess.Step = 2
	msg := tgbotapi.NewMessage(chatID, "Want to give this a quick internal nickname? (Type it or say 'skip'.)")
	bot.Send(msg)
}

func (m *TenantBotManager) ContinuePostFlow(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, text string) {
	sess, ok := tenantSessions[chatID]
	if !ok {
		return
	}
	// Ensure tenant ID is set
	if sess.TenantID == 0 {
		sess.TenantID = tenantID
	}

	lower := strings.ToLower(strings.TrimSpace(text))

	switch sess.Step {
	case 0:
		if lower == "yes" || lower == "y" {
			m.promptFirstStep(bot, chatID, sess)
			return
		}
		if lower == "no" || lower == "not now" || lower == "skip" {
			delete(tenantSessions, chatID)
			bot.Send(tgbotapi.NewMessage(chatID, "No problem—just drop me a note when you want to schedule it."))
			return
		}
		bot.Send(tgbotapi.NewMessage(chatID, "Just say yes if you want me to prep it now, or "+"‘not now’ to leave it for later."))

	case 1:
		if lower == "yes" || lower == "y" {
			sess.IsCarousel = true
			sess.Step = 2
			bot.Send(tgbotapi.NewMessage(chatID, "Perfect. Want to tag it with an internal name? (Type it or say 'skip'.)"))
			return
		}
		if lower == "no" || lower == "n" {
			sess.IsCarousel = false
			sess.Step = 2
			bot.Send(tgbotapi.NewMessage(chatID, "Got it. Want to tag it with an internal name? (Type it or say 'skip'.)"))
			return
		}
		bot.Send(tgbotapi.NewMessage(chatID, "Give me a quick yes or no so I know whether to keep these together."))

	case 2:
		if lower != "skip" && text != "" {
			sess.Name = text
		}
		sess.Step = 3
		// Show options with inline keyboard
		msg := tgbotapi.NewMessage(chatID, "How would you like to handle the caption?")
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✍️ Write my own", "caption:write"),
				tgbotapi.NewInlineKeyboardButtonData("🔑 Keywords → AI", "caption:keywords"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🤖 AI generate", "caption:ai"),
				tgbotapi.NewInlineKeyboardButtonData("⏭️ Skip", "caption:skip"),
			),
		)
		bot.Send(msg)

	case 3:
		// Handle caption input based on mode
		if sess.CaptionMode == "write" {
			// User is writing their own caption
			if lower == "skip" || text == "" {
				sess.Caption = ""
			} else {
				sess.Caption = text
			}
			sess.Step = 4
			bot.Send(tgbotapi.NewMessage(chatID, "Nice! Which platforms are we queuing it for? (Instagram, Facebook, TikTok, YouTube, WhatsApp, or 'all')."))
			return
		} else if sess.CaptionMode == "keywords" {
			// User provided keywords - generate caption
			if lower == "skip" || text == "" {
				sess.Caption = ""
				sess.Step = 4
				bot.Send(tgbotapi.NewMessage(chatID, "No worries. Which platforms should we hit? (Instagram, Facebook, TikTok, YouTube, WhatsApp, or 'all')."))
				return
			}
			// Generate caption from keywords
			m.generateCaptionFromKeywords(bot, tenantID, chatID, sess, text)
			return
		} else if sess.CaptionMode == "ai" {
			// AI caption mode - will be handled after platform selection
			sess.UseAICaption = true
			sess.Step = 4
			bot.Send(tgbotapi.NewMessage(chatID, "Love it. Where are we posting? (Instagram, Facebook, TikTok, etc., or just say 'all')."))
			return
		} else {
			// Fallback to old behavior for backward compatibility
			if strings.EqualFold(lower, "ai caption") || strings.EqualFold(lower, "ai") || strings.EqualFold(lower, "generate") {
				sess.UseAICaption = true
				sess.Step = 4
				bot.Send(tgbotapi.NewMessage(chatID, "Love it. Where are we posting? (Instagram, Facebook, TikTok, etc., or just say 'all')."))
				return
			}
			if lower == "skip" || text == "" {
				sess.Caption = ""
				sess.Step = 4
				bot.Send(tgbotapi.NewMessage(chatID, "No worries. Which platforms should we hit? (Instagram, Facebook, TikTok, YouTube, WhatsApp, or 'all')."))
				return
			}
			sess.Caption = text
			sess.Step = 4
			bot.Send(tgbotapi.NewMessage(chatID, "Nice! Which platforms are we queuing it for? (Instagram, Facebook, TikTok, YouTube, WhatsApp, or 'all')."))
		}

	case 4:
		// Check if we're in TikTok options mode (after platforms selected)
		if sess.TikTokOptionsAsked == false && len(sess.Platforms) > 0 {
			// Check if TikTok is selected and we haven't asked for options yet
			hasTikTok := false
			for _, p := range sess.Platforms {
				if p == "tiktok" {
					hasTikTok = true
					break
				}
			}
			
			if hasTikTok {
				// Handle TikTok options via text input
				lower := strings.ToLower(strings.TrimSpace(text))
				if lower == "skip" || lower == "none" || lower == "default" {
					// Use defaults (all options enabled)
					sess.TikTokDisableDuet = false
					sess.TikTokDisableStitch = false
					sess.TikTokDisableComment = false
					sess.TikTokOptionsAsked = true
					sess.Step = 5
					bot.Send(tgbotapi.NewMessage(chatID, "✅ Using default TikTok settings (duets, stitches, and comments enabled).\n\nWhen should I publish it? Try \"now\", \"tomorrow at 9am\", or \"next week at 10am\"."))
					return
				}
				// Try to parse TikTok options from text
				if strings.Contains(lower, "disable") || strings.Contains(lower, "no") {
					// Parse which options to disable
					if strings.Contains(lower, "duet") {
						sess.TikTokDisableDuet = true
					}
					if strings.Contains(lower, "stitch") {
						sess.TikTokDisableStitch = true
					}
					if strings.Contains(lower, "comment") {
						sess.TikTokDisableComment = true
					}
					sess.TikTokOptionsAsked = true
					sess.Step = 5
					bot.Send(tgbotapi.NewMessage(chatID, "✅ TikTok options saved.\n\nWhen should I publish it? Try \"now\", \"tomorrow at 9am\", or \"next week at 10am\"."))
					return
				}
				// If unclear, show options again
				m.showTikTokPostingOptions(bot, chatID, sess)
				return
			}
		}
		
		// Try to parse platforms from text first (for backward compatibility)
		platforms := m.parsePlatforms(text)
		if len(platforms) == 0 {
			// Show platform selection with inline buttons instead of text input
			m.showPlatformSelection(bot, chatID, sess)
			return
		}
		sess.Platforms = platforms
		
		// Check if TikTok is selected - if so, ask for TikTok posting options
		hasTikTok := false
		for _, p := range platforms {
			if p == "tiktok" {
				hasTikTok = true
				break
			}
		}
		
		if hasTikTok && !sess.TikTokOptionsAsked {
			// Show TikTok options UI
			m.showTikTokPostingOptions(bot, chatID, sess)
			return
		}
		
		sess.Step = 5
		bot.Send(tgbotapi.NewMessage(chatID, "When should I publish it? Try \"now\", \"tomorrow at 9am\", or \"next week at 10am\"."))

	case 5:
		sess.ScheduleInput = text
		m.Logger.Info("user provided schedule input", zap.Uint("tenant_id", tenantID), zap.String("input", text))
		
		// Check if scheduling for "now" - auto-confirm and post immediately
		lower := strings.ToLower(strings.TrimSpace(text))
		if lower == "now" || strings.Contains(lower, "now") {
			// For immediate posts, skip preview and finalize directly
			m.Logger.Info("immediate post requested, skipping preview", zap.Uint("tenant_id", tenantID))
			m.finalizePostFlow(bot, tenantID, chatID, sess)
			delete(tenantSessions, chatID)
			return
		}
		
		// For future scheduling, show preview
		m.showPostPreview(bot, tenantID, chatID, sess)
		return
	case 6:
		// Preview step - allow text confirmation as fallback
		lower := strings.ToLower(strings.TrimSpace(text))
		if lower == "yes" || lower == "y" || lower == "confirm" || lower == "ok" {
			m.finalizePostFlow(bot, tenantID, chatID, sess)
			delete(tenantSessions, chatID)
			return
		}
		if lower == "no" || lower == "cancel" {
			delete(tenantSessions, chatID)
			bot.Send(tgbotapi.NewMessage(chatID, "👌 Cancelled. Your media is still saved - you can schedule it later."))
			return
		}
		// If user types something else, remind them about the buttons
		bot.Send(tgbotapi.NewMessage(chatID, "💡 Please use the buttons above to confirm or edit your post, or type 'yes' to confirm, 'cancel' to cancel."))
		return
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

// showTikTokPostingOptions displays TikTok posting options with inline buttons
func (m *TenantBotManager) showTikTokPostingOptions(bot *tgbotapi.BotAPI, chatID int64, sess *PostSession) {
	text := `🎵 *TikTok Posting Options*

Configure how your TikTok video will be posted:

• *Duets*: %s
• *Stitches*: %s
• *Comments*: %s

You can toggle these options using the buttons below, or type:
• "disable duet" - Disable duets
• "disable stitch" - Disable stitches  
• "disable comment" - Disable comments
• "disable all" - Disable all three
• "skip" - Use defaults (all enabled)`

	// Get current status
	duetStatus := "✅ Enabled"
	if sess.TikTokDisableDuet {
		duetStatus = "❌ Disabled"
	}
	stitchStatus := "✅ Enabled"
	if sess.TikTokDisableStitch {
		stitchStatus = "❌ Disabled"
	}
	commentStatus := "✅ Enabled"
	if sess.TikTokDisableComment {
		commentStatus = "❌ Disabled"
	}
	
	text = fmt.Sprintf(text, duetStatus, stitchStatus, commentStatus)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("Duet: %s", func() string {
					if sess.TikTokDisableDuet {
						return "❌"
					}
					return "✅"
				}()),
				fmt.Sprintf("tiktok_option:toggle:duet:%v", !sess.TikTokDisableDuet),
			),
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("Stitch: %s", func() string {
					if sess.TikTokDisableStitch {
						return "❌"
					}
					return "✅"
				}()),
				fmt.Sprintf("tiktok_option:toggle:stitch:%v", !sess.TikTokDisableStitch),
			),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("Comment: %s", func() string {
					if sess.TikTokDisableComment {
						return "❌"
					}
					return "✅"
				}()),
				fmt.Sprintf("tiktok_option:toggle:comment:%v", !sess.TikTokDisableComment),
			),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Use Defaults", "tiktok_option:default"),
			tgbotapi.NewInlineKeyboardButtonData("➡️ Continue", "tiktok_option:continue"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

// showPlatformSelection displays platform selection with inline checkboxes
func (m *TenantBotManager) showPlatformSelection(bot *tgbotapi.BotAPI, chatID int64, sess *PostSession) {
	if sess.TenantID == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Error: Could not determine your account. Please try again."))
		return
	}
	
	// Check which platforms are connected
	var accounts []storage.SocialAccount
	m.DB.Where("tenant_id = ? AND connected = ?", sess.TenantID, true).Find(&accounts)
	
	connectedPlatforms := make(map[string]bool)
	for _, acc := range accounts {
		connectedPlatforms[acc.Platform] = true
	}
	
	selectedPlatforms := make(map[string]bool)
	for _, p := range sess.Platforms {
		selectedPlatforms[p] = true
	}
	
	// Build keyboard with checkboxes
	availablePlatforms := []string{"instagram", "facebook", "whatsapp", "tiktok", "youtube"}
	
	var rows [][]tgbotapi.InlineKeyboardButton
	var row []tgbotapi.InlineKeyboardButton
	
	for i, platform := range availablePlatforms {
		isConnected := connectedPlatforms[platform]
		isSelected := selectedPlatforms[platform]
		
		icon := "☐"
		if isSelected {
			icon = "☑️"
		}
		
		label := fmt.Sprintf("%s %s", icon, strings.Title(platform))
		if !isConnected {
			label += " (not connected)"
		}
		
		// Toggle selection state in callback data
		newSelected := !isSelected
		callbackData := fmt.Sprintf("platforms:toggle:%s:%v", platform, newSelected)
		
		button := tgbotapi.NewInlineKeyboardButtonData(label, callbackData)
		row = append(row, button)
		
		// Add 2 buttons per row
		if len(row) == 2 || i == len(availablePlatforms)-1 {
			rows = append(rows, row)
			row = []tgbotapi.InlineKeyboardButton{}
		}
	}
	
	// Add action buttons
	actionRow := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("✅ Continue", "platforms:confirm"),
		tgbotapi.NewInlineKeyboardButtonData("All Platforms", "platforms:all"),
	)
	rows = append(rows, actionRow)
	
	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
	
	msgText := "📱 *Select Platforms*\n\nChoose where to post. Tap platforms to select/deselect."
	if len(selectedPlatforms) > 0 {
		platformList := make([]string, 0, len(selectedPlatforms))
		for p := range selectedPlatforms {
			platformList = append(platformList, strings.Title(p))
		}
		msgText += fmt.Sprintf("\n\n✅ Selected: %s", strings.Join(platformList, ", "))
	}
	
	msg := tgbotapi.NewMessage(chatID, msgText)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
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

	// Save TikTok posting options to MediaAsset Metadata if TikTok is selected
	hasTikTok := false
	for _, p := range platforms {
		if p == "tiktok" {
			hasTikTok = true
			break
		}
	}
	
	if hasTikTok {
		metadata := make(map[string]interface{})
		// Try to parse existing metadata
		if ma.Metadata != "" {
			_ = json.Unmarshal([]byte(ma.Metadata), &metadata)
		}
		
		// Add TikTok posting options
		metadata["tiktok_disable_duet"] = sess.TikTokDisableDuet
		metadata["tiktok_disable_stitch"] = sess.TikTokDisableStitch
		metadata["tiktok_disable_comment"] = sess.TikTokDisableComment
		if sess.TikTokScheduleTime != nil {
			metadata["tiktok_schedule_time"] = sess.TikTokScheduleTime.Format(time.RFC3339)
		}
		
		metadataJSON, _ := json.Marshal(metadata)
		m.DB.Model(&ma).Update("metadata", string(metadataJSON))
		ma.Metadata = string(metadataJSON)
	}

	// Validate platform connections before proceeding
	var connectedAccounts []storage.SocialAccount
	m.DB.Where("tenant_id = ? AND connected = ?", tenantID, true).Find(&connectedAccounts)
	
	connectedPlatforms := make(map[string]bool)
	for _, acc := range connectedAccounts {
		connectedPlatforms[acc.Platform] = true
	}
	
	// Check which platforms are not connected
	var missingPlatforms []string
	var validPlatforms []string
	for _, platform := range platforms {
		if !connectedPlatforms[platform] {
			missingPlatforms = append(missingPlatforms, platform)
		} else {
			validPlatforms = append(validPlatforms, platform)
		}
	}
	
	// If some platforms are missing, warn user but continue with connected ones
	if len(missingPlatforms) > 0 {
		warningMsg := fmt.Sprintf("⚠️ *Platform Not Connected*\n\n")
		for _, p := range missingPlatforms {
			warningMsg += fmt.Sprintf("• *%s* is not connected yet\n", strings.Title(p))
		}
		warningMsg += "\nI'll only post to connected platforms. Connect missing platforms in the menu:\n`/menu` → Connect"
		
		bot.Send(tgbotapi.NewMessage(chatID, warningMsg))
		
		// If no platforms are connected, stop here
		if len(validPlatforms) == 0 {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ None of the selected platforms are connected. Please connect at least one platform before posting."))
			return
		}
		
		// Update platforms to only include connected ones
		platforms = validPlatforms
		logger.Info("filtered out unconnected platforms",
			zap.Strings("missing", missingPlatforms),
			zap.Strings("valid", platforms))
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
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Couldn't understand that time: %v. Try 'now', 'tomorrow morning', or 'next week at 10am'.", err)))
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
		logger.Error("failed to create scheduled post", 
			zap.Error(err),
			zap.Uint("tenant_id", tenantID),
			zap.Uint("asset_id", sess.MediaAssetID),
			zap.Strings("platforms", platforms))
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Couldn't schedule the post: %v. Please try again or contact support.", err)))
		return
	}
	
	logger.Info("✅ scheduled post created successfully",
		zap.Uint("post_id", sp.ID),
		zap.Uint("tenant_id", tenantID),
		zap.String("name", sp.Name),
		zap.Strings("platforms", platforms),
		zap.Time("scheduled_at", *sp.ScheduledAt))

	// Check if post is scheduled for "now" (within 5 seconds) and trigger immediate execution
	now := time.Now().UTC()
	timeUntilPost := scheduledAtUTC.Sub(now)
	if timeUntilPost <= 5*time.Second && m.Scheduler != nil {
		logger.Info("🚀 Post scheduled for now, triggering immediate execution",
			zap.Uint("post_id", sp.ID),
			zap.Duration("time_until", timeUntilPost))
		if err := m.Scheduler.ExecutePostImmediately(sp.ID); err != nil {
			logger.Warn("failed to execute post immediately, will be picked up by scheduler",
				zap.Uint("post_id", sp.ID),
				zap.Error(err))
		} else {
			logger.Info("✅ Immediate post execution triggered", zap.Uint("post_id", sp.ID))
		}
	}

	// Auto-update content calendar
	calendarManager := NewContentCalendarManager(m.DB, m.Logger)
	calendarCtx := context.Background()
	if err := calendarManager.AutoUpdateCalendarFromPosts(calendarCtx, tenantID, sp.ID, sp.ScheduledAt); err != nil {
		logger.Warn("failed to update calendar", zap.Error(err))
	}

	// Generate friendly confirmation message with timezone
	summary := m.generateConfirmationMessageWithTimezone(ma, platforms, scheduledAt, tenant.Timezone, sess.UseAICaption)
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

// generateConfirmationMessageWithTimezone generates confirmation message with timezone info
func (m *TenantBotManager) generateConfirmationMessageWithTimezone(ma storage.MediaAsset, platforms []string, scheduledAt time.Time, timezone string, useAI bool) string {
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

	// Get timezone location for display
	loc := time.UTC
	if timezone != "" {
		if l, err := time.LoadLocation(timezone); err == nil {
			loc = l
		}
	}
	scheduledAtLocal := scheduledAt.In(loc)

	// Natural time description with timezone
	now := time.Now().In(loc)
	timeUntil := scheduledAtLocal.Sub(now)
	
	timezoneAbbr := timezone
	if timezoneAbbr == "" {
		timezoneAbbr = "UTC"
	}
	
	var scheduleStr string
	if timeUntil < 5*time.Minute {
		scheduleStr = fmt.Sprintf("⏰ *Posting now* (%s)", timezoneAbbr)
	} else if scheduledAtLocal.Year() == now.Year() && scheduledAtLocal.YearDay() == now.YearDay() {
		scheduleStr = fmt.Sprintf("⏰ *Today at %s* (%s)", scheduledAtLocal.Format("3:04 PM"), timezoneAbbr)
	} else if scheduledAtLocal.Year() == now.Year() && scheduledAtLocal.YearDay() == now.YearDay()+1 {
		scheduleStr = fmt.Sprintf("⏰ *Tomorrow at %s* (%s)", scheduledAtLocal.Format("3:04 PM"), timezoneAbbr)
	} else {
		scheduleStr = fmt.Sprintf("⏰ *%s* (%s)", scheduledAtLocal.Format("Jan 2 at 3:04 PM"), timezoneAbbr)
	}
	parts = append(parts, scheduleStr)

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

// generateCaptionFromKeywords generates a caption from keywords and updates the session
func (m *TenantBotManager) generateCaptionFromKeywords(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, sess *PostSession, keywords string) {
	// Get media asset to extract image URLs
	var ma storage.MediaAsset
	if err := m.DB.First(&ma, sess.MediaAssetID).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "Couldn't find that media anymore—try re-uploading."))
		delete(tenantSessions, chatID)
		return
	}

	// Extract image URLs
	var imageURLs []string
	if ma.URLs != "" {
		_ = json.Unmarshal([]byte(ma.URLs), &imageURLs)
	}

	// Determine platform (use first platform if available, otherwise default to instagram)
	platform := "instagram"
	if len(sess.Platforms) > 0 {
		platform = sess.Platforms[0]
	}

	// Create caption optimizer
	optimizer := &ai.CaptionOptimizer{
		Client: m.AIClient,
		DB:     m.DB,
	}

	ctx := context.Background()
	caption, err := optimizer.GenerateCaptionFromKeywords(ctx, tenantID, keywords, platform, sess.MediaType, imageURLs)
	if err != nil {
		m.Logger.Error("failed to generate caption from keywords", zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "Sorry, I couldn't generate a caption right now. Try typing your own or say 'skip'."))
		sess.Step = 3
		return
	}

	sess.Caption = caption
	sess.Step = 3 // Keep at step 3 to show caption and allow rephrase/fine-tune

	// Show generated caption with options
	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("✨ *Generated Caption:*\n\n%s\n\n", caption))
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Use This", "caption:use"),
			tgbotapi.NewInlineKeyboardButtonData("🔄 Rephrase", "caption:rephrase"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✨ Fine-tune", "caption:fine_tune"),
			tgbotapi.NewInlineKeyboardButtonData("✏️ Edit Manually", "caption:edit"),
		),
	)
	bot.Send(msg)
}
