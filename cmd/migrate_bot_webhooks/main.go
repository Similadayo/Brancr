package main

import (
	"context"
	"fmt"
	"log"
	"os"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	// Get database URL from environment
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	// Get base webhook URL from environment
	webhookBaseURL := os.Getenv("WEBHOOK_BASE_URL")
	if webhookBaseURL == "" {
		log.Fatal("WEBHOOK_BASE_URL environment variable is required (e.g., https://api.brancr.com)")
	}

	// Connect to database
	db, err := gorm.Open(postgres.Open(dbURL), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	ctx := context.Background()

	// Get all tenants with Telegram bots
	var tenants []storage.Tenant
	if err := db.Where("telegram_bot_token IS NOT NULL AND telegram_bot_token != ''").Find(&tenants).Error; err != nil {
		log.Fatalf("Failed to query tenants: %v", err)
	}

	log.Printf("Found %d tenants with Telegram bots", len(tenants))

	// Update webhook for each bot
	for _, tenant := range tenants {
		if err := updateWebhook(ctx, tenant.ID, tenant.TelegramBotToken, webhookBaseURL); err != nil {
			log.Printf("Failed to update webhook for tenant %s: %v", tenant.ID, err)
		} else {
			log.Printf("✅ Updated webhook for tenant %s (%s)", tenant.ID, tenant.Name)
		}
	}

	log.Println("Migration complete!")
}

func updateWebhook(ctx context.Context, tenantID uuid.UUID, botToken, baseURL string) error {
	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		return fmt.Errorf("failed to create bot API: %w", err)
	}

	// Construct new webhook URL with UUID
	webhookURL := fmt.Sprintf("%s/bot/webhook/%s", baseURL, tenantID.String())

	// Set webhook
	webhook, err := tgbotapi.NewWebhook(webhookURL)
	if err != nil {
		return fmt.Errorf("failed to create webhook config: %w", err)
	}

	if _, err := bot.Request(webhook); err != nil {
		return fmt.Errorf("failed to set webhook: %w", err)
	}

	log.Printf("  Set webhook: %s", webhookURL)
	return nil
}
