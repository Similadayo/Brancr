package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/onboarding"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// 🧠 Conversational AI-driven Onboarding Handler
type OnboardingHandler struct {
	DB            *gorm.DB
	Manager       *TenantBotManager
	AlertTriggers interface {
		OnTelegramConnected(ctx context.Context, tenantID uuid.UUID, tenantName, botUsername string)
		OnOnboardingComplete(ctx context.Context, tenantID uuid.UUID, tenantName string)
	}
	sessions sync.Map // chatID → onboarding state
}

func NewOnboardingHandler(db *gorm.DB, manager *TenantBotManager) *OnboardingHandler {
	return &OnboardingHandler{DB: db, Manager: manager}
}

// ✅ Webhook entrypoint for the Brancr Admin bot
func (h *OnboardingHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var update tgbotapi.Update
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, "invalid telegram update", http.StatusBadRequest)
		return
	}

	if update.Message == nil || update.Message.Chat == nil {
		return
	}

	h.HandleOnboarding(h.Manager.AdminBot, &update)
}

// ✅ Handles AI onboarding flow messages - simplified to only link bot token to existing tenant
func (h *OnboardingHandler) HandleOnboarding(bot *tgbotapi.BotAPI, update *tgbotapi.Update) {
	if update.Message == nil || update.Message.Text == "" {
		return
	}

	chatID := update.Message.Chat.ID
	text := strings.TrimSpace(update.Message.Text)
	ctx := context.Background()

	// ✅ Check if tenant already exists and has bot linked
	var existing storage.Tenant
	if err := h.DB.Where("owner_telegram_id = ?", chatID).First(&existing).Error; err == nil {
		if existing.TelegramBotToken != "" {
			if existing.TelegramBotToken != "" {
				msg := tgbotapi.NewMessage(chatID,
					fmt.Sprintf("Hi! 👋 Your assistant is already online for *%s*.\n\nType 'menu' to jump back into your control center.", existing.Name))
				msg.ParseMode = "Markdown"
				bot.Send(msg)
				return
			}
		}
	}

	// 🧠 Load or initialize onboarding session (simplified: just email + token)
	stateRaw, _ := h.sessions.LoadOrStore(chatID, map[string]string{"step": "ask_email"})
	state, _ := stateRaw.(map[string]string)
	step := state["step"]

	// 🔗 Check for deep link token (/start TOKEN from Dashboard)
	if strings.HasPrefix(text, "/start ") {
		token := strings.TrimSpace(strings.TrimPrefix(text, "/start "))
		if token != "" && len(token) >= 16 {
			// Look up tenant by connect token
			var tenant storage.Tenant

			// First, try to find ANY tenant with this token (ignore expiration for logging)
			var debugTenant storage.Tenant
			debugErr := h.DB.Where("telegram_connect_token = ?", token).First(&debugTenant).Error

			if debugErr != nil {
				h.Manager.Logger.Warn("🔍 Token lookup failed - token not found in any tenant",
					zap.String("token_prefix", token[:8]+"..."),
					zap.Int("token_length", len(token)),
					zap.Error(debugErr))
			} else {
				// Token exists, check if expired
				h.Manager.Logger.Info("🔍 Token found, checking expiration",
					zap.String("token_prefix", token[:8]+"..."),
					zap.Stringer("tenant_id", debugTenant.ID),
					zap.Time("token_expires", func() time.Time {
						if debugTenant.TelegramConnectExpires != nil {
							return *debugTenant.TelegramConnectExpires
						}
						return time.Time{}
					}()),
					zap.Time("current_time", time.Now()),
					zap.Bool("is_expired", debugTenant.TelegramConnectExpires == nil || debugTenant.TelegramConnectExpires.Before(time.Now())))
			}

			// Now do the actual lookup with expiration check
			// First check if token exists at all (ignoring expiry)
			if err := h.DB.Where("telegram_connect_token = ?", token).First(&tenant).Error; err != nil {
				// Token not found
				h.Manager.Logger.Warn("❌ Token not found",
					zap.String("token_prefix", token[:8]+"..."),
					zap.Error(err))
				msg := tgbotapi.NewMessage(chatID, `❌ Invalid link.

This link may have been overridden by a newer one. Please go back to your Dashboard and click "Connect Telegram" again for a fresh link.`)
				msg.ParseMode = "Markdown"
				bot.Send(msg)
				return
			}

			// Token exists, check expiry
			if tenant.TelegramConnectExpires != nil && tenant.TelegramConnectExpires.Before(time.Now()) {
				// Expired
				h.Manager.Logger.Warn("⏰ Token expired",
					zap.String("token_prefix", token[:8]+"..."),
					zap.Time("expires_at", *tenant.TelegramConnectExpires))
				msg := tgbotapi.NewMessage(chatID, `⏰ That link has expired.

Please go back to your Dashboard and click "Connect Telegram" again to get a fresh link.`)
				msg.ParseMode = "Markdown"
				bot.Send(msg)
				return
			}

			// Valid token! Proceed to link
			tenant.OwnerTelegramID = chatID
			tenant.TelegramConnectToken = "" // Clear used token
			tenant.TelegramConnectExpires = nil

			// Save the update
			if err := h.DB.Save(&tenant).Error; err == nil {
				h.Manager.Logger.Info("✅ Telegram account linked successfully",
					zap.Stringer("tenant_id", tenant.ID),
					zap.Int64("chat_id", chatID))

				// Update session to ask for bot token
				state["step"] = "ask_token"
				state["tenant_id"] = tenant.ID.String()
				state["email"] = tenant.Email
				h.sessions.Store(chatID, state)

				msg := tgbotapi.NewMessage(chatID, fmt.Sprintf(`✅ *Account linked!*

Welcome, %s!

Now let's connect your Telegram bot. Here's how:
1️⃣ Open @BotFather
2️⃣ Type /newbot (or /mybots if you have one)
3️⃣ Copy the bot token and paste it here

It looks like: '123456789:ABCdef...'`, tenant.Name))
				msg.ParseMode = "Markdown"
				bot.Send(msg)
				return
			}
		}
	}

	if text == "/start" && step == "ask_email" {
		welcome := `Hi! 👋 Welcome to Brancr.

I'm here to help you connect your AI assistant to your account.

*First step: do you have a Brancr account yet?*
If not, head over to https://www.brancr.com to sign up fast.

Once you're ready, what's the *email address* you used to sign up?`

		msg := tgbotapi.NewMessage(chatID, welcome)
		msg.ParseMode = "Markdown"
		msg.DisableWebPagePreview = true
		bot.Send(msg)
		return
	}

	switch step {

	case "ask_email":
		email := strings.TrimSpace(text)
		if email == "" {
			msg := tgbotapi.NewMessage(chatID, "Please provide your email address or type /start to begin.")
			bot.Send(msg)
			return
		}

		// Check if tenant exists with this email
		var tenant storage.Tenant
		if err := h.DB.Where("LOWER(email) = ?", strings.ToLower(email)).First(&tenant).Error; err != nil {
			signupMsg := fmt.Sprintf(`I couldn't find an account with email *%s*.

Please sign up at the dashboard first:
📱 https://www.brancr.com

Once you've created your account, come back and type /start to link your bot.`, email)
			msg := tgbotapi.NewMessage(chatID, signupMsg)
			msg.ParseMode = "Markdown"
			msg.DisableWebPagePreview = true
			bot.Send(msg)
			h.sessions.Delete(chatID)
			return
		}

		// Check if already linked
		if tenant.OwnerTelegramID == chatID && tenant.TelegramBotToken != "" {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Your bot is already linked to %s!", tenant.Name))
			bot.Send(msg)
			h.sessions.Delete(chatID)
			return
		}

		// Store email and move to token step
		state["email"] = email
		state["step"] = "ask_token"
		h.sessions.Store(chatID, state)

		tokenHelp := `Great! Found your account: *%s*

Now, let's bring your personalized assistant to life.

1.  Open @BotFather
2.  Send */newbot* and give it a name (e.g., "%s Assistant")
3.  Copy the *API Token* it gives you (looks like ` + "`123:ABC...`" + `)
4.  Paste that token here.`

		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf(strings.TrimSpace(tokenHelp), tenant.Name, tenant.Name))
		msg.ParseMode = "Markdown"
		bot.Send(msg)
		return

	case "ask_token":
		token := strings.TrimSpace(text)
		if token == "" {
			msg := tgbotapi.NewMessage(chatID, "Please paste your bot token from @BotFather.")
			bot.Send(msg)
			return
		}

		email := state["email"]

		loading := tgbotapi.NewMessage(chatID, "✨ Connecting your assistant...")
		bot.Send(loading)
		bot.Send(tgbotapi.NewChatAction(chatID, "typing"))

		tenant, err := onboarding.LinkBotToTenant(h.DB, chatID, email, token)
		if err != nil {
			h.Manager.Logger.Error("bot linking failed", zap.Error(err))
			errorMsg := fmt.Sprintf("❌ Couldn't link your bot: %s\n\nMake sure:\n• The bot is started in Telegram\n• Privacy mode is turned off\n• The token is correct", err.Error())
			msg := tgbotapi.NewMessage(chatID, errorMsg)
			bot.Send(msg)
			h.sessions.Delete(chatID)
			return
		}

		// Decrypt token to get bot username for display
		decrypted, err := utils.DecryptToken(tenant.TelegramBotToken)
		var handle string
		botUsername := ""

		if err != nil {
			// Log decryption failure but don't block onboarding
			if h.Manager.Logger != nil {
				h.Manager.Logger.Warn("failed to decrypt bot token for display",
					zap.Stringer("tenant_id", tenant.ID),
					zap.String("tenant_name", tenant.Name),
					zap.Error(err))
			}
			handle = "your assistant"
		} else {
			// Try to get bot info
			tbot, botErr := tgbotapi.NewBotAPI(decrypted)
			if botErr == nil && tbot.Self.UserName != "" {
				botUsername = tbot.Self.UserName
				handle = fmt.Sprintf("[@%s](https://t.me/%s)", botUsername, botUsername)
			} else {
				handle = "your assistant"
			}
		}

		success := tgbotapi.NewMessage(chatID,
			fmt.Sprintf("🚀 *You're all set!* Your assistant for %s is online.\n\nType 'menu' to your new assistant %s to start managing your business.", tenant.Name, handle))
		success.ParseMode = "Markdown"
		bot.Send(success)

		// Trigger alerts for Telegram connection and onboarding complete
		if h.AlertTriggers != nil {
			h.AlertTriggers.OnTelegramConnected(context.Background(), tenant.ID, tenant.Name, botUsername)
			h.AlertTriggers.OnOnboardingComplete(context.Background(), tenant.ID, tenant.Name)
		}

		// Note: removed unnecessary "voice" prompt - let them explore the menu first
		h.sessions.Delete(chatID)

		controlCenter := tgbotapi.NewMessage(chatID,
			"💡 *Quick Tip:*\nAccess your *Control Center* anytime by typing *'menu'* to your assistant.")
		controlCenter.ParseMode = "Markdown"
		bot.Send(controlCenter)

		h.sessions.Delete(chatID)

		// Start the tenant bot
		if err := h.Manager.StartBotForTenant(tenant); err != nil {
			h.Manager.Logger.Error("failed to start tenant bot", zap.Error(err))
			bot.Send(tgbotapi.NewMessage(chatID, "Setup finished, but I couldn't start the assistant. Ping support and we'll take care of it."))
			return
		}

		tenantBot := h.Manager.WaitForBot(tenant.ID, 5*time.Second)
		if tenantBot == nil {
			h.Manager.Logger.Warn("tenant bot not ready after timeout", zap.Stringer("tenant_id", tenant.ID))
			bot.Send(tgbotapi.NewMessage(chatID, "Your assistant is still warming up. Give it a moment and message your bot to get started."))
			return
		}

		ownerID := tenant.OwnerTelegramID

		// Check if onboarding is complete (tenant signed up via dashboard)
		var persona storage.Persona
		var businessProfile storage.BusinessProfile
		_ = h.DB.Where("tenant_id = ?", tenant.ID).First(&persona).Error
		_ = h.DB.Where("tenant_id = ?", tenant.ID).First(&businessProfile).Error

		onboardingComplete := tenant.OnboardingComplete && persona.BotName != "" && businessProfile.Niche != ""

		if botUsername != "" {
			startLink := fmt.Sprintf("https://t.me/%s?start=hello", botUsername)
			bot.Send(tgbotapi.NewMessage(chatID,
				fmt.Sprintf("✨ Your assistant is ready! [Click here to chat with %s](%s)", botUsername, startLink)))
		}

		if onboardingComplete {
			// Onboarding already done via dashboard - send professional welcome message
			personaName := persona.BotName
			if personaName == "" {
				personaName = "Assistant"
			}

			tone := persona.Tone
			if tone == "" {
				tone = "friendly"
			}

			businessName := businessProfile.Name
			if businessName == "" {
				businessName = tenant.Name
			}

			welcomeMsg := fmt.Sprintf(
				"Welcome back, *%s*! 🎉\n\n"+
					"Your AI assistant is configured as *%s* with a *%s* tone.\n\n"+
					"🎛️ *Brancr Control Center*\n"+
					"Your assistant is ready to help customers. You can:\n\n"+
					"• Manage social connections\n"+
					"• View analytics\n"+
					"• Configure alerts\n"+
					"• Update settings\n\n"+
					"Type *menu* to see all available commands.",
				businessName, personaName, tone)

			msg := tgbotapi.NewMessage(ownerID, welcomeMsg)
			msg.ParseMode = "Markdown"
			tenantBot.Send(msg)

			// Show connection status if available
			var accounts []storage.SocialAccount
			h.DB.Where("tenant_id = ? AND connected = ?", tenant.ID, true).Find(&accounts)
			if len(accounts) > 0 {
				connections := "📱 *Connected Accounts:*\n"
				for _, acc := range accounts {
					status := "✅"
					if !acc.Connected {
						status = "❌"
					}
					connections += fmt.Sprintf("%s %s", status, strings.Title(acc.Platform))
					if acc.Username != "" {
						connections += fmt.Sprintf(" (@%s)", acc.Username)
					}
					connections += "\n"
				}
				connMsg := tgbotapi.NewMessage(ownerID, connections)
				connMsg.ParseMode = "Markdown"
				tenantBot.Send(connMsg)
			}
		} else {
			// Legacy flow: onboarding not complete, ask persona questions
			intro := tgbotapi.NewMessage(ownerID,
				fmt.Sprintf("Hey! I'm your new assistant for %s. What should I call myself?", tenant.Name))
			tenantBot.Send(intro)

			followUp := tgbotapi.NewMessage(ownerID, "You can reply with the style, tone, or any must-know info so I sound on-brand. Type *menu* anytime to see what I can do.")
			followUp.ParseMode = "Markdown"
			tenantBot.Send(followUp)

			go ai.StartPersonaOnboarding(ctx, h.DB, tenantBot, tenant.ID, ownerID, h.Manager.Responder.Client, h.Manager.Logger)
		}
		return

	default:
		bot.Send(tgbotapi.NewMessage(chatID, "Type /start to link your Telegram bot to your Brancr account."))
	}
}
