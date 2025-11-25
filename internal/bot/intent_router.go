package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/storage"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"
)

// IntentRouter uses AI to understand tenant intent and route to appropriate actions
type IntentRouter struct {
	Manager       *TenantBotManager
	IntentDetector *ai.IntentDetector
	Classifier    *ai.Classifier
	Emotion       *ai.EmotionAnalyzer
}

// NewIntentRouter creates a new intent-aware router
func NewIntentRouter(manager *TenantBotManager) *IntentRouter {
	return &IntentRouter{
		Manager:        manager,
		IntentDetector: nil, // Will be created on-demand if needed
		Classifier:     manager.Classifier,
		Emotion:        manager.Emotion,
	}
}

// TenantIntent represents what the tenant wants to do
type TenantIntent struct {
	Action      string            // "schedule_post", "connect_account", "view_stats", "chat", etc.
	Entity      string            // "instagram", "facebook", "post", etc.
	Confidence  float64           // 0-1
	Parameters  map[string]string // Extracted parameters (platform, post_name, etc.)
	RawMessage  string
	RequiresInput bool            // Whether more info is needed
}

// Route determines tenant intent and executes appropriate action
func (r *IntentRouter) Route(ctx context.Context, tenantID uint, chatID int64, text string, isOwner bool) (bool, error) {
	if !isOwner {
		return false, nil // Only route owner messages
	}

	// Step 1: Detect intent using AI
	intent, err := r.detectTenantIntent(ctx, tenantID, text)
	if err != nil {
		r.Manager.Logger.Warn("intent detection failed, using fallback",
			zap.Error(err),
			zap.Uint("tenant_id", tenantID))
		intent = r.fallbackIntentDetection(text)
	}

	r.Manager.Logger.Info("🧠 Intent detected",
		zap.String("action", intent.Action),
		zap.Float64("confidence", intent.Confidence),
		zap.Uint("tenant_id", tenantID))

	// Step 2: Execute action based on intent
	if intent.Confidence > 0.7 {
		return r.executeIntent(ctx, tenantID, chatID, intent)
	}

	// Step 3: Low confidence - ask for clarification or use AI chat
	if intent.Confidence < 0.5 {
		return false, nil // Let AI handle it naturally
	}

	// Step 4: Medium confidence - try to execute but be cautious
	return r.executeIntent(ctx, tenantID, chatID, intent)
}

// detectTenantIntent uses AI to understand what the tenant wants
func (r *IntentRouter) detectTenantIntent(ctx context.Context, tenantID uint, message string) (*TenantIntent, error) {
	// Load business context for better understanding
	var biz storage.BusinessProfile
	_ = r.Manager.DB.First(&biz, "tenant_id = ?", tenantID).Error

	// Get recent conversation for context
	recentHistory := r.Manager.Memory.GetRecentTenantConversationHistory(ctx, tenantID, 5)
	historyContext := ""
	if len(recentHistory) > 0 {
		// Extract last few user messages for context
		for i := len(recentHistory) - 1; i >= 0 && i >= len(recentHistory)-3; i-- {
			if recentHistory[i].Role == "user" {
				historyContext += recentHistory[i].Content + "\n"
			}
		}
	}

	// Build intelligent prompt
	systemPrompt := fmt.Sprintf(`You are analyzing messages from a business owner using Brancr (social media management platform).

Business: %s
Niche: %s

Recent context:
%s

Analyze this message and determine what the owner wants to do. Possible actions:
- schedule_post: Schedule a social media post
- connect_account: Connect Instagram/Facebook/TikTok/YouTube/WhatsApp
- view_stats: See analytics or statistics
- view_posts: View scheduled or posted content
- cancel_post: Cancel a scheduled post
- upload_media: Upload photos/videos
- chat: General conversation or questions
- get_help: Need help or information

Return JSON only:
{
  "action": "schedule_post",
  "entity": "instagram",
  "confidence": 0.9,
  "parameters": {
    "platform": "instagram",
    "post_name": "Launch Week"
  },
  "requires_input": false,
  "reasoning": "Owner wants to schedule a post on Instagram"
}

Message: "%s"`, biz.Name, biz.Niche, historyContext, message)

	messages := []ai.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: message},
	}

	response, err := r.Manager.Responder.Client.Generate(ctx, messages)
	if err != nil {
		return nil, err
	}

	// Parse JSON response
	var intent TenantIntent
	if err := parseJSONResponse(response, &intent); err != nil {
		return nil, fmt.Errorf("failed to parse intent response: %w", err)
	}

	intent.RawMessage = message
	return &intent, nil
}

// executeIntent executes the detected intent
func (r *IntentRouter) executeIntent(ctx context.Context, tenantID uint, chatID int64, intent *TenantIntent) (bool, error) {
	bot, ok := r.Manager.GetBot(tenantID)
	if !ok {
		return false, fmt.Errorf("bot not found for tenant %d", tenantID)
	}

	switch intent.Action {
	case "schedule_post":
		return r.handleSchedulePost(bot, tenantID, chatID, intent)
	case "connect_account":
		return r.handleConnectAccount(bot, tenantID, chatID, intent)
	case "view_stats":
		return r.handleViewStats(bot, tenantID, chatID, intent)
	case "view_posts":
		return r.handleViewPosts(bot, tenantID, chatID, intent)
	case "cancel_post":
		return r.handleCancelPost(bot, tenantID, chatID, intent)
	case "upload_media":
		return r.handleUploadMedia(bot, tenantID, chatID, intent)
	case "get_help":
		return r.handleGetHelp(bot, tenantID, chatID, intent)
	default:
		// For "chat" or unknown, return false to let AI handle it
		return false, nil
	}
}

// handleSchedulePost handles scheduling posts intelligently
func (r *IntentRouter) handleSchedulePost(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, intent *TenantIntent) (bool, error) {
	// Check if there are recent media assets
	var recentAssets []storage.MediaAsset
	r.Manager.DB.Where("tenant_id = ? AND status = ?", tenantID, "uploaded").
		Order("created_at DESC").
		Limit(5).
		Find(&recentAssets)

	if len(recentAssets) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "📸 No recent media found. Upload photos/videos first, then I can help schedule them!"))
		return true, nil
	}

	// If only one asset, start scheduling flow with it
	if len(recentAssets) == 1 {
		r.Manager.StartPostFlow(bot, chatID, recentAssets[0].ID)
		return true, nil
	}

	// Multiple assets - ask which one or use the most recent
	postName := intent.Parameters["post_name"]
	if postName != "" {
		// Try to find asset by name
		var asset storage.MediaAsset
		if err := r.Manager.DB.Where("tenant_id = ? AND carousel_name = ?", tenantID, postName).
			First(&asset).Error; err == nil {
			r.Manager.StartPostFlow(bot, chatID, asset.ID)
			return true, nil
		}
	}

	// Use most recent
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("📅 Starting scheduling for your latest media. Or specify which post: \"schedule [post name]\".")))
	r.Manager.StartPostFlow(bot, chatID, recentAssets[0].ID)
	return true, nil
}

// handleConnectAccount handles connecting social accounts
func (r *IntentRouter) handleConnectAccount(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, intent *TenantIntent) (bool, error) {
	platform := intent.Parameters["platform"]
	if platform == "" {
		platform = intent.Entity
	}
	if platform == "" {
		bot.Send(tgbotapi.NewMessage(chatID, "Which platform? Instagram, Facebook, TikTok, YouTube, or WhatsApp?"))
		return true, nil
	}

	platform = strings.ToLower(platform)
	r.Manager.StartSocialConnectFlow(bot, tenantID, chatID, platform)
	return true, nil
}

// handleViewStats handles viewing statistics - now uses analytics service
func (r *IntentRouter) handleViewStats(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, intent *TenantIntent) (bool, error) {
	// Use analytics handler if available
	if r.Manager.AnalyticsHandler != nil {
		ctx := context.Background()
		text := intent.RawMessage
		if text == "" {
			text = "analytics"
		}
		err := r.Manager.AnalyticsHandler.HandleAnalyticsRequest(ctx, tenantID, chatID, text)
		if err == nil {
			return true, nil
		}
		// Fall through to basic stats if analytics handler fails
	}

	// Fallback to basic stats
	var postLogs []storage.PostLog
	r.Manager.DB.Where("tenant_id = ?", tenantID).
		Where("created_at > ?", time.Now().AddDate(0, 0, -7)).
		Order("created_at DESC").
		Limit(10).
		Find(&postLogs)

	successCount := 0
	failedCount := 0
	for _, log := range postLogs {
		if log.Status == "success" {
			successCount++
		} else if log.Status == "failed" {
			failedCount++
		}
	}

	msg := fmt.Sprintf("📊 *Last 7 Days Stats:*\n\n✅ Successful posts: %d\n❌ Failed posts: %d\n📱 Total activity: %d\n\n_Ask for 'analytics', 'insights', or 'growth suggestions' for detailed reports!_",
		successCount, failedCount, len(postLogs))

	response := tgbotapi.NewMessage(chatID, msg)
	response.ParseMode = "Markdown"
	bot.Send(response)
	return true, nil
}

// handleViewPosts handles viewing scheduled posts
func (r *IntentRouter) handleViewPosts(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, intent *TenantIntent) (bool, error) {
	var posts []storage.ScheduledPost
	r.Manager.DB.Where("tenant_id = ? AND status IN ?", tenantID, []string{"scheduled", "posting"}).
		Order("scheduled_at ASC").
		Limit(10).
		Find(&posts)

	if len(posts) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "📅 No scheduled posts. Upload media and schedule them!"))
		return true, nil
	}

	response := "*📅 Your Scheduled Posts:*\n\n"
	var keyboardRows [][]tgbotapi.InlineKeyboardButton
	
	for i, post := range posts {
		scheduledTime := "not set"
		if post.ScheduledAt != nil {
			scheduledTime = post.ScheduledAt.Format("Jan 2, 3:04PM")
		}
		
		var platforms []string
		json.Unmarshal([]byte(post.Platforms), &platforms)
		platformStr := strings.Join(platforms, ", ")
		
		response += fmt.Sprintf("%d. *%s*\n   📱 %s\n   ⏰ %s\n   Status: %s\n\n", 
			i+1, post.Name, platformStr, scheduledTime, post.Status)
		
		// Add cancel button for each post
		postNameDisplay := post.Name
		if len(postNameDisplay) > 25 {
			postNameDisplay = postNameDisplay[:22] + "..."
		}
		cancelBtn := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("❌ Cancel %s", postNameDisplay),
			fmt.Sprintf("post:cancel:%d", post.ID),
		)
		keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{cancelBtn})
	}
	
	// Add "Cancel All" button
	cancelAllBtn := tgbotapi.NewInlineKeyboardButtonData("🚫 Cancel All Posts", "post:cancel_all")
	keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{cancelAllBtn})
	
	msg := tgbotapi.NewMessage(chatID, response)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(keyboardRows...)
	bot.Send(msg)
	return true, nil
}

// handleCancelPost handles cancelling posts
func (r *IntentRouter) handleCancelPost(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, intent *TenantIntent) (bool, error) {
	postName := intent.Parameters["post_name"]
	if postName == "" {
		bot.Send(tgbotapi.NewMessage(chatID, "Which post would you like to cancel? Type: cancel [post name]"))
		return true, nil
	}

	var post storage.ScheduledPost
	if err := r.Manager.DB.Where("tenant_id = ? AND name = ? AND status = ?", tenantID, postName, "scheduled").
		First(&post).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Couldn't find scheduled post: %s", postName)))
		return true, nil
	}

	if err := r.Manager.DB.Model(&post).Update("status", "cancelled").Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Unable to cancel the post. Please try again later."))
		return true, nil
	}

	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Cancelled: %s", postName)))
	return true, nil
}

// handleUploadMedia handles media upload requests
func (r *IntentRouter) handleUploadMedia(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, intent *TenantIntent) (bool, error) {
	bot.Send(tgbotapi.NewMessage(chatID, "📸 Send me photos or videos to upload! I'll help you organize and schedule them."))
	return true, nil
}

// handleGetHelp handles help requests
func (r *IntentRouter) handleGetHelp(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, intent *TenantIntent) (bool, error) {
	helpMsg := `🎛️ *Brancr Control Center*

*What I can do:*
• Schedule posts naturally: "Schedule a post for Instagram tomorrow"
• Connect accounts: "Connect my Instagram"
• View stats: "Show me my posting stats"
• View posts: "What posts are scheduled?"
• Cancel posts: "Cancel the Launch Week post"

*Or just chat naturally!* I understand what you want and will help you.`

	msg := tgbotapi.NewMessage(chatID, helpMsg)
	msg.ParseMode = "Markdown"
	bot.Send(msg)
	return true, nil
}

// fallbackIntentDetection provides keyword-based intent detection
func (r *IntentRouter) fallbackIntentDetection(text string) *TenantIntent {
	lower := strings.ToLower(text)
	
	intent := &TenantIntent{
		Action:     "chat",
		Confidence: 0.5,
		Parameters: make(map[string]string),
		RawMessage: text,
	}

	// Schedule post detection
	if strings.Contains(lower, "schedule") || strings.Contains(lower, "post") {
		intent.Action = "schedule_post"
		intent.Confidence = 0.75
		if strings.Contains(lower, "instagram") {
			intent.Parameters["platform"] = "instagram"
		} else if strings.Contains(lower, "facebook") {
			intent.Parameters["platform"] = "facebook"
		}
	}

	// Connect account detection
	if strings.Contains(lower, "connect") {
		intent.Action = "connect_account"
		intent.Confidence = 0.8
		if strings.Contains(lower, "instagram") {
			intent.Parameters["platform"] = "instagram"
		} else if strings.Contains(lower, "facebook") {
			intent.Parameters["platform"] = "facebook"
		} else if strings.Contains(lower, "tiktok") {
			intent.Parameters["platform"] = "tiktok"
		} else if strings.Contains(lower, "youtube") {
			intent.Parameters["platform"] = "youtube"
		} else if strings.Contains(lower, "whatsapp") {
			intent.Parameters["platform"] = "whatsapp"
		}
	}

	// View stats
	if strings.Contains(lower, "stat") || strings.Contains(lower, "analytics") || strings.Contains(lower, "performance") {
		intent.Action = "view_stats"
		intent.Confidence = 0.85
	}

	// View posts
	if strings.Contains(lower, "scheduled") || strings.Contains(lower, "my posts") || strings.Contains(lower, "what posts") {
		intent.Action = "view_posts"
		intent.Confidence = 0.85
	}

	// Cancel post
	if strings.Contains(lower, "cancel") {
		intent.Action = "cancel_post"
		intent.Confidence = 0.8
		// Try to extract post name
		if strings.Contains(lower, "cancel") {
			parts := strings.Split(lower, "cancel")
			if len(parts) > 1 {
				intent.Parameters["post_name"] = strings.TrimSpace(parts[1])
			}
		}
	}

	// Help
	if strings.Contains(lower, "help") || strings.Contains(lower, "what can you") || strings.Contains(lower, "commands") {
		intent.Action = "get_help"
		intent.Confidence = 0.9
	}

	return intent
}

// parseJSONResponse parses JSON from AI response, handling markdown code blocks
func parseJSONResponse(response string, target interface{}) error {
	// Remove markdown code blocks if present
	response = strings.TrimSpace(response)
	if strings.HasPrefix(response, "```") {
		lines := strings.Split(response, "\n")
		response = strings.Join(lines[1:len(lines)-1], "\n")
	}
	response = strings.Trim(response, "`")

	// Try to find JSON object
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start != -1 && end != -1 && end > start {
		response = response[start : end+1]
	}

	return json.Unmarshal([]byte(response), target)
}

