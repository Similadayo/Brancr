package onboarding

import (
	"fmt"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"gorm.io/gorm"
)

func RegisterTenant(db *gorm.DB, ownerTelegramID int64, businessName, email, botToken string) (*storage.Tenant, error) {
	// Prevent duplicates
	var existing storage.Tenant
	if err := db.Where("owner_telegram_id = ?", ownerTelegramID).First(&existing).Error; err == nil {
		return nil, fmt.Errorf("you already have a registered business: %s", existing.Name)
	}

	// Verify bot token validity
	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		return nil, fmt.Errorf("invalid bot token")
	}

	me, err := bot.GetMe()
	if err != nil {
		return nil, fmt.Errorf("unable to verify bot token")
	}
	log.Printf("✅ Verified tenant bot: @%s (ID: %d)", me.UserName, me.ID)

	// Encrypt and store token
	encryptedToken, err := utils.EncryptToken(botToken)
	if err != nil {
		return nil, fmt.Errorf("token encryption failed")
	}

	tenant := storage.Tenant{
		Name:             businessName,
		Email:            strings.TrimSpace(email),
		TelegramBotToken: encryptedToken,
		OwnerTelegramID:  ownerTelegramID,
		PlanType:         "Free",
		Status:           "Active",
	}

	if err := db.Create(&tenant).Error; err != nil {
		return nil, fmt.Errorf("failed to save tenant: %v", err)
	}

	// Business Profile
	profile := storage.BusinessProfile{
		TenantID: tenant.ID,
		Name:     businessName,
		Tone:     "friendly",
	}
	db.Create(&profile)

	publicURL, err := utils.GetPublicURL()
	if err == nil {
		webhook := fmt.Sprintf("%s/bot/tenant/%d/webhook", publicURL, tenant.ID)
		webhookCfg, _ := tgbotapi.NewWebhook(webhook)
		resp, err := bot.Request(webhookCfg)
		if err != nil {
			log.Printf("⚠️ Webhook setup failed for tenant %d: %v", tenant.ID, err)
		} else {
			log.Printf("🌐 Webhook set for tenant %d → %s (%v)", tenant.ID, webhook, resp.Ok)
		}
	}

	log.Printf("✅ Tenant onboarded successfully: %s (ID: %d)", businessName, tenant.ID)
	return &tenant, nil
}
