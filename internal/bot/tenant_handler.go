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
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/quota"
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

// convertTelegramToIncomingMessage converts a Telegram message to the IncomingMessage format
func convertTelegramToIncomingMessage(update tgbotapi.Update, tenantID uuid.UUID, customer *storage.Customer, text string) social.IncomingMessage {
	msg := update.Message
	timestamp := time.Now()
	if msg != nil && !msg.Time().IsZero() {
		timestamp = msg.Time()
	}

	messageID := ""
	if msg != nil {
		messageID = fmt.Sprintf("%d", msg.MessageID)
	}

	threadID := ""
	if msg != nil {
		threadID = fmt.Sprintf("%d", msg.Chat.ID)
	}

	customerName := customer.DisplayName
	if customerName == "" && msg != nil && msg.From != nil {
		customerName = strings.TrimSpace(msg.From.FirstName + " " + msg.From.LastName)
	}
	if customerName == "" {
		customerName = "Customer"
	}

	customerUsername := customer.Username
	if customerUsername == "" && msg != nil && msg.From != nil {
		customerUsername = msg.From.UserName
	}

	metadata := make(map[string]interface{})
	if msg != nil {
		metadata["telegram_chat_id"] = msg.Chat.ID
		metadata["telegram_message_id"] = msg.MessageID
		if msg.From != nil {
			metadata["telegram_user_id"] = msg.From.ID
		}
	}

	return social.IncomingMessage{
		Platform:         "telegram",
		TenantID:         tenantID,
		CustomerID:       fmt.Sprintf("%d", customer.ID),
		CustomerName:     customerName,
		CustomerUsername: customerUsername,
		MessageID:        messageID,
		Content:          text,
		MessageType:      "text",
		ThreadID:         threadID,
		Timestamp:        timestamp,
		Metadata:         metadata,
	}
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

	tenantID, err := uuid.Parse(idStr)
	if err != nil {
		h.Manager.Logger.Error("invalid tenant id", zap.String("idStr", idStr), zap.Error(err))
		return
	}

	bot, botOk := h.Manager.GetBot(tenantID)
	if !botOk || bot == nil {
		h.Manager.Logger.Warn("⚠️ Bot instance not found in memory, attempting lazy load...", zap.Stringer("tenant", tenantID))

		// Lazy Load: Try to fetch tenant from DB and start bot
		var tenant storage.Tenant
		if err := h.Manager.DB.First(&tenant, tenantID).Error; err != nil {
			h.Manager.Logger.Error("❌ Failed to load tenant for lazy bot startup", zap.Stringer("tenant", tenantID), zap.Error(err))
			return
		}

		status := strings.ToLower(tenant.Status)
		if status != "active" && status != "live" {
			h.Manager.Logger.Warn("⚠️ Tenant is not active/live, skipping lazy bot startup", zap.Stringer("tenant", tenantID), zap.String("status", tenant.Status))
			return
		}

		// Attempt to start the bot
		if err := h.Manager.StartBotForTenant(&tenant); err != nil {
			h.Manager.Logger.Error("❌ Failed to lazy start bot", zap.Stringer("tenant", tenantID), zap.Error(err))
			return
		}

		// Retry getting the bot
		bot, botOk = h.Manager.GetBot(tenantID)
		if !botOk || bot == nil {
			h.Manager.Logger.Error("❌ Bot instance still not found after lazy start", zap.Stringer("tenant", tenantID))
			return
		}

		h.Manager.Logger.Info("✅ Lazy loaded bot instance successfully", zap.Stringer("tenant", tenantID))
	}

	var update tgbotapi.Update
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		h.Manager.Logger.Error("invalid telegram update", zap.Error(err))
		return
	}
	if update.CallbackQuery != nil {
		go h.handleCallbackQuery(bot, tenantID, update.CallbackQuery)
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
	ownerID := h.Manager.GetOwnerID(tenantID)
	isOwner := senderID == ownerID

	// Check if this is media (photo/video/document/animation) - don't return early for media
	hasMedia := len(update.Message.Photo) > 0 ||
		update.Message.Video != nil ||
		update.Message.Document != nil ||
		update.Message.Animation != nil

	// Only return if no text AND no media
	if text == "" && !hasMedia {
		return
	}

	// 🚀 Proactive Assistance: Check for triggers
	// We check this early so we can decide if we need to preempt other logic
	// But we use a goroutine and sleep slightly to make it feel natural (and not block)
	if isOwner && h.Manager.ProactiveTriggers != nil {
		go func(tID uuid.UUID, cID int64) {
			// Don't spam: check triggers
			triggers := h.Manager.ProactiveTriggers.CheckTriggers(context.Background(), tID, cID)
			if len(triggers) > 0 {
				trigger := triggers[0] // Get highest priority
				if h.Manager.ProactiveTriggers.ShouldShowTrigger(context.Background(), tID, cID, trigger.Type) {
					// Wait a random bit (2-5 seconds) to feel like a "thought"
					time.Sleep(3 * time.Second)

					// Send the proactive message
					bot, ok := h.Manager.GetBot(tID)
					if ok && bot != nil {
						msg := tgbotapi.NewMessage(cID, trigger.Message)
						bot.Send(msg)
					}
				}
			}
		}(tenantID, chatID)
	}

	if isOwner {
		h.Manager.Logger.Info("👔 Owner detected - tenant conversation mode",
			zap.Stringer("tenant_id", tenantID),
			zap.Int64("owner_id", senderID))
	}

	// 🧠 Step 2: Only track as customer if NOT the owner
	var customer *storage.Customer
	if !isOwner {
		h.Manager.Logger.Info("👥 Customer detected - customer service mode",
			zap.Stringer("tenant_id", tenantID),
			zap.Int64("customer_id", senderID))
		customer, _ = h.Manager.TrackOrCreateCustomer(
			ctx,
			tenantID,
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
	hasDocument := update.Message.Document != nil
	hasAnimation := update.Message.Animation != nil

	h.Manager.Logger.Info("📸 Media check",
		zap.Bool("has_photo", hasPhoto),
		zap.Bool("has_video", hasVideo),
		zap.Bool("has_document", hasDocument),
		zap.Bool("has_animation", hasAnimation),
		zap.Bool("is_owner", isOwner))

	if (hasPhoto || hasVideo || hasDocument || hasAnimation) && isOwner {
		h.Manager.Logger.Info("🖼️ Media detected from owner - uploading to Cloudinary",
			zap.Stringer("tenant_id", tenantID))

		go func(u tgbotapi.Update) {
			defer func() {
				if rec := recover(); rec != nil {
					h.Manager.Logger.Error("panic in media goroutine",
						zap.Any("recover", rec),
						zap.Stringer("tenant_id", tenantID),
						zap.String("stack", string(debug.Stack())))
				}
			}()
			h.Manager.HandleMediaUpdate(bot, tenantID, u)
		}(update)
		return
	}

	// Step 3️: Handle active sessions (post flow or social connect)
	if isOwner && strings.HasPrefix(textLower, "verify ") {
		platform := strings.TrimSpace(strings.TrimPrefix(textLower, "verify"))
		if platform == "" {
			bot.Send(tgbotapi.NewMessage(chatID, "Please specify a platform to verify (e.g., 'verify facebook')."))
		} else {
			h.handleVerifyCommand(bot, tenantID, chatID, platform)
		}
		return
	}

	// Check for custom grouping session first
	if isOwner {
		if _, ok := CustomGroupingSessions.Load(chatID); ok {
			h.handleCustomGroupingInput(bot, tenantID, chatID, text)
			return
		}
	}

	// Check for escalation resolution session first
	if isOwner && h.Manager.EscalationHandler != nil && h.Manager.SessionManager != nil {
		if session, ok := h.Manager.SessionManager.GetEscalationResolutionSession(ctx, chatID); ok && session != nil {
			if session.TenantID == tenantID {
				// Handle resolution message
				if err := h.Manager.EscalationHandler.HandleResolutionMessage(ctx, tenantID, chatID, text); err != nil {
					h.Manager.Logger.Error("failed to process resolution message", zap.Error(err))
					formatter := &UXFormatter{}
					bot.Send(tgbotapi.NewMessage(chatID, formatter.FormatErrorMessage(err, "process your request")))
				}
				return
			}
		}
	}

	// Check for new workflow orchestrator first (if available)
	if isOwner && h.Manager.WorkflowOrchestrator != nil {
		if orchestrator, ok := h.Manager.WorkflowOrchestrator.(interface {
			HandlePostingInput(ctx context.Context, tenantID uuid.UUID, chatID int64, input string) error
		}); ok {
			// Try to handle as workflow input
			ctx := context.Background()
			if err := orchestrator.HandlePostingInput(ctx, tenantID, chatID, text); err == nil {
				// Workflow handled it, return
				return
			}
			// If workflow didn't handle it (no active workflow), fall through to old session check
		}
	}

	// Check V2 post session first (new simplified flow)
	if isOwner && h.Manager != nil && h.Manager.SessionManager != nil {
		sessV2, sessV2Ok := h.Manager.SessionManager.GetPostSessionV2(ctx, chatID)
		if sessV2Ok && sessV2 != nil {
			// V2 post flow active - route to ContinuePostFlowV2
			h.Manager.ContinuePostFlowV2(bot, tenantID, chatID, text)
			return
		}
	}

	// Check old session system (backwards compatibility) - now database-backed
	var sess *PostSession
	var sessOk bool
	if h.Manager != nil && h.Manager.SessionManager != nil {
		sess, sessOk = h.Manager.SessionManager.GetPostSession(ctx, chatID)
	}
	if sessOk && sess != nil {
		if isOwner {
			// Check if this is social connect flow (Step 99) or post flow (Steps 1-5)
			if sess.Step == 99 {
				h.Manager.ContinueSocialConnect(bot, tenantID, chatID, text)
			} else if sess.Step >= 1 && sess.Step <= 5 {
				// Post flow continuation (Steps 1-5: carousel/name, name/caption, caption, platforms, schedule)
				h.Manager.ContinuePostFlow(bot, tenantID, chatID, text)
			}
		} else {
			bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Only the business owner can manage integrations."))
		}
		return
	}

	if isOwner {
		var businessSess *BusinessSession
		var businessOk bool
		if h.Manager != nil && h.Manager.SessionManager != nil {
			businessSess, businessOk = h.Manager.SessionManager.GetBusinessSession(ctx, chatID)
		}
		if businessOk && businessSess != nil {
			if h.handleBusinessSessionInput(bot, tenantID, chatID, text, businessSess) {
				return
			}
		}
	}

	// Step 4️: Quick Command Checks (handle before intent router)
	if isOwner {
		// Handle menu/help commands immediately (before intent routing)
		if text == "/help" || text == "/menu" || text == "/commands" || textLower == "menu" || textLower == "help" {
			h.sendOwnerMenu(bot, chatID, tenantID)
			return
		}
	}

	// Step 5: AI-Powered Intent Routing (for owners)
	if isOwner {
		// 🚀 V3 Smart Router (Feature Flagged)
		if IsV3EnabledForTenant(tenantID) {
			if h.Manager.RouteMessageV3(bot, tenantID, chatID, text) {
				return
			}
		}

		// Try AI intent routing first (before hardcoded commands)
		intentRouter := NewIntentRouter(h.Manager)
		handled, err := intentRouter.Route(ctx, tenantID, chatID, text, true)
		if err != nil {
			h.Manager.Logger.Warn("intent routing failed", zap.Error(err))
		}
		if handled {
			return // Intent router handled it
		}

		// Fallback to explicit commands (for backward compatibility)
		switch {
		// Analytics commands - handle directly before intent router
		case strings.Contains(textLower, "analytics") || strings.Contains(textLower, "weekly summary") ||
			strings.Contains(textLower, "insights") || strings.Contains(textLower, "growth suggestions") ||
			strings.Contains(textLower, "stats") || strings.Contains(textLower, "performance"):
			if h.Manager.AnalyticsHandler != nil {
				ctx := context.Background()
				if err := h.Manager.AnalyticsHandler.HandleAnalyticsRequest(ctx, tenantID, chatID, text); err != nil {
					h.Manager.Logger.Error("analytics request failed", zap.Error(err), zap.Stringer("tenant_id", tenantID))
					formatter := &UXFormatter{}
					bot.Send(tgbotapi.NewMessage(chatID, formatter.FormatErrorMessage(err, "fetch analytics")))
				}
				return
			} else {
				h.Manager.Logger.Warn("analytics handler not available", zap.Stringer("tenant_id", tenantID))
				bot.Send(tgbotapi.NewMessage(chatID, "📊 Analytics aren't ready just yet. Check back soon."))
				return
			}
		case text == "/webhook" || strings.HasPrefix(strings.ToLower(text), "webhook info") || strings.HasPrefix(strings.ToLower(text), "webhook"):
			h.showWebhookInfo(bot, chatID, tenantID)
			return
		case text == "fix instagram" || text == "update instagram id" || strings.HasPrefix(strings.ToLower(text), "fix instagram"):
			h.fixInstagramAccountID(bot, chatID, tenantID)
			return
		case text == "/start":
			// Check onboarding status on /start
			h.Manager.CheckAndStartOnboarding(bot, tenantID, chatID)
			return
		case text == "checklist" || text == "setup checklist" || text == "setup":
			h.Manager.showSetupChecklist(bot, tenantID, chatID)
			return
		case text == "examples" || text == "example posts":
			h.Manager.showExamplePosts(bot, chatID)
			return
		case strings.HasPrefix(strings.ToLower(text), "offer:"):
			// Tenant wants to add a promotional offer
			offerText := strings.TrimSpace(strings.TrimPrefix(text, "offer:"))
			offerText = strings.TrimPrefix(offerText, "Offer:")
			offerText = strings.TrimPrefix(offerText, "OFFER:")
			if offerText == "" {
				bot.Send(tgbotapi.NewMessage(chatID, "Please provide an offer. Example:\n`offer: Buy 2 Get 1 Free`\n`offer: 20% off on orders above ₦5000`"))
				return
			}
			// Create offer
			offer := storage.TenantOffer{
				TenantID:    tenantID,
				Title:       offerText,
				Description: "",
				IsActive:    true,
			}
			if err := h.Manager.DB.Create(&offer).Error; err != nil {
				h.Manager.Logger.Error("failed to create tenant offer", zap.Error(err))
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Failed to save offer. Please try again."))
				return
			}
			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Offer saved: *%s*\n\nYour AI can now suggest this offer to customers. To add more details, send:\n`offer: %s - [description]`", offerText, offerText)))
			return
		case strings.HasPrefix(strings.ToLower(text), "list offers") || strings.HasPrefix(strings.ToLower(text), "my offers"):
			// List all active offers
			var offers []storage.TenantOffer
			h.Manager.DB.Where("tenant_id = ? AND is_active = ?", tenantID, true).
				Order("created_at DESC").
				Find(&offers)
			if len(offers) == 0 {
				bot.Send(tgbotapi.NewMessage(chatID, "No active offers. Add one with:\n`offer: [your offer]`"))
				return
			}
			msg := "*📋 Your Active Offers:*\n\n"
			for i, offer := range offers {
				msg += fmt.Sprintf("%d. *%s*", i+1, offer.Title)
				if offer.Description != "" {
					msg += fmt.Sprintf("\n   %s", offer.Description)
				}
				msg += fmt.Sprintf("\n   ID: %d\n\n", offer.ID)
			}
			msg += "To remove an offer, send: `remove offer [ID]`"
			bot.Send(tgbotapi.NewMessage(chatID, msg))
			return
		case strings.HasPrefix(strings.ToLower(text), "remove offer"):
			// Remove an offer
			parts := strings.Fields(text)
			if len(parts) < 3 {
				bot.Send(tgbotapi.NewMessage(chatID, "Please specify offer ID. Example:\n`remove offer 1`"))
				return
			}
			offerIDStr := parts[2]
			offerID, err := uuid.Parse(offerIDStr)
			if err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "Invalid offer ID. Use the ID from `list offers`"))
				return
			}
			var offer storage.TenantOffer
			if err := h.Manager.DB.Where("id = ? AND tenant_id = ?", offerID, tenantID).First(&offer).Error; err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "Offer not found. Use `list offers` to see your offers."))
				return
			}
			offer.IsActive = false
			if err := h.Manager.DB.Save(&offer).Error; err != nil {
				h.Manager.Logger.Error("failed to deactivate offer", zap.Error(err))
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Failed to remove offer. Please try again."))
				return
			}
			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Offer removed: *%s*", offer.Title)))
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

				response += fmt.Sprintf("%d. *%s*\n", i+1, post.Name)
				response += fmt.Sprintf("   📱 Platforms: %s\n", platformStr)
				response += fmt.Sprintf("   ⏰ Scheduled: %s\n", scheduledTime)
				response += fmt.Sprintf("   Status: %s\n\n", post.Status)

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
				formatter := &UXFormatter{}
				bot.Send(tgbotapi.NewMessage(chatID, formatter.FormatErrorMessage(err, "find scheduled post")))
				return
			}

			if err := h.Manager.DB.Model(&post).Update("status", "cancelled").Error; err != nil {
				formatter := &UXFormatter{}
				bot.Send(tgbotapi.NewMessage(chatID, formatter.FormatErrorMessage(err, "cancel post")))
				return
			}

			formatter := &UXFormatter{}
			bot.Send(tgbotapi.NewMessage(chatID, formatter.FormatSuccessMessage(fmt.Sprintf("Cancelled: %s", postName))))
			return

		case textLower == "status" || textLower == "connections":
			h.sendConnectionStatus(bot, chatID, tenantID)
			return

		case strings.HasPrefix(strings.ToLower(text), "connect "):
			platform := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(text), "connect"))
			go h.Manager.StartSocialConnectFlow(bot, tenantID, chatID, platform)
			return

		case strings.HasPrefix(textLower, "verify "):
			platform := strings.TrimSpace(strings.TrimPrefix(textLower, "verify"))
			h.handleVerifyCommand(bot, tenantID, chatID, platform)
			return

		// New feature commands
		case textLower == "/orders" || textLower == "orders" || strings.HasPrefix(textLower, "orders"):
			h.showOrdersMenu(bot, chatID, tenantID)
			return
		case textLower == "/products" || textLower == "products" || strings.HasPrefix(textLower, "products"):
			h.showProductsMenu(bot, chatID, tenantID)
			return
		case textLower == "/payments" || textLower == "payments" || strings.HasPrefix(textLower, "payments"):
			h.showPaymentsMenu(bot, chatID, tenantID)
			return
		case strings.HasPrefix(textLower, "order "):
			// Handle "order <order_number>" or "order <id>"
			arg := strings.TrimSpace(strings.TrimPrefix(textLower, "order "))
			if orderID, err := uuid.Parse(arg); err == nil {
				h.viewOrderDetails(bot, chatID, tenantID, orderID)
			} else {
				// Try to find by order number
				var order storage.Order
				if err := h.Manager.DB.Where("tenant_id = ? AND order_number = ?", tenantID, strings.ToUpper(arg)).First(&order).Error; err == nil {
					h.viewOrderDetails(bot, chatID, tenantID, order.ID)
				} else {
					bot.Send(tgbotapi.NewMessage(chatID, "❌ Order not found. Use 'orders' to see all orders."))
				}
			}
			return
		case strings.HasPrefix(textLower, "payment "):
			// Handle "payment <reference>"
			arg := strings.TrimSpace(strings.TrimPrefix(textLower, "payment "))
			var payment storage.Payment
			if err := h.Manager.DB.Where("tenant_id = ? AND payment_reference = ?", tenantID, strings.ToUpper(arg)).First(&payment).Error; err == nil {
				h.viewPaymentDetails(bot, chatID, tenantID, payment.ID)
			} else {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Payment not found. Use 'payments' to see all payments."))
			}
			return
		}

		// 🧠 AI Conversational Fallback: If no command matched, use AI to respond naturally
		// NOTE: This path is ONLY for owners who typed something that doesn't match commands
		// It returns early, so it won't conflict with the customer message flow below
		if h.Manager.Responder != nil && isOwner {
			go func(tenantID uuid.UUID, chatID int64, messageText string) {
				defer func() {
					if rec := recover(); rec != nil {
						h.Manager.Logger.Error("panic in AI conversational reply",
							zap.Any("recover", rec),
							zap.Stringer("tenant_id", tenantID),
							zap.String("stack", string(debug.Stack())))
					}
				}()

				ctx := context.Background()
				reply, err := h.Manager.Responder.GenerateTenantAssistantReply(ctx, tenantID, messageText)
				if err != nil {
					h.Manager.Logger.Error("failed to generate AI reply",
						zap.Error(err),
						zap.Stringer("tenant_id", tenantID))
					// Send a friendly fallback message
					formatter := &UXFormatter{}
					bot.Send(tgbotapi.NewMessage(chatID, formatter.FormatInfoMessage("How can I help?", []string{
						"I can help you with:",
						"• Connecting social media accounts",
						"• Scheduling posts",
						"• Viewing analytics",
						"• Managing your business info",
						"",
						"Use the menu button or type /menu to see all options.",
					})))
					return
				}

				if reply != "" {
					bot.Send(tgbotapi.NewMessage(chatID, reply))
				}
			}(tenantID, chatID, text)
			return
		}
	}

	// 🧩 Step 5️: Check onboarding status - if complete, use DB data; otherwise trigger onboarding
	var tenant storage.Tenant
	if err := h.Manager.DB.First(&tenant, tenantID).Error; err == nil {
		// Check if onboarding is complete (dashboard signup)
		if tenant.OnboardingComplete {
			// Onboarding already done via dashboard - load persona and business profile from DB
			persona := ai.GetPersona(h.Manager.DB, tenantID)
			var profile storage.BusinessProfile
			_ = h.Manager.DB.First(&profile, "tenant_id = ?", tenantID).Error

			// Use existing data - no onboarding questions needed
			// If persona/business profile is missing despite OnboardingComplete flag,
			// we'll still continue but log a warning (data should exist from dashboard)
			if persona.BotName == "" || profile.Niche == "" {
				h.Manager.Logger.Warn("OnboardingComplete=true but missing persona/business profile data",
					zap.Stringer("tenant_id", tenantID),
					zap.Bool("has_persona", persona.BotName != ""),
					zap.Bool("has_profile", profile.Niche != ""))
				// Continue anyway - user can configure via dashboard or menu
			}
		} else {
			// Legacy flow: onboarding not complete - check for missing persona/business profile
			persona := ai.GetPersona(h.Manager.DB, tenantID)
			var profile storage.BusinessProfile
			_ = h.Manager.DB.First(&profile, "tenant_id = ?", tenantID).Error

			personaMissing := persona.BotName == "" || persona.Tone == ""
			businessProfileMissing := profile.Niche == "" || profile.Description == "" || profile.Tone == ""

			if personaMissing {
				if isOwner {
					if _, active := ai.PersonaSessions.Load(chatID); !active {
						go ai.StartPersonaOnboarding(ctx, h.Manager.DB, bot, tenantID, chatID, h.Manager.Responder.Client, h.Manager.Logger)
					} else {
						go ai.ContinuePersonaOnboarding(ctx, h.Manager.DB, bot, tenantID, chatID, text, h.Manager.Responder.Client, h.Manager.Logger)
					}
				} else {
					bot.Send(tgbotapi.NewMessage(chatID, "Hold on, still setting things up."))
				}
				return
			}

			if businessProfileMissing {
				if isOwner {
					if _, active := ai.BusinessProfileSessions.Load(chatID); !active {
						go ai.StartBusinessProfileOnboarding(ctx, h.Manager.DB, bot, tenantID, chatID, h.Manager.Responder.Client, h.Manager.Logger)
					} else {
						go ai.ContinueBusinessProfileOnboarding(ctx, h.Manager.DB, bot, tenantID, chatID, text, h.Manager.Responder.Client, h.Manager.Logger)
					}
				} else {
					bot.Send(tgbotapi.NewMessage(chatID, "Still configuring, give me a sec."))
				}
				return
			}
		}
	}

	// 🧠 Step 6️: Check if this is a customer message instruction (before AI processing)
	if isOwner {
		if handled := h.handleCustomerMessageInstruction(ctx, bot, tenantID, chatID, text); handled {
			return // Instruction was handled, don't continue to AI
		}
	}

	// 🧠 Step 7️: Context-Aware AI Assistant flow (tenant + customer aware)
	// Send typing indicator
	bot.Send(tgbotapi.NewChatAction(chatID, "typing"))

	// Use context-aware generator for better responses
	// Note: QuotaService would need to be passed if available from Manager
	// For now, we'll check if Responder has it and pass it through
	var quotaSvc *quota.QuotaService
	if h.Manager.Responder != nil && h.Manager.Responder.QuotaService != nil {
		quotaSvc = h.Manager.Responder.QuotaService
	}
	contextGen := ai.NewContextAwareGeneratorWithQuota(
		h.Manager.Responder.Client,
		h.Manager.DB,
		h.Manager.Memory,
		quotaSvc,
	)

	var reply string
	var replyErr error

	if isOwner {
		// Owner gets business management assistance with full context
		reply, replyErr = contextGen.GenerateWithFullContext(ctx, tenantID, text, true, uuid.Nil)

		// Add proactive suggestions for owners (occasionally)
		if replyErr == nil && reply != "" {
			proactive := ai.NewProactiveAssistant(h.Manager.Responder.Client, h.Manager.DB, h.Manager.Memory)
			enhanced, err := proactive.EnhanceResponseWithProactiveSuggestions(ctx, tenantID, reply)
			if err == nil {
				reply = enhanced
			}
		}
	} else {
		// Only customers reach this point - owner is handled above
		if customer == nil {
			// Generate contextual error response
			if h.Manager.Responder != nil && h.Manager.Responder.Client != nil {
				fallbackGen := ai.NewContextualFallbackGenerator(
					h.Manager.Responder.Client,
					h.Manager.DB,
					h.Manager.Logger,
					h.Manager.Memory,
				)
				errorMsg, fallbackErr := fallbackGen.GenerateErrorResponse(ctx, tenantID, uuid.Nil, text, "system_error")
				if fallbackErr == nil && errorMsg != "" {
					bot.Send(tgbotapi.NewMessage(chatID, errorMsg))
				} else {
					bot.Send(tgbotapi.NewMessage(chatID, "I'm having trouble identifying your account. Can you try again?"))
				}
			} else {
				bot.Send(tgbotapi.NewMessage(chatID, "I'm having trouble identifying your account. Can you try again?"))
			}
			return
		}

		// Route customer message through MessageHandler for full escalation support
		if h.Manager.MessageHandler != nil {
			// Convert Telegram message to IncomingMessage format
			telegramMsg := convertTelegramToIncomingMessage(
				update,
				tenantID,
				customer,
				text,
			)

			// Process message through MessageHandler (includes escalation, intent detection, etc.)
			go func() {
				// Type assert to get the actual MessageHandler
				if handler, ok := h.Manager.MessageHandler.(interface {
					ProcessMessage(ctx context.Context, msg interface{}) error
				}); ok {
					if err := handler.ProcessMessage(ctx, telegramMsg); err != nil {
						h.Manager.Logger.Error("failed to process customer message",
							zap.Stringer("tenant_id", tenantID),
							zap.Stringer("customer_id", customer.ID),
							zap.Error(err))
						// Generate contextual error response
						if h.Manager.Responder != nil && h.Manager.Responder.Client != nil {
							fallbackGen := ai.NewContextualFallbackGenerator(
								h.Manager.Responder.Client,
								h.Manager.DB,
								h.Manager.Logger,
								h.Manager.Memory,
							)
							errorMsg, fallbackErr := fallbackGen.GenerateErrorResponse(ctx, tenantID, customer.ID, text, "processing_error")
							if fallbackErr == nil && errorMsg != "" {
								bot.Send(tgbotapi.NewMessage(chatID, errorMsg))
							} else {
								bot.Send(tgbotapi.NewMessage(chatID, "Got it! Working on that for you."))
							}
						} else {
							bot.Send(tgbotapi.NewMessage(chatID, "Got it! Working on that for you."))
						}
					}
				} else {
					h.Manager.Logger.Warn("MessageHandler does not implement ProcessMessage with IncomingMessage",
						zap.Stringer("tenant_id", tenantID))
					// Generate contextual error response
					if h.Manager.Responder != nil && h.Manager.Responder.Client != nil {
						fallbackGen := ai.NewContextualFallbackGenerator(
							h.Manager.Responder.Client,
							h.Manager.DB,
							h.Manager.Logger,
							h.Manager.Memory,
						)
						errorMsg, fallbackErr := fallbackGen.GenerateErrorResponse(ctx, tenantID, customer.ID, text, "system_error")
						if fallbackErr == nil && errorMsg != "" {
							bot.Send(tgbotapi.NewMessage(chatID, errorMsg))
						} else {
							bot.Send(tgbotapi.NewMessage(chatID, "Got it! Working on that for you."))
						}
					} else {
						bot.Send(tgbotapi.NewMessage(chatID, "Got it! Working on that for you."))
					}
				}
			}()
			return // MessageHandler handles everything from here
		}

		// Fallback: Direct AI response if MessageHandler not available
		customerID := customer.ID
		reply, replyErr := contextGen.GenerateWithFullContext(ctx, tenantID, text, false, customerID)

		if replyErr != nil {
			h.Manager.Logger.Error("AI responder failed", zap.Error(replyErr))
			// Generate contextual error response
			if contextGen != nil && h.Manager.Responder != nil && h.Manager.Responder.Client != nil {
				fallbackGen := ai.NewContextualFallbackGenerator(
					h.Manager.Responder.Client,
					h.Manager.DB,
					h.Manager.Logger,
					h.Manager.Memory,
				)
				errorMsg, fallbackErr := fallbackGen.GenerateErrorResponse(ctx, tenantID, customer.ID, text, "processing_error")
				if fallbackErr == nil && errorMsg != "" {
					bot.Send(tgbotapi.NewMessage(chatID, errorMsg))
				} else {
					bot.Send(tgbotapi.NewMessage(chatID, "Got it! Working on that for you."))
				}
			} else {
				bot.Send(tgbotapi.NewMessage(chatID, "Got it! Working on that for you."))
			}
			return
		}

		// Send with markdown formatting for cleaner messages
		msg := tgbotapi.NewMessage(chatID, reply)
		msg.ParseMode = "Markdown"
		bot.Send(msg)
	}
}

func (h *TenantWebhookHandler) handleVerifyCommand(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, platform string) {
	platform = strings.TrimSpace(strings.ToLower(platform))
	if platform == "" {
		bot.Send(tgbotapi.NewMessage(chatID, "Please specify which platform to verify (e.g., 'verify facebook')."))
		return
	}

	// Clear any existing post session
	if h.Manager != nil && h.Manager.SessionManager != nil {
		h.Manager.SessionManager.DeletePostSession(context.Background(), chatID)
	}

	// Send loading/verifying message immediately
	loadingMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("🔍 Verifying your %s connection...", strings.Title(platform)))
	loadingSent, _ := bot.Send(loadingMsg)

	go func() {
		ctx := context.Background()
		var acc storage.SocialAccount
		if err := h.Manager.DB.Where("tenant_id = ? AND platform = ?", tenantID, platform).First(&acc).Error; err != nil {
			// Delete loading message and send error
			bot.Request(tgbotapi.NewDeleteMessage(chatID, loadingSent.MessageID))
			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ No %s account connected yet. Please connect it first.", strings.Title(platform))))
			// Show connection menu after error
			time.Sleep(1 * time.Second)
			h.sendConnectionStatus(bot, chatID, tenantID)
			return
		}

		if acc.AccessToken != "" {
			if decrypted, err := utils.DecryptToken(acc.AccessToken); err == nil {
				acc.AccessToken = decrypted
			}
		}

		msg, err := social.VerifySocialConnection(ctx, &acc, h.Manager.DB)

		// Delete loading message
		bot.Request(tgbotapi.NewDeleteMessage(chatID, loadingSent.MessageID))

		if err != nil {
			errorMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ I couldn't verify your %s connection:\n\n%s", strings.Title(platform), err.Error()))
			bot.Send(errorMsg)
			// Show connection menu after error
			time.Sleep(1 * time.Second)
			h.sendConnectionStatus(bot, chatID, tenantID)
			return
		}

		// Send success message
		successMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ %s", msg))
		bot.Send(successMsg)

		// Show connection status menu after successful verification
		time.Sleep(1 * time.Second)
		h.sendConnectionStatus(bot, chatID, tenantID)
	}()
}

func (h *TenantWebhookHandler) handleCallbackQuery(bot *tgbotapi.BotAPI, tenantID uuid.UUID, cq *tgbotapi.CallbackQuery) {
	defer func() {
		if rec := recover(); rec != nil {
			h.Manager.Logger.Error("panic recovered in callback handler",
				zap.Any("recover", rec),
				zap.Stringer("tenant_id", tenantID),
				zap.String("stack", string(debug.Stack())))
		}
	}()

	if cq == nil {
		h.Manager.Logger.Warn("handleCallbackQuery called with nil callback query",
			zap.Stringer("tenant_id", tenantID))
		return
	}

	h.Manager.Logger.Debug("Callback query received",
		zap.String("callback_data", cq.Data),
		zap.Stringer("tenant_id", tenantID),
		zap.Int64("chat_id", cq.Message.Chat.ID))

	// Handle bulk upload callbacks
	if strings.HasPrefix(cq.Data, "bulk:") {
		h.handleBulkUploadCallback(bot, tenantID, cq)
		return
	}

	// Acknowledge callback to avoid "button spinning"
	callback := tgbotapi.NewCallback(cq.ID, "")
	if _, err := bot.Request(callback); err != nil {
		h.Manager.Logger.Error("failed to acknowledge callback",
			zap.Error(err),
			zap.String("callback_data", cq.Data),
			zap.Stringer("tenant_id", tenantID))
		// Continue anyway - callback might still work
	}

	data := strings.TrimSpace(cq.Data)
	if data == "" {
		h.Manager.Logger.Warn("Empty callback data",
			zap.Stringer("tenant_id", tenantID))
		return
	}

	chatID := cq.Message.Chat.ID

	parts := strings.Split(data, ":")
	action := parts[0]
	arg := ""
	if len(parts) > 1 {
		arg = strings.Join(parts[1:], ":")
	}

	h.Manager.Logger.Debug("Processing callback",
		zap.String("action", action),
		zap.String("arg", arg),
		zap.Stringer("tenant_id", tenantID),
		zap.Int64("chat_id", chatID))

	switch action {
	case "posting":
		// Handle new workflow posting callbacks
		// arg format: "confirm:init", "save:later", "cancel", "carousel:yes", etc.
		h.handlePostingWorkflowCallback(bot, tenantID, chatID, arg)
	case "menu":
		switch arg {
		case "", "main":
			h.sendOwnerMenu(bot, chatID, tenantID)
		case "connect":
			h.sendConnectionStatus(bot, chatID, tenantID)
		case "business":
			h.sendBusinessMenu(bot, chatID, tenantID)
		case "analytics":
			h.Manager.Logger.Info("Analytics button tapped",
				zap.Stringer("tenant_id", tenantID),
				zap.Int64("chat_id", chatID))
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
			h.sendConnectionStatus(bot, chatID, tenantID)
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
	case "caption":
		// Handle caption mode selection and actions
		if arg == "use" {
			// User accepted the caption, continue to platform selection
			sess, ok := getTenantSession(cq.Message.Chat.ID)
			if ok {
				// If this is called after user wrote their own caption and clicked "Use As-Is",
				// ensure UseAICaption=false so scheduler uses caption as-is (Mode 2)
				// If caption was enhanced by AI, UseAICaption is already true from enhanceCaptionInSession
				// We only set it to false if it's the original caption (not enhanced)
				if sess.CaptionMode == "write" && !sess.UseAICaption {
					// User wrote their own caption and wants to use it as-is
					sess.UseAICaption = false
				}
				sess.Step = 4
				if h.Manager != nil && h.Manager.SessionManager != nil {
					h.Manager.SessionManager.SavePostSession(context.Background(), cq.Message.Chat.ID, sess)
				}
				bot.Send(tgbotapi.NewMessage(cq.Message.Chat.ID, "Nice! Which platforms are we queuing it for? (Instagram, Facebook, TikTok, YouTube, WhatsApp, or 'all')."))
			}
		} else if arg == "edit" {
			// User wants to edit manually
			sess, ok := getTenantSession(cq.Message.Chat.ID)
			if ok && h.Manager != nil && h.Manager.SessionManager != nil {
				sess.CaptionMode = "write"
				sess.Step = 3
				h.Manager.SessionManager.SavePostSession(context.Background(), cq.Message.Chat.ID, sess)
				bot.Send(tgbotapi.NewMessage(cq.Message.Chat.ID, "Perfect! Type your caption, or say 'skip' to leave it blank."))
			}
		} else {
			// Handle other caption actions (rephrase, fine_tune, write, keywords, ai, skip)
			sess, ok := getTenantSession(cq.Message.Chat.ID)
			if !ok {
				bot.Send(tgbotapi.NewMessage(cq.Message.Chat.ID, "Session expired. Please start over."))
				return
			}

			switch arg {
			case "write":
				sess.CaptionMode = "write"
				sess.Step = 3
				if h.Manager != nil && h.Manager.SessionManager != nil {
					h.Manager.SessionManager.SavePostSession(context.Background(), cq.Message.Chat.ID, sess)
				}
				bot.Send(tgbotapi.NewMessage(cq.Message.Chat.ID, "Perfect! Type your caption, or say 'skip' to leave it blank."))
			case "keywords":
				sess.CaptionMode = "keywords"
				sess.Step = 3
				if h.Manager != nil && h.Manager.SessionManager != nil {
					h.Manager.SessionManager.SavePostSession(context.Background(), cq.Message.Chat.ID, sess)
				}
				bot.Send(tgbotapi.NewMessage(cq.Message.Chat.ID, "Great! Drop some keywords or phrases, and I'll spin a caption for you. (e.g., 'new product launch, exciting, limited time')"))
			case "ai":
				sess.CaptionMode = "ai"
				sess.UseAICaption = true
				sess.Step = 4
				if h.Manager != nil && h.Manager.SessionManager != nil {
					h.Manager.SessionManager.SavePostSession(context.Background(), cq.Message.Chat.ID, sess)
				}
				bot.Send(tgbotapi.NewMessage(cq.Message.Chat.ID, "Love it. Where are we posting? (Instagram, Facebook, TikTok, etc., or just say 'all')."))
			case "skip":
				sess.Caption = ""
				sess.Step = 4
				if h.Manager != nil && h.Manager.SessionManager != nil {
					h.Manager.SessionManager.SavePostSession(context.Background(), cq.Message.Chat.ID, sess)
				}
				bot.Send(tgbotapi.NewMessage(cq.Message.Chat.ID, "No worries. Which platforms should we hit? (Instagram, Facebook, TikTok, YouTube, WhatsApp, or 'all')."))
			case "rephrase":
				if sess.Caption == "" {
					bot.Send(tgbotapi.NewMessage(cq.Message.Chat.ID, "No caption to rephrase. Please provide a caption first."))
					return
				}
				h.rephraseCaptionInSession(bot, tenantID, cq.Message.Chat.ID, sess)
			case "fine_tune":
				if sess.Caption == "" {
					bot.Send(tgbotapi.NewMessage(cq.Message.Chat.ID, "No caption to fine-tune. Please provide a caption first."))
					return
				}
				h.fineTuneCaptionInSession(bot, tenantID, cq.Message.Chat.ID, sess)
			case "enhance":
				// Mode 3: AI-enhanced - Enhance tenant's written caption
				if sess.Caption == "" {
					bot.Send(tgbotapi.NewMessage(cq.Message.Chat.ID, "No caption to enhance. Please provide a caption first."))
					return
				}
				// Set UseAICaption=true so scheduler knows to enhance this caption
				sess.UseAICaption = true
				h.enhanceCaptionInSession(bot, tenantID, cq.Message.Chat.ID, sess)
			default:
				bot.Send(tgbotapi.NewMessage(cq.Message.Chat.ID, "Invalid option. Please try again."))
			}
		}
	case "postflow":
		// Check if it's a preview action or regular postflow
		previewActions := map[string]bool{
			"confirm": true, "post_now": true, "edit_caption": true,
			"edit_schedule": true, "edit_platforms": true, "cancel": true,
			"schedule": true,
		}
		if previewActions[arg] {
			h.handlePostPreviewCallback(bot, tenantID, chatID, arg)
		} else {
			h.handlePostFlowCallback(bot, tenantID, chatID, arg)
		}
	case "escalate":
		// Handle escalation actions: approve, edit, ignore
		// arg format: "approve:123" or "edit:123" or "ignore:123"
		h.handleEscalationCallback(bot, tenantID, chatID, cq.From.ID, arg)
	case "platforms":
		// Handle platform selection for posts
		// arg format: "toggle:instagram:true" or "all" or "confirm"
		h.handlePlatformSelectionCallback(bot, tenantID, chatID, arg)
	case "post":
		// Handle post cancellation callbacks
		// arg format: "cancel:<post_id>" or "cancel_all"
		h.handlePostCancelCallback(bot, tenantID, chatID, arg)
	case "onboarding":
		// Handle onboarding wizard callbacks
		h.handleOnboardingCallback(bot, tenantID, chatID, arg)
	case "tiktok":
		// Handle TikTok management callbacks
		// arg format: "list", "analytics", "comments", "status"
		h.handleTikTokCallback(bot, tenantID, chatID, arg)
	case "tiktok_option":
		// Handle TikTok posting options during post flow
		// arg format: "toggle:duet:true", "toggle:stitch:false", "default", "continue"
		h.handleTikTokPostingOptionCallback(bot, tenantID, chatID, arg)
	case "industry":
		// Handle industry selection callbacks
		// arg format: "select:<industry_id>"
		if strings.HasPrefix(arg, "select:") {
			industryIDStr := strings.TrimPrefix(arg, "select:")
			if h.Manager.IndustrySelectionHandler != nil {
				h.Manager.IndustrySelectionHandler.HandleIndustrySelectionCallback(bot, tenantID, chatID, industryIDStr)
			}
		}
	case "orders":
		// Handle order management callbacks
		// arg format: "menu", "list:<status>", "view:<id>", "confirm:<id>", etc.
		parts := strings.SplitN(arg, ":", 2)
		action := parts[0]
		actionArg := ""
		if len(parts) > 1 {
			actionArg = parts[1]
		}
		h.handleOrderCallback(bot, tenantID, chatID, action, actionArg)
	case "order":
		// Handle single order callbacks
		// arg format: "view:<id>", "confirm:<id>", "complete:<id>", "receipt:<id>", "notes:<id>"
		parts := strings.SplitN(arg, ":", 2)
		action := parts[0]
		actionArg := ""
		if len(parts) > 1 {
			actionArg = parts[1]
		}
		h.handleOrderCallback(bot, tenantID, chatID, action, actionArg)
	case "products":
		// Handle product/menu/service management callbacks
		// arg format: "menu", "list:<type>", "add:<type>", "view:<id>:<type>", etc.
		parts := strings.SplitN(arg, ":", 2)
		action := parts[0]
		actionArg := ""
		if len(parts) > 1 {
			actionArg = parts[1]
		}
		h.handleProductCallback(bot, tenantID, chatID, action, actionArg)
	case "product":
		// Handle single product/menu/service callbacks
		// arg format: "view:<id>:<type>", "edit:<id>:<type>", "delete:<id>:<type>"
		parts := strings.SplitN(arg, ":", 2)
		action := parts[0]
		actionArg := ""
		if len(parts) > 1 {
			actionArg = parts[1]
		}
		h.handleProductCallback(bot, tenantID, chatID, action, actionArg)
	case "payments":
		// Handle payment management callbacks
		// arg format: "menu", "list:<status>", "view:<id>", "verify:<id>", etc.
		parts := strings.SplitN(arg, ":", 2)
		action := parts[0]
		actionArg := ""
		if len(parts) > 1 {
			actionArg = parts[1]
		}
		h.handlePaymentCallback(bot, tenantID, chatID, action, actionArg)
	case "payment":
		// Handle single payment callbacks
		// arg format: "view:<id>", "verify:<id>", "dispute:<id>", "receipt:<id>"
		parts := strings.SplitN(arg, ":", 2)
		action := parts[0]
		actionArg := ""
		if len(parts) > 1 {
			actionArg = parts[1]
		}
		h.handlePaymentCallback(bot, tenantID, chatID, action, actionArg)
	case "postv2":
		// Handle V2 post flow callbacks (simplified UX)
		// arg format: "platforms:all", "caption:use", "schedule:now", etc.
		h.HandlePostFlowV2Callback(bot, tenantID, chatID, arg)
	case "v3":
		// Handle V3 ultra-simplified post flow callbacks
		// arg format: "caption:ai", "time:now", "undo", etc.
		h.Manager.HandlePostV3Callback(bot, chatID, arg)
	case "v3esc", "v3pay":
		// Handle V3 escalation and payment callbacks
		// data format: "v3esc:send:uuid" or "v3pay:confirm:uuid"
		h.Manager.HandleEscalationV3Callback(bot, chatID, data)
	case "v3home":
		// Handle V3 smart home screen callbacks
		h.Manager.HandleHomeV3Callback(bot, tenantID, chatID, arg)
	case "social":
		// Handle legacy "social:connect:..." callbacks by showing connection menu
		h.sendConnectionStatus(bot, chatID, tenantID)
	default:
		h.Manager.Logger.Debug("unhandled callback", zap.String("data", data))
	}
}

func (h *TenantWebhookHandler) sendOwnerMenu(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID) {
	// 🚀 V3 Smart Home Screen (Feature Flagged)
	if IsV3EnabledForTenant(tenantID) {
		h.Manager.SendSmartHomeScreen(bot, chatID, tenantID)
		return
	}

	var tenant storage.Tenant
	var persona storage.Persona
	var accounts []storage.SocialAccount

	h.Manager.DB.First(&tenant, tenantID)
	persona = ai.GetPersona(h.Manager.DB, tenantID)
	h.Manager.DB.Where("tenant_id = ?", tenantID).Find(&accounts)

	// Use new UX formatter for clean, professional menu
	formatter := &UXFormatter{}
	stats := GetTodayStats(h.Manager.DB, tenantID)

	text := formatter.FormatMainMenu(tenant, persona, accounts, stats)
	keyboard := formatter.CreateMainMenuKeyboard()

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func (h *TenantWebhookHandler) sendAnalyticsPeek(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID) {
	h.Manager.Logger.Info("sendAnalyticsPeek called",
		zap.Stringer("tenant_id", tenantID),
		zap.Int64("chat_id", chatID),
		zap.Bool("has_analytics_handler", h.Manager.AnalyticsHandler != nil))

	if h.Manager.AnalyticsHandler == nil {
		h.Manager.Logger.Warn("AnalyticsHandler not initialized",
			zap.Stringer("tenant_id", tenantID))
		msg := tgbotapi.NewMessage(chatID, "⚠️ Analytics aren't ready just yet. Check back soon.")
		if _, err := bot.Send(msg); err != nil {
			h.Manager.Logger.Error("failed to send analytics error message",
				zap.Error(err),
				zap.Stringer("tenant_id", tenantID))
		}
		return
	}

	// Send typing indicator to show we're working
	bot.Send(tgbotapi.NewChatAction(chatID, "typing"))

	ctx := context.Background()
	if err := h.Manager.AnalyticsHandler.SendQuickSummary(ctx, bot, tenantID, chatID); err != nil {
		h.Manager.Logger.Error("failed to send analytics summary",
			zap.Error(err),
			zap.Stringer("tenant_id", tenantID),
			zap.String("error_type", fmt.Sprintf("%T", err)))
		errorMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("⚠️ I couldn't pull analytics right now: %v\n\nTry again shortly or type 'analytics' to retry.", err))
		if _, sendErr := bot.Send(errorMsg); sendErr != nil {
			h.Manager.Logger.Error("failed to send analytics error message",
				zap.Error(sendErr),
				zap.Stringer("tenant_id", tenantID))
		}
	} else {
		h.Manager.Logger.Info("Analytics summary sent successfully",
			zap.Stringer("tenant_id", tenantID),
			zap.Int64("chat_id", chatID))
	}
}

func (h *TenantWebhookHandler) sendConnectionStatus(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID) {
	var accounts []storage.SocialAccount
	h.Manager.DB.Where("tenant_id = ?", tenantID).Find(&accounts)

	// Use new UX formatter for clean connection status
	formatter := &UXFormatter{}
	text := formatter.FormatConnectionStatus(accounts)
	keyboard := formatter.CreateConnectionKeyboard()

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func (h *TenantWebhookHandler) sendHelpOverview(bot *tgbotapi.BotAPI, chatID int64) {
	formatter := &UXFormatter{}
	helpMsg := formatter.FormatHelpMessage()

	msg := tgbotapi.NewMessage(chatID, helpMsg)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("← Back to Menu", "menu:main"),
		),
	)
	bot.Send(msg)
}

func (h *TenantWebhookHandler) handleBusinessCallback(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, arg string) {
	switch arg {
	case "", "menu":
		h.sendBusinessMenu(bot, chatID, tenantID)
	case "industry":
		h.startIndustryUpdate(bot, chatID, tenantID)
	case "description":
		h.startDescriptionUpdate(bot, chatID, tenantID)
	case "hours":
		h.startHoursUpdate(bot, chatID, tenantID)
	case "knowledge":
		h.startKnowledgeCapture(bot, chatID, tenantID)
	case "menu_manager":
		h.sendMenuManager(bot, chatID, tenantID)
	case "menu_add":
		h.startMenuAdd(bot, chatID, tenantID)
	case "menu_list":
		h.sendMenuList(bot, chatID, tenantID)
	case "faq_manager":
		h.sendFAQManager(bot, chatID, tenantID)
	case "faq_add":
		h.startFAQAdd(bot, chatID, tenantID)
	case "faq_list":
		h.sendFAQList(bot, chatID, tenantID)
	default:
		h.Manager.Logger.Debug("unhandled business callback", zap.String("arg", arg))
	}
}

func (h *TenantWebhookHandler) handlePostFlowCallback(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, arg string) {
	var sess *PostSession
	var ok bool
	if h.Manager != nil && h.Manager.SessionManager != nil {
		sess, ok = h.Manager.SessionManager.GetPostSession(context.Background(), chatID)
	}
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
			if h.Manager != nil && h.Manager.SessionManager != nil {
				h.Manager.SessionManager.DeletePostSession(context.Background(), chatID)
			}
			bot.Send(tgbotapi.NewMessage(chatID, "No problem—just let me know when you want to schedule it."))
			return
		}
	case 1:
		if choice == "yes" {
			sess.IsCarousel = true
			sess.Step = 2
			if h.Manager != nil && h.Manager.SessionManager != nil {
				h.Manager.SessionManager.SavePostSession(context.Background(), chatID, sess)
			}
			bot.Send(tgbotapi.NewMessage(chatID, "Perfect. Want to tag it with an internal name? (Type it or say 'skip'.)"))
			return
		}
		if choice == "no" || choice == "skip" {
			sess.IsCarousel = false
			sess.Step = 2
			if h.Manager != nil && h.Manager.SessionManager != nil {
				h.Manager.SessionManager.SavePostSession(context.Background(), chatID, sess)
			}
			bot.Send(tgbotapi.NewMessage(chatID, "Got it. Want to tag it with an internal name? (Type it or say 'skip'.)"))
			return
		}
	}

	// Fallback: prompt user to respond with text
	bot.Send(tgbotapi.NewMessage(chatID, "Go ahead and reply with your choice so we can keep going."))
}

func (h *TenantWebhookHandler) sendBusinessMenu(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID) {
	profile, err := h.ensureBusinessProfile(tenantID)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Unable to load your business profile right now. Please try again later."))
		return
	}

	// Calculate total items from industry-specific tables
	var totalItems int64
	ctx := context.Background()
	if h.Manager != nil && h.Manager.IndustrySelectionHandler != nil && h.Manager.IndustrySelectionHandler.IndustryService != nil {
		capabilities, err := h.Manager.IndustrySelectionHandler.IndustryService.GetIndustryCapabilities(ctx, tenantID)
		if err == nil {
			if capabilities.HasProducts {
				var productCount int64
				h.Manager.DB.Model(&storage.Product{}).Where("tenant_id = ?", tenantID).Count(&productCount)
				totalItems += productCount
			}
			if capabilities.HasMenu {
				var menuCount int64
				h.Manager.DB.Model(&storage.MenuItem{}).Where("tenant_id = ?", tenantID).Count(&menuCount)
				totalItems += menuCount
			}
			if capabilities.HasServices {
				var serviceCount int64
				h.Manager.DB.Model(&storage.Service{}).Where("tenant_id = ?", tenantID).Count(&serviceCount)
				totalItems += serviceCount
			}
		}
	}

	var faqCount int64
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
		fmt.Sprintf("• Menu / Services: %d items", totalItems),
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

func (h *TenantWebhookHandler) sendMenuManager(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID) {
	// Count industry-specific items based on tenant's industry capabilities
	ctx := context.Background()
	var totalCount int64

	if h.Manager != nil && h.Manager.IndustrySelectionHandler != nil && h.Manager.IndustrySelectionHandler.IndustryService != nil {
		capabilities, err := h.Manager.IndustrySelectionHandler.IndustryService.GetIndustryCapabilities(ctx, tenantID)
		if err == nil {
			if capabilities.HasProducts {
				var productCount int64
				h.Manager.DB.Model(&storage.Product{}).Where("tenant_id = ?", tenantID).Count(&productCount)
				totalCount += productCount
			}
			if capabilities.HasMenu {
				var menuCount int64
				h.Manager.DB.Model(&storage.MenuItem{}).Where("tenant_id = ?", tenantID).Count(&menuCount)
				totalCount += menuCount
			}
			if capabilities.HasServices {
				var serviceCount int64
				h.Manager.DB.Model(&storage.Service{}).Where("tenant_id = ?", tenantID).Count(&serviceCount)
				totalCount += serviceCount
			}
		}
	}

	text := fmt.Sprintf("🍽 *Menu & Services*\nYou currently have %d items. Use the options below to add new offerings or review what Brancr knows.", totalCount)
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

func (h *TenantWebhookHandler) sendMenuList(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID) {
	ctx := context.Background()
	var allItems []struct {
		Name        string
		Price       string
		Description string
		Type        string
		UpdatedAt   time.Time
	}

	// Get items based on tenant's industry capabilities
	if h.Manager != nil && h.Manager.IndustrySelectionHandler != nil && h.Manager.IndustrySelectionHandler.IndustryService != nil {
		capabilities, err := h.Manager.IndustrySelectionHandler.IndustryService.GetIndustryCapabilities(ctx, tenantID)
		if err == nil {
			if capabilities.HasProducts {
				var products []storage.Product
				h.Manager.DB.Where("tenant_id = ?", tenantID).Order("updated_at DESC").Limit(10).Find(&products)
				for _, p := range products {
					allItems = append(allItems, struct {
						Name        string
						Price       string
						Description string
						Type        string
						UpdatedAt   time.Time
					}{
						Name:        p.Name,
						Price:       fmt.Sprintf("%s %.2f", p.Currency, p.Price),
						Description: p.Description,
						Type:        "product",
						UpdatedAt:   p.UpdatedAt,
					})
				}
			}
			if capabilities.HasMenu {
				var menuItems []storage.MenuItem
				h.Manager.DB.Where("tenant_id = ?", tenantID).Order("updated_at DESC").Limit(10).Find(&menuItems)
				for _, m := range menuItems {
					allItems = append(allItems, struct {
						Name        string
						Price       string
						Description string
						Type        string
						UpdatedAt   time.Time
					}{
						Name:        m.Name,
						Price:       fmt.Sprintf("%s %.2f", m.Currency, m.Price),
						Description: m.Description,
						Type:        "menu",
						UpdatedAt:   m.UpdatedAt,
					})
				}
			}
			if capabilities.HasServices {
				var services []storage.Service
				h.Manager.DB.Where("tenant_id = ?", tenantID).Order("updated_at DESC").Limit(10).Find(&services)
				for _, s := range services {
					priceText := "Price varies"
					if s.Pricing != "" {
						var pricing map[string]interface{}
						if err := json.Unmarshal([]byte(s.Pricing), &pricing); err == nil {
							if pType, ok := pricing["type"].(string); ok {
								if pType == "fixed" {
									if amount, ok := pricing["amount"].(float64); ok {
										priceText = fmt.Sprintf("NGN %.2f", amount)
									}
								} else if pType == "hourly" {
									if rate, ok := pricing["rate"].(float64); ok {
										priceText = fmt.Sprintf("NGN %.2f/hr", rate)
									}
								}
							}
						}
					}
					allItems = append(allItems, struct {
						Name        string
						Price       string
						Description string
						Type        string
						UpdatedAt   time.Time
					}{
						Name:        s.Name,
						Price:       priceText,
						Description: s.Description,
						Type:        "service",
						UpdatedAt:   s.UpdatedAt,
					})
				}
			}
		}
	}

	// Also include legacy BusinessMenuItem for backward compatibility during migration
	var legacyItems []storage.BusinessMenuItem
	h.Manager.DB.Where("tenant_id = ?", tenantID).Order("updated_at DESC").Limit(10).Find(&legacyItems)
	for _, item := range legacyItems {
		allItems = append(allItems, struct {
			Name        string
			Price       string
			Description string
			Type        string
			UpdatedAt   time.Time
		}{
			Name:        item.Name,
			Price:       fallbackValue(item.Price, "no price listed"),
			Description: item.Description,
			Type:        "legacy",
			UpdatedAt:   item.UpdatedAt,
		})
	}

	// Sort by UpdatedAt descending and limit to 10
	if len(allItems) > 10 {
		// Simple sort by UpdatedAt (bubble sort for small list)
		for i := 0; i < len(allItems)-1; i++ {
			for j := i + 1; j < len(allItems); j++ {
				if allItems[i].UpdatedAt.Before(allItems[j].UpdatedAt) {
					allItems[i], allItems[j] = allItems[j], allItems[i]
				}
			}
		}
		allItems = allItems[:10]
	}

	if len(allItems) == 0 {
		msg := tgbotapi.NewMessage(chatID, "You haven't added any menu or service items yet.")
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
	for i, item := range allItems {
		price := fallbackValue(item.Price, "no price listed")
		desc := fallbackValue(item.Description, "no description")
		typeIcon := "🍽️"
		if item.Type == "product" {
			typeIcon = "📦"
		} else if item.Type == "service" {
			typeIcon = "💼"
		}
		lines = append(lines, fmt.Sprintf("%d. %s *%s* — %s\n   %s", i+1, typeIcon, item.Name, price, desc))
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

func (h *TenantWebhookHandler) sendFAQManager(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID) {
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

func (h *TenantWebhookHandler) sendFAQList(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID) {
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

func (h *TenantWebhookHandler) startIndustryUpdate(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID) {
	if h.Manager != nil && h.Manager.SessionManager != nil {
		startBusinessSession(chatID, tenantID, "industry", h.Manager.SessionManager)
	}
	msg := tgbotapi.NewMessage(chatID, "What industry or business type best describes you? (e.g., Bakery, E-commerce store, Marketing agency). Type 'cancel' to stop.")
	bot.Send(msg)
}

func (h *TenantWebhookHandler) startDescriptionUpdate(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID) {
	if h.Manager != nil && h.Manager.SessionManager != nil {
		startBusinessSession(chatID, tenantID, "description", h.Manager.SessionManager)
	}
	msg := tgbotapi.NewMessage(chatID, "Describe your business in a few sentences so I can speak about it accurately. Type 'cancel' to stop.")
	bot.Send(msg)
}

func (h *TenantWebhookHandler) startHoursUpdate(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID) {
	if h.Manager != nil && h.Manager.SessionManager != nil {
		startBusinessSession(chatID, tenantID, "hours", h.Manager.SessionManager)
	}
	msg := tgbotapi.NewMessage(chatID, "Share your operating hours (e.g., Mon-Fri 9am-6pm, Sat 10am-4pm). Type 'clear' to remove hours or 'cancel' to stop.")
	bot.Send(msg)
}

func (h *TenantWebhookHandler) startKnowledgeCapture(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID) {
	if h.Manager != nil && h.Manager.SessionManager != nil {
		startBusinessSession(chatID, tenantID, "knowledge", h.Manager.SessionManager)
	}
	msg := tgbotapi.NewMessage(chatID, "Teach Brancr something important (e.g., delivery fees, return policy, signature products). Send as many messages as you'd like, type 'done' when finished or 'cancel' to stop.")
	bot.Send(msg)
}

func (h *TenantWebhookHandler) startMenuAdd(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID) {
	// Determine item type based on tenant's industry capabilities
	ctx := context.Background()
	itemType := "menu" // Default to menu

	if h.Manager != nil && h.Manager.IndustrySelectionHandler != nil && h.Manager.IndustrySelectionHandler.IndustryService != nil {
		capabilities, err := h.Manager.IndustrySelectionHandler.IndustryService.GetIndustryCapabilities(ctx, tenantID)
		if err == nil {
			// Priority: Services > Products > Menu (to match industry-specific needs)
			if capabilities.HasServices {
				itemType = "service"
			} else if capabilities.HasProducts {
				itemType = "product"
			} else if capabilities.HasMenu {
				itemType = "menu"
			}
		}
	}

	var sess *BusinessSession
	if h.Manager != nil && h.Manager.SessionManager != nil {
		sess = startBusinessSession(chatID, tenantID, "menu_add", h.Manager.SessionManager)
		if sess != nil {
			sess.Step = 1
			sess.ItemType = itemType
			// Initialize the appropriate item type
			switch itemType {
			case "product":
				sess.Product.TenantID = tenantID
				sess.Product.Currency = "NGN"
				sess.Product.IsActive = true
				sess.Product.Availability = "in_stock"
			case "service":
				sess.Service.TenantID = tenantID
				sess.Service.IsActive = true
			case "menu":
				fallthrough
			default:
				sess.MenuItem.TenantID = tenantID
				sess.MenuItem.Currency = "NGN"
				sess.MenuItem.IsActive = true
				sess.MenuItem.Availability = "available"
			}
			// Save updated step
			h.Manager.SessionManager.SaveBusinessSession(context.Background(), chatID, tenantID, sess)
		}
	}
	msg := tgbotapi.NewMessage(chatID, "Great! Let's add a product/service.\n\n1️⃣ What's the name of the item? (Type 'cancel' to stop.)")
	bot.Send(msg)
}

func (h *TenantWebhookHandler) startFAQAdd(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID) {
	var sess *BusinessSession
	if h.Manager != nil && h.Manager.SessionManager != nil {
		sess = startBusinessSession(chatID, tenantID, "faq_add", h.Manager.SessionManager)
		if sess != nil {
			sess.Step = 1
			// Save updated step
			h.Manager.SessionManager.SaveBusinessSession(context.Background(), chatID, tenantID, sess)
		}
	}
	msg := tgbotapi.NewMessage(chatID, "Let's add an FAQ.\n\n1️⃣ Send the question customers often ask. (Type 'cancel' to stop.)")
	bot.Send(msg)
}

func (h *TenantWebhookHandler) ensureBusinessProfile(tenantID uuid.UUID) (*storage.BusinessProfile, error) {
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

func (h *TenantWebhookHandler) handleBusinessSessionInput(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, text string, sess *BusinessSession) bool {
	if sess == nil {
		return false
	}

	trimmed := strings.TrimSpace(text)
	lower := strings.ToLower(trimmed)

	if lower == "cancel" {
		var sm *SessionManager
		if h.Manager != nil {
			sm = h.Manager.SessionManager
		}
		cancelBusinessSession(bot, chatID, sm)
		return true
	}

	profile, err := h.ensureBusinessProfile(tenantID)
	if err != nil {
		if h.Manager != nil && h.Manager.SessionManager != nil {
			clearBusinessSession(chatID, h.Manager.SessionManager)
		}
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
			if h.Manager != nil && h.Manager.SessionManager != nil {
				clearBusinessSession(chatID, h.Manager.SessionManager)
			}
			return true
		}
		if h.Manager != nil && h.Manager.SessionManager != nil {
			clearBusinessSession(chatID, h.Manager.SessionManager)
		}
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
			if h.Manager != nil && h.Manager.SessionManager != nil {
				clearBusinessSession(chatID, h.Manager.SessionManager)
			}
			return true
		}
		if h.Manager != nil && h.Manager.SessionManager != nil {
			clearBusinessSession(chatID, h.Manager.SessionManager)
		}
		bot.Send(tgbotapi.NewMessage(chatID, "✅ Description updated!"))
		h.sendBusinessMenu(bot, chatID, tenantID)
		return true

	case "hours":
		if lower == "clear" {
			profile.OperatingHours = ""
			if err := h.Manager.DB.Save(profile).Error; err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Couldn't clear hours right now."))
				if h.Manager != nil && h.Manager.SessionManager != nil {
					clearBusinessSession(chatID, h.Manager.SessionManager)
				}
				return true
			}
			if h.Manager != nil && h.Manager.SessionManager != nil {
				clearBusinessSession(chatID, h.Manager.SessionManager)
			}
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
			if h.Manager != nil && h.Manager.SessionManager != nil {
				clearBusinessSession(chatID, h.Manager.SessionManager)
			}
			return true
		}
		if h.Manager != nil && h.Manager.SessionManager != nil {
			clearBusinessSession(chatID, h.Manager.SessionManager)
		}
		bot.Send(tgbotapi.NewMessage(chatID, "✅ Operating hours saved!"))
		h.sendBusinessMenu(bot, chatID, tenantID)
		return true

	case "knowledge":
		if lower == "done" {
			if h.Manager != nil && h.Manager.SessionManager != nil {
				clearBusinessSession(chatID, h.Manager.SessionManager)
			}
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
			// Set name based on item type
			switch sess.ItemType {
			case "product":
				sess.Product.Name = trimmed
			case "service":
				sess.Service.Name = trimmed
			case "menu":
				fallthrough
			default:
				sess.MenuItem.Name = trimmed
			}
			sess.Step = 2
			if h.Manager != nil && h.Manager.SessionManager != nil {
				h.Manager.SessionManager.SaveBusinessSession(context.Background(), chatID, tenantID, sess)
			}
			bot.Send(tgbotapi.NewMessage(chatID, "2️⃣ What price do you charge? You can include currency, or type 'skip'."))
			return true
		case 2:
			if lower != "skip" {
				// Parse price string to float64
				priceStr := strings.ReplaceAll(trimmed, "₦", "")
				priceStr = strings.ReplaceAll(priceStr, ",", "")
				priceStr = strings.ReplaceAll(priceStr, " ", "")
				if price, err := strconv.ParseFloat(priceStr, 64); err == nil {
					switch sess.ItemType {
					case "product":
						sess.Product.Price = price
					case "menu":
						fallthrough
					default:
						sess.MenuItem.Price = price
					}
				}
				// For services, we'll handle pricing differently (could be fixed or hourly)
				if sess.ItemType == "service" {
					// Store as fixed pricing for now (can be enhanced later)
					pricing := map[string]interface{}{
						"type":   "fixed",
						"amount": 0.0,
					}
					if price, err := strconv.ParseFloat(priceStr, 64); err == nil {
						pricing["amount"] = price
					}
					if pricingJSON, err := json.Marshal(pricing); err == nil {
						sess.Service.Pricing = string(pricingJSON)
					}
				}
			}
			sess.Step = 3
			if h.Manager != nil && h.Manager.SessionManager != nil {
				h.Manager.SessionManager.SaveBusinessSession(context.Background(), chatID, tenantID, sess)
			}
			bot.Send(tgbotapi.NewMessage(chatID, "3️⃣ Add a short description or selling point (or type 'skip')."))
			return true
		case 3:
			if lower != "skip" {
				switch sess.ItemType {
				case "product":
					sess.Product.Description = text
				case "service":
					sess.Service.Description = text
				case "menu":
					fallthrough
				default:
					sess.MenuItem.Description = text
				}
			}
			sess.Step = 4
			if h.Manager != nil && h.Manager.SessionManager != nil {
				h.Manager.SessionManager.SaveBusinessSession(context.Background(), chatID, tenantID, sess)
			}
			bot.Send(tgbotapi.NewMessage(chatID, "4️⃣ Any tags or categories? (e.g., vegan, premium). Type them comma separated or 'skip'."))
			return true
		case 4:
			if lower != "skip" {
				// Parse tags/categories
				tags := strings.Split(trimmed, ",")
				for i := range tags {
					tags[i] = strings.TrimSpace(tags[i])
				}
				category := ""
				if len(tags) > 0 {
					category = tags[0] // Use first tag as category
				}
				tagsJSON, _ := json.Marshal(tags)

				switch sess.ItemType {
				case "product":
					sess.Product.Category = category
					sess.Product.Tags = string(tagsJSON)
				case "service":
					sess.Service.Category = category
				case "menu":
					fallthrough
				default:
					sess.MenuItem.Category = category
					if dietaryJSON, err := json.Marshal(tags); err == nil {
						sess.MenuItem.DietaryInfo = string(dietaryJSON)
					}
				}
			}

			// Create the appropriate item type
			var itemName string
			var createErr error

			switch sess.ItemType {
			case "product":
				sess.Product.TenantID = tenantID
				createErr = h.Manager.DB.Create(&sess.Product).Error
				itemName = sess.Product.Name
			case "service":
				sess.Service.TenantID = tenantID
				createErr = h.Manager.DB.Create(&sess.Service).Error
				itemName = sess.Service.Name
			case "menu":
				fallthrough
			default:
				sess.MenuItem.TenantID = tenantID
				createErr = h.Manager.DB.Create(&sess.MenuItem).Error
				itemName = sess.MenuItem.Name
			}

			if createErr != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Unable to save that item right now."))
				if h.Manager != nil && h.Manager.SessionManager != nil {
					clearBusinessSession(chatID, h.Manager.SessionManager)
				}
				return true
			}

			itemName = fallbackValue(itemName, "Item")
			if h.Manager != nil && h.Manager.SessionManager != nil {
				clearBusinessSession(chatID, h.Manager.SessionManager)
			}

			itemTypeLabel := "menu item"
			switch sess.ItemType {
			case "product":
				itemTypeLabel = "product"
			case "service":
				itemTypeLabel = "service"
			}

			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Added *%s* to your %s!", itemName, itemTypeLabel)))
			h.sendMenuManager(bot, chatID, tenantID)
			return true
		default:
			if h.Manager != nil && h.Manager.SessionManager != nil {
				clearBusinessSession(chatID, h.Manager.SessionManager)
			}
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
			if h.Manager != nil && h.Manager.SessionManager != nil {
				h.Manager.SessionManager.SaveBusinessSession(context.Background(), chatID, tenantID, sess)
			}
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
				if h.Manager != nil && h.Manager.SessionManager != nil {
					clearBusinessSession(chatID, h.Manager.SessionManager)
				}
				return true
			}
			if h.Manager != nil && h.Manager.SessionManager != nil {
				clearBusinessSession(chatID, h.Manager.SessionManager)
			}
			bot.Send(tgbotapi.NewMessage(chatID, "✅ FAQ added! Brancr will use it when customers ask."))
			h.sendFAQManager(bot, chatID, tenantID)
			return true
		default:
			if h.Manager != nil && h.Manager.SessionManager != nil {
				clearBusinessSession(chatID, h.Manager.SessionManager)
			}
			return true
		}
	}

	// Unknown mode fallback
	if h.Manager != nil && h.Manager.SessionManager != nil {
		clearBusinessSession(chatID, h.Manager.SessionManager)
	}
	return true
}

func schedulePost(db *gorm.DB, tenantID uuid.UUID, assetName string, when time.Time) {
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

func renameLastUpload(db *gorm.DB, tenantID uuid.UUID, name string) {
	var asset storage.MediaAsset
	db.Where("tenant_id = ?", tenantID).Order("id desc").First(&asset)
	asset.CarouselName = name
	db.Save(&asset)
}

// showWebhookInfo displays webhook configuration details for Meta platforms
func (h *TenantWebhookHandler) showWebhookInfo(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID) {
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
		info = append(info, "\nConnect an account first. Type 'connect' to see all options.")
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

// handleOnboardingCallback processes onboarding wizard callbacks
func (h *TenantWebhookHandler) handleOnboardingCallback(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, arg string) {
	switch arg {
	case "create_post":
		bot.Send(tgbotapi.NewMessage(chatID, "📸 Great! Just upload a photo or video and I'll guide you through creating your first post."))
	case "examples":
		h.Manager.showExamplePosts(bot, chatID)
	case "upload":
		bot.Send(tgbotapi.NewMessage(chatID, "📸 Upload a photo or video to get started with your first post!"))
	case "checklist":
		h.Manager.showSetupChecklist(bot, tenantID, chatID)
	case "skip":
		bot.Send(tgbotapi.NewMessage(chatID, "👌 No problem! You can access the setup checklist anytime by typing 'checklist' or use 'menu' for all options."))
	default:
		h.Manager.Logger.Debug("unhandled onboarding callback", zap.String("arg", arg))
	}
}

// handleTikTokCallback handles TikTok inline button callbacks
func (h *TenantWebhookHandler) handleTikTokCallback(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, arg string) {
	intentRouter := NewIntentRouter(h.Manager)

	switch arg {
	case "list":
		intentRouter.handleTikTokListVideos(bot, tenantID, chatID)
	case "analytics":
		// TikTok analytics removed
		bot.Send(tgbotapi.NewMessage(chatID, "❌ TikTok analytics functionality has been removed."))
	case "comments":
		bot.Send(tgbotapi.NewMessage(chatID, "💬 Please provide a video ID.\n\nExample: `tiktok comments video_123`"))
	case "status":
		bot.Send(tgbotapi.NewMessage(chatID, "🔍 Please provide a publish ID.\n\nExample: `tiktok status publish_123`"))
	default:
		intentRouter.showTikTokMenu(bot, chatID, tenantID)
	}
}

// handleTikTokPostingOptionCallback handles TikTok posting options during post flow
func (h *TenantWebhookHandler) handleTikTokPostingOptionCallback(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, arg string) {
	var sess *PostSession
	var ok bool
	if h.Manager != nil && h.Manager.SessionManager != nil {
		sess, ok = h.Manager.SessionManager.GetPostSession(context.Background(), chatID)
	}
	if !ok {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Session expired. Please start over."))
		return
	}

	parts := strings.Split(arg, ":")
	if len(parts) == 0 {
		return
	}

	switch parts[0] {
	case "toggle":
		// Format: "toggle:duet:true" or "toggle:stitch:false"
		if len(parts) >= 3 {
			option := parts[1]
			enabled := parts[2] == "true"

			switch option {
			case "duet":
				sess.TikTokDisableDuet = !enabled
			case "stitch":
				sess.TikTokDisableStitch = !enabled
			case "comment":
				sess.TikTokDisableComment = !enabled
			}
			// Save session after modification
			if h.Manager != nil && h.Manager.SessionManager != nil {
				h.Manager.SessionManager.SavePostSession(context.Background(), chatID, sess)
			}
			// Show updated options
			h.Manager.showTikTokPostingOptions(bot, chatID, sess)
		}
	case "default":
		// Reset to defaults (all enabled)
		sess.TikTokDisableDuet = false
		sess.TikTokDisableStitch = false
		sess.TikTokDisableComment = false
		sess.TikTokOptionsAsked = true
		sess.Step = 5
		if h.Manager != nil && h.Manager.SessionManager != nil {
			h.Manager.SessionManager.SavePostSession(context.Background(), chatID, sess)
		}
		bot.Send(tgbotapi.NewMessage(chatID, "✅ Using default TikTok settings (duets, stitches, and comments enabled).\n\nWhen should I publish it? Try \"now\", \"tomorrow at 9am\", or \"next week at 10am\"."))
	case "continue":
		// Continue to scheduling step
		sess.TikTokOptionsAsked = true
		sess.Step = 5
		if h.Manager != nil && h.Manager.SessionManager != nil {
			h.Manager.SessionManager.SavePostSession(context.Background(), chatID, sess)
		}
		bot.Send(tgbotapi.NewMessage(chatID, "✅ TikTok options saved.\n\nWhen should I publish it? Try \"now\", \"tomorrow at 9am\", or \"next week at 10am\"."))
	}
}

// fixInstagramAccountID updates existing Instagram accounts with their Instagram Business Account ID
func (h *TenantWebhookHandler) fixInstagramAccountID(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID) {
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

// handlePostingWorkflowCallback handles callbacks from the new posting workflow
func (h *TenantWebhookHandler) handlePostingWorkflowCallback(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, arg string) {
	if h.Manager.WorkflowOrchestrator == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Workflow system not initialized. Please try again."))
		return
	}

	ctx := context.Background()

	// Parse callback action
	parts := strings.Split(arg, ":")
	if len(parts) < 1 {
		return
	}

	action := parts[0]
	var subAction string
	var platform string
	if len(parts) > 1 {
		subAction = parts[1]
	}
	if len(parts) > 2 {
		platform = parts[2] // For platform:toggle:platform_name
	}

	// Handle platform toggle directly (requires state modification)
	if action == "platform" && subAction == "toggle" && platform != "" {
		if orchestrator, ok := h.Manager.WorkflowOrchestrator.(interface {
			HandlePostingPlatformToggle(ctx context.Context, tenantID uuid.UUID, chatID int64, platform string) error
		}); ok {
			if err := orchestrator.HandlePostingPlatformToggle(ctx, tenantID, chatID, platform); err != nil {
				h.Manager.Logger.Error("failed to toggle platform", zap.Error(err))
				bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Error toggling platform. Please try again."))
			}
			return
		}
	}

	// Handle platform confirmation directly
	if action == "platforms" && subAction == "confirm" {
		if orchestrator, ok := h.Manager.WorkflowOrchestrator.(interface {
			HandlePostingPlatformConfirm(ctx context.Context, tenantID uuid.UUID, chatID int64) error
		}); ok {
			if err := orchestrator.HandlePostingPlatformConfirm(ctx, tenantID, chatID); err != nil {
				h.Manager.Logger.Error("failed to confirm platforms", zap.Error(err))
				bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Error confirming platforms. Please select at least one platform."))
			}
			return
		}
	}

	// Convert callback to text input for workflow
	var input string
	switch action {
	case "confirm":
		if subAction == "init" {
			input = "yes"
		} else if subAction == "platforms" {
			input = "confirm"
		}
	case "save":
		if subAction == "later" {
			input = "save"
		}
	case "cancel":
		input = "cancel"
	case "carousel":
		if subAction == "yes" {
			input = "yes"
		} else if subAction == "no" {
			input = "no"
		}
	case "schedule":
		if subAction == "now" {
			input = "now"
		} else if subAction == "later" {
			input = "schedule later"
		}
	case "review":
		if subAction == "confirm" {
			input = "confirm"
		} else if subAction == "edit" && len(parts) > 2 {
			if parts[2] == "caption" {
				input = "edit caption"
			} else if parts[2] == "platforms" {
				input = "edit platforms"
			}
		}
	case "caption":
		if subAction == "confirm" {
			input = "confirm"
		} else if subAction == "edit" {
			input = "edit"
		}
	default:
		input = action
	}

	if input == "" {
		bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Invalid callback action."))
		return
	}

	// Handle through workflow orchestrator
	if orchestrator, ok := h.Manager.WorkflowOrchestrator.(interface {
		HandlePostingInput(ctx context.Context, tenantID uuid.UUID, chatID int64, input string) error
	}); ok {
		if err := orchestrator.HandlePostingInput(ctx, tenantID, chatID, input); err != nil {
			h.Manager.Logger.Error("failed to handle posting workflow input", zap.Error(err))
			bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Error processing your action. Please try again."))
		}
	} else {
		bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Workflow orchestrator not properly configured."))
	}
}

// enhanceCaptionInSession enhances the tenant's written caption with AI (Mode 3: AI-enhanced)
func (h *TenantWebhookHandler) enhanceCaptionInSession(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, sess *PostSession) {
	// Get media asset to extract image URLs
	var ma storage.MediaAsset
	if err := h.Manager.DB.First(&ma, sess.MediaAssetID).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "Couldn't find that media anymore."))
		return
	}

	var imageURLs []string
	if ma.URLs != "" {
		_ = json.Unmarshal([]byte(ma.URLs), &imageURLs)
	}

	platform := "instagram"
	if len(sess.Platforms) > 0 {
		platform = sess.Platforms[0]
	}

	optimizer := &ai.CaptionOptimizer{
		Client: h.Manager.AIClient,
		DB:     h.Manager.DB,
	}

	ctx := context.Background()
	// Use OptimizeCaption to enhance the tenant's caption (Mode 3)
	req := ai.OptimizeCaptionRequest{
		OriginalCaption: sess.Caption,
		Platform:        platform,
		MediaType:       sess.MediaType,
		TenantID:        tenantID,
		ImageURLs:       imageURLs,
	}

	result, err := optimizer.OptimizeCaption(ctx, req)
	if err != nil {
		h.Manager.Logger.Error("failed to enhance caption", zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "Sorry, I couldn't enhance the caption right now. Using your original caption."))
		// Continue with original caption (Mode 2 fallback)
		sess.Step = 4
		bot.Send(tgbotapi.NewMessage(chatID, "Which platforms are we queuing it for? (Instagram, Facebook, TikTok, YouTube, WhatsApp, or 'all')."))
		return
	}

	if result != nil && result.OptimizedCaption != "" {
		// Update session with enhanced caption
		sess.Caption = result.OptimizedCaption
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("✨ *AI-Enhanced Caption:*\n\n%s\n\n", result.OptimizedCaption))
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Use This", "caption:use"),
				tgbotapi.NewInlineKeyboardButtonData("🔄 Rephrase", "caption:rephrase"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✨ Fine-tune", "caption:fine_tune"),
				tgbotapi.NewInlineKeyboardButtonData("✏️ Edit", "caption:edit"),
			),
		)
		bot.Send(msg)
	} else {
		// Fallback to original
		bot.Send(tgbotapi.NewMessage(chatID, "Using your original caption."))
		sess.Step = 4
		bot.Send(tgbotapi.NewMessage(chatID, "Which platforms are we queuing it for? (Instagram, Facebook, TikTok, YouTube, WhatsApp, or 'all')."))
	}
}

// rephraseCaptionInSession rephrases the caption in the current session
func (h *TenantWebhookHandler) rephraseCaptionInSession(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, sess *PostSession) {
	// Get media asset to extract image URLs
	var ma storage.MediaAsset
	if err := h.Manager.DB.First(&ma, sess.MediaAssetID).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "Couldn't find that media anymore."))
		return
	}

	var imageURLs []string
	if ma.URLs != "" {
		_ = json.Unmarshal([]byte(ma.URLs), &imageURLs)
	}

	platform := "instagram"
	if len(sess.Platforms) > 0 {
		platform = sess.Platforms[0]
	}

	optimizer := &ai.CaptionOptimizer{
		Client: h.Manager.AIClient,
		DB:     h.Manager.DB,
	}

	ctx := context.Background()
	caption, tokenUsage, err := optimizer.RephraseCaption(ctx, tenantID, sess.Caption, platform, sess.MediaType, imageURLs)
	if err != nil {
		h.Manager.Logger.Error("failed to rephrase caption", zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "Sorry, I couldn't rephrase the caption right now. Try again later."))
		return
	}

	// Track cost if token usage is available
	if tokenUsage != nil {
		qs := quota.NewQuotaService(h.Manager.DB, h.Manager.Logger, nil)
		model := "default"
		if h.Manager.AIClient != nil && h.Manager.AIClient.Model != "" {
			model = h.Manager.AIClient.Model
		}
		quotaTokenUsage := quota.TokenUsageFromAI(tokenUsage.PromptTokens, tokenUsage.CompletionTokens, tokenUsage.TotalTokens)
		if incErr := qs.IncrementAICallCost(ctx, tenantID, model, quotaTokenUsage); incErr != nil {
			h.Manager.Logger.Warn("failed to increment AI call cost", zap.Error(incErr))
		}
	}

	sess.Caption = caption

	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("✨ *Rephrased Caption:*\n\n%s\n\n", caption))
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Use This", "caption:use"),
			tgbotapi.NewInlineKeyboardButtonData("🔄 Rephrase Again", "caption:rephrase"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✨ Fine-tune", "caption:fine_tune"),
			tgbotapi.NewInlineKeyboardButtonData("✏️ Edit Manually", "caption:edit"),
		),
	)
	bot.Send(msg)
}

// fineTuneCaptionInSession fine-tunes the caption in the current session
func (h *TenantWebhookHandler) fineTuneCaptionInSession(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, sess *PostSession) {
	// Get media asset to extract image URLs
	var ma storage.MediaAsset
	if err := h.Manager.DB.First(&ma, sess.MediaAssetID).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "Couldn't find that media anymore."))
		return
	}

	var imageURLs []string
	if ma.URLs != "" {
		_ = json.Unmarshal([]byte(ma.URLs), &imageURLs)
	}

	platform := "instagram"
	if len(sess.Platforms) > 0 {
		platform = sess.Platforms[0]
	}

	optimizer := &ai.CaptionOptimizer{
		Client: h.Manager.AIClient,
		DB:     h.Manager.DB,
	}

	ctx := context.Background()
	caption, tokenUsage, err := optimizer.FineTuneCaption(ctx, tenantID, sess.Caption, platform, sess.MediaType, imageURLs)
	if err != nil {
		h.Manager.Logger.Error("failed to fine-tune caption", zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "Sorry, I couldn't fine-tune the caption right now. Try again later."))
		return
	}

	// Track cost if token usage is available
	if tokenUsage != nil {
		qs := quota.NewQuotaService(h.Manager.DB, h.Manager.Logger, nil)
		model := "default"
		if h.Manager.AIClient != nil && h.Manager.AIClient.Model != "" {
			model = h.Manager.AIClient.Model
		}
		quotaTokenUsage := quota.TokenUsageFromAI(tokenUsage.PromptTokens, tokenUsage.CompletionTokens, tokenUsage.TotalTokens)
		if incErr := qs.IncrementAICallCost(ctx, tenantID, model, quotaTokenUsage); incErr != nil {
			h.Manager.Logger.Warn("failed to increment AI call cost", zap.Error(incErr))
		}
	}

	sess.Caption = caption

	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("✨ *Fine-tuned Caption:*\n\n%s\n\n", caption))
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Use This", "caption:use"),
			tgbotapi.NewInlineKeyboardButtonData("🔄 Rephrase", "caption:rephrase"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✨ Fine-tune Again", "caption:fine_tune"),
			tgbotapi.NewInlineKeyboardButtonData("✏️ Edit Manually", "caption:edit"),
		),
	)
	bot.Send(msg)
}
