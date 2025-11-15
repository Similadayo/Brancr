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
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/onboarding"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// 🧠 Conversational AI-driven Onboarding Handler
type OnboardingHandler struct {
	DB       *gorm.DB
	Manager  *TenantBotManager
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
		msg := tgbotapi.NewMessage(chatID,
				fmt.Sprintf("Hey %s! Your bot is already set up. Type 'menu' to your assistant to get started.", existing.Name))
		bot.Send(msg)
		return
		}
	}

	// 🧠 Load or initialize onboarding session (simplified: just email + token)
	stateRaw, _ := h.sessions.LoadOrStore(chatID, map[string]string{"step": "ask_email"})
	state, _ := stateRaw.(map[string]string)
	step := state["step"]

	if text == "/start" && step == "ask_email" {
		welcome := `Hey there! 👋 I'm Brancr.

To connect your Telegram bot, I need to link it to your Brancr account.

*First, please sign up at the dashboard if you haven't already:*
📱 Visit: https://www.brancr.com

Once you've signed up, I'll help you connect your bot here. What's the email address you used to sign up?`

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

		tokenHelp := `Perfect! Found your account: *%s*

Now let's connect your Telegram bot. Get your bot token from *@BotFather*:

1. Open Telegram and message **@BotFather**
2. Send */newbot* and follow the prompts (give it a display name + username)
3. BotFather replies with a token like ` + "`123456:ABC-DEF...`" + `
4. Copy that token and paste it here

Already have a token? Just paste it now.`

		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf(strings.TrimSpace(tokenHelp), tenant.Name))
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

		loading := tgbotapi.NewMessage(chatID, "✨ Linking your bot… give me a second.")
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

		decrypted, _ := utils.DecryptToken(tenant.TelegramBotToken)
		tbot, err := tgbotapi.NewBotAPI(decrypted)
		var handle string
		botUsername := ""
		if err == nil && tbot.Self.UserName != "" {
			botUsername = tbot.Self.UserName
			handle = fmt.Sprintf("[@%s](https://t.me/%s)", botUsername, botUsername)
		} else {
			handle = "your assistant"
		}

		success := tgbotapi.NewMessage(chatID,
			fmt.Sprintf("🎉 Perfect! Your bot is now linked to %s.\n\nYour assistant %s is connected and ready to help.", tenant.Name, handle))
		success.ParseMode = "Markdown"
		bot.Send(success)

		next := tgbotapi.NewMessage(chatID,
			"Let's give your assistant a voice that matches your brand. I'll nudge them in a second so you can personalize everything.")
		bot.Send(next)

		controlCenter := tgbotapi.NewMessage(chatID,
			"🎛️ *Brancr Control Center*\nWhen you're ready, you can:\n\n• Connect social accounts\n• Manage your business info\n• View analytics\n• Customize how your assistant speaks\n• Get help and tutorials\n\nSay \"menu\" to your assistant anytime to pull this back up.")
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
			h.Manager.Logger.Warn("tenant bot not ready after timeout", zap.Uint("tenant_id", tenant.ID))
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
