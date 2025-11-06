package bot

import (
	"fmt"
	"log"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/storage"
)

func SendDailySummary(manager *TenantBotManager) {
	var tenants []storage.Tenant
	if err := manager.DB.Find(&tenants).Error; err != nil {
		log.Printf("❌ Failed to fetch tenants: %v", err)
		return
	}

	for _, t := range tenants {
		bot, ok := manager.GetBot(t.ID)
		if !ok {
			continue
		}

		var counts struct {
			Payments, Deliveries, Appointments int64
		}
		manager.DB.Model(&storage.EventLog{}).
			Where("tenant_id = ? AND created_at > ?", t.ID, time.Now().Add(-24*time.Hour)).
			Select("SUM(CASE WHEN event_type = 'payment' THEN 1 ELSE 0 END) as payments," +
				"SUM(CASE WHEN event_type = 'delivery' THEN 1 ELSE 0 END) as deliveries," +
				"SUM(CASE WHEN event_type = 'appointment' THEN 1 ELSE 0 END) as appointments").
			Scan(&counts)

		if counts.Payments == 0 && counts.Deliveries == 0 && counts.Appointments == 0 {
			continue
		}

		summary := fmt.Sprintf(
			"🕓 *Daily Summary*\n\n💰 Payments: *%d*\n📦 Deliveries: *%d*\n🗓 Appointments: *%d*\n\n_Date: %s_",
			counts.Payments, counts.Deliveries, counts.Appointments, time.Now().Format("02 Jan 2006"))

		msg := tgbotapi.NewMessage(t.OwnerTelegramID, summary)
		msg.ParseMode = "Markdown"
		bot.Send(msg)
	}
}
