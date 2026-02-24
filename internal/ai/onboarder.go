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

	bot.Send(tgbotapi.NewMessage(chatID, "Hey there! I'm Brancr - ready to spin up your business assistant. What's the name of your business?"))
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

		msg := fmt.Sprintf("Love it. Do you have a business email for %s? (Reply with the email or type *skip*.)", text)
		out := tgbotapi.NewMessage(chatID, msg)
		out.ParseMode = "Markdown"
		bot.Send(out)
		return

	case "ask_email":
		if strings.EqualFold(text, "skip") {
			state["email"] = ""
		} else {
			state["email"] = text
		}
		state["step"] = "ask_token"
		OnboardingSessions.Store(chatID, state)

		bot.Send(tgbotapi.NewMessage(chatID, "Great! Drop your Telegram bot token from @BotFather and I’ll connect straight away."))
		return

	case "ask_token":
		state["token"] = text
		OnboardingSessions.Store(chatID, state)

		bot.Send(tgbotapi.NewChatAction(chatID, "typing"))

		business := state["business_name"]
		token := state["token"]

		bot.Send(tgbotapi.NewMessage(chatID, "✨ Nice! I'm plugging everything in now - hang tight."))

		tenant, err := onboarding.RegisterTenant(db, chatID, business, state["email"], token)
		if err != nil {
			logger.Error("tenant registration failed", zap.Error(err))
			msg := "Hmm, I couldn’t connect with that token. Make sure your bot is active in Telegram, privacy mode is off, and try pasting the token again."
			bot.Send(tgbotapi.NewMessage(chatID, msg))
			return
		}

		OnboardingSessions.Delete(chatID)

		// ✅ Confirmation Message
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🎉 All set! %s is live and ready to work.", business)))
		bot.Send(tgbotapi.NewMessage(chatID, "Next up, we’ll teach your assistant how to speak like your brand."))

		control := tgbotapi.NewMessage(chatID,
			"🎛️ *Brancr Control Center*\nWhenever you say “menu” to your assistant you can:\n\n• Connect social accounts\n• Manage business info\n• Peek at analytics\n• Customize AI responses\n• Grab tutorials\n\nPing your assistant anytime to explore.")
		control.ParseMode = "Markdown"
		bot.Send(control)

		// ⏳ Wait briefly for clarity before starting persona onboarding
		time.Sleep(2 * time.Second)

		// ✅ Automatically start persona onboarding
		go func() {
			StartPersonaOnboarding(ctx, db, bot, tenant.ID, chatID, client, logger)
		}()
		return
	}
}
