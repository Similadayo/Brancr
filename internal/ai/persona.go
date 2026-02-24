package ai

import (
	"github.com/google/uuid"
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
func GetPersona(db *gorm.DB, tenantID uuid.UUID) storage.Persona {
	var p storage.Persona
	if err := db.First(&p, "tenant_id = ?", tenantID).Error; err != nil {
		return storage.Persona{}
	}
	return p
}

// 🧠 StartPersonaOnboarding begins a warm, brand-aligned AI persona setup
func StartPersonaOnboarding(ctx context.Context, db *gorm.DB, bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, client *AIClient, logger *zap.Logger) {
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
func ContinuePersonaOnboarding(ctx context.Context, db *gorm.DB, bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, text string, client *AIClient, logger *zap.Logger) {
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

		reply := fmt.Sprintf("Awesome - %s it is 💁🏽‍♀️\n\nHow would you like me to sound?\n• Friendly & confident\n• Calm & kind\n• Bold & witty\n• Corporate & clear", text)
		bot.Send(tgbotapi.NewMessage(chatID, reply))
		return

	// --------------------------------------------
	case "ask_tone":
		canonical, ok := normalizeToneChoice(text)
		if !ok {
			bot.Send(tgbotapi.NewMessage(chatID, "Got it! Pick a style from the list (Friendly & confident, Calm & kind, Bold & witty, Corporate & clear)."))
			return
		}
		state["tone"] = canonical
		state["step"] = "ask_humor"
		PersonaSessions.Store(chatID, state)

		bot.Send(tgbotapi.NewMessage(chatID, "Should I have a sense of humor? (Yes or No)"))
		return

	// --------------------------------------------
	case "ask_humor":
		humor := strings.TrimSpace(strings.ToLower(text))
		if humor != "yes" && humor != "no" && humor != "y" && humor != "n" {
			bot.Send(tgbotapi.NewMessage(chatID, "Just say Yes or No - do you want me to keep things playful?"))
			return
		}
		state["humor"] = fmt.Sprintf("%v", humor == "yes" || humor == "y")
		state["step"] = "ask_style"
		PersonaSessions.Store(chatID, state)

		bot.Send(tgbotapi.NewMessage(chatID, "Last thing - what's the overall vibe?\n• Luxury\n• Youthful\n• Relaxed\n• Corporate\n• Trendy"))
		return

	// --------------------------------------------
	case "ask_style":
		style, ok := normalizeVibeChoice(text)
		if !ok {
			bot.Send(tgbotapi.NewMessage(chatID, "Pick whichever fits best: Luxury, Youthful, Relaxed, Corporate, or Trendy."))
			return
		}
		state["style"] = style
		botName := state["bot_name"]
		tone := state["tone"]
		humor := state["humor"] == "true"
		styleChoice := state["style"]

		persona := storage.Persona{
			TenantID:   tenantID,
			BotName:    botName,
			Tone:       tone,
			Language:   "en",
			Humor:      humor,
			StyleNotes: styleChoice,
		}

		if err := db.Where("tenant_id = ?", tenantID).Assign(persona).FirstOrCreate(&persona).Error; err != nil {
			logger.Error("failed to save persona", zap.Error(err))
			bot.Send(tgbotapi.NewMessage(chatID, "Something went wrong saving that. Try again later."))
			return
		}

		PersonaSessions.Delete(chatID)

		humorLine := "not really into jokes"
		if humor {
			humorLine = "always ready with a light joke"
		}
		summary := fmt.Sprintf("✨ All set!\nI’m %s, your %s assistant with a %s vibe.\nI’m %s and ready to help with content, captions, and customer chats.", botName, tone, styleChoice, humorLine)
		bot.Send(tgbotapi.NewMessage(chatID, summary))

		hint := "Send me any media or say “menu” when you want to connect accounts, schedule posts, or check stats."
		bot.Send(tgbotapi.NewMessage(chatID, hint))
	}
	go StartBusinessProfileOnboarding(ctx, db, bot, tenantID, chatID, client, logger)
}

func normalizeToneChoice(input string) (string, bool) {
	value := strings.ToLower(strings.TrimSpace(input))
	switch value {
	case "friendly & confident", "friendly confident", "friendly", "confident", "friendly and confident":
		return "Friendly & confident", true
	case "calm & kind", "calm kind", "calm", "kind", "calm and kind":
		return "Calm & kind", true
	case "bold & witty", "bold witty", "bold", "witty", "bold and witty":
		return "Bold & witty", true
	case "corporate & clear", "corporate clear", "corporate", "clear", "corporate and clear":
		return "Corporate & clear", true
	default:
		return "", false
	}
}

func normalizeVibeChoice(input string) (string, bool) {
	value := strings.ToLower(strings.TrimSpace(input))
	switch value {
	case "luxury":
		return "Luxury", true
	case "youthful":
		return "Youthful", true
	case "relaxed":
		return "Relaxed", true
	case "corporate":
		return "Corporate", true
	case "trendy":
		return "Trendy", true
	default:
		return "", false
	}
}
