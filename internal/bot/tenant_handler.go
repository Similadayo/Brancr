package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/social"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type TenantWebhookHandler struct {
	Manager *TenantBotManager
}

func NewTenantWebhookHandler(m *TenantBotManager) *TenantWebhookHandler {
	return &TenantWebhookHandler{Manager: m}
}

func (h *TenantWebhookHandler) ServeHTTPWithID(w http.ResponseWriter, r *http.Request, idStr string) {
	defer func() {
		if rec := recover(); rec != nil {
			h.Manager.Logger.Error("panic recovered in webhook handler",
				zap.Any("recover", rec),
				zap.String("tenant_id_str", idStr),
				zap.String("stack", string(debug.Stack())))
		}
	}()

	defer r.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(w, "{}") // immediate OK for Telegram

	tenantID, err := strconv.Atoi(idStr)
	if err != nil {
		h.Manager.Logger.Error("invalid tenant id", zap.String("idStr", idStr), zap.Error(err))
		return
	}

	bot, ok := h.Manager.GetBot(uint(tenantID))
	if !ok || bot == nil {
		h.Manager.Logger.Warn("bot instance not found for tenant", zap.Int("tenant", tenantID))
		return
	}

	var update tgbotapi.Update
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		h.Manager.Logger.Error("invalid telegram update", zap.Error(err))
		return
	}
	if update.CallbackQuery != nil {
		go h.handleCallbackQuery(bot, uint(tenantID), update.CallbackQuery)
		return
	}

	if update.Message == nil {
		return
	}

	sender := update.Message.From
	text := strings.TrimSpace(update.Message.Text)
	textLower := strings.ToLower(text)
	chatID := update.Message.Chat.ID
	senderID := sender.ID
	ctx := context.Background()

	// 🧩 Step 1: Determine ownership FIRST (before tracking as customer)
	ownerID := h.Manager.GetOwnerID(uint(tenantID))
	isOwner := senderID == ownerID

	// Check if this is media (photo/video) - don't return early for media
	hasMedia := len(update.Message.Photo) > 0 || update.Message.Video != nil

	// Only return if no text AND no media
	if text == "" && !hasMedia {
		return
	}

	if isOwner {
		h.Manager.Logger.Info("👔 Owner detected - tenant conversation mode",
			zap.Uint("tenant_id", uint(tenantID)),
			zap.Int64("owner_id", senderID))
	}

	// 🧠 Step 2: Only track as customer if NOT the owner
	var customer *storage.Customer
	if !isOwner {
		h.Manager.Logger.Info("👥 Customer detected - customer service mode",
			zap.Uint("tenant_id", uint(tenantID)),
			zap.Int64("customer_id", senderID))
		customer, _ = h.Manager.TrackOrCreateCustomer(
			ctx,
			uint(tenantID),
			"telegram",
			fmt.Sprintf("%d", sender.ID),
			strings.TrimSpace(sender.FirstName+" "+sender.LastName),
			sender.UserName,
			"",
		)
	}

	// 🖼 Step 2: Media uploads (owner only)
	hasPhoto := len(update.Message.Photo) > 0
	hasVideo := update.Message.Video != nil

	h.Manager.Logger.Info("📸 Media check",
		zap.Bool("has_photo", hasPhoto),
		zap.Bool("has_video", hasVideo),
		zap.Bool("is_owner", isOwner))

	if (hasPhoto || hasVideo) && isOwner {
		h.Manager.Logger.Info("🖼️ Media detected from owner - uploading to Cloudinary",
			zap.Uint("tenant_id", uint(tenantID)))

		go func(u tgbotapi.Update) {
			defer func() {
				if rec := recover(); rec != nil {
					h.Manager.Logger.Error("panic in media goroutine",
						zap.Any("recover", rec),
						zap.Uint("tenant_id", uint(tenantID)),
						zap.String("stack", string(debug.Stack())))
				}
			}()
			h.Manager.HandleMediaUpdate(bot, uint(tenantID), u)
		}(update)
		return
	}

	// Step 3️: Handle active sessions (post flow or social connect)
	if isOwner && strings.HasPrefix(textLower, "verify ") {
		platform := strings.TrimSpace(strings.TrimPrefix(textLower, "verify"))
		if platform == "" {
			bot.Send(tgbotapi.NewMessage(chatID, "Please specify a platform to verify (e.g., 'verify facebook')."))
		} else {
			h.handleVerifyCommand(bot, uint(tenantID), chatID, platform)
		}
		return
	}

	if sess, ok := tenantSessions[chatID]; ok {
		if isOwner {
			// Check if this is social connect flow (Step 99) or post flow (Steps 1-5)
			if sess.Step == 99 {
				h.Manager.ContinueSocialConnect(bot, uint(tenantID), chatID, text)
			} else if sess.Step >= 1 && sess.Step <= 5 {
				// Post flow continuation (Steps 1-5: carousel/name, name/caption, caption, platforms, schedule)
				h.Manager.ContinuePostFlow(bot, uint(tenantID), chatID, text)
			}
		} else {
			bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Only the business owner can manage integrations."))
		}
		return
	}

	if isOwner {
		if _, ok := businessSessions[chatID]; ok {
			if h.handleBusinessSessionInput(bot, uint(tenantID), chatID, text) {
				return
			}
		}
	}

	// Step 4️: AI-Powered Intent Routing (for owners)
	if isOwner {
		// Try AI intent routing first (before hardcoded commands)
		intentRouter := NewIntentRouter(h.Manager)
		handled, err := intentRouter.Route(ctx, uint(tenantID), chatID, text, true)
		if err != nil {
			h.Manager.Logger.Warn("intent routing failed", zap.Error(err))
		}
		if handled {
			return // Intent router handled it
		}

		// Fallback to explicit commands (for backward compatibility)
		switch {
		case text == "/webhook" || strings.HasPrefix(strings.ToLower(text), "webhook info") || strings.HasPrefix(strings.ToLower(text), "webhook"):
			h.showWebhookInfo(bot, chatID, uint(tenantID))
			return
		case text == "fix instagram" || text == "update instagram id" || strings.HasPrefix(strings.ToLower(text), "fix instagram"):
			h.fixInstagramAccountID(bot, chatID, uint(tenantID))
			return
		case text == "/help" || text == "/menu" || text == "/commands" || text == "/start":
			h.sendOwnerMenu(bot, chatID, uint(tenantID))
			return

		case text == "schedule posts" || text == "scheduled posts" || text == "my posts":
			var posts []storage.ScheduledPost
			h.Manager.DB.Where("tenant_id = ? AND status IN ?", tenantID, []string{"scheduled", "posting"}).
				Order("scheduled_at ASC").
				Limit(10).
				Find(&posts)

			if len(posts) == 0 {
				bot.Send(tgbotapi.NewMessage(chatID, "No scheduled posts. Upload media and schedule them!"))
				return
			}

			response := "*📅 Your Scheduled Posts:*\n\n"
			for i, post := range posts {
				scheduledTime := "not set"
				if post.ScheduledAt != nil {
					scheduledTime = post.ScheduledAt.Format("Jan 2, 3:04PM")
				}

				var platforms []string
				json.Unmarshal([]byte(post.Platforms), &platforms)
				platformStr := strings.Join(platforms, ", ")

				response += fmt.Sprintf("%d. *%s*\n", i+1, post.Name)
				response += fmt.Sprintf("   📱 Platforms: %s\n", platformStr)
				response += fmt.Sprintf("   ⏰ Scheduled: %s\n", scheduledTime)
				response += fmt.Sprintf("   Status: %s\n\n", post.Status)
			}

			response += "Type `cancel <name>` to cancel a post."
			msg := tgbotapi.NewMessage(chatID, response)
			msg.ParseMode = "Markdown"
			bot.Send(msg)
			return

		case text == "post history" || text == "post logs" || text == "posting activity":
			var logs []storage.PostLog
			h.Manager.DB.Where("tenant_id = ?", tenantID).
				Order("created_at DESC").
				Limit(20).
				Find(&logs)

			if len(logs) == 0 {
				bot.Send(tgbotapi.NewMessage(chatID, "No posting activity yet."))
				return
			}

			response := "*📊 Recent Posting Activity:*\n\n"
			for _, log := range logs {
				statusEmoji := "⏳"
				if log.Status == "success" {
					statusEmoji = "✅"
				} else if log.Status == "failed" {
					statusEmoji = "❌"
				}

				response += fmt.Sprintf("%s *%s* - %s\n", statusEmoji, log.Platform, log.Action)
				response += fmt.Sprintf("   📅 %s\n", log.CreatedAt.Format("Jan 2, 3:04PM"))

				if log.ErrorMessage != "" && log.Status == "failed" {
					response += fmt.Sprintf("   ⚠️ Error: %s\n", log.ErrorMessage)
				}
				response += "\n"
			}

			msg := tgbotapi.NewMessage(chatID, response)
			msg.ParseMode = "Markdown"
			bot.Send(msg)
			return

		case strings.HasPrefix(strings.ToLower(text), "cancel "):
			postName := strings.TrimSpace(strings.TrimPrefix(text, "cancel "))
			postName = strings.TrimPrefix(postName, "Cancel ")

			var post storage.ScheduledPost
			if err := h.Manager.DB.Where("tenant_id = ? AND name = ? AND status = ?", tenantID, postName, "scheduled").
				First(&post).Error; err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Couldn't find scheduled post: %s", postName)))
				return
			}

			if err := h.Manager.DB.Model(&post).Update("status", "cancelled").Error; err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Unable to cancel the post. Please try again later."))
				return
			}

			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Cancelled: %s", postName)))
			return

		case textLower == "status" || textLower == "connections":
			h.sendConnectionStatus(bot, chatID, uint(tenantID))
			return

		case strings.HasPrefix(strings.ToLower(text), "connect "):
			platform := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(text), "connect"))
			go h.Manager.StartSocialConnectFlow(bot, uint(tenantID), chatID, platform)
			return

		case strings.HasPrefix(textLower, "verify "):
			platform := strings.TrimSpace(strings.TrimPrefix(textLower, "verify"))
			h.handleVerifyCommand(bot, uint(tenantID), chatID, platform)
			return
		}
	}

	// 🧩 Step 5️: Check onboarding status - if complete, use DB data; otherwise trigger onboarding
	var tenant storage.Tenant
	if err := h.Manager.DB.First(&tenant, tenantID).Error; err == nil {
		// Check if onboarding is complete (dashboard signup)
		if tenant.OnboardingComplete {
			// Onboarding already done via dashboard - load persona and business profile from DB
			persona := ai.GetPersona(h.Manager.DB, uint(tenantID))
			var profile storage.BusinessProfile
			_ = h.Manager.DB.First(&profile, "tenant_id = ?", tenantID).Error
			
			// Use existing data - no onboarding questions needed
			// If persona/business profile is missing despite OnboardingComplete flag, 
			// we'll still continue but log a warning (data should exist from dashboard)
			if persona.BotName == "" || profile.Niche == "" {
				h.Manager.Logger.Warn("OnboardingComplete=true but missing persona/business profile data",
					zap.Uint("tenant_id", uint(tenantID)),
					zap.Bool("has_persona", persona.BotName != ""),
					zap.Bool("has_profile", profile.Niche != ""))
				// Continue anyway - user can configure via dashboard or menu
			}
		} else {
			// Legacy flow: onboarding not complete - check for missing persona/business profile
			persona := ai.GetPersona(h.Manager.DB, uint(tenantID))
			var profile storage.BusinessProfile
			_ = h.Manager.DB.First(&profile, "tenant_id = ?", tenantID).Error

			personaMissing := persona.BotName == "" || persona.Tone == ""
			businessProfileMissing := profile.Niche == "" || profile.Description == "" || profile.Tone == ""

			if personaMissing {
				if isOwner {
					if _, active := ai.PersonaSessions.Load(chatID); !active {
						go ai.StartPersonaOnboarding(ctx, h.Manager.DB, bot, uint(tenantID), chatID, h.Manager.Responder.Client, h.Manager.Logger)
					} else {
						go ai.ContinuePersonaOnboarding(ctx, h.Manager.DB, bot, uint(tenantID), chatID, text, h.Manager.Responder.Client, h.Manager.Logger)
					}
				} else {
					bot.Send(tgbotapi.NewMessage(chatID, "Hold on, still setting things up."))
				}
				return
			}

			if businessProfileMissing {
				if isOwner {
					if _, active := ai.BusinessProfileSessions.Load(chatID); !active {
						go ai.StartBusinessProfileOnboarding(ctx, h.Manager.DB, bot, uint(tenantID), chatID, h.Manager.Responder.Client, h.Manager.Logger)
					} else {
						go ai.ContinueBusinessProfileOnboarding(ctx, h.Manager.DB, bot, uint(tenantID), chatID, text, h.Manager.Responder.Client, h.Manager.Logger)
					}
				} else {
					bot.Send(tgbotapi.NewMessage(chatID, "Still configuring, give me a sec."))
				}
				return
			}
		}
	}

	// 🧠 Step 6️: Context-Aware AI Assistant flow (tenant + customer aware)
	// Send typing indicator
	bot.Send(tgbotapi.NewChatAction(chatID, "typing"))

	// Use context-aware generator for better responses
	contextGen := ai.NewContextAwareGenerator(
		h.Manager.Responder.Client,
		h.Manager.DB,
		h.Manager.Memory,
	)

	var reply string
	var replyErr error
	var customerID uint

	if isOwner {
		// Owner gets business management assistance with full context
		reply, replyErr = contextGen.GenerateWithFullContext(ctx, uint(tenantID), text, true, 0)

		// Add proactive suggestions for owners (occasionally)
		if replyErr == nil && reply != "" {
			proactive := ai.NewProactiveAssistant(h.Manager.Responder.Client, h.Manager.DB, h.Manager.Memory)
			enhanced, err := proactive.EnhanceResponseWithProactiveSuggestions(ctx, uint(tenantID), reply)
			if err == nil {
				reply = enhanced
			}
		}
	} else {
		// Only customers reach this point - owner is handled above
		if customer == nil {
			bot.Send(tgbotapi.NewMessage(chatID, "Something went wrong identifying your account."))
			return
		}
		customerID = customer.ID
		// Customer gets brand representation with customer context
		reply, replyErr = contextGen.GenerateWithFullContext(ctx, uint(tenantID), text, false, customerID)
	}

	if replyErr != nil {
		h.Manager.Logger.Error("AI responder failed", zap.Error(replyErr))

		// Check for specific error types and provide appropriate responses
		var errorMsg string

		// Check for moderation error
		if ai.IsModerationError(replyErr) {
			errorMsg = "I'm having trouble processing that request right now. Could you try rephrasing your message?"
			h.Manager.Logger.Warn("AI moderation triggered",
				zap.Uint("tenant_id", uint(tenantID)),
				zap.Error(replyErr))
		} else if strings.Contains(replyErr.Error(), "timeout") || strings.Contains(replyErr.Error(), "deadline exceeded") {
			errorMsg = "Taking a bit longer than usual, try again."
		} else if strings.Contains(replyErr.Error(), "403") || strings.Contains(replyErr.Error(), "Forbidden") {
			errorMsg = "Unable to process your request at this time. Please try again in a moment."
		} else {
			errorMsg = "I'm having trouble processing that right now. Please try again in a moment."
		}

		bot.Send(tgbotapi.NewMessage(chatID, errorMsg))
		return
	}

	// Send with markdown formatting for cleaner messages
	msg := tgbotapi.NewMessage(chatID, reply)
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}

func (h *TenantWebhookHandler) handleVerifyCommand(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, platform string) {
	platform = strings.TrimSpace(strings.ToLower(platform))
	if platform == "" {
		bot.Send(tgbotapi.NewMessage(chatID, "Please specify which platform to verify (e.g., 'verify facebook')."))
		return
	}

	delete(tenantSessions, chatID)

	go func() {
		ctx := context.Background()
		var acc storage.SocialAccount
		if err := h.Manager.DB.Where("tenant_id = ? AND platform = ?", tenantID, platform).First(&acc).Error; err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("No %s account connected yet.", platform)))
			return
		}

		if acc.AccessToken != "" {
			if decrypted, err := utils.DecryptToken(acc.AccessToken); err == nil {
				acc.AccessToken = decrypted
			}
		}

		msg, err := social.VerifySocialConnection(ctx, &acc, h.Manager.DB)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("I couldn't verify your %s connection: %v", strings.Title(platform), err)))
			return
		}

		bot.Send(tgbotapi.NewMessage(chatID, msg))
	}()
}

func (h *TenantWebhookHandler) handleCallbackQuery(bot *tgbotapi.BotAPI, tenantID uint, cq *tgbotapi.CallbackQuery) {
	if cq == nil {
		return
	}

	// Handle bulk upload callbacks
	if strings.HasPrefix(cq.Data, "bulk:") {
		h.handleBulkUploadCallback(bot, tenantID, cq)
		return
	}

	// Acknowledge callback to avoid "button spinning"
	_, _ = bot.Request(tgbotapi.NewCallback(cq.ID, ""))

	data := strings.TrimSpace(cq.Data)
	if data == "" {
		return
	}

	chatID := cq.Message.Chat.ID

	parts := strings.Split(data, ":")
	action := parts[0]
	arg := ""
	if len(parts) > 1 {
		arg = strings.Join(parts[1:], ":")
	}

	switch action {
	case "menu":
		switch arg {
		case "", "main":
			h.sendOwnerMenu(bot, chatID, tenantID)
		case "connect":
			h.sendConnectionStatus(bot, chatID, tenantID)
		case "business":
			h.sendBusinessMenu(bot, chatID, tenantID)
		case "analytics":
			h.sendAnalyticsPeek(bot, chatID, tenantID)
		case "ai":
			var tenant storage.Tenant
			h.Manager.DB.First(&tenant, tenantID)
			persona := ai.GetPersona(h.Manager.DB, tenantID)
			
			// If onboarding is complete, show current persona info; otherwise start onboarding
			if tenant.OnboardingComplete && persona.BotName != "" {
				tone := fallbackValue(persona.StyleNotes, persona.Tone)
				if tone == "" {
					tone = "friendly"
				}
				msg := fmt.Sprintf(
					"🤖 *AI Persona Configuration*\n\n"+
					"Current settings:\n"+
					"• Name: *%s*\n"+
					"• Tone: *%s*\n\n"+
					"To update your AI persona, visit the dashboard:\n"+
					"📱 https://www.brancr.com/dashboard",
					persona.BotName, tone)
				out := tgbotapi.NewMessage(chatID, msg)
				out.ParseMode = "Markdown"
				bot.Send(out)
			} else if persona.BotName == "" {
				// Legacy: start onboarding if not complete
				go ai.StartPersonaOnboarding(context.Background(), h.Manager.DB, bot, tenantID, chatID, h.Manager.Responder.Client, h.Manager.Logger)
			} else {
				msg := fmt.Sprintf("I'm already answering as %s with a %s vibe. Want to tweak anything? Just tell me how you'd like me to sound.", persona.BotName, fallbackValue(persona.StyleNotes, persona.Tone))
				bot.Send(tgbotapi.NewMessage(chatID, msg))
			}
		default:
			h.Manager.Logger.Debug("unhandled menu callback", zap.String("arg", arg))
		}
	case "connect":
		if arg == "" {
			return
		}
		go h.Manager.StartSocialConnectFlow(bot, tenantID, chatID, arg)
	case "verify":
		if arg == "" {
			return
		}
		h.handleVerifyCommand(bot, tenantID, chatID, arg)
	case "status":
		h.sendConnectionStatus(bot, chatID, tenantID)
	case "help":
		h.sendHelpOverview(bot, chatID)
	case "business":
		h.handleBusinessCallback(bot, tenantID, chatID, arg)
	case "analytics":
		h.sendAnalyticsPeek(bot, chatID, tenantID)
	case "ai":
		// TODO: Implement AI customization flow
		bot.Send(tgbotapi.NewMessage(chatID, "AI customization is coming soon!"))
	case "postflow":
		h.handlePostFlowCallback(bot, tenantID, chatID, arg)
	default:
		h.Manager.Logger.Debug("unhandled callback", zap.String("data", data))
	}
}

func (h *TenantWebhookHandler) sendOwnerMenu(bot *tgbotapi.BotAPI, chatID int64, tenantID uint) {
	var tenant storage.Tenant
	var persona storage.Persona
	var businessProfile storage.BusinessProfile
	var accounts []storage.SocialAccount
	
	h.Manager.DB.First(&tenant, tenantID)
	persona = ai.GetPersona(h.Manager.DB, tenantID)
	_ = h.Manager.DB.Where("tenant_id = ?", tenantID).First(&businessProfile).Error
	h.Manager.DB.Where("tenant_id = ? AND connected = ?", tenantID, true).Find(&accounts)
	
	// Build context-aware menu text
	businessName := tenant.Name
	if businessProfile.Name != "" {
		businessName = businessProfile.Name
	}
	
	text := fmt.Sprintf("🎛️ *Brancr Control Center*\n\n*%s*", businessName)
	
	if tenant.OnboardingComplete && persona.BotName != "" {
		tone := persona.Tone
		if tone == "" {
			tone = "friendly"
		}
		text += fmt.Sprintf("\n\n🤖 AI: *%s* (%s tone)", persona.BotName, tone)
	}
	
	if len(accounts) > 0 {
		text += "\n\n📱 *Connected:* "
		platforms := make([]string, 0, len(accounts))
		for _, acc := range accounts {
			platforms = append(platforms, strings.Title(acc.Platform))
		}
		text += strings.Join(platforms, ", ")
	}
	
	text += "\n\nWhat would you like to do?"
	
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📱 Connect Social Accounts", "menu:connect"),
			tgbotapi.NewInlineKeyboardButtonData("💬 Manage Business Info", "menu:business"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📊 View Analytics", "menu:analytics"),
			tgbotapi.NewInlineKeyboardButtonData("🤖 Customize AI Assistant", "menu:ai"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🧩 Help & Tutorials", "help:menu"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func (h *TenantWebhookHandler) sendAnalyticsPeek(bot *tgbotapi.BotAPI, chatID int64, tenantID uint) {
	if h.Manager.AnalyticsHandler == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "Analytics aren’t ready just yet. Check back soon."))
		return
	}
	ctx := context.Background()
	if err := h.Manager.AnalyticsHandler.SendQuickSummary(ctx, bot, tenantID, chatID); err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "I couldn’t pull analytics right now. Try again shortly."))
	}
}

func (h *TenantWebhookHandler) sendConnectionStatus(bot *tgbotapi.BotAPI, chatID int64, tenantID uint) {
	var accounts []storage.SocialAccount
	h.Manager.DB.Where("tenant_id = ?", tenantID).Find(&accounts)

	status := map[string]string{
		"instagram": "⚪️ Not connected",
		"facebook":  "⚪️ Not connected",
		"whatsapp":  "⚪️ Not connected",
		"tiktok":    "⚪️ Not connected",
	}

	for _, acc := range accounts {
		if _, ok := status[acc.Platform]; !ok {
			continue
		}
		if acc.Connected {
			timestamp := "just now"
			if !acc.UpdatedAt.IsZero() {
				timestamp = acc.UpdatedAt.Format("Jan 2 15:04")
			}
			status[acc.Platform] = fmt.Sprintf("✅ Connected · updated %s", timestamp)
		} else {
			status[acc.Platform] = "⚠️ Needs attention"
		}
	}

	lines := []string{
		"🔐 *Account Connections*",
		fmt.Sprintf("• Instagram: %s", status["instagram"]),
		fmt.Sprintf("• Facebook: %s", status["facebook"]),
		fmt.Sprintf("• WhatsApp: %s", status["whatsapp"]),
		fmt.Sprintf("• TikTok: %s", status["tiktok"]),
		"",
		"Tap a button below to manage a specific platform.",
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Connect IG", "connect:instagram"),
			tgbotapi.NewInlineKeyboardButtonData("Verify IG", "verify:instagram"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Connect FB", "connect:facebook"),
			tgbotapi.NewInlineKeyboardButtonData("Verify FB", "verify:facebook"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Connect WA", "connect:whatsapp"),
			tgbotapi.NewInlineKeyboardButtonData("Verify WA", "verify:whatsapp"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Connect TikTok", "connect:tiktok"),
			tgbotapi.NewInlineKeyboardButtonData("Verify TikTok", "verify:tiktok"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Back to menu", "menu:main"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, strings.Join(lines, "\n"))
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func (h *TenantWebhookHandler) sendHelpOverview(bot *tgbotapi.BotAPI, chatID int64) {
	helpMsg := `💡 *Quick Help & Tips*

• Connect platforms with the menu buttons above.
• Use "Verify" after connecting to double-check access.
• Send media files here to store them for scheduling.
• Type naturally to ask Brancr anything for your business.`

	msg := tgbotapi.NewMessage(chatID, helpMsg)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Back to menu ⬅️", "menu:main"),
		),
	)
	bot.Send(msg)
}

func (h *TenantWebhookHandler) handleBusinessCallback(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, arg string) {
	switch arg {
	case "", "menu":
		h.sendBusinessMenu(bot, chatID, tenantID)
	case "industry":
		h.startIndustryUpdate(bot, chatID)
	case "description":
		h.startDescriptionUpdate(bot, chatID)
	case "hours":
		h.startHoursUpdate(bot, chatID)
	case "knowledge":
		h.startKnowledgeCapture(bot, chatID)
	case "menu_manager":
		h.sendMenuManager(bot, chatID, tenantID)
	case "menu_add":
		h.startMenuAdd(bot, chatID)
	case "menu_list":
		h.sendMenuList(bot, chatID, tenantID)
	case "faq_manager":
		h.sendFAQManager(bot, chatID, tenantID)
	case "faq_add":
		h.startFAQAdd(bot, chatID)
	case "faq_list":
		h.sendFAQList(bot, chatID, tenantID)
	default:
		h.Manager.Logger.Debug("unhandled business callback", zap.String("arg", arg))
	}
}

func (h *TenantWebhookHandler) handlePostFlowCallback(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, arg string) {
	sess, ok := tenantSessions[chatID]
	if !ok {
		return
	}

	choice := strings.ToLower(strings.TrimSpace(arg))

	switch sess.Step {
	case 0:
		if choice == "yes" {
			h.Manager.promptFirstStep(bot, chatID, sess)
			return
		}
		if choice == "skip" || choice == "no" || choice == "not now" {
			delete(tenantSessions, chatID)
			bot.Send(tgbotapi.NewMessage(chatID, "No problem—just let me know when you want to schedule it."))
			return
		}
	case 1:
		if choice == "yes" {
			sess.IsCarousel = true
			sess.Step = 2
			bot.Send(tgbotapi.NewMessage(chatID, "Perfect. Want to tag it with an internal name? (Type it or say 'skip'.)"))
			return
		}
		if choice == "no" || choice == "skip" {
			sess.IsCarousel = false
			sess.Step = 2
			bot.Send(tgbotapi.NewMessage(chatID, "Got it. Want to tag it with an internal name? (Type it or say 'skip'.)"))
			return
		}
	}

	// Fallback: prompt user to respond with text
	bot.Send(tgbotapi.NewMessage(chatID, "Go ahead and reply with your choice so we can keep going."))
}

func (h *TenantWebhookHandler) sendBusinessMenu(bot *tgbotapi.BotAPI, chatID int64, tenantID uint) {
	profile, err := h.ensureBusinessProfile(tenantID)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Unable to load your business profile right now. Please try again later."))
		return
	}

	var menuCount int64
	var faqCount int64
	h.Manager.DB.Model(&storage.BusinessMenuItem{}).Where("tenant_id = ?", tenantID).Count(&menuCount)
	h.Manager.DB.Model(&storage.BusinessFAQ{}).Where("tenant_id = ?", tenantID).Count(&faqCount)

	industry := fallbackValue(profile.Industry, "Not set")
	description := fallbackValue(profile.Description, "Not set")
	hours := fallbackValue(profile.OperatingHours, "Not set")
	knowledgePoints := "None yet"
	if profile.KnowledgeBase != "" {
		lines := strings.Split(profile.KnowledgeBase, "\n")
		if len(lines) > 3 {
			lines = lines[len(lines)-3:]
		}
		knowledgePoints = strings.Join(lines, "\n")
	}

	lines := []string{
		"🗂 *Business Profile*",
		fmt.Sprintf("• Industry: %s", industry),
		fmt.Sprintf("• Description: %s", description),
		fmt.Sprintf("• Hours: %s", hours),
		fmt.Sprintf("• Menu / Services: %d items", menuCount),
		fmt.Sprintf("• FAQs: %d articles", faqCount),
		"",
		"*Latest teachings:*",
		knowledgePoints,
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Update Industry", "business:industry"),
			tgbotapi.NewInlineKeyboardButtonData("Update Description", "business:description"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Update Hours", "business:hours"),
			tgbotapi.NewInlineKeyboardButtonData("Teach Brancr", "business:knowledge"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Manage Menu / Services", "business:menu_manager"),
			tgbotapi.NewInlineKeyboardButtonData("Manage FAQs", "business:faq_manager"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Back to menu ⬅️", "menu:main"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, strings.Join(lines, "\n"))
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func (h *TenantWebhookHandler) sendMenuManager(bot *tgbotapi.BotAPI, chatID int64, tenantID uint) {
	var count int64
	h.Manager.DB.Model(&storage.BusinessMenuItem{}).Where("tenant_id = ?", tenantID).Count(&count)

	text := fmt.Sprintf("🍽 *Menu & Services*\nYou currently have %d items. Use the options below to add new offerings or review what Brancr knows.", count)
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Add item", "business:menu_add"),
			tgbotapi.NewInlineKeyboardButtonData("📋 View items", "business:menu_list"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Back", "business:menu"),
		),
	)
	bot.Send(msg)
}

func (h *TenantWebhookHandler) sendMenuList(bot *tgbotapi.BotAPI, chatID int64, tenantID uint) {
	var items []storage.BusinessMenuItem
	h.Manager.DB.Where("tenant_id = ?", tenantID).Order("updated_at DESC").Limit(10).Find(&items)

	if len(items) == 0 {
		msg := tgbotapi.NewMessage(chatID, "You haven’t added any menu or service items yet.")
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Add item ➕", "business:menu_add"),
				tgbotapi.NewInlineKeyboardButtonData("Back ⬅️", "business:menu_manager"),
			),
		)
		bot.Send(msg)
		return
	}

	var lines []string
	lines = append(lines, "📋 *Latest Menu / Services*")
	for i, item := range items {
		price := fallbackValue(item.Price, "no price listed")
		desc := fallbackValue(item.Description, "no description")
		lines = append(lines, fmt.Sprintf("%d. *%s* — %s\n   %s", i+1, item.Name, price, desc))
	}

	msg := tgbotapi.NewMessage(chatID, strings.Join(lines, "\n\n"))
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Add another ➕", "business:menu_add"),
			tgbotapi.NewInlineKeyboardButtonData("Back ⬅️", "business:menu_manager"),
		),
	)
	bot.Send(msg)
}

func (h *TenantWebhookHandler) sendFAQManager(bot *tgbotapi.BotAPI, chatID int64, tenantID uint) {
	var count int64
	h.Manager.DB.Model(&storage.BusinessFAQ{}).Where("tenant_id = ?", tenantID).Count(&count)

	text := fmt.Sprintf("❓ *Frequently Asked Questions*\nYou’ve saved %d answers. Keep Brancr updated so it can respond perfectly.", count)
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Add FAQ", "business:faq_add"),
			tgbotapi.NewInlineKeyboardButtonData("📚 View FAQs", "business:faq_list"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Back", "business:menu"),
		),
	)
	bot.Send(msg)
}

func (h *TenantWebhookHandler) sendFAQList(bot *tgbotapi.BotAPI, chatID int64, tenantID uint) {
	var faqs []storage.BusinessFAQ
	h.Manager.DB.Where("tenant_id = ?", tenantID).Order("updated_at DESC").Limit(10).Find(&faqs)

	if len(faqs) == 0 {
		msg := tgbotapi.NewMessage(chatID, "No FAQs yet. Add questions customers usually ask so Brancr can answer for you.")
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Add FAQ ➕", "business:faq_add"),
				tgbotapi.NewInlineKeyboardButtonData("Back ⬅️", "business:faq_manager"),
			),
		)
		bot.Send(msg)
		return
	}

	lines := []string{"📚 *Saved FAQs*"}
	for i, faq := range faqs {
		lines = append(lines, fmt.Sprintf("%d. *%s*\n   %s", i+1, faq.Question, faq.Answer))
	}

	msg := tgbotapi.NewMessage(chatID, strings.Join(lines, "\n\n"))
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Add more ➕", "business:faq_add"),
			tgbotapi.NewInlineKeyboardButtonData("Back ⬅️", "business:faq_manager"),
		),
	)
	bot.Send(msg)
}

func (h *TenantWebhookHandler) startIndustryUpdate(bot *tgbotapi.BotAPI, chatID int64) {
	startBusinessSession(chatID, "industry")
	msg := tgbotapi.NewMessage(chatID, "What industry or business type best describes you? (e.g., Bakery, E-commerce store, Marketing agency). Type 'cancel' to stop.")
	bot.Send(msg)
}

func (h *TenantWebhookHandler) startDescriptionUpdate(bot *tgbotapi.BotAPI, chatID int64) {
	startBusinessSession(chatID, "description")
	msg := tgbotapi.NewMessage(chatID, "Describe your business in a few sentences so I can speak about it accurately. Type 'cancel' to stop.")
	bot.Send(msg)
}

func (h *TenantWebhookHandler) startHoursUpdate(bot *tgbotapi.BotAPI, chatID int64) {
	startBusinessSession(chatID, "hours")
	msg := tgbotapi.NewMessage(chatID, "Share your operating hours (e.g., Mon-Fri 9am-6pm, Sat 10am-4pm). Type 'clear' to remove hours or 'cancel' to stop.")
	bot.Send(msg)
}

func (h *TenantWebhookHandler) startKnowledgeCapture(bot *tgbotapi.BotAPI, chatID int64) {
	startBusinessSession(chatID, "knowledge")
	msg := tgbotapi.NewMessage(chatID, "Teach Brancr something important (e.g., delivery fees, return policy, signature products). Send as many messages as you'd like, type 'done' when finished or 'cancel' to stop.")
	bot.Send(msg)
}

func (h *TenantWebhookHandler) startMenuAdd(bot *tgbotapi.BotAPI, chatID int64) {
	sess := startBusinessSession(chatID, "menu_add")
	sess.Step = 1
	msg := tgbotapi.NewMessage(chatID, "Great! Let's add a product/service.\n\n1️⃣ What’s the name of the item? (Type 'cancel' to stop.)")
	bot.Send(msg)
}

func (h *TenantWebhookHandler) startFAQAdd(bot *tgbotapi.BotAPI, chatID int64) {
	sess := startBusinessSession(chatID, "faq_add")
	sess.Step = 1
	msg := tgbotapi.NewMessage(chatID, "Let's add an FAQ.\n\n1️⃣ Send the question customers often ask. (Type 'cancel' to stop.)")
	bot.Send(msg)
}

func (h *TenantWebhookHandler) ensureBusinessProfile(tenantID uint) (*storage.BusinessProfile, error) {
	var profile storage.BusinessProfile
	err := h.Manager.DB.Where("tenant_id = ?", tenantID).First(&profile).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		profile = storage.BusinessProfile{TenantID: tenantID}
		if createErr := h.Manager.DB.Create(&profile).Error; createErr != nil {
			return nil, createErr
		}
		return &profile, nil
	}
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

func fallbackValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (h *TenantWebhookHandler) handleBusinessSessionInput(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, text string) bool {
	sess, ok := businessSessions[chatID]
	if !ok {
		return false
	}

	trimmed := strings.TrimSpace(text)
	lower := strings.ToLower(trimmed)

	if lower == "cancel" {
		cancelBusinessSession(bot, chatID)
		return true
	}

	profile, err := h.ensureBusinessProfile(tenantID)
	if err != nil {
		clearBusinessSession(chatID)
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Couldn't load your business profile. Please try again later."))
		return true
	}

	switch sess.Mode {
	case "industry":
		if trimmed == "" {
			bot.Send(tgbotapi.NewMessage(chatID, "Please share a short industry description (e.g., Bakery, Digital agency)."))
			return true
		}
		profile.Industry = trimmed
		if err := h.Manager.DB.Save(profile).Error; err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Couldn't update your industry right now."))
			clearBusinessSession(chatID)
			return true
		}
		clearBusinessSession(chatID)
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Noted! I’ll treat you as a *%s* business.", trimmed)))
		h.sendBusinessMenu(bot, chatID, tenantID)
		return true

	case "description":
		if trimmed == "" {
			bot.Send(tgbotapi.NewMessage(chatID, "Please share a short description or type 'cancel' to stop."))
			return true
		}
		profile.Description = text
		if err := h.Manager.DB.Save(profile).Error; err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Couldn't update your description right now."))
			clearBusinessSession(chatID)
			return true
		}
		clearBusinessSession(chatID)
		bot.Send(tgbotapi.NewMessage(chatID, "✅ Description updated!"))
		h.sendBusinessMenu(bot, chatID, tenantID)
		return true

	case "hours":
		if lower == "clear" {
			profile.OperatingHours = ""
			if err := h.Manager.DB.Save(profile).Error; err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Couldn't clear hours right now."))
				clearBusinessSession(chatID)
				return true
			}
			clearBusinessSession(chatID)
			bot.Send(tgbotapi.NewMessage(chatID, "✅ Operating hours removed."))
			h.sendBusinessMenu(bot, chatID, tenantID)
			return true
		}
		if trimmed == "" {
			bot.Send(tgbotapi.NewMessage(chatID, "Please share your operating hours, or type 'clear' to remove them."))
			return true
		}
		profile.OperatingHours = text
		if err := h.Manager.DB.Save(profile).Error; err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Couldn't update hours right now."))
			clearBusinessSession(chatID)
			return true
		}
		clearBusinessSession(chatID)
		bot.Send(tgbotapi.NewMessage(chatID, "✅ Operating hours saved!"))
		h.sendBusinessMenu(bot, chatID, tenantID)
		return true

	case "knowledge":
		if lower == "done" {
			clearBusinessSession(chatID)
			bot.Send(tgbotapi.NewMessage(chatID, "👍 All set. I’m keeping those details in mind."))
			h.sendBusinessMenu(bot, chatID, tenantID)
			return true
		}
		if trimmed == "" {
			bot.Send(tgbotapi.NewMessage(chatID, "Share a teaching (e.g., delivery fees, seasonal offers) or type 'done' when finished."))
			return true
		}
		entry := trimmed
		if profile.KnowledgeBase != "" {
			profile.KnowledgeBase += "\n"
		}
		profile.KnowledgeBase += entry
		if err := h.Manager.DB.Save(profile).Error; err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Couldn't save that detail right now."))
			return true
		}
		bot.Send(tgbotapi.NewMessage(chatID, "🧠 Got it! Add more or type 'done' when finished."))
		return true

	case "menu_add":
		switch sess.Step {
		case 1:
			if trimmed == "" {
				bot.Send(tgbotapi.NewMessage(chatID, "Please provide a name for the item (or type 'cancel')."))
				return true
			}
			sess.MenuItem.Name = trimmed
			sess.Step = 2
			bot.Send(tgbotapi.NewMessage(chatID, "2️⃣ What price do you charge? You can include currency, or type 'skip'."))
			return true
		case 2:
			if lower != "skip" {
				sess.MenuItem.Price = trimmed
			}
			sess.Step = 3
			bot.Send(tgbotapi.NewMessage(chatID, "3️⃣ Add a short description or selling point (or type 'skip')."))
			return true
		case 3:
			if lower != "skip" {
				sess.MenuItem.Description = text
			}
			sess.Step = 4
			bot.Send(tgbotapi.NewMessage(chatID, "4️⃣ Any tags or categories? (e.g., vegan, premium). Type them comma separated or 'skip'."))
			return true
		case 4:
			if lower != "skip" {
				sess.MenuItem.Tags = trimmed
			}
			sess.MenuItem.TenantID = tenantID
			if err := h.Manager.DB.Create(&sess.MenuItem).Error; err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Unable to save that item right now."))
				clearBusinessSession(chatID)
				return true
			}
			itemName := fallbackValue(sess.MenuItem.Name, "Item")
			clearBusinessSession(chatID)
			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Added *%s* to your menu/services!", itemName)))
			h.sendMenuManager(bot, chatID, tenantID)
			return true
		default:
			clearBusinessSession(chatID)
			return true
		}

	case "faq_add":
		switch sess.Step {
		case 1:
			if trimmed == "" {
				bot.Send(tgbotapi.NewMessage(chatID, "Please share the full question or type 'cancel'."))
				return true
			}
			sess.FAQ.Question = text
			sess.Step = 2
			bot.Send(tgbotapi.NewMessage(chatID, "2️⃣ What's the answer I should give customers?"))
			return true
		case 2:
			if trimmed == "" {
				bot.Send(tgbotapi.NewMessage(chatID, "Please provide an answer or type 'cancel'."))
				return true
			}
			sess.FAQ.Answer = text
			sess.FAQ.TenantID = tenantID
			if err := h.Manager.DB.Create(&sess.FAQ).Error; err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Couldn't save that FAQ right now."))
				clearBusinessSession(chatID)
				return true
			}
			clearBusinessSession(chatID)
			bot.Send(tgbotapi.NewMessage(chatID, "✅ FAQ added! Brancr will use it when customers ask."))
			h.sendFAQManager(bot, chatID, tenantID)
			return true
		default:
			clearBusinessSession(chatID)
			return true
		}
	}

	// Unknown mode fallback
	clearBusinessSession(chatID)
	return true
}

func schedulePost(db *gorm.DB, tenantID uint, assetName string, when time.Time) {
	var asset storage.MediaAsset
	if err := db.Where("tenant_id = ? AND name = ?", tenantID, assetName).First(&asset).Error; err != nil {
		return
	}
	job := storage.ScheduledPost{
		TenantID:      tenantID,
		MediaAssetIDs: fmt.Sprintf("%d", asset.ID),
		Platforms:     `["instagram"]`, // default for now
		Caption:       asset.Caption,
		ScheduledAt:   &when,
		Status:        "pending",
	}
	db.Create(&job)
}

func renameLastUpload(db *gorm.DB, tenantID uint, name string) {
	var asset storage.MediaAsset
	db.Where("tenant_id = ?", tenantID).Order("id desc").First(&asset)
	asset.CarouselName = name
	db.Save(&asset)
}

// showWebhookInfo displays webhook configuration details for Meta platforms
func (h *TenantWebhookHandler) showWebhookInfo(bot *tgbotapi.BotAPI, chatID int64, tenantID uint) {
	ctx := context.Background()
	_ = ctx // TODO: Use context if needed

	// Get public URL
	publicURL, err := utils.GetPublicURL()
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Could not determine public URL. Set BRANCR_PUBLIC_URL environment variable."))
		return
	}

	// Get verify token from environment
	verifyToken := os.Getenv("META_WEBHOOK_VERIFY_TOKEN")
	if verifyToken == "" {
		verifyToken = os.Getenv("WEBHOOK_VERIFY_TOKEN")
	}
	if verifyToken == "" {
		verifyToken = "⚠️ Not set - set META_WEBHOOK_VERIFY_TOKEN environment variable"
	}

	// Check connected Meta platforms
	var accounts []storage.SocialAccount
	h.Manager.DB.Where("tenant_id = ? AND platform IN (?)", tenantID, []string{"instagram", "facebook"}).Find(&accounts)

	var info []string
	info = append(info, "📋 **Webhook Configuration for Meta Platforms**\n")

	if len(accounts) == 0 {
		info = append(info, "⚠️ No Instagram or Facebook accounts connected.")
		info = append(info, "\nConnect an account first using 'connect instagram' or 'connect facebook'")
	} else {
		// Single global webhook URL for all Meta platforms
		webhookURL := fmt.Sprintf("%s/api/webhooks/meta", publicURL)

		info = append(info, fmt.Sprintf("**Verify Token:** `%s`\n", verifyToken))
		info = append(info, fmt.Sprintf("\n**Webhook URL (Global):**\n`%s`\n", webhookURL))
		info = append(info, "\n**Steps to configure in Meta Developer Console:**")
		info = append(info, "1. Go to your Meta App → Webhooks")
		info = append(info, fmt.Sprintf("2. Set callback URL: `%s`", webhookURL))
		info = append(info, fmt.Sprintf("3. Enter verify token: `%s` (exact match, case-sensitive)", verifyToken))
		info = append(info, "4. Subscribe to: `messages`, `messaging_postbacks`")
		info = append(info, "5. Click 'Verify and Save'")
		info = append(info, "\n⚠️ **Important:**")
		info = append(info, "- Token must match exactly (case-sensitive)")
		info = append(info, "- If verification fails, delete webhook, wait 30s, then retry")
		info = append(info, "- Make sure ngrok tunnel is running and server is active")
	}

	msg := tgbotapi.NewMessage(chatID, strings.Join(info, "\n"))
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}

// fixInstagramAccountID updates existing Instagram accounts with their Instagram Business Account ID
func (h *TenantWebhookHandler) fixInstagramAccountID(bot *tgbotapi.BotAPI, chatID int64, tenantID uint) {
	bot.Send(tgbotapi.NewMessage(chatID, "🔍 Fetching Instagram Business Account ID..."))

	// Find Instagram account
	var acc storage.SocialAccount
	if err := h.Manager.DB.Where("tenant_id = ? AND platform = ?", tenantID, "instagram").First(&acc).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ No Instagram account found. Connect Instagram first."))
		return
	}

	// Decrypt token
	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		// Token might be plain text, try using as-is
		decryptedToken = acc.AccessToken
	}

	// Fetch Instagram Business Account ID
	ctx := context.Background()
	igID, err := h.Manager.fetchInstagramBusinessAccountID(ctx, decryptedToken)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Unable to fetch your Instagram Business Account ID. Please try reconnecting your Instagram account."))
		return
	}

	// Update account
	acc.ExternalID = igID
	acc.PageID = igID
	if err := h.Manager.DB.Save(&acc).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Unable to update your account. Please try again later or contact support if the issue persists."))
		return
	}

	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Instagram Business Account ID updated!\n\nID: `%s`\n\nWebhook routing should work now. Try sending a message to your Instagram account.", igID)))
}
