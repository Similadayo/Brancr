package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// IntentRouter uses AI to understand tenant intent and route to appropriate actions
type IntentRouter struct {
	Manager        *TenantBotManager
	IntentDetector *ai.IntentDetector
	Classifier     *ai.Classifier
	Emotion        *ai.EmotionAnalyzer
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
	Action        string                 // "schedule_post", "connect_account", "view_stats", "chat", etc.
	Entity        string                 // "instagram", "facebook", "post", etc.
	Confidence    float64                // 0-1
	Parameters    map[string]interface{} // Extracted parameters (can be strings, bools, numbers)
	RawMessage    string
	RequiresInput bool // Whether more info is needed
}

// GetParam safely extracts a string parameter from the Parameters map
func (t *TenantIntent) GetParam(key string) string {
	if t.Parameters == nil {
		return ""
	}
	val, ok := t.Parameters[key]
	if !ok {
		return ""
	}
	// Handle different types
	switch v := val.(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		return fmt.Sprintf("%v", v)
	case int:
		return fmt.Sprintf("%d", v)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// Route determines tenant intent and executes appropriate action
func (r *IntentRouter) Route(ctx context.Context, tenantID uuid.UUID, chatID int64, text string, isOwner bool) (bool, error) {
	if !isOwner {
		return false, nil // Only route owner messages
	}

	// Step 0: Check for active conversational task first
	if r.Manager.TaskManager != nil {
		activeTask, hasTask := r.Manager.TaskManager.GetActiveTask(ctx, tenantID, chatID)
		if hasTask {
			// Check if user wants to cancel/abandon the task
			if r.isAbandonIntent(text) {
				r.Manager.TaskManager.AbandonTask(ctx, activeTask.ID)
				bot, _ := r.Manager.GetBot(tenantID)
				if bot != nil {
					bot.Send(tgbotapi.NewMessage(chatID, "No problem! Let me know if you need anything else. 👍"))
				}
				return true, nil
			}

			// Try to handle within context of active task
			handled, err := r.handleActiveTask(ctx, tenantID, chatID, text, activeTask)
			if handled {
				return true, err
			}
			// If not handled, fall through to detect new intent
			// (user might be changing topics)
		}
	}

	// Step 1: Detect intent using AI
	intent, err := r.detectTenantIntent(ctx, tenantID, text)
	if err != nil {
		r.Manager.Logger.Warn("intent detection failed, using fallback",
			zap.Error(err),
			zap.Stringer("tenant_id", tenantID))
		intent = r.fallbackIntentDetection(text)
	}

	r.Manager.Logger.Info("🧠 Intent detected",
		zap.String("action", intent.Action),
		zap.Float64("confidence", intent.Confidence),
		zap.Stringer("tenant_id", tenantID))

	// Step 2: Execute action based on intent
	if intent.Confidence > 0.7 {
		return r.executeIntent(ctx, tenantID, chatID, intent)
	}

	// Step 3: Low confidence - ask for clarification or use AI chat
	if intent.Confidence < 0.5 {
		return false, nil // Let AI handle it naturally
	}

	// Step 4: Medium confidence - try to execute but be cautious
	if intent.Confidence < 0.7 {
		r.Manager.Logger.Warn("⚠️ Low confidence intent detected",
			zap.String("action", intent.Action),
			zap.Float64("confidence", intent.Confidence),
			zap.String("message", text),
			zap.Stringer("tenant_id", tenantID))
	}

	return r.executeIntent(ctx, tenantID, chatID, intent)
}

// isAbandonIntent checks if the user wants to cancel the current task
func (r *IntentRouter) isAbandonIntent(text string) bool {
	lower := strings.ToLower(text)
	abandonPhrases := []string{
		"never mind", "nevermind", "cancel", "forget it", "stop",
		"skip this", "no thanks", "not now", "maybe later", "abort",
	}
	for _, phrase := range abandonPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// handleActiveTask handles messages in the context of an active conversational task
func (r *IntentRouter) handleActiveTask(ctx context.Context, tenantID uuid.UUID, chatID int64, text string, task *ActiveTask) (bool, error) {
	bot, ok := r.Manager.GetBot(tenantID)
	if !ok {
		return false, fmt.Errorf("bot not found for tenant %d", tenantID)
	}

	// Get missing slots
	missing := r.Manager.TaskManager.GetMissingSlots(task)

	// If task is complete, execute it
	if len(missing) == 0 {
		return r.executeCompletedTask(ctx, tenantID, chatID, task)
	}

	// Try to extract slot value from the user's message
	nextSlot := missing[0]
	value := r.extractSlotValue(ctx, text, nextSlot, task.TaskType)

	if value != "" {
		// Fill the slot
		r.Manager.TaskManager.FillSlot(ctx, task.ID, nextSlot.Name, value)

		// Refresh task to check remaining slots
		updatedTask, _ := r.Manager.TaskManager.GetActiveTask(ctx, tenantID, chatID)
		remaining := r.Manager.TaskManager.GetMissingSlots(updatedTask)

		if len(remaining) == 0 {
			// All slots filled - execute the task
			return r.executeCompletedTask(ctx, tenantID, chatID, updatedTask)
		}

		// Ask for the next slot with suggestions
		nextPrompt := remaining[0].Prompt
		if len(remaining[0].Alternatives) > 0 {
			// Send with suggestion buttons
			keyboard := r.buildSuggestionKeyboard(remaining[0].Alternatives)
			msg := tgbotapi.NewMessage(chatID, nextPrompt)
			msg.ReplyMarkup = keyboard
			bot.Send(msg)
		} else {
			bot.Send(tgbotapi.NewMessage(chatID, nextPrompt))
		}
		return true, nil
	}

	// Couldn't extract value - check if this might be a different intent
	newIntent := r.fallbackIntentDetection(text)
	if newIntent.Confidence > 0.7 && newIntent.Action != "chat" && newIntent.Action != task.TaskType {
		// User seems to be changing topics - abandon current task and handle new intent
		r.Manager.TaskManager.AbandonTask(ctx, task.ID)
		r.Manager.Logger.Info("🔄 User changed topics, abandoning task",
			zap.Stringer("task_id", task.ID),
			zap.String("new_intent", newIntent.Action))
		return r.executeIntent(ctx, tenantID, chatID, newIntent)
	}

	// Re-prompt for the current slot
	reprompt := fmt.Sprintf("I didn't quite catch that. %s", nextSlot.Prompt)
	if len(nextSlot.Alternatives) > 0 {
		keyboard := r.buildSuggestionKeyboard(nextSlot.Alternatives)
		msg := tgbotapi.NewMessage(chatID, reprompt)
		msg.ReplyMarkup = keyboard
		bot.Send(msg)
	} else {
		bot.Send(tgbotapi.NewMessage(chatID, reprompt))
	}
	return true, nil
}

// buildSuggestionKeyboard creates inline keyboard with slot suggestions
func (r *IntentRouter) buildSuggestionKeyboard(suggestions []string) tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, suggestion := range suggestions {
		btn := tgbotapi.NewInlineKeyboardButtonData(suggestion, fmt.Sprintf("slot:%s", suggestion))
		rows = append(rows, []tgbotapi.InlineKeyboardButton{btn})
	}
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// extractSlotValue attempts to extract a slot value from user text
func (r *IntentRouter) extractSlotValue(ctx context.Context, text string, slot TaskSlot, taskType string) string {
	lower := strings.ToLower(strings.TrimSpace(text))

	switch slot.Name {
	case "platforms", "platform":
		return r.extractPlatform(lower)
	case "schedule_time":
		return r.extractScheduleTime(lower)
	case "media":
		return r.extractMediaChoice(lower)
	case "caption":
		// Caption is whatever the user types (unless they want to skip or auto-generate)
		if strings.Contains(lower, "generate") || strings.Contains(lower, "ai") || strings.Contains(lower, "auto") {
			return "auto"
		}
		if strings.Contains(lower, "skip") || strings.Contains(lower, "no caption") || lower == "no" {
			return "none"
		}
		// Use the text as caption if it's substantial
		if len(text) > 3 {
			return text
		}
		return ""
	default:
		// For other slots, use the text if it's not too short
		if len(text) > 2 {
			return text
		}
		return ""
	}
}

// extractPlatform extracts platform name from text
func (r *IntentRouter) extractPlatform(text string) string {
	platforms := map[string]string{
		"instagram": "instagram",
		"ig":        "instagram",
		"insta":     "instagram",
		"facebook":  "facebook",
		"fb":        "facebook",
		"tiktok":    "tiktok",
		"tt":        "tiktok",
		"youtube":   "youtube",
		"yt":        "youtube",
		"whatsapp":  "whatsapp",
		"wa":        "whatsapp",
		"all":       "all",
	}

	for keyword, platform := range platforms {
		if strings.Contains(text, keyword) {
			return platform
		}
	}
	return ""
}

// extractScheduleTime extracts scheduling time from text
func (r *IntentRouter) extractScheduleTime(text string) string {
	// Common time expressions
	if strings.Contains(text, "now") {
		return "now"
	}
	if strings.Contains(text, "tomorrow") {
		if strings.Contains(text, "morning") {
			return "tomorrow 10:00"
		}
		if strings.Contains(text, "evening") || strings.Contains(text, "night") {
			return "tomorrow 18:00"
		}
		if strings.Contains(text, "afternoon") {
			return "tomorrow 14:00"
		}
		return "tomorrow 10:00" // Default to morning
	}
	if strings.Contains(text, "today") {
		if strings.Contains(text, "evening") || strings.Contains(text, "night") {
			return "today 18:00"
		}
		return "today 14:00" // Default to afternoon
	}
	if strings.Contains(text, "best") || strings.Contains(text, "optimal") {
		return "best_time"
	}

	// If the user just types a time, try to use it
	if len(text) < 20 {
		return text
	}
	return ""
}

// extractMediaChoice determines which media the user wants to use
func (r *IntentRouter) extractMediaChoice(text string) string {
	if strings.Contains(text, "latest") || strings.Contains(text, "recent") || strings.Contains(text, "last") {
		return "latest"
	}
	if strings.Contains(text, "yes") || strings.Contains(text, "that") || strings.Contains(text, "use") {
		return "latest"
	}
	if strings.Contains(text, "show") || strings.Contains(text, "options") || strings.Contains(text, "list") {
		return "show_options"
	}
	return ""
}

// executeCompletedTask executes a task when all slots are filled
func (r *IntentRouter) executeCompletedTask(ctx context.Context, tenantID uuid.UUID, chatID int64, task *ActiveTask) (bool, error) {
	bot, ok := r.Manager.GetBot(tenantID)
	if !ok {
		return false, fmt.Errorf("bot not found")
	}

	switch task.TaskType {
	case "schedule_post":
		return r.executeSchedulePostTask(ctx, bot, tenantID, chatID, task)
	case "connect_account":
		return r.executeConnectAccountTask(ctx, bot, tenantID, chatID, task)
	default:
		// Mark completed and let user know
		r.Manager.TaskManager.CompleteTask(ctx, task.ID)
		bot.Send(tgbotapi.NewMessage(chatID, "✅ Got it, I've noted that down!"))
		return true, nil
	}
}

// executeSchedulePostTask executes a schedule_post task
func (r *IntentRouter) executeSchedulePostTask(ctx context.Context, bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, task *ActiveTask) (bool, error) {
	filled := r.Manager.TaskManager.GetFilledSlots(task)

	// Get media asset
	var asset storage.MediaAsset
	mediaChoice := filled["media"]
	if mediaChoice == "latest" || mediaChoice == "" {
		if err := r.Manager.DB.Where("tenant_id = ? AND status = ?", tenantID, "uploaded").
			Order("created_at DESC").First(&asset).Error; err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ No uploaded media found. Please upload a photo or video first!"))
			r.Manager.TaskManager.AbandonTask(ctx, task.ID)
			return true, nil
		}
	}

	// Get platforms
	platforms := filled["platforms"]
	if platforms == "" {
		platforms = "instagram"
	}

	// Get schedule time
	scheduleTime := filled["schedule_time"]

	// Get caption
	caption := filled["caption"]
	if caption == "auto" {
		caption = "" // Will be auto-generated
	} else if caption == "none" {
		caption = ""
	}

	// Mark task complete
	r.Manager.TaskManager.CompleteTask(ctx, task.ID)

	// Send confirmation and start the actual scheduling
	confirmMsg := fmt.Sprintf("📅 *Scheduling your post:*\n\n"+
		"📱 Platform: %s\n"+
		"⏰ Time: %s\n"+
		"📝 Caption: %s\n\n"+
		"Setting it up now...", platforms, scheduleTime, func() string {
		if caption == "" {
			return "(will generate)"
		}
		if len(caption) > 50 {
			return caption[:47] + "..."
		}
		return caption
	}())

	msg := tgbotapi.NewMessage(chatID, confirmMsg)
	msg.ParseMode = "Markdown"
	bot.Send(msg)

	// Start the actual post flow with the gathered info
	r.Manager.StartPostFlowWithWorkflow(bot, chatID, asset.ID)

	return true, nil
}

// executeConnectAccountTask executes a connect_account task
func (r *IntentRouter) executeConnectAccountTask(ctx context.Context, bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, task *ActiveTask) (bool, error) {
	filled := r.Manager.TaskManager.GetFilledSlots(task)
	platform := filled["platform"]

	r.Manager.TaskManager.CompleteTask(ctx, task.ID)

	// Start the social connect flow
	r.Manager.StartSocialConnectFlow(bot, tenantID, chatID, platform)

	return true, nil
}

// detectTenantIntent uses AI to understand what the tenant wants
func (r *IntentRouter) detectTenantIntent(ctx context.Context, tenantID uuid.UUID, message string) (*TenantIntent, error) {
	// 1. Load Business Profile & Persona (Context)
	var biz storage.BusinessProfile
	if err := r.Manager.DB.First(&biz, "tenant_id = ?", tenantID).Error; err != nil {
		// Fallback if no profile yet
		biz = storage.BusinessProfile{Niche: "General Business"}
	}

	// 2. Load Conversation History (Short-term memory)
	recentHistory := r.Manager.Memory.GetRecentTenantConversationHistory(ctx, tenantID, 5)
	var historyBuilder strings.Builder
	for _, msg := range recentHistory {
		role := "Owner"
		if msg.Role == "assistant" {
			role = "Assistant"
		}

		content := ""
		switch v := msg.Content.(type) {
		case string:
			content = v
		case []ai.ContentPart:
			for _, part := range v {
				if part.Type == "text" {
					content += part.Text
				}
			}
		}

		if content != "" {
			historyBuilder.WriteString(fmt.Sprintf("%s: %s\n", role, content))
		}
	}
	historyContext := historyBuilder.String()

	// 3. Load Real-time Business Stats (Situational Awareness)
	// This mirrors the ContextAwareGenerator architecture
	stats := r.getTenantStats(ctx, tenantID)

	// 4. Construct Rich Prompt
	// We inject "System Thinking" context so the AI knows the *state* of the business
	prompt := fmt.Sprintf(`SYSTEM CONTEXT:
You are the AI assistant for a business owner. Your goal is to map their request to a specific action.

BUSINESS SNAPSHOT:
- Niche: %s
- Pending Posts: %d (scheduled/posting)
- Recent Uploads: %d (unscheduled media)
- Active Offers: %d
- Connected Accounts: %d

RECENT CONVERSATION:
%s

POSSIBLE ACTIONS:
- schedule_post: Schedule social media content (use if they mention "post", "schedule", "upload", or time)
- connect_account: Connect/Link platforms (Instagram, Facebook, etc.)
- view_stats: View analytics, performance, insights
- view_posts: List/Show scheduled or past posts
- cancel_post: Cancel/Delete a scheduled post
- add_offer: Add a new deal/promotion
- chat: General chitchat, questions, or clarification (use for "hello", "thanks", "how are you")
- view_orders: Show pending, confirmed, or completed orders
- get_help: Explicit requests for help or menu

EXAMPLES:
User: "How much did I make today?"
JSON: {"action": "view_stats", "entity": "revenue", "confidence": 0.95, "parameters": {"metric": "revenue", "period": "today"}}

User: "Any new orders?"
JSON: {"action": "view_orders", "entity": "orders", "confidence": 0.95, "parameters": {"status": "pending"}}

User: "Delete that last post"
JSON: {"action": "cancel_post", "entity": "post", "confidence": 0.95, "parameters": {"post_name": "latest"}}

User: "Post this on Insta tomorrow at 5pm"
JSON: {"action": "schedule_post", "entity": "instagram", "confidence": 0.98, "parameters": {"platform": "instagram", "schedule_time": "tomorrow 17:00", "media": "latest"}}

TASK:
Analyze the owner's message: "%s"
Return a JSON object with the best matching action.

JSON SCHEMA:
{
  "action": "action_name",
  "entity": "primary_entity",
  "confidence": 0.0-1.0,
  "parameters": {
    "key": "value"
  },
  "reasoning": "brief explanation"
}

RESPONSE (JSON ONLY):`,
		biz.Niche,
		stats.PendingPosts,
		stats.UnscheduledMedia,
		stats.ActiveOffers,
		stats.ConnectedAccounts,
		historyContext,
		message,
	)

	// 5. Call AI
	// We use the raw client here for structured output
	messages := []ai.Message{
		{Role: "user", Content: prompt},
	}

	resp, err := r.Manager.AIClient.Generate(ctx, messages)
	if err != nil {
		return nil, err
	}

	// 6. Parse Response
	var intent TenantIntent
	if err := json.Unmarshal([]byte(cleanJSON(resp)), &intent); err != nil {
		return nil, fmt.Errorf("failed to parse intent json: %w", err)
	}

	intent.RawMessage = message
	return &intent, nil
}

// cleanJSON removes markdown code blocks from JSON string
func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// TenantStats holds real-time stats for context injection
type TenantStats struct {
	PendingPosts      int64
	UnscheduledMedia  int64
	ActiveOffers      int64
	ConnectedAccounts int64
}

// getTenantStats gathers quick stats for the prompt
func (r *IntentRouter) getTenantStats(ctx context.Context, tenantID uuid.UUID) TenantStats {
	var stats TenantStats

	// Count pending posts
	r.Manager.DB.Model(&storage.ScheduledPost{}).
		Where("tenant_id = ? AND status IN ?", tenantID, []string{"scheduled", "posting"}).
		Count(&stats.PendingPosts)

	// Count unscheduled media (uploaded > 10 mins ago to avoid race with immediate flow)
	// Actually, just count all uploaded media that isn't attached to a scheduled post yet
	// For simplicity in this snapshot, we just count "uploaded" status media assets
	r.Manager.DB.Model(&storage.MediaAsset{}).
		Where("tenant_id = ? AND status = ?", tenantID, "uploaded").
		Count(&stats.UnscheduledMedia)

	// Count active offers
	r.Manager.DB.Model(&storage.TenantOffer{}).
		Where("tenant_id = ? AND is_active = ?", tenantID, true).
		Count(&stats.ActiveOffers)

	// Count connected accounts
	r.Manager.DB.Model(&storage.SocialAccount{}).
		Where("tenant_id = ? AND connected = ?", tenantID, true).
		Count(&stats.ConnectedAccounts)

	return stats
}

// executeIntent executes the detected intent
func (r *IntentRouter) executeIntent(ctx context.Context, tenantID uuid.UUID, chatID int64, intent *TenantIntent) (bool, error) {
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
	case "add_offer":
		return r.handleAddOffer(bot, tenantID, chatID, intent)
	case "get_help":
		return r.handleGetHelp(bot, tenantID, chatID, intent)
	case "view_orders":
		return r.handleViewOrders(bot, tenantID, chatID, intent)
	case "tiktok_command", "tiktok":
		// Check if message contains TikTok-related keywords
		lower := strings.ToLower(intent.RawMessage)
		if strings.Contains(lower, "tiktok") {
			return r.handleTikTokCommand(bot, tenantID, chatID, intent)
		}
		fallthrough
	default:
		// For "chat" or unknown, return false to let AI handle it
		return false, nil
	}
}

// handleSchedulePost handles scheduling posts intelligently
func (r *IntentRouter) handleSchedulePost(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, intent *TenantIntent) (bool, error) {
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
		r.Manager.StartPostFlowWithWorkflow(bot, chatID, recentAssets[0].ID)
		return true, nil
	}

	// Multiple assets - ask which one or use the most recent
	postName := intent.GetParam("post_name")
	if postName != "" {
		// Try to find asset by name
		var asset storage.MediaAsset
		if err := r.Manager.DB.Where("tenant_id = ? AND carousel_name = ?", tenantID, postName).
			First(&asset).Error; err == nil {
			r.Manager.StartPostFlowWithWorkflow(bot, chatID, asset.ID)
			return true, nil
		}
	}

	// Use most recent
	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("📅 Starting scheduling for your latest media. Or specify which post: \"schedule [post name]\".")))
	r.Manager.StartPostFlowWithWorkflow(bot, chatID, recentAssets[0].ID)
	return true, nil
}

// handleConnectAccount handles connecting social accounts
func (r *IntentRouter) handleConnectAccount(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, intent *TenantIntent) (bool, error) {
	platform := intent.GetParam("platform")
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
func (r *IntentRouter) handleViewStats(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, intent *TenantIntent) (bool, error) {
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
func (r *IntentRouter) handleViewPosts(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, intent *TenantIntent) (bool, error) {
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
			// Use stored timezone or default to UTC
			timezone := post.Timezone
			if timezone == "" {
				timezone = "UTC"
			}

			loc, err := time.LoadLocation(timezone)
			if err != nil {
				loc = time.UTC
			}

			// Convert UTC time to tenant's timezone for display
			localTime := post.ScheduledAt.In(loc)
			scheduledTime = localTime.Format("Jan 2, 3:04PM")
			if timezone != "UTC" {
				scheduledTime += fmt.Sprintf(" (%s)", timezone)
			}
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
func (r *IntentRouter) handleCancelPost(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, intent *TenantIntent) (bool, error) {
	postName := intent.GetParam("post_name")
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
func (r *IntentRouter) handleUploadMedia(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, intent *TenantIntent) (bool, error) {
	bot.Send(tgbotapi.NewMessage(chatID, "📸 Send me photos or videos to upload! I'll help you organize and schedule them."))
	return true, nil
}

// handleAddOffer handles adding promotional offers naturally
func (r *IntentRouter) handleAddOffer(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, intent *TenantIntent) (bool, error) {
	offerText := intent.GetParam("offer_text")
	if offerText == "" {
		offerText = intent.RawMessage
	}

	offerTitle := strings.TrimSpace(offerText)

	// Create the offer in DB
	offer := storage.TenantOffer{
		TenantID: tenantID,
		Title:    offerTitle,
		IsActive: true,
	}

	if err := r.Manager.DB.Create(&offer).Error; err != nil {
		return true, fmt.Errorf("failed to create offer: %w", err)
	}

	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Added active offer: \"%s\". I'll mention it when customers ask about deals!", offerTitle)))
	return true, nil
}

// handleViewOrders handles requests to view orders
func (r *IntentRouter) handleViewOrders(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, intent *TenantIntent) (bool, error) {
	status := intent.GetParam("status")
	if status == "" {
		status = "pending" // Default to pending
	}

	var orders []storage.Order
	query := r.Manager.DB.Where("tenant_id = ?", tenantID)

	if status != "all" {
		query = query.Where("status = ?", status)
	}

	// Get recent orders
	query.Order("created_at DESC").Limit(5).Find(&orders)

	if len(orders) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("📦 No %s orders found.", status)))
		return true, nil
	}

	response := fmt.Sprintf("*📦 Your %s Orders:*\n\n", strings.Title(status))
	for _, order := range orders {
		response += fmt.Sprintf("• *%s* - %s\n  Amount: %s %.2f\n  Customer: %s\n\n",
			order.OrderNumber, order.Status, order.Currency, order.TotalAmount, order.CustomerName)
	}

	msg := tgbotapi.NewMessage(chatID, response)
	msg.ParseMode = "Markdown"
	bot.Send(msg)
	return true, nil
}

// handleGetHelp handles help requests
func (r *IntentRouter) handleGetHelp(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, intent *TenantIntent) (bool, error) {
	helpMsg := `🎛️ *Brancr Control Center*

*What I can do:*
• Schedule posts naturally: "Schedule a post for Instagram tomorrow"
• Connect accounts: "Connect my Instagram"
• View stats: "Show me my posting stats"
• View posts: "What posts are scheduled?"
• Cancel posts: "Cancel the Launch Week post"
• Add offers: "Tell customers buy 2 get 1 free" or "20%% off on orders above ₦5000"

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
		Parameters: make(map[string]interface{}),
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

	// TikTok commands
	if strings.Contains(lower, "tiktok") {
		if strings.Contains(lower, "video") || strings.Contains(lower, "analytics") ||
			strings.Contains(lower, "comment") || strings.Contains(lower, "status") ||
			strings.Contains(lower, "delete") || strings.Contains(lower, "reply") ||
			strings.Contains(lower, "list") {
			intent.Action = "tiktok_command"
			intent.Confidence = 0.85
			// Extract parameters
			if strings.Contains(lower, "video") {
				intent.Parameters["tiktok_action"] = "list_videos"
			} else if strings.Contains(lower, "analytics") || strings.Contains(lower, "stats") {
				// TikTok analytics removed - no longer supported
			} else if strings.Contains(lower, "comment") {
				intent.Parameters["tiktok_action"] = "comments"
			} else if strings.Contains(lower, "status") {
				intent.Parameters["tiktok_action"] = "status"
			} else if strings.Contains(lower, "delete") {
				intent.Parameters["tiktok_action"] = "delete"
			} else if strings.Contains(lower, "reply") {
				intent.Parameters["tiktok_action"] = "reply"
			}
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

	// Add offer detection - natural language
	offerKeywords := []string{"offer", "deal", "discount", "promo", "special", "sale", "combo", "bundle", "buy", "get", "free", "off", "%"}
	hasOfferKeyword := false
	for _, keyword := range offerKeywords {
		if strings.Contains(lower, keyword) {
			hasOfferKeyword = true
			break
		}
	}
	if hasOfferKeyword && (strings.Contains(lower, "customer") || strings.Contains(lower, "tell") || strings.Contains(lower, "say") || strings.Contains(lower, "offer") || strings.Contains(lower, "deal")) {
		intent.Action = "add_offer"
		intent.Confidence = 0.75
		// Extract offer text from message
		// Try to extract the actual offer description
		intent.Parameters["offer_text"] = text
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
