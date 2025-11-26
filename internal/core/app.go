package core

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/bot"
	"github.com/similadayo/brancr/internal/config"
	"github.com/similadayo/brancr/internal/social"
	"github.com/similadayo/brancr/internal/storage"
)

// CaptionOptimizerImpl wraps ai.CaptionOptimizer for scheduler
type CaptionOptimizerImpl struct {
	Client *ai.AIClient
	DB     *gorm.DB
}

func (c *CaptionOptimizerImpl) OptimizeCaption(ctx context.Context, req social.OptimizeCaptionRequest) (*social.OptimizeCaptionResult, error) {
	optimizer := &ai.CaptionOptimizer{
		Client: c.Client,
		DB:     c.DB,
	}

	aiReq := ai.OptimizeCaptionRequest{
		OriginalCaption: req.OriginalCaption,
		Platform:        req.Platform,
		MediaType:       req.MediaType,
		TenantID:        req.TenantID,
	}

	result, err := optimizer.OptimizeCaption(ctx, aiReq)
	if err != nil {
		return nil, err
	}

	return &social.OptimizeCaptionResult{
		OptimizedCaption: result.OptimizedCaption,
		Hashtags:         result.Hashtags,
		SuggestedTime:    result.SuggestedTime,
		Improvements:     result.Improvements,
	}, nil
}

// ✅ App represents the core Brancr system
type App struct {
	Config          *config.Config
	Services        *Services
	Server          *http.Server
	SchedulerCancel context.CancelFunc
}

// ✅ NewApp initializes all components using the Services pattern
func NewApp(cfg *config.Config) (*App, error) {
	logger, _ := zap.NewProduction()

	// 🗄️ Database
	db, err := gorm.Open(postgres.Open(cfg.PostgresDSN), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// 🧱 Run migrations
	if err := storage.AutoMigrate(db); err != nil {
		logger.Warn("migration failed", zap.Error(err))
	} else {
		logger.Info("migrations complete")
	}

	// 🧑‍💼 Seed admin
	if err := storage.SeedAdminUser(db, cfg.AdminEmail, cfg.AdminPassword); err != nil {
		logger.Warn("seed admin failed", zap.Error(err))
	}

	// 🧪 Seed test tenant for Meta App Review
	if err := storage.SeedTestTenant(db); err != nil {
		logger.Warn("seed test tenant failed", zap.Error(err))
	}

	// 🧠 Redis (optional)
	var rdb *redis.Client
	if addr := strings.TrimSpace(cfg.RedisAddr); addr != "" {
		client := redis.NewClient(&redis.Options{Addr: addr})
		if err := client.Ping(context.Background()).Err(); err != nil {
			logger.Warn("redis unavailable, proceeding without it", zap.Error(err))
		} else {
			rdb = client
			logger.Info("redis connected", zap.String("addr", addr))
		}
	} else {
		logger.Info("redis not configured; queueing and cache features running in-memory")
	}

	// ✅ Initialize all services with proper dependency injection
	services, err := NewServices(cfg, db, rdb, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize services: %w", err)
	}

	app := &App{
		Config:   cfg,
		Services: services,
	}

	// 🚦 HTTP Router (services are initialized)
	router := SetupRouter(app)
	app.Server = &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	// ✅ Initialize Admin Bot for onboarding
	adminToken := cfg.AdminBotToken
	if adminToken == "" {
		logger.Warn("Admin bot token not configured — onboarding disabled")
	} else {
		adminBot, err := tgbotapi.NewBotAPI(adminToken)
		if err != nil {
			logger.Error("failed to start admin bot", zap.Error(err))
		} else {
			// Assign admin bot to the bot manager
			services.BotManager.AdminBot = adminBot

			logger.Info("🚀 Admin Onboarding Bot started",
				zap.String("username", adminBot.Self.UserName))

			// ✅ Launch onboarding handler
			go startOnboardingBot(db, adminBot, services.BotManager, logger)
		}
	}

	// ✅ Start tenant bots (after admin bot readiness)
	go func() {
		time.Sleep(3 * time.Second)
		if err := services.BotManager.InitBots(); err != nil {
			logger.Warn("bot manager init failed", zap.Error(err))
		} else {
			logger.Info("✅ Tenant bots initialized successfully")
		}
	}()

	return app, nil
}

// 🧩 Start AI-based Onboarding Bot
func startOnboardingBot(db *gorm.DB, adminBot *tgbotapi.BotAPI, manager *bot.TenantBotManager, logger *zap.Logger) {
	// Always clear webhook to prevent conflict
	adminBot.Request(tgbotapi.DeleteWebhookConfig{})

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := adminBot.GetUpdatesChan(u)

	onboarding := bot.NewOnboardingHandler(db, manager)

	for update := range updates {
		if update.Message == nil {
			continue
		}

		text := strings.TrimSpace(update.Message.Text)
		if strings.HasPrefix(text, "/start") {
			onboarding.HandleOnboarding(adminBot, &update)
		} else {
			// Let the AI onboarding handle continuation logic
			onboarding.HandleOnboarding(adminBot, &update)
		}
	}
}

// ✅ Run starts HTTP + background workers
func (a *App) Run() error {
	a.Services.Logger.Info("🚀 Brancr Core started", zap.String("port", a.Config.Port))

	// HTTP server
	go func() {
		if err := a.Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.Services.Logger.Fatal("HTTP server error", zap.Error(err))
		}
	}()

	// Scheduler with AI Caption Optimizer
	ctxSched, cancel := context.WithCancel(context.Background())
	a.SchedulerCancel = cancel
	go func() {
		poll := 10 * time.Second
		maxAttempts := 3
		a.Services.Scheduler.Run(ctxSched, poll, maxAttempts)
	}()
	a.Services.Logger.Info("🕓 Scheduler worker started (polling every 10s)")

	// Recurring Template Processor
	go func() {
		ticker := time.NewTicker(1 * time.Minute) // Check every minute
		defer ticker.Stop()
		for {
			select {
			case <-ctxSched.Done():
				return
			case <-ticker.C:
				// Process due recurring templates
				recurringManager := bot.NewRecurringTemplateManager(a.Services.DB, a.Services.Logger)
				if err := recurringManager.ProcessDueTemplates(ctxSched); err != nil {
					a.Services.Logger.Warn("recurring template processor error", zap.Error(err))
				}
			}
		}
	}()
	a.Services.Logger.Info("🔄 Recurring template processor started (checking every minute)")

	// WhatsApp Phone Number Sync from Meta
	go func() {
		// Sync immediately on startup
		if err := social.SyncPhoneNumbersFromMeta(a.Services.DB, a.Services.Logger, a.Services.Config); err != nil {
			a.Services.Logger.Warn("initial phone number sync failed", zap.Error(err))
		} else {
			a.Services.Logger.Info("✅ Initial phone number sync completed")
		}

		// Then sync every 15 minutes
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctxSched.Done():
				return
			case <-ticker.C:
				if err := social.SyncPhoneNumbersFromMeta(a.Services.DB, a.Services.Logger, a.Services.Config); err != nil {
					a.Services.Logger.Warn("periodic phone number sync failed", zap.Error(err))
				} else {
					a.Services.Logger.Debug("✅ Periodic phone number sync completed")
				}
			}
		}
	}()
	a.Services.Logger.Info("📞 WhatsApp phone number sync started (syncing every 15 minutes)")

	// Graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	a.Services.Logger.Info("🛑 Shutting down gracefully...")

	if a.SchedulerCancel != nil {
		a.SchedulerCancel()
		a.Services.Logger.Info("🕹 Scheduler stopped")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.Server.Shutdown(ctx); err != nil {
		a.Services.Logger.Fatal("Forced shutdown", zap.Error(err))
	}

	a.Services.Logger.Sync()
	return nil
}
