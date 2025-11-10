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

// ✅ Handles AI onboarding flow messages
func (h *OnboardingHandler) HandleOnboarding(bot *tgbotapi.BotAPI, update *tgbotapi.Update) {
	if update.Message == nil || update.Message.Text == "" {
		return
	}

	chatID := update.Message.Chat.ID
	text := strings.TrimSpace(update.Message.Text)
	ctx := context.Background()

	// ✅ Check if tenant already exists
	var existing storage.Tenant
	if err := h.DB.Where("owner_telegram_id = ?", chatID).First(&existing).Error; err == nil {
		msg := tgbotapi.NewMessage(chatID,
			fmt.Sprintf("Hey %s, you're already set up!", existing.Name))
		bot.Send(msg)
		return
	}

	// 🧠 Load or initialize onboarding session
	stateRaw, _ := h.sessions.LoadOrStore(chatID, map[string]string{"step": "ask_business_name"})
	state, _ := stateRaw.(map[string]string)
	step := state["step"]

	if text == "/start" && step == "ask_business_name" {
		msg := tgbotapi.NewMessage(chatID,
			"Hey there! I’m Brancr — your AI automation partner. Let’s get your business assistant running in under a minute.\n\nFirst up, what’s your business called?")
		bot.Send(msg)
		return
	}

	switch step {

	case "ask_business_name":
		state["business_name"] = text
		state["step"] = "ask_email"
		h.sessions.Store(chatID, state)

		msg := tgbotapi.NewMessage(chatID, "Beautiful. Do you have a business email you’d like me to keep on file? (You can reply with your email or type *skip*.)")
		msg.ParseMode = "Markdown"
		bot.Send(msg)
		return

	case "ask_email":
		if strings.EqualFold(text, "skip") {
			state["email"] = ""
		} else {
			state["email"] = text
		}
		state["step"] = "ask_token"
		h.sessions.Store(chatID, state)
		bot.Send(tgbotapi.NewMessage(chatID, "Great! Now paste your Telegram bot token from @BotFather so I can connect to your assistant."))
		return

	case "ask_token":
		state["token"] = text
		h.sessions.Store(chatID, state)

		business := state["business_name"]
		token := state["token"]

		loading := tgbotapi.NewMessage(chatID, "✨ Setting things up… give me a second while I connect everything.")
		bot.Send(loading)

		bot.Send(tgbotapi.NewChatAction(chatID, "typing"))

		tenant, err := onboarding.RegisterTenant(h.DB, chatID, business, state["email"], token)
		if err != nil {
			h.Manager.Logger.Error("tenant registration failed", zap.Error(err))
			msg := tgbotapi.NewMessage(chatID, "Hmm, I couldn’t connect with that token.\n\nMake sure the bot is started in Telegram, privacy mode is turned off, and try pasting the token again.")
			bot.Send(msg)
			return
		}

		decrypted, _ := utils.DecryptToken(tenant.TelegramBotToken)
		tbot, err := tgbotapi.NewBotAPI(decrypted)
		var handle string
		if err == nil && tbot.Self.UserName != "" {
			handle = fmt.Sprintf("[@%s](https://t.me/%s)", tbot.Self.UserName, tbot.Self.UserName)
		} else {
			handle = "your new bot"
		}

		success := tgbotapi.NewMessage(chatID,
			fmt.Sprintf("🎉 Perfect! %s is now live.\nYour assistant %s is connected and ready to help.", business, handle))
		success.ParseMode = "Markdown"
		bot.Send(success)

		next := tgbotapi.NewMessage(chatID,
			"Next, let’s give your assistant a voice that matches your brand. I’ll send a quick intro to your bot so we can personalize everything.")
		bot.Send(next)

		menu := tgbotapi.NewMessage(chatID,
			"🎛️ *Brancr Control Center*\nWhen you’re ready, you can:\n\n• Connect social accounts\n• Manage your business info\n• View analytics\n• Customize how your assistant speaks\n• Get help and tutorials\n\nJust message your assistant and say “menu” any time to open these options.")
		menu.ParseMode = "Markdown"
		bot.Send(menu)

		h.sessions.Delete(chatID)

		if err := h.Manager.StartBotForTenant(tenant); err != nil {
			h.Manager.Logger.Error("failed to start tenant bot", zap.Error(err))
			bot.Send(tgbotapi.NewMessage(chatID, "Setup finished, but I couldn’t start the assistant. Ping support and we’ll take care of it."))
			return
		}

		tenantBot := h.Manager.WaitForBot(tenant.ID, 5*time.Second)
		if tenantBot == nil {
			h.Manager.Logger.Warn("tenant bot not ready after timeout", zap.Uint("tenant_id", tenant.ID))
			bot.Send(tgbotapi.NewMessage(chatID, "Your assistant is still warming up. Give it a moment and say hi again."))
			return
		}

		ownerID := tenant.OwnerTelegramID
		intro := tgbotapi.NewMessage(ownerID,
			fmt.Sprintf("Hey! I’m your new assistant for %s. What should I call myself?", tenant.Name))
		tenantBot.Send(intro)

		go ai.StartPersonaOnboarding(ctx, h.DB, tenantBot, tenant.ID, ownerID, h.Manager.Responder.Client, h.Manager.Logger)
		return

	default:
		bot.Send(tgbotapi.NewMessage(chatID, "Type /start whenever you’re ready to set up your business assistant."))
	}
}
