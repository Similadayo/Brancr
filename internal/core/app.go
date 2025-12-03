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
		ImageURLs:       req.ImageURLs, // Pass through image URLs for analysis
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

	// ✅ Initialize Admin Bot for onboarding (with retry)
	adminToken := cfg.AdminBotToken
	if adminToken == "" {
		logger.Warn("Admin bot token not configured — onboarding disabled")
	} else {
		// Retry admin bot startup with exponential backoff
		go func() {
			maxRetries := 5
			var adminBot *tgbotapi.BotAPI
			var err error
			
			for attempt := 0; attempt < maxRetries; attempt++ {
				if attempt > 0 {
					backoff := time.Duration(1<<uint(attempt-1)) * time.Second
					if backoff > 2*time.Minute {
						backoff = 2 * time.Minute
					}
					logger.Info("🔄 Retrying admin bot startup",
						zap.Int("attempt", attempt+1),
						zap.Duration("backoff", backoff))
					time.Sleep(backoff)
				}
				
				adminBot, err = tgbotapi.NewBotAPI(adminToken)
				if err == nil {
					// Success!
					services.BotManager.AdminBot = adminBot
					logger.Info("🚀 Admin Onboarding Bot started",
						zap.String("username", adminBot.Self.UserName))
					
					// Launch onboarding handler
					go startOnboardingBot(db, adminBot, services.BotManager, logger)
					return
				}
				
				logger.Warn("❌ Failed to start admin bot",
					zap.Int("attempt", attempt+1),
					zap.Int("max_retries", maxRetries),
					zap.Error(err))
			}
			
			logger.Error("❌ Failed to start admin bot after all retries",
				zap.Int("max_retries", maxRetries),
				zap.Error(err))
		}()
	}

	// ✅ Start tenant bots (after admin bot readiness)
	go func() {
		time.Sleep(3 * time.Second)
		if err := services.BotManager.InitBots(); err != nil {
			logger.Warn("bot manager init failed", zap.Error(err))
		} else {
			logger.Info("✅ Tenant bots initialized successfully")
		}
		
		// Start background retry worker for failed bots
		go services.BotManager.StartRetryWorker(context.Background())
		logger.Info("🔄 Bot retry worker started")
	}()

	// ✅ Start Analytics Scheduler (periodic aggregation jobs)
	if services.AnalyticsScheduler != nil {
		ctx := context.Background()
		go services.AnalyticsScheduler.Run(ctx)
		logger.Info("📊 Analytics scheduler started")
	}

	// ✅ Start Gupshup Polling Worker (checks pending WhatsApp onboarding apps)
	if services.GupshupWebhookHandler != nil {
		ctx := context.Background()
		// Poll every 10 minutes for apps stuck in pending_onboarding
		go services.GupshupWebhookHandler.StartPollingWorker(ctx, 10*time.Minute)
		logger.Info("🔄 Gupshup polling worker started")
	}

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

	// Daily Escalation Reminders
	go func() {
		// Calculate time until next 9 AM UTC
		now := time.Now().UTC()
		nextRun := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, time.UTC)
		if now.After(nextRun) {
			nextRun = nextRun.Add(24 * time.Hour)
		}
		
		// Wait until 9 AM UTC
		time.Sleep(time.Until(nextRun))
		
		// Then run daily at 9 AM UTC
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		
		// Run immediately on startup if it's close to 9 AM
		if time.Until(nextRun) < 1*time.Hour {
			go func() {
				ctx := context.Background()
				if err := a.Services.EscalationHandler.SendDailyReminders(ctx); err != nil {
					a.Services.Logger.Error("daily reminder failed", zap.Error(err))
				}
			}()
		}
		
		for {
			select {
			case <-ctxSched.Done():
				return
			case <-ticker.C:
				ctx := context.Background()
				if err := a.Services.EscalationHandler.SendDailyReminders(ctx); err != nil {
					a.Services.Logger.Error("daily reminder failed", zap.Error(err))
				}
			}
		}
	}()
	a.Services.Logger.Info("📋 Daily escalation reminders scheduled (9 AM UTC)")

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

	// 📊 Periodic analytics refresh (daily at 2 AM UTC)
	go func() {
		// Calculate time until next 2 AM UTC
		now := time.Now().UTC()
		nextRun := time.Date(now.Year(), now.Month(), now.Day(), 2, 0, 0, 0, time.UTC)
		if nextRun.Before(now) {
			nextRun = nextRun.Add(24 * time.Hour)
		}
		initialDelay := nextRun.Sub(now)
		
		// Wait for initial delay
		time.Sleep(initialDelay)
		
		// Run immediately on first tick
		ctx := context.Background()
		if a.Services.AnalyticsService != nil {
			var tenants []storage.Tenant
			if err := a.Services.DB.Find(&tenants).Error; err == nil {
				for _, tenant := range tenants {
					if err := a.Services.AnalyticsService.CollectMetricsForTenant(ctx, tenant.ID, 7); err != nil {
						a.Services.Logger.Warn("periodic analytics refresh failed",
							zap.Uint("tenant_id", tenant.ID),
							zap.Error(err))
					} else {
						a.Services.Logger.Info("✅ Refreshed analytics",
							zap.Uint("tenant_id", tenant.ID))
					}
				}
			}
		}
		
		// Then run daily at 2 AM UTC
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		
		for {
			select {
			case <-ctxSched.Done():
				return
			case <-ticker.C:
				ctx := context.Background()
				if a.Services.AnalyticsService != nil {
					var tenants []storage.Tenant
					if err := a.Services.DB.Find(&tenants).Error; err == nil {
						for _, tenant := range tenants {
							if err := a.Services.AnalyticsService.CollectMetricsForTenant(ctx, tenant.ID, 7); err != nil {
								a.Services.Logger.Warn("periodic analytics refresh failed",
									zap.Uint("tenant_id", tenant.ID),
									zap.Error(err))
							} else {
								// Trigger insights generation after analytics collection
								go func(tID uint) {
									periodEnd := time.Now()
									periodStart := periodEnd.AddDate(0, 0, -30)
									if _, err := a.Services.AnalyticsService.GenerateInsights(ctx, tID, periodStart, periodEnd); err != nil {
										a.Services.Logger.Warn("auto insights generation failed",
											zap.Uint("tenant_id", tID),
											zap.Error(err))
									}
								}(tenant.ID)
							}
						}
					}
				}
			}
		}
	}()
	a.Services.Logger.Info("📊 Analytics refresh scheduled (daily at 2 AM UTC)")

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
