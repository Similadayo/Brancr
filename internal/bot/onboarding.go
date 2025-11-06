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
			"Hey! I'm Brancr. Let's set up your business.\n\nWhat's your business name?")
		bot.Send(msg)
		return
	}

	switch step {

	case "ask_business_name":
		state["business_name"] = text
		state["step"] = "ask_email"
		h.sessions.Store(chatID, state)
		bot.Send(tgbotapi.NewMessage(chatID, "Cool. What's your business email?"))
		return

	case "ask_email":
		state["email"] = text
		state["step"] = "ask_token"
		h.sessions.Store(chatID, state)
		bot.Send(tgbotapi.NewMessage(chatID, "Got it. Now send me your Telegram bot token (from @BotFather)."))
		return

	case "ask_token":
		bot.Send(tgbotapi.NewChatAction(chatID, "typing"))
		state["token"] = text
		h.sessions.Store(chatID, state)

		business := state["business_name"]
		token := state["token"]

		// ✅ Register tenant (verifies bot token & sets webhook)
		tenant, err := onboarding.RegisterTenant(h.DB, chatID, business, token)
		if err != nil {
			h.Manager.Logger.Error("tenant registration failed", zap.Error(err))
			msg := tgbotapi.NewMessage(chatID, "Couldn't complete setup. Check your bot token and try again.")
			bot.Send(msg)
			return
		}

		// ✅ Fetch bot username for display
		decrypted, _ := utils.DecryptToken(tenant.TelegramBotToken)
		tbot, err := tgbotapi.NewBotAPI(decrypted)
		var handle string
		if err == nil && tbot.Self.UserName != "" {
			handle = fmt.Sprintf("[@%s](https://t.me/%s)", tbot.Self.UserName, tbot.Self.UserName)
		} else {
			handle = "your new bot"
		}

		// ✅ Confirmation message with clickable handle
		confirm := tgbotapi.NewMessage(chatID,
			fmt.Sprintf("Done! %s is connected. Your assistant %s is live.\n\nYou can schedule posts, manage AI replies, and view analytics.\n\nLet's personalize your assistant now.", business, handle))
		confirm.ParseMode = "Markdown"
		bot.Send(confirm)

		// ✅ Clear session
		h.sessions.Delete(chatID)

		// ✅ Start tenant bot instance
		if err := h.Manager.StartBotForTenant(tenant); err != nil {
			h.Manager.Logger.Error("failed to start tenant bot", zap.Error(err))
			bot.Send(tgbotapi.NewMessage(chatID, "Connected, but couldn't start your bot. Contact support."))
			return
		}

		// ✅ Wait until tenant bot is ready (fixes race condition)
		tenantBot := h.Manager.WaitForBot(tenant.ID, 5*time.Second)
		if tenantBot == nil {
			h.Manager.Logger.Warn("tenant bot not ready after timeout", zap.Uint("tenant_id", tenant.ID))
			bot.Send(tgbotapi.NewMessage(chatID, "Still setting up, wait a few seconds and try again."))
			return
		}

		// ✅ Start persona onboarding INSIDE tenant bot
		ownerID := tenant.OwnerTelegramID
		intro := tgbotapi.NewMessage(ownerID,
			fmt.Sprintf("Hey %s! Let's personalize how I sound. What do you want to call me?", tenant.Name))
		tenantBot.Send(intro)

		go ai.StartPersonaOnboarding(ctx, h.DB, tenantBot, tenant.ID, ownerID, h.Manager.Responder.Client, h.Manager.Logger)
		return

	default:
		bot.Send(tgbotapi.NewMessage(chatID, "Type /start to set up your business."))
	}
}
