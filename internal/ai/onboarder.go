package ai

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/onboarding"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// 🧠 Global state for AI onboarding sessions
var OnboardingSessions sync.Map // chatID → map[string]string

// StartAIOnboarding launches conversational onboarding (called only once at /start)
func StartAIOnboarding(ctx context.Context, db *gorm.DB, bot *tgbotapi.BotAPI, chatID int64, client *AIClient, logger *zap.Logger) {
	session := map[string]string{"step": "ask_business_name"}
	OnboardingSessions.Store(chatID, session)

	bot.Send(tgbotapi.NewMessage(chatID, "Hey! I'm Brancr. Let's set up your business.\n\nWhat's your business name?"))
}

// ContinueAIOnboarding handles next questions dynamically
func ContinueAIOnboarding(ctx context.Context, db *gorm.DB, bot *tgbotapi.BotAPI, chatID int64, text string, client *AIClient, logger *zap.Logger) {
	raw, _ := OnboardingSessions.Load(chatID)
	state, _ := raw.(map[string]string)
	if state == nil {
		StartAIOnboarding(ctx, db, bot, chatID, client, logger)
		return
	}

	step := state["step"]
	text = strings.TrimSpace(text)

	switch step {

	case "ask_business_name":
		state["business_name"] = text
		state["step"] = "ask_email"
		OnboardingSessions.Store(chatID, state)

		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Cool. What's your business email for %s?", text)))
		return

	case "ask_email":
		state["email"] = text
		state["step"] = "ask_token"
		OnboardingSessions.Store(chatID, state)

		bot.Send(tgbotapi.NewMessage(chatID, "Got it. Now send me your Telegram bot token (from @BotFather)."))
		return

	case "ask_token":
		state["token"] = text
		OnboardingSessions.Store(chatID, state)

		bot.Send(tgbotapi.NewChatAction(chatID, "typing"))

		business := state["business_name"]
		token := state["token"]

		// ✅ Register Tenant using centralized helper (handles bot verification + webhook)
		tenant, err := onboarding.RegisterTenant(db, chatID, business, token)
		if err != nil {
			logger.Error("tenant registration failed", zap.Error(err))
			msg := fmt.Sprintf("Couldn't finish setup: %v\n\nCheck your bot token and try again.", err)
			bot.Send(tgbotapi.NewMessage(chatID, msg))
			return
		}

		OnboardingSessions.Delete(chatID)

		// ✅ Confirmation Message
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Done! %s is connected. You can schedule posts, manage AI replies, and view analytics.\n\nLet's personalize your assistant now.", business)))

		// ⏳ Wait briefly for clarity before starting persona onboarding
		time.Sleep(2 * time.Second)

		// ✅ Automatically start persona onboarding
		go func() {
			StartPersonaOnboarding(ctx, db, bot, tenant.ID, chatID, client, logger)
		}()
		return
	}
}
