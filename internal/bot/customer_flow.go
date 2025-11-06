package bot

import (
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/storage"
)

func (m *TenantBotManager) HandleCustomerMessage(bot *tgbotapi.BotAPI, tenantID uint, update tgbotapi.Update) {
	msg := update.Message
	if msg == nil {
		return
	}

	// log customer inquiry
	m.DB.Create(&storage.EventLog{
		TenantID:   tenantID,
		EventType:  "customer_chat",
		SenderName: msg.From.UserName,
		Message:    msg.Text,
	})

	// Optional: use your AI responder to auto-reply
	reply := fmt.Sprintf("Thanks %s, got your message. We'll get back to you soon.", msg.From.FirstName)
	bot.Send(tgbotapi.NewMessage(msg.Chat.ID, reply))
}
