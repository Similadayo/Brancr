package bot

import (
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/storage"
)

// BusinessSession tracks state for multi-step business profile updates.
type BusinessSession struct {
	Mode     string
	Step     int
	MenuItem storage.BusinessMenuItem
	FAQ      storage.BusinessFAQ
}

var businessSessions = make(map[int64]*BusinessSession)

func startBusinessSession(chatID int64, mode string) *BusinessSession {
	sess := &BusinessSession{
		Mode: mode,
		Step: 1,
	}
	businessSessions[chatID] = sess
	return sess
}

func clearBusinessSession(chatID int64) {
	delete(businessSessions, chatID)
}

func cancelBusinessSession(bot *tgbotapi.BotAPI, chatID int64) {
	clearBusinessSession(chatID)
	msg := tgbotapi.NewMessage(chatID, "Cancelled. Nothing was changed.")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Back to menu ⬅️", "menu:main"),
		),
	)
	bot.Send(msg)
}

func normalizeInput(text string) string {
	return strings.TrimSpace(text)
}
