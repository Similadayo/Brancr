package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/config"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Standalone migration runner for Railway/deployment
// Usage: go run ./cmd/migrate
// Or: go run ./cmd/migrate -dsn "postgres://user:pass@host/db"
func main() {
	var dsn string
	var migrationsPath string
	flag.StringVar(&dsn, "dsn", "", "PostgreSQL connection string (or use DATABASE_URL env var)")
	flag.StringVar(&migrationsPath, "path", "", "Path to migrations directory (default: migrations/ relative to project root)")
	flag.Parse()

	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		// Try to load from config
		cfg := config.Load()
		dsn = cfg.PostgresDSN
	}

	if dsn == "" {
		log.Fatal("❌ DATABASE_URL, POSTGRES_DSN, or -dsn flag required")
	}

	// Find migrations directory (try multiple locations)
	if migrationsPath == "" {
		// Try relative to current working directory
		if _, err := os.Stat("migrations"); err == nil {
			migrationsPath = "migrations"
		} else {
			// Try relative to executable location
			exe, err := os.Executable()
			if err == nil {
				exeDir := filepath.Dir(exe)
				// Go up from bin/ to project root
				projectRoot := filepath.Join(exeDir, "..")
				candidate := filepath.Join(projectRoot, "migrations")
				if _, err := os.Stat(candidate); err == nil {
					migrationsPath = candidate
				} else {
					// Last resort: try current directory
					migrationsPath = "migrations"
				}
			} else {
				migrationsPath = "migrations"
			}
		}
	}

	logger, _ := zap.NewProduction()
	defer logger.Sync()

	logger.Info("🔌 Connecting to database...",
		zap.String("migrations_path", migrationsPath))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("❌ Failed to connect to database: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("❌ Failed to get sql.DB: %v", err)
	}
	defer sqlDB.Close()

	if err := sqlDB.Ping(); err != nil {
		log.Fatalf("❌ Database ping failed: %v", err)
	}
	logger.Info("✅ Database connected")

	// Check current migration status
	status, err := storage.GetMigrationStatus(db)
	if err != nil {
		logger.Warn("⚠️ Could not get migration status", zap.Error(err))
	} else {
		logger.Info("📊 Current migration status",
			zap.Any("status", status))
	}

	// Run migrations
	if err := storage.RunMigrations(db, migrationsPath, logger); err != nil {
		log.Fatalf("❌ Migration failed: %v", err)
	}

	// Check final status
	status, err = storage.GetMigrationStatus(db)
	if err != nil {
		logger.Warn("⚠️ Could not get final migration status", zap.Error(err))
	} else {
		logger.Info("✅ Migration complete",
			zap.Any("final_status", status))
	}

	fmt.Println("✅ All migrations applied successfully!")

	// Migrate bot webhooks to use UUIDs
	logger.Info("🤖 Migrating Telegram bot webhooks to UUID format...")
	if err := migrateBotWebhooks(db, logger); err != nil {
		logger.Error("⚠️ Bot webhook migration failed (non-fatal)", zap.Error(err))
	} else {
		logger.Info("✅ Bot webhook migration complete")
	}
}

func migrateBotWebhooks(db *gorm.DB, logger *zap.Logger) error {
	webhookBaseURL := os.Getenv("PUBLIC_URL")
	if webhookBaseURL == "" {
		logger.Warn("PUBLIC_URL not set, skipping bot webhook migration")
		return nil
	}

	var tenants []storage.Tenant
	if err := db.Where("telegram_bot_token IS NOT NULL AND telegram_bot_token != ''").Find(&tenants).Error; err != nil {
		return fmt.Errorf("failed to query tenants: %w", err)
	}

	logger.Info("Found tenants with bots", zap.Int("count", len(tenants)))

	for _, tenant := range tenants {
		if err := updateWebhook(tenant.ID, tenant.TelegramBotToken, webhookBaseURL, logger); err != nil {
			logger.Warn("Failed to update webhook",
				zap.Stringer("tenant_id", tenant.ID),
				zap.String("tenant_name", tenant.Name),
				zap.Error(err))
		} else {
			logger.Info("Updated webhook",
				zap.Stringer("tenant_id", tenant.ID),
				zap.String("tenant_name", tenant.Name))
		}
	}

	return nil
}

func updateWebhook(tenantID uuid.UUID, encryptedToken, baseURL string, logger *zap.Logger) error {
	// Decrypt token
	botToken, err := utils.DecryptToken(encryptedToken)
	if err != nil {
		return fmt.Errorf("failed to decrypt token: %w", err)
	}

	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		return fmt.Errorf("failed to create bot API: %w", err)
	}

	webhookURL := fmt.Sprintf("%s/bot/webhook/%s", baseURL, tenantID.String())
	webhook, err := tgbotapi.NewWebhook(webhookURL)
	if err != nil {
		return fmt.Errorf("failed to create webhook config: %w", err)
	}

	if _, err := bot.Request(webhook); err != nil {
		return fmt.Errorf("failed to set webhook: %w", err)
	}

	return nil
}
