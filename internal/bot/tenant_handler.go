package bot

import (
	"context"
	"encoding/json"
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
	if update.Message == nil {
		return
	}

	sender := update.Message.From
	text := strings.TrimSpace(update.Message.Text)
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
			case text == "/help" || text == "/menu" || text == "/commands":
			helpMsg := `🎛️ *Control Center Commands*

*Social Media:*
• connect instagram - Connect Instagram
• connect facebook - Connect Facebook  
• connect tiktok - Connect TikTok
• connect youtube - Connect YouTube
• connect whatsapp - Connect WhatsApp
• verify <platform> - Verify connection

*Content Management:*
• Send photos/videos - Upload media
• schedule posts - View scheduled posts
• post logs - View posting activity
• cancel <post_name> - Cancel a scheduled post

*General:*
• Just chat naturally for business help!`
			msg := tgbotapi.NewMessage(chatID, helpMsg)
			msg.ParseMode = "Markdown"
			bot.Send(msg)
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

		case strings.HasPrefix(strings.ToLower(text), "connect "):
			platform := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(text), "connect"))
			go h.Manager.StartSocialConnectFlow(bot, uint(tenantID), chatID, platform)
			return

		case strings.HasPrefix(strings.ToLower(text), "verify "):
			platform := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(text), "verify"))
			go func() {
				ctx := context.Background()
				var acc storage.SocialAccount
				if err := h.Manager.DB.Where("tenant_id = ? AND platform = ?", tenantID, platform).First(&acc).Error; err != nil {
					bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("No %s account connected.", platform)))
					return
				}
				decrypted, _ := utils.DecryptToken(acc.AccessToken)
				acc.AccessToken = decrypted

				msg, err := social.VerifySocialConnection(ctx, &acc, h.Manager.DB)
				if err != nil {
					bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Unable to verify your %s connection. Please check your account settings and try again.", strings.Title(platform))))
					return
				}
				bot.Send(tgbotapi.NewMessage(chatID, msg))
			}()
			return
		}
	}

	// 🧩 Step 5️: Persona and Business Profile setup checks
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
