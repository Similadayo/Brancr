package bot

import (
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
	UseAICaption    bool
	Platforms      []string // Selected platforms
	ScheduleInput  string
	BatchSchedule  bool   // Batch scheduling multiple posts
	BatchSpacing   string // e.g., "24 hours apart"
	OriginalInputs []string // For batch scheduling
}

var tenantSessions = make(map[int64]*PostSession) // chatID → session

func (m *TenantBotManager) StartPostFlow(bot *tgbotapi.BotAPI, chatID int64, assetID uint) {
	// Load media asset to check type
	var ma storage.MediaAsset
	if err := m.DB.First(&ma, assetID).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "Couldn't find that media."))
		return
	}

	sess := &PostSession{
		Step:         1,
		MediaAssetID: assetID,
		MediaType:    ma.Type,
		IsCarousel:   ma.Type == "carousel",
	}
	tenantSessions[chatID] = sess

	// Step 1: Carousel confirmation (if multiple images)
	if ma.Type == "carousel" {
		var urls []string
		json.Unmarshal([]byte(ma.URLs), &urls)
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("📸 I see %d images. Should I make this a carousel? (yes/no)", len(urls))))
		return
	}

	// Step 1: Name (for single media) - conversational
	bot.Send(tgbotapi.NewMessage(chatID, "Want to name this post? (or type 'skip')"))
}

func (m *TenantBotManager) ContinuePostFlow(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, text string) {
	sess, ok := tenantSessions[chatID]
	if !ok {
		return // no active session
	}
	text = strings.TrimSpace(strings.ToLower(text))

	switch sess.Step {
	case 1: // Carousel confirmation OR name
		if sess.MediaType == "carousel" {
			// Carousel confirmation
			if text == "yes" || text == "y" || text == "yeah" || text == "sure" {
				sess.IsCarousel = true
			} else if text == "no" || text == "n" {
				sess.IsCarousel = false
			} else {
				bot.Send(tgbotapi.NewMessage(chatID, "Just say 'yes' or 'no' 😊"))
				return
			}
			sess.Step = 2 // Move to name step
			bot.Send(tgbotapi.NewMessage(chatID, "Want to name this post? (or type 'skip')"))
			return
		}
		// Name step (single media)
		if text != "skip" && text != "" {
			sess.Name = text
		}
		sess.Step = 3 // Move to caption step
		bot.Send(tgbotapi.NewMessage(chatID, "Use your caption or should I generate one with AI? (type your caption, 'AI caption', or 'skip')"))
		return

	case 2: // Name (if carousel confirmed) OR Caption (if single media)
		// Check if this is name step (after carousel confirmation)
		if sess.MediaType == "carousel" && sess.Name == "" {
			// This is the name step after carousel confirmation
			if text != "skip" && text != "" {
				sess.Name = text
			}
			sess.Step = 3 // Move to caption
			bot.Send(tgbotapi.NewMessage(chatID, "Use your caption or should I generate one with AI? (type your caption, 'AI caption', or 'skip')"))
			return
		}
		// Otherwise this is caption step (single media - step 2 was name)
		if strings.EqualFold(text, "ai caption") || strings.EqualFold(text, "ai") || strings.EqualFold(text, "generate") {
			sess.UseAICaption = true
			sess.Step = 4
			bot.Send(tgbotapi.NewMessage(chatID, "Got it! Which platforms should I post this to? (Instagram, Facebook, YouTube, TikTok, WhatsApp, or 'all')"))
		} else if text == "skip" || text == "" {
			sess.Caption = ""
			sess.Step = 4
			bot.Send(tgbotapi.NewMessage(chatID, "Which platforms? (Instagram, Facebook, YouTube, TikTok, WhatsApp, or 'all')"))
		} else {
			sess.Caption = text
			sess.Step = 4
			bot.Send(tgbotapi.NewMessage(chatID, "Which platforms should I post this to? (Instagram, Facebook, YouTube, TikTok, WhatsApp, or 'all')"))
		}
		return

	case 3: // Caption (fallthrough - shouldn't normally hit this)
		if strings.EqualFold(text, "ai caption") || strings.EqualFold(text, "ai") || strings.EqualFold(text, "generate") {
			sess.UseAICaption = true
			sess.Step = 4
			bot.Send(tgbotapi.NewMessage(chatID, "Got it! Which platforms should I post this to? (Instagram, Facebook, YouTube, TikTok, WhatsApp, or 'all')"))
		} else if text == "skip" || text == "" {
			sess.Caption = ""
			sess.Step = 4
			bot.Send(tgbotapi.NewMessage(chatID, "Which platforms? (Instagram, Facebook, YouTube, TikTok, WhatsApp, or 'all')"))
		} else {
			sess.Caption = text
			sess.Step = 4
			bot.Send(tgbotapi.NewMessage(chatID, "Which platforms should I post this to? (Instagram, Facebook, YouTube, TikTok, WhatsApp, or 'all')"))
		}
		return

	case 4: // Platform selection
		platforms := m.parsePlatforms(text)
		if len(platforms) == 0 {
			bot.Send(tgbotapi.NewMessage(chatID, "I didn't catch that. Which platforms? (e.g., 'Instagram and Facebook' or 'all')"))
			return
		}
		sess.Platforms = platforms
		sess.Step = 5
		
		// Check for batch scheduling hints
		lower := strings.ToLower(text)
		if strings.Contains(lower, "batch") || strings.Contains(lower, "each") || strings.Contains(lower, "apart") {
			bot.Send(tgbotapi.NewMessage(chatID, "When should I start? (e.g., 'tomorrow morning', 'next week at 10am') and I'll space them out."))
		} else {
			bot.Send(tgbotapi.NewMessage(chatID, "When should I post it? (e.g., 'now', 'tomorrow morning', 'next week at 10am', or 'schedule each 24 hours apart' for batch)"))
		}
		return

	case 5: // Schedule
		// Check for batch scheduling with spacing
		lower := strings.ToLower(text)
		if strings.Contains(lower, "each") || strings.Contains(lower, "apart") {
			spacing := m.extractSpacing(text)
			if spacing > 0 {
				sess.BatchSchedule = true
				sess.BatchSpacing = fmt.Sprintf("%d hours", spacing)
			}
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

	// Parse schedule with enhanced natural language support
	scheduledAt, err := m.parseNaturalSchedule(sess.ScheduleInput)
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

	sp := storage.ScheduledPost{
		TenantID:      tenantID,
		Name:          ma.Name,
		Caption:       caption,
		MediaAssetIDs: string(idsJSON),
		Platforms:     string(platformsJSON),
		ScheduledAt:   &scheduledAt,
		Status:        "scheduled",
	}

	if err := m.DB.Create(&sp).Error; err != nil {
		logger.Error("failed to create scheduled post", zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "Couldn't schedule the post."))
		return
	}

	// Generate friendly confirmation message
	summary := m.generateConfirmationMessage(ma, platforms, scheduledAt, sess.UseAICaption)
	bot.Send(tgbotapi.NewMessage(chatID, summary))
}

// parseNaturalSchedule parses natural language scheduling input with enhanced parsing
func (m *TenantBotManager) parseNaturalSchedule(input string) (time.Time, error) {
	input = strings.ToLower(strings.TrimSpace(input))
	now := time.Now()

	loc := time.UTC
	if tz := os.Getenv("DEFAULT_TIMEZONE"); tz != "" {
		if l, e := time.LoadLocation(tz); e == nil {
			loc = l
			now = now.In(l)
		}
	}

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
		
		sp := storage.ScheduledPost{
			TenantID:      tenantID,
			Name:          ma.Name,
			Caption:       caption,
			MediaAssetIDs: string(idsJSON),
			Platforms:     string(platformsJSON),
			ScheduledAt:   &scheduledAt,
			Status:        "scheduled",
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
