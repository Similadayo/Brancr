package bot

import (
	"fmt"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// 🔎 Helper – simple keyword detector (Phase 7 logic)
func detectEventType(text string) string {
	t := strings.ToLower(text)
	switch {
	case strings.Contains(t, "pay"), strings.Contains(t, "paid"), strings.Contains(t, "invoice"):
		return "payment"
	case strings.Contains(t, "deliver"), strings.Contains(t, "pickup"), strings.Contains(t, "ship"), strings.Contains(t, "order"):
		return "delivery"
	case strings.Contains(t, "appointment"), strings.Contains(t, "book"), strings.Contains(t, "schedule"), strings.Contains(t, "meeting"):
		return "appointment"
	default:
		return "general"
	}
}

// Handle a possible event alert
func HandleTenantAlert(db *gorm.DB, tenant *storage.Tenant, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	eventType := detectEventType(msg.Text)
	if eventType == "" {
		return
	}

	// store in DB
	ev := storage.EventLog{
		TenantID:   tenant.ID,
		EventType:  eventType,
		SenderName: msg.From.UserName,
		Message:    msg.Text,
		Forwarded:  false,
	}
	if err := db.Create(&ev).Error; err != nil {
		log.Printf("❌ failed to log event: %v", err)
		return
	}

	// forward to owner control center
	alert := fmt.Sprintf(
		"🚨 *%s Alert*\n\n👤 Customer: @%s\n💬 Message: %s\n\nReply here once handled.",
		strings.Title(eventType),
		msg.From.UserName,
		msg.Text,
	)

	msgCfg := tgbotapi.NewMessage(tenant.OwnerTelegramID, alert)
	msgCfg.ParseMode = "Markdown"
	if _, err := bot.Send(msgCfg); err != nil {
		log.Printf("⚠️ failed to forward alert to owner: %v", err)
		return
	}

	// mark forwarded
	ev.Forwarded = true
	db.Save(&ev)

	log.Printf("📢 forwarded %s alert to owner (tenant %s)", eventType, tenant.Name)
}
