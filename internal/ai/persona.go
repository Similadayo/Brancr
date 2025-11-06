package ai

import (
	"context"
	"fmt"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// PersonaSessions holds state for ongoing onboarding chats
var PersonaSessions sync.Map // chatID → map[string]string{"step": "intro"}

// GetPersona safely returns a tenant’s persona (empty if none)
func GetPersona(db *gorm.DB, tenantID uint) storage.Persona {
	var p storage.Persona
	if err := db.First(&p, "tenant_id = ?", tenantID).Error; err != nil {
		return storage.Persona{}
	}
	return p
}

// 🧠 StartPersonaOnboarding begins a warm, brand-aligned AI persona setup
func StartPersonaOnboarding(ctx context.Context, db *gorm.DB, bot *tgbotapi.BotAPI, tenantID uint, chatID int64, client *AIClient, logger *zap.Logger) {
	existing := GetPersona(db, tenantID)
	if existing.BotName != "" {
		msg := fmt.Sprintf("Hi again! I'm %s, already set up with a %s tone.", existing.BotName, existing.Tone)
		out := tgbotapi.NewMessage(chatID, msg)
		bot.Send(out)
		return
	}

	state := map[string]string{"step": "ask_name"}
	PersonaSessions.Store(chatID, state)

	bot.Send(tgbotapi.NewMessage(chatID, "What should I call myself? (e.g., Luna, Mimi, or BossBot)"))
}

// 🧭 ContinuePersonaOnboarding dynamically moves through persona-building flow
func ContinuePersonaOnboarding(ctx context.Context, db *gorm.DB, bot *tgbotapi.BotAPI, tenantID uint, chatID int64, text string, client *AIClient, logger *zap.Logger) {
	raw, ok := PersonaSessions.Load(chatID)
	if !ok {
		StartPersonaOnboarding(ctx, db, bot, tenantID, chatID, client, logger)
		return
	}

	state := raw.(map[string]string)
	step := state["step"]
	text = strings.TrimSpace(text)

	switch step {
	// --------------------------------------------
	case "ask_name":
		state["bot_name"] = text
		state["step"] = "ask_tone"
		PersonaSessions.Store(chatID, state)

		reply := fmt.Sprintf("Cool, %s it is. How should %s sound when talking to customers? (e.g., friendly, calm, confident, witty)", text, text)
		bot.Send(tgbotapi.NewMessage(chatID, reply))
		return

	// --------------------------------------------
	case "ask_tone":
		state["tone"] = text
		state["step"] = "ask_humor"
		PersonaSessions.Store(chatID, state)

		bot.Send(tgbotapi.NewMessage(chatID, "Got it. Should I have a sense of humor? (yes/no)"))
		return

	// --------------------------------------------
	case "ask_humor":
		humor := strings.Contains(strings.ToLower(text), "y")
		state["humor"] = fmt.Sprintf("%v", humor)
		state["step"] = "ask_style"
		PersonaSessions.Store(chatID, state)

		bot.Send(tgbotapi.NewMessage(chatID, "Last thing - describe my vibe. (e.g., luxury, bold, youthful, corporate, relaxed)"))
		return

	// --------------------------------------------
	case "ask_style":
		state["style"] = text
		botName := state["bot_name"]
		tone := state["tone"]
		humor := state["humor"] == "true"
		style := state["style"]

		persona := storage.Persona{
			TenantID:   tenantID,
			BotName:    botName,
			Tone:       tone,
			Language:   "en",
			Humor:      humor,
			StyleNotes: style,
		}

		if err := db.Where("tenant_id = ?", tenantID).Assign(persona).FirstOrCreate(&persona).Error; err != nil {
			logger.Error("failed to save persona", zap.Error(err))
			bot.Send(tgbotapi.NewMessage(chatID, "Something went wrong saving that. Try again later."))
			return
		}

		PersonaSessions.Delete(chatID)

		reply := fmt.Sprintf("Done! I'm %s now.\nTone: %s | Humor: %v | Style: %s\n\nReady to go.", botName, tone, humor, style)
		bot.Send(tgbotapi.NewMessage(chatID, reply))
	}
	go StartBusinessProfileOnboarding(ctx, db, bot, tenantID, chatID, client, logger)
}
