// Package workflows provides professional, UX-friendly workflow management for:
// - Post creation and scheduling (Telegram bot)
// - Customer-tenant conversations across social platforms
// - Escalation management from customer to tenant
// - Dashboard workflows for managing conversations and escalations
//
// All workflows are orchestrated through WorkflowOrchestrator which provides
// a unified interface for all workflow operations.
package workflows

import (
	"context"
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// PostingWorkflow orchestrates the entire post creation and scheduling flow
type PostingWorkflow struct {
	DB         *gorm.DB
	Logger     *zap.Logger
	Bot        *tgbotapi.BotAPI
	BotManager interface{} // TenantBotManager for getting bot instances
	AIClient   interface{} // AIClient for generating captions
}

// getBot gets bot instance for a tenant (from BotManager or direct Bot field)
func (w *PostingWorkflow) getBot(tenantID uint) *tgbotapi.BotAPI {
	if w.Bot != nil {
		return w.Bot
	}
	if w.BotManager != nil {
		if manager, ok := w.BotManager.(interface {
			GetBot(id uint) (*tgbotapi.BotAPI, bool)
		}); ok {
			if bot, ok := manager.GetBot(tenantID); ok {
				return bot
			}
		}
	}
	return nil
}

// PostingFlowState tracks the state of an active posting workflow
type PostingFlowState struct {
	TenantID     uint
	ChatID       int64
	Step         PostingStep
	MediaAssetID uint
	MediaType    string
	Name         string
	Caption      string
	Platforms    []string
	ScheduledAt  *time.Time
	CreatedAt    time.Time
	LastUpdated  time.Time
	Context      map[string]interface{} // For storing additional context
}

type PostingStep string

const (
	PostingStepInit          PostingStep = "init"           // Media uploaded, awaiting confirmation
	PostingStepMediaType     PostingStep = "media_type"     // Confirm carousel or single
	PostingStepName          PostingStep = "name"           // Optional internal name
	PostingStepCaption       PostingStep = "caption"        // Caption creation/editing
	PostingStepPlatforms     PostingStep = "platforms"      // Platform selection
	PostingStepSchedule      PostingStep = "schedule"       // Scheduling
	PostingStepCaptionReview PostingStep = "caption_review" // ALWAYS show caption to owner before posting
	PostingStepReview        PostingStep = "review"         // Final review before posting
	PostingStepComplete      PostingStep = "complete"       // Post scheduled
	PostingStepCancelled     PostingStep = "cancelled"      // User cancelled
)

// StartPostingFlow begins a new posting workflow when media is uploaded
func (w *PostingWorkflow) StartPostingFlow(ctx context.Context, tenantID uint, chatID int64, mediaAssetID uint) error {
	// Load media asset
	var asset storage.MediaAsset
	if err := w.DB.First(&asset, mediaAssetID).Error; err != nil {
		return fmt.Errorf("media asset not found: %w", err)
	}

	// Verify ownership
	if asset.TenantID != tenantID {
		return fmt.Errorf("unauthorized: media asset belongs to different tenant")
	}

	// Initialize workflow state
	state := &PostingFlowState{
		TenantID:     tenantID,
		ChatID:       chatID,
		Step:         PostingStepInit,
		MediaAssetID: mediaAssetID,
		MediaType:    asset.Type,
		CreatedAt:    time.Now(),
		LastUpdated:  time.Now(),
		Context:      make(map[string]interface{}),
	}

	// Save state (in production, this would be in Redis or DB)
	if err := w.saveState(ctx, state); err != nil {
		return fmt.Errorf("failed to save workflow state: %w", err)
	}

	// Send initial prompt
	return w.sendInitPrompt(ctx, state, &asset)
}

// sendInitPrompt sends the initial message asking if user wants to create a post
func (w *PostingWorkflow) sendInitPrompt(ctx context.Context, state *PostingFlowState, asset *storage.MediaAsset) error {
	var prompt string
	if asset.Type == "carousel" {
		prompt = fmt.Sprintf("✅ *Upload Successful*\n\n📸 You've uploaded a carousel with multiple images.\n\nWould you like to create a post from this media?")
	} else {
		prompt = fmt.Sprintf("✅ *Upload Successful*\n\n📸 You've uploaded a %s.\n\nWould you like to create a post from this media?", strings.Title(asset.Type))
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Yes, create post", "posting:confirm:init"),
			tgbotapi.NewInlineKeyboardButtonData("💾 Save for later", "posting:save:later"),
			tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "posting:cancel"),
		),
	)

	msg := tgbotapi.NewMessage(state.ChatID, prompt)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard

	bot := w.getBot(state.TenantID)
	if bot == nil {
		return fmt.Errorf("bot instance not found for tenant %d", state.TenantID)
	}

	if _, err := bot.Send(msg); err != nil {
		return fmt.Errorf("failed to send init prompt: %w", err)
	}

	return nil
}

// HandleUserInput processes user input for the current step
func (w *PostingWorkflow) HandleUserInput(ctx context.Context, tenantID uint, chatID int64, input string) error {
	state, err := w.loadState(ctx, chatID)
	if err != nil {
		return fmt.Errorf("workflow state not found: %w", err)
	}

	if state.TenantID != tenantID {
		return fmt.Errorf("unauthorized: workflow belongs to different tenant")
	}

	// Normalize input
	input = strings.TrimSpace(input)
	inputLower := strings.ToLower(input)

	// Handle cancellation
	if inputLower == "cancel" || inputLower == "stop" || inputLower == "abort" {
		return w.CancelWorkflow(ctx, state, "User cancelled")
	}

	// Route based on current step
	switch state.Step {
	case PostingStepInit:
		return w.handleInitStep(ctx, state, inputLower)
	case PostingStepMediaType:
		return w.handleMediaTypeStep(ctx, state, inputLower)
	case PostingStepName:
		return w.handleNameStep(ctx, state, input)
	case PostingStepCaption:
		return w.handleCaptionStep(ctx, state, input)
	case PostingStepPlatforms:
		return w.handlePlatformsStep(ctx, state, input)
	case PostingStepSchedule:
		return w.handleScheduleStep(ctx, state, input)
	case PostingStepCaptionReview:
		return w.handleCaptionReviewStep(ctx, state, inputLower)
	case PostingStepReview:
		return w.handleReviewStep(ctx, state, inputLower)
	default:
		return fmt.Errorf("invalid workflow step: %s", state.Step)
	}
}

// handleMediaTypeStep processes carousel confirmation
func (w *PostingWorkflow) handleMediaTypeStep(ctx context.Context, state *PostingFlowState, input string) error {
	inputLower := strings.ToLower(input)
	if inputLower == "yes" || strings.Contains(inputLower, "keep") || strings.Contains(inputLower, "together") || strings.Contains(inputLower, "carousel") {
		// Keep as carousel
		state.Step = PostingStepName
		state.LastUpdated = time.Now()
		if err := w.saveState(ctx, state); err != nil {
			return err
		}
		return w.sendNamePrompt(ctx, state)
	} else if inputLower == "no" || strings.Contains(inputLower, "split") {
		return w.sendInvalidInputMessage(ctx, state, "Splitting carousels into separate posts is not yet supported. Please keep as one carousel or cancel.")
	} else {
		return w.sendInvalidInputMessage(ctx, state, "Please choose: keep as carousel (yes) or cancel")
	}
}

// handleInitStep processes input for the initialization step
func (w *PostingWorkflow) handleInitStep(ctx context.Context, state *PostingFlowState, input string) error {
	if input == "yes" || input == "confirm" || strings.Contains(input, "create") {
		// Move to next step based on media type
		if state.MediaType == "carousel" {
			state.Step = PostingStepMediaType
			return w.sendMediaTypePrompt(ctx, state)
		} else {
			state.Step = PostingStepName
			return w.sendNamePrompt(ctx, state)
		}
	} else if input == "save" || strings.Contains(input, "later") {
		return w.saveForLater(ctx, state)
	} else {
		return w.sendInvalidInputMessage(ctx, state, "Please choose: create post, save for later, or cancel")
	}
}

// sendMediaTypePrompt asks if user wants to keep carousel together or split
func (w *PostingWorkflow) sendMediaTypePrompt(ctx context.Context, state *PostingFlowState) error {
	prompt := "📸 *Carousel Detected*\n\nWould you like to keep all images together as a single carousel post, or split them into separate posts?"

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Keep as carousel", "posting:carousel:yes"),
			tgbotapi.NewInlineKeyboardButtonData("✂️ Split into separate posts", "posting:carousel:no"),
		),
	)

	msg := tgbotapi.NewMessage(state.ChatID, prompt)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard

	state.LastUpdated = time.Now()
	if err := w.saveState(ctx, state); err != nil {
		return err
	}

	bot := w.getBot(state.TenantID)
	if bot == nil {
		return fmt.Errorf("bot not found for tenant %d", state.TenantID)
	}
	_, err := bot.Send(msg)
	return err
}

// sendNamePrompt asks for optional internal name
func (w *PostingWorkflow) sendNamePrompt(ctx context.Context, state *PostingFlowState) error {
	prompt := "📝 *Optional: Internal Name*\n\nGive this post an internal name to help you organize later (e.g., 'Summer Sale Product A').\n\nYou can also type 'skip' to continue without a name."

	msg := tgbotapi.NewMessage(state.ChatID, prompt)
	msg.ParseMode = "Markdown"

	state.LastUpdated = time.Now()
	state.Step = PostingStepName
	if err := w.saveState(ctx, state); err != nil {
		return err
	}

	bot := w.getBot(state.TenantID)
	if bot == nil {
		return fmt.Errorf("bot not found for tenant %d", state.TenantID)
	}
	_, err := bot.Send(msg)
	return err
}

// handleNameStep processes name input
func (w *PostingWorkflow) handleNameStep(ctx context.Context, state *PostingFlowState, input string) error {
	inputLower := strings.ToLower(input)
	if inputLower == "skip" || inputLower == "none" {
		state.Name = ""
	} else {
		state.Name = strings.TrimSpace(input)
	}

	// Move to caption step
	state.Step = PostingStepCaption
	state.LastUpdated = time.Now()
	if err := w.saveState(ctx, state); err != nil {
		return err
	}

	return w.sendCaptionPrompt(ctx, state)
}

// sendCaptionPrompt asks for caption
func (w *PostingWorkflow) sendCaptionPrompt(ctx context.Context, state *PostingFlowState) error {
	prompt := "✍️ *Write Your Caption*\n\nType the caption for your post. You can:\n• Write it yourself\n• Type 'ai' for AI-generated caption\n• Type 'skip' to post without caption"

	msg := tgbotapi.NewMessage(state.ChatID, prompt)
	msg.ParseMode = "Markdown"

	bot := w.getBot(state.TenantID)
	if bot == nil {
		return fmt.Errorf("bot not found for tenant %d", state.TenantID)
	}
	_, err := bot.Send(msg)
	return err
}

// handleCaptionStep processes caption input
func (w *PostingWorkflow) handleCaptionStep(ctx context.Context, state *PostingFlowState, input string) error {
	inputLower := strings.ToLower(input)
	if inputLower == "skip" || inputLower == "none" {
		state.Caption = ""
	} else if inputLower == "ai" || strings.Contains(inputLower, "generate") {
		// AI caption will be generated later during review step
		// For now, set empty and mark for AI generation
		state.Caption = ""
		state.Context["use_ai_caption"] = true
	} else {
		state.Caption = strings.TrimSpace(input)
	}

	// Move to platform selection
	state.Step = PostingStepPlatforms
	state.LastUpdated = time.Now()
	if err := w.saveState(ctx, state); err != nil {
		return err
	}

	return w.sendPlatformPrompt(ctx, state)
}

// sendPlatformPrompt shows platform selection
func (w *PostingWorkflow) sendPlatformPrompt(ctx context.Context, state *PostingFlowState) error {
	// Check connected platforms
	var accounts []storage.SocialAccount
	if err := w.DB.Where("tenant_id = ? AND connected = ?", state.TenantID, true).Find(&accounts).Error; err != nil {
		w.Logger.Warn("failed to load social accounts", zap.Error(err))
	}

	connectedPlatforms := make(map[string]bool)
	for _, acc := range accounts {
		connectedPlatforms[acc.Platform] = true
	}

	var prompt strings.Builder
	prompt.WriteString("📱 *Select Platforms*\n\n")
	prompt.WriteString("Choose which platforms to publish to:\n\n")

	keyboardRows := [][]tgbotapi.InlineKeyboardButton{}
	allPlatforms := []string{"instagram", "facebook", "whatsapp", "tiktok", "youtube"}

	for _, platform := range allPlatforms {
		status := "❌"
		if connectedPlatforms[platform] {
			status = "✅"
		}
		selected := "⬜"
		for _, p := range state.Platforms {
			if p == platform {
				selected = "☑️"
				break
			}
		}

		buttonText := fmt.Sprintf("%s %s %s", selected, status, strings.Title(platform))
		buttonData := fmt.Sprintf("posting:platform:toggle:%s", platform)
		keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(buttonText, buttonData),
		})
	}

	// Add confirm button (only if at least one platform selected)
	if len(state.Platforms) > 0 {
		keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("✅ Confirm Selection", "posting:platforms:confirm"),
		})
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(keyboardRows...)

	msg := tgbotapi.NewMessage(state.ChatID, prompt.String())
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard

	bot := w.getBot(state.TenantID)
	if bot == nil {
		return fmt.Errorf("bot not found for tenant %d", state.TenantID)
	}
	_, err := bot.Send(msg)
	return err
}

// handlePlatformsStep processes platform selection
func (w *PostingWorkflow) handlePlatformsStep(ctx context.Context, state *PostingFlowState, input string) error {
	// Platform selection is usually done via inline buttons, but can handle text input too
	parts := strings.Fields(strings.ToLower(input))

	// Clear existing selection if user says "clear" or "reset"
	if len(parts) > 0 && (parts[0] == "clear" || parts[0] == "reset") {
		state.Platforms = []string{}
		state.LastUpdated = time.Now()
		if err := w.saveState(ctx, state); err != nil {
			return err
		}
		return w.sendPlatformPrompt(ctx, state)
	}

	// Parse platform names from input
	platformMap := map[string]string{
		"ig": "instagram", "instagram": "instagram",
		"fb": "facebook", "facebook": "facebook",
		"wa": "whatsapp", "whatsapp": "whatsapp",
		"tt": "tiktok", "tiktok": "tiktok",
		"yt": "youtube", "youtube": "youtube",
	}

	var selectedPlatforms []string
	for _, part := range parts {
		if platform, ok := platformMap[part]; ok {
			// Check if already selected
			found := false
			for _, p := range selectedPlatforms {
				if p == platform {
					found = true
					break
				}
			}
			if !found {
				selectedPlatforms = append(selectedPlatforms, platform)
			}
		}
	}

	if len(selectedPlatforms) == 0 {
		return w.sendInvalidInputMessage(ctx, state, "Please select at least one platform, or type 'cancel' to abort")
	}

	state.Platforms = selectedPlatforms
	state.Step = PostingStepSchedule
	state.LastUpdated = time.Now()
	if err := w.saveState(ctx, state); err != nil {
		return err
	}

	return w.sendSchedulePrompt(ctx, state)
}

// TogglePlatform toggles a platform in the current selection
func (w *PostingWorkflow) TogglePlatform(ctx context.Context, tenantID uint, chatID int64, platform string) error {
	state, err := w.loadState(ctx, chatID)
	if err != nil {
		return fmt.Errorf("workflow state not found: %w", err)
	}

	if state.TenantID != tenantID {
		return fmt.Errorf("unauthorized: workflow belongs to different tenant")
	}

	if state.Step != PostingStepPlatforms {
		return fmt.Errorf("not in platform selection step")
	}

	// Check if platform is already selected
	found := false
	newPlatforms := []string{}
	for _, p := range state.Platforms {
		if p == platform {
			found = true
			// Don't add it - toggle off
		} else {
			newPlatforms = append(newPlatforms, p)
		}
	}

	if !found {
		// Toggle on - check if platform is connected
		var account storage.SocialAccount
		if err := w.DB.Where("tenant_id = ? AND platform = ? AND connected = ?", tenantID, platform, true).First(&account).Error; err != nil {
			bot := w.getBot(tenantID)
			if bot != nil {
				bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("⚠️ %s is not connected. Please connect it first.", strings.Title(platform))))
			}
			return fmt.Errorf("platform %s not connected", platform)
		}
		newPlatforms = append(newPlatforms, platform)
	}

	state.Platforms = newPlatforms
	state.LastUpdated = time.Now()
	if err := w.saveState(ctx, state); err != nil {
		return err
	}

	// Resend platform prompt with updated selection
	return w.sendPlatformPrompt(ctx, state)
}

// ConfirmPlatforms confirms platform selection and moves to next step
func (w *PostingWorkflow) ConfirmPlatforms(ctx context.Context, tenantID uint, chatID int64) error {
	state, err := w.loadState(ctx, chatID)
	if err != nil {
		return fmt.Errorf("workflow state not found: %w", err)
	}

	if state.TenantID != tenantID {
		return fmt.Errorf("unauthorized: workflow belongs to different tenant")
	}

	if state.Step != PostingStepPlatforms {
		return fmt.Errorf("not in platform selection step")
	}

	if len(state.Platforms) == 0 {
		return w.sendInvalidInputMessage(ctx, state, "Please select at least one platform, or type 'cancel' to abort")
	}

	// Move to schedule step
	state.Step = PostingStepSchedule
	state.LastUpdated = time.Now()
	if err := w.saveState(ctx, state); err != nil {
		return err
	}

	return w.sendSchedulePrompt(ctx, state)
}

// sendSchedulePrompt asks for scheduling
func (w *PostingWorkflow) sendSchedulePrompt(ctx context.Context, state *PostingFlowState) error {
	prompt := "⏰ *When to Publish?*\n\nWhen would you like to publish this post?\n\n" +
		"You can type:\n" +
		"• 'now' or 'immediately' - Publish right away\n" +
		"• 'tomorrow at 9am' - Schedule for specific time\n" +
		"• 'next monday at 10am' - Schedule for future date\n" +
		"• Natural language like 'in 2 hours' or 'on Friday'\n\n" +
		"Or use the quick buttons below:"

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⏰ Publish Now", "posting:schedule:now"),
			tgbotapi.NewInlineKeyboardButtonData("📅 Schedule Later", "posting:schedule:later"),
		),
	)

	msg := tgbotapi.NewMessage(state.ChatID, prompt)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard

	bot := w.getBot(state.TenantID)
	if bot == nil {
		return fmt.Errorf("bot not found for tenant %d", state.TenantID)
	}
	_, err := bot.Send(msg)
	return err
}

// handleScheduleStep processes scheduling input
func (w *PostingWorkflow) handleScheduleStep(ctx context.Context, state *PostingFlowState, input string) error {
	inputLower := strings.ToLower(input)

	// Parse scheduling
	var scheduledTime *time.Time
	if inputLower == "now" || inputLower == "immediately" || strings.Contains(inputLower, "right now") {
		now := time.Now()
		scheduledTime = &now
	} else {
		// Parse natural language scheduling (would use NLP service here)
		// For now, default to now
		now := time.Now()
		scheduledTime = &now
	}

	state.ScheduledAt = scheduledTime
	
	// Generate AI caption if requested before showing review
	if val, ok := state.Context["use_ai_caption"].(bool); ok && val && state.Caption == "" {
		if err := w.generateAICaption(ctx, state); err != nil {
			w.Logger.Error("failed to generate AI caption", zap.Error(err))
			// Continue without caption - user can add manually
			bot := w.getBot(state.TenantID)
			if bot != nil {
				bot.Send(tgbotapi.NewMessage(state.ChatID, "⚠️ AI caption generation failed. You can add a caption manually in the review step."))
			}
		}
	}
	
	state.Step = PostingStepCaptionReview
	state.LastUpdated = time.Now()
	if err := w.saveState(ctx, state); err != nil {
		return err
	}

	return w.sendCaptionReviewPrompt(ctx, state)
}

// generateAICaption generates an AI caption for the post
func (w *PostingWorkflow) generateAICaption(ctx context.Context, state *PostingFlowState) error {
	// Create caption optimizer
	if w.AIClient == nil {
		return fmt.Errorf("AI client not available")
	}
	
	var aiClient *ai.AIClient
	switch client := w.AIClient.(type) {
	case *ai.AIClient:
		aiClient = client
	default:
		return fmt.Errorf("invalid AI client type")
	}
	
	optimizer := &ai.CaptionOptimizer{
		Client: aiClient,
		DB:     w.DB,
	}
	
	// Load media asset to determine media type
	var asset storage.MediaAsset
	if err := w.DB.First(&asset, state.MediaAssetID).Error; err != nil {
		return fmt.Errorf("failed to load media asset: %w", err)
	}
	
	// Use first platform or default to instagram for caption generation
	platform := "instagram"
	if len(state.Platforms) > 0 {
		platform = state.Platforms[0]
	}
	
	// Generate caption with empty original (full AI generation)
	req := ai.OptimizeCaptionRequest{
		OriginalCaption: "", // Empty means full AI generation
		Platform:        platform,
		MediaType:       asset.Type,
		TenantID:        state.TenantID,
	}
	
	result, err := optimizer.OptimizeCaption(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to optimize caption: %w", err)
	}
	
	// Store generated caption
	state.Caption = result.OptimizedCaption
	state.Context["ai_generated"] = true
	state.Context["caption_hashtags"] = result.Hashtags
	
	w.Logger.Info("AI caption generated",
		zap.Uint("tenant_id", state.TenantID),
		zap.String("platform", platform),
		zap.String("media_type", asset.Type))
	
	return nil
}

// sendReviewPrompt shows final review
func (w *PostingWorkflow) sendReviewPrompt(ctx context.Context, state *PostingFlowState) error {
	// Load media asset for preview
	var asset storage.MediaAsset
	if err := w.DB.First(&asset, state.MediaAssetID).Error; err != nil {
		return err
	}

	var review strings.Builder
	review.WriteString("📋 *Review Your Post*\n\n")
	review.WriteString("Please review everything before publishing:\n\n")

	if state.Name != "" {
		review.WriteString(fmt.Sprintf("📝 Name: %s\n", state.Name))
	}

	review.WriteString(fmt.Sprintf("📸 Media: %s\n", strings.Title(state.MediaType)))

	// Caption already reviewed in previous step, but show it again in summary
	if state.Caption != "" {
		review.WriteString(fmt.Sprintf("✍️ Caption: %s\n", state.Caption))
	} else {
		review.WriteString("✍️ Caption: (none)\n")
	}

	review.WriteString(fmt.Sprintf("📱 Platforms: %s\n", strings.Join(state.Platforms, ", ")))

	if state.ScheduledAt != nil {
		review.WriteString(fmt.Sprintf("⏰ Schedule: %s\n", state.ScheduledAt.Format("Jan 2, 2006 at 3:04 PM")))
	}

	review.WriteString("\nReady to publish?")

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Publish", "posting:review:confirm"),
			tgbotapi.NewInlineKeyboardButtonData("✏️ Edit Caption", "posting:review:edit:caption"),
			tgbotapi.NewInlineKeyboardButtonData("🔄 Change Platforms", "posting:review:edit:platforms"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "posting:cancel"),
		),
	)

	msg := tgbotapi.NewMessage(state.ChatID, review.String())
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard

	bot := w.getBot(state.TenantID)
	if bot == nil {
		return fmt.Errorf("bot not found for tenant %d", state.TenantID)
	}

	_, err := bot.Send(msg)
	return err
}

// sendCaptionReviewPrompt ALWAYS shows the FULL caption to owner before posting
func (w *PostingWorkflow) sendCaptionReviewPrompt(ctx context.Context, state *PostingFlowState) error {
	var captionDisplay strings.Builder
	captionDisplay.WriteString("📝 *CAPTION REVIEW - REQUIRED*\n\n")
	captionDisplay.WriteString("Please review your caption before we proceed:\n\n")
	captionDisplay.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	// Check if caption is empty or just a placeholder
	captionText := strings.TrimSpace(state.Caption)
	aiGenerated := false
	if val, ok := state.Context["ai_generated"].(bool); ok {
		aiGenerated = val
	}
	
	if captionText == "" || captionText == "[AI Caption - To be generated]" || strings.HasPrefix(captionText, "[AI Caption") {
		captionDisplay.WriteString("⚠️ *No Caption*\n\n")
		captionDisplay.WriteString("This post will be published WITHOUT a caption.\n\n")
		captionDisplay.WriteString("If you want to add a caption, click '✏️ Edit Caption' below.\n\n")
	} else {
		// Show FULL caption - no truncation, indicate if AI-generated
		if aiGenerated {
			captionDisplay.WriteString("✨ *AI-Generated Caption:*\n\n")
		} else {
			captionDisplay.WriteString("*Your Caption:*\n\n")
		}
		captionDisplay.WriteString(fmt.Sprintf("%s\n\n", state.Caption))
	}

	captionDisplay.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━\n\n")
	captionDisplay.WriteString("Please confirm this is correct:")

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Caption looks good", "posting:caption:confirm"),
			tgbotapi.NewInlineKeyboardButtonData("✏️ Edit Caption", "posting:caption:edit"),
		),
	)

	msg := tgbotapi.NewMessage(state.ChatID, captionDisplay.String())
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard

	bot := w.getBot(state.TenantID)
	if bot == nil {
		return fmt.Errorf("bot not found for tenant %d", state.TenantID)
	}
	_, err := bot.Send(msg)
	return err
}

// handleCaptionReviewStep processes caption review confirmation
func (w *PostingWorkflow) handleCaptionReviewStep(ctx context.Context, state *PostingFlowState, input string) error {
	if input == "confirm" || strings.Contains(input, "good") || strings.Contains(input, "looks") {
		// Caption confirmed, move to final review
		state.Step = PostingStepReview
		state.LastUpdated = time.Now()
		if err := w.saveState(ctx, state); err != nil {
			return err
		}
		return w.sendReviewPrompt(ctx, state)
	} else if strings.Contains(input, "edit") {
		// Go back to caption editing
		state.Step = PostingStepCaption
		state.LastUpdated = time.Now()
		if err := w.saveState(ctx, state); err != nil {
			return err
		}
		return w.sendCaptionPrompt(ctx, state)
	} else {
		return w.sendInvalidInputMessage(ctx, state, "Please confirm the caption or choose to edit it")
	}
}

// handleReviewStep processes review confirmation
func (w *PostingWorkflow) handleReviewStep(ctx context.Context, state *PostingFlowState, input string) error {
	inputLower := strings.ToLower(input)

	if inputLower == "confirm" || inputLower == "publish" || inputLower == "yes" {
		return w.completeWorkflow(ctx, state)
	} else if strings.Contains(inputLower, "edit") {
		// Handle edit requests (would navigate back to appropriate step)
		return w.sendInvalidInputMessage(ctx, state, "Use the edit buttons above to modify specific parts")
	} else {
		return w.sendInvalidInputMessage(ctx, state, "Please confirm to publish or use the buttons to edit")
	}
}

// completeWorkflow finalizes the post and creates scheduled post
func (w *PostingWorkflow) completeWorkflow(ctx context.Context, state *PostingFlowState) error {
	// Create scheduled post
	platformsJSON := fmt.Sprintf(`["%s"]`, strings.Join(state.Platforms, `","`))

	post := storage.ScheduledPost{
		TenantID:      state.TenantID,
		Name:          state.Name,
		Caption:       state.Caption,
		MediaAssetIDs: fmt.Sprintf("[%d]", state.MediaAssetID),
		Platforms:     platformsJSON,
		ScheduledAt:   state.ScheduledAt,
		Status:        "scheduled",
	}

	if err := w.DB.Create(&post).Error; err != nil {
		return fmt.Errorf("failed to create scheduled post: %w", err)
	}

	// Update state
	state.Step = PostingStepComplete
	state.LastUpdated = time.Now()
	if err := w.saveState(ctx, state); err != nil {
		w.Logger.Warn("failed to update workflow state after completion", zap.Error(err))
	}

	// Send confirmation
	confirmMsg := fmt.Sprintf("✅ *Post Scheduled!*\n\nYour post has been scheduled and will be published at the selected time.\n\nPost ID: %d", post.ID)

	msg := tgbotapi.NewMessage(state.ChatID, confirmMsg)
	msg.ParseMode = "Markdown"

	bot := w.getBot(state.TenantID)
	if bot == nil {
		return fmt.Errorf("bot not found for tenant %d", state.TenantID)
	}
	_, err := bot.Send(msg)
	return err
}

// CancelWorkflow cancels the workflow
func (w *PostingWorkflow) CancelWorkflow(ctx context.Context, state *PostingFlowState, reason string) error {
	state.Step = PostingStepCancelled
	state.LastUpdated = time.Now()

	// Clean up state
	if err := w.deleteState(ctx, state.ChatID); err != nil {
		w.Logger.Warn("failed to delete workflow state", zap.Error(err))
	}

	msg := tgbotapi.NewMessage(state.ChatID, "❌ Post creation cancelled. Your media has been saved for later use.")
	bot := w.getBot(state.TenantID)
	if bot == nil {
		return fmt.Errorf("bot not found for tenant %d", state.TenantID)
	}
	_, err := bot.Send(msg)
	return err
}

// saveForLater saves media without creating post
func (w *PostingWorkflow) saveForLater(ctx context.Context, state *PostingFlowState) error {
	if err := w.deleteState(ctx, state.ChatID); err != nil {
		w.Logger.Warn("failed to delete workflow state", zap.Error(err))
	}

	msg := tgbotapi.NewMessage(state.ChatID, "💾 Media saved! You can create a post from it anytime by uploading media again or accessing your media library.")
	bot := w.getBot(state.TenantID)
	if bot == nil {
		return fmt.Errorf("bot not found for tenant %d", state.TenantID)
	}
	_, err := bot.Send(msg)
	return err
}

// Helper functions for state management (simplified - would use Redis in production)
var workflowStates = make(map[int64]*PostingFlowState)

func (w *PostingWorkflow) saveState(ctx context.Context, state *PostingFlowState) error {
	workflowStates[state.ChatID] = state
	return nil
}

func (w *PostingWorkflow) loadState(ctx context.Context, chatID int64) (*PostingFlowState, error) {
	state, ok := workflowStates[chatID]
	if !ok {
		return nil, fmt.Errorf("workflow state not found")
	}
	return state, nil
}

func (w *PostingWorkflow) deleteState(ctx context.Context, chatID int64) error {
	delete(workflowStates, chatID)
	return nil
}

func (w *PostingWorkflow) sendInvalidInputMessage(ctx context.Context, state *PostingFlowState, message string) error {
	msg := tgbotapi.NewMessage(state.ChatID, fmt.Sprintf("⚠️ %s", message))
	bot := w.getBot(state.TenantID)
	if bot == nil {
		return fmt.Errorf("bot not found for tenant %d", state.TenantID)
	}
	_, err := bot.Send(msg)
	return err
}
