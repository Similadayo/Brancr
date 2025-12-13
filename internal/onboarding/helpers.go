package onboarding

import (
	"context"
	"fmt"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"gorm.io/gorm"
)

// RegisterTenant creates a new tenant (legacy - used by Telegram bot only if no frontend signup exists)
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
		PlanType:         "trial",
		Status:           "pending_onboarding",
	}

	if err := db.Create(&tenant).Error; err != nil {
		return nil, fmt.Errorf("failed to save tenant: %v", err)
	}

	if err := storage.EnsureTenantOnboarding(context.Background(), db, tenant.ID, businessName); err != nil {
		return nil, fmt.Errorf("failed to initialize onboarding: %v", err)
	}

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

// LinkBotToTenant links a Telegram bot token to an existing tenant account (created via frontend)
func LinkBotToTenant(db *gorm.DB, ownerTelegramID int64, email, botToken string) (*storage.Tenant, error) {
	// Find tenant by email (case-insensitive)
	var tenant storage.Tenant
	if err := db.Where("LOWER(email) = ?", strings.ToLower(strings.TrimSpace(email))).First(&tenant).Error; err != nil {
		return nil, fmt.Errorf("no account found with email %s. Please sign up at the dashboard first", email)
	}

	// Check if tenant already has a bot linked
	if tenant.OwnerTelegramID != 0 && tenant.OwnerTelegramID != ownerTelegramID {
		return nil, fmt.Errorf("this account is already linked to a different Telegram account")
	}

	if tenant.TelegramBotToken != "" {
		// Bot already linked - verify it's the same user
		if tenant.OwnerTelegramID == ownerTelegramID {
			return nil, fmt.Errorf("bot already linked to this account")
		}
	}

	// Verify bot token validity
	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		return nil, fmt.Errorf("invalid bot token: %v", err)
	}

	me, err := bot.GetMe()
	if err != nil {
		return nil, fmt.Errorf("unable to verify bot token")
	}
	log.Printf("✅ Verified tenant bot: @%s (ID: %d) for tenant %d", me.UserName, me.ID, tenant.ID)

	// Encrypt and store token
	encryptedToken, err := utils.EncryptToken(botToken)
	if err != nil {
		return nil, fmt.Errorf("token encryption failed")
	}

	// Update tenant with bot token and owner Telegram ID
	tenant.TelegramBotToken = encryptedToken
	tenant.OwnerTelegramID = ownerTelegramID
	tenant.BotUsername = me.UserName

	if err := db.Save(&tenant).Error; err != nil {
		return nil, fmt.Errorf("failed to link bot: %v", err)
	}

	// Setup webhook
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

	log.Printf("✅ Bot linked successfully to tenant: %s (ID: %d)", tenant.Name, tenant.ID)
	return &tenant, nil
}
