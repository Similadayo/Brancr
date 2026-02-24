package bot

import (
	"context"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
)

// BusinessSession tracks state for multi-step business profile updates.
type BusinessSession struct {
	Mode     string
	Step     int
	ItemType string // "menu", "product", or "service" - determines which item field is used
	MenuItem storage.MenuItem
	Product  storage.Product
	Service  storage.Service
	FAQ      storage.BusinessFAQ
}

// Note: businessSessions is now database-backed via SessionManager
// These functions are kept for backward compatibility but require TenantBotManager
var globalBusinessSessionManager *SessionManager

func setGlobalBusinessSessionManager(sm *SessionManager) {
	globalBusinessSessionManager = sm
}

func startBusinessSession(chatID int64, tenantID uuid.UUID, mode string, sm *SessionManager) *BusinessSession {
	sess := &BusinessSession{
		Mode: mode,
		Step: 1,
	}
	if sm != nil {
		sm.SaveBusinessSession(context.Background(), chatID, tenantID, sess)
	}
	return sess
}

func clearBusinessSession(chatID int64, sm *SessionManager) {
	if sm != nil {
		sm.DeleteBusinessSession(context.Background(), chatID)
	}
}

func cancelBusinessSession(bot *tgbotapi.BotAPI, chatID int64, sm *SessionManager) {
	if sm != nil {
		clearBusinessSession(chatID, sm)
	}
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
