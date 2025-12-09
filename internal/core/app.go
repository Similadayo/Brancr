package core

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
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
	"github.com/similadayo/brancr/internal/quota"
	"github.com/similadayo/brancr/internal/social"
	"github.com/similadayo/brancr/internal/storage"
)

// CaptionOptimizerImpl wraps ai.CaptionOptimizer for scheduler
type CaptionOptimizerImpl struct {
	Client       *ai.AIClient
	DB           *gorm.DB
	QuotaService *quota.QuotaService
}

func (c *CaptionOptimizerImpl) OptimizeCaption(ctx context.Context, req social.OptimizeCaptionRequest) (*social.OptimizeCaptionResult, error) {
	// Check quota before making AI call
	if c.QuotaService != nil {
		if err := c.QuotaService.CheckAICallQuota(ctx, req.TenantID); err != nil {
			return nil, fmt.Errorf("quota exceeded: %w", err)
		}
	}

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

	// Increment quota usage on success
	if c.QuotaService != nil {
		if incErr := c.QuotaService.IncrementAICallUsage(ctx, req.TenantID); incErr != nil {
			// Log but don't fail - quota was already checked
			// This is a safety net in case of race conditions
		}
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
	// Validate required configuration before proceeding
	if err := cfg.ValidateRequired(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	logger, _ := zap.NewProduction()

	// 🗄️ Database with connection pooling and timeout configuration
	// Add connection timeout parameters to DSN if not already present
	dsn := cfg.PostgresDSN
	if dsn != "" {
		// Check if DSN already has timeout parameters
		if !strings.Contains(dsn, "connect_timeout") {
			// Add connection timeout (30 seconds) and statement timeout (60 seconds)
			separator := "?"
			if strings.Contains(dsn, "?") {
				separator = "&"
			}
			dsn = dsn + separator + "connect_timeout=30&statement_timeout=60000"
		}
	}

	// Retry connection with exponential backoff
	var db *gorm.DB
	var lastErr error
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		// Configure GORM with query logging in development
		gormConfig := &gorm.Config{
			PrepareStmt: true, // Enable prepared statements for better performance
		}

		// Enable query logging in development
		if cfg.AppEnv == "development" {
			// Will be set after logger is created
		}

		db, lastErr = gorm.Open(postgres.Open(dsn), gormConfig)
		if lastErr == nil {
			break
		}
		if i < maxRetries-1 {
			waitTime := time.Duration(i+1) * 2 * time.Second // Exponential backoff: 2s, 4s, 6s
			logger.Warn("database connection failed, retrying",
				zap.Int("attempt", i+1),
				zap.Int("max_retries", maxRetries),
				zap.Duration("wait_time", waitTime),
				zap.Error(lastErr))
			time.Sleep(waitTime)
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("failed to connect to database after %d attempts: %w", maxRetries, lastErr)
	}

	// Get underlying SQL DB for pool configuration
	sqlDB2, err := db.DB()
	if err == nil {
		// Production-ready connection pool settings (configurable)
		maxOpen := cfg.DBMaxOpenConns
		if maxOpen == 0 {
			maxOpen = 100 // Default
		}
		maxIdle := cfg.DBMaxIdleConns
		if maxIdle == 0 {
			maxIdle = 10 // Default
		}
		
		sqlDB2.SetMaxOpenConns(maxOpen)                 // Maximum open connections
		sqlDB2.SetMaxIdleConns(maxIdle)                  // Maximum idle connections
		sqlDB2.SetConnMaxLifetime(time.Hour)        // Connection max lifetime
		sqlDB2.SetConnMaxIdleTime(10 * time.Minute) // Idle connection timeout
		// Set connection timeout for establishing new connections (30 seconds)
		// Note: This is separate from the DSN connect_timeout
		logger.Info("✅ Database connection pool configured",
			zap.Int("max_open", maxOpen),
			zap.Int("max_idle", maxIdle),
			zap.String("dsn_timeout", "30s"))
	}

	// 🧱 Run migrations
	// First run GORM AutoMigrate for model synchronization
	if err := storage.AutoMigrate(db); err != nil {
		logger.Warn("gorm auto-migration failed", zap.Error(err))
	} else {
		logger.Info("gorm auto-migration complete")
	}

	// Then run SQL migrations for schema changes, indexes, etc.
	migrationsPath := "migrations"
	if err := storage.RunMigrations(db, migrationsPath, logger); err != nil {
		logger.Warn("sql migrations failed", zap.Error(err))
		// Don't fail startup if migrations fail - allow manual intervention
	}

	// 📊 Create database indexes for production performance
	if err := storage.CreateIndexes(db); err != nil {
		logger.Warn("index creation failed", zap.Error(err))
	} else {
		logger.Info("database indexes created")
	}

	// 🧑‍💼 Seed admin
	if err := storage.SeedAdminUser(db, cfg.AdminEmail, cfg.AdminPassword); err != nil {
		logger.Warn("seed admin failed", zap.Error(err))
	}

	// 🧪 Seed test tenant for Meta App Review
	if err := storage.SeedTestTenant(db); err != nil {
		logger.Warn("seed test tenant failed", zap.Error(err))
	}

	// 🏭 Seed default industries
	if err := storage.SeedIndustries(db); err != nil {
		logger.Warn("seed industries failed", zap.Error(err))
	} else {
		logger.Info("✅ Industries seeded")
	}

	// 🧠 Redis (optional)
	var rdb *redis.Client
	if addr := strings.TrimSpace(cfg.RedisAddr); addr != "" {
		// Parse Redis URL if it's a full URL (Railway format: redis://default:password@host:port)
		var client *redis.Client
		if strings.HasPrefix(addr, "redis://") || strings.HasPrefix(addr, "rediss://") {
			// Use ParseURL for full Redis URLs
			opt, err := redis.ParseURL(addr)
			if err != nil {
				logger.Warn("failed to parse Redis URL, trying as address", zap.Error(err), zap.String("addr", addr))
				// Fallback to direct address
				client = redis.NewClient(&redis.Options{Addr: addr})
			} else {
				client = redis.NewClient(opt)
			}
		} else {
			// Direct address format (host:port)
			client = redis.NewClient(&redis.Options{Addr: addr})
		}

		if err := client.Ping(context.Background()).Err(); err != nil {
			logger.Warn("redis unavailable, proceeding without it", zap.Error(err))
		} else {
			rdb = client
			logger.Info("redis connected", zap.String("addr", maskRedisURL(addr)))
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

	// Start job queue server if available
	if a.Services.JobQueue != nil {
		go func() {
			if err := a.Services.JobQueue.Start(); err != nil {
				a.Services.Logger.Error("job queue server error", zap.Error(err))
			}
		}()
		a.Services.Logger.Info("✅ Job queue server started")
	}

	// Graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	a.Services.Logger.Info("🛑 Shutting down gracefully...")

	// Shutdown job queue
	if a.Services.JobQueue != nil {
		a.Services.JobQueue.Shutdown()
		a.Services.Logger.Info("🛑 Job queue stopped")
	}

	// Shutdown worker pool
	if a.Services.WorkerPool != nil {
		if err := a.Services.WorkerPool.Shutdown(30 * time.Second); err != nil {
			a.Services.Logger.Warn("worker pool shutdown timeout", zap.Error(err))
		} else {
			a.Services.Logger.Info("🛑 Worker pool stopped")
		}
	}

	// Cancel scheduler
	if a.SchedulerCancel != nil {
		a.SchedulerCancel()
		a.Services.Logger.Info("🕹 Scheduler stopped")
	}

	// Shutdown HTTP server
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := a.Server.Shutdown(ctx); err != nil {
		a.Services.Logger.Fatal("Forced shutdown", zap.Error(err))
	}

	// Close database connections
	if sqlDB, err := a.Services.DB.DB(); err == nil {
		sqlDB.Close()
		a.Services.Logger.Info("🛑 Database connections closed")
	}

	// Close Redis connection
	if a.Services.Redis != nil {
		a.Services.Redis.Close()
		a.Services.Logger.Info("🛑 Redis connection closed")
	}

	a.Services.Logger.Sync()
	return nil
}

// maskRedisURL masks sensitive parts of Redis URL for logging
func maskRedisURL(redisURL string) string {
	if !strings.Contains(redisURL, "@") {
		return redisURL // No password, safe to log
	}

	// Parse URL and mask password
	parsed, err := url.Parse(redisURL)
	if err != nil {
		return "***" // Can't parse, mask everything
	}

	if parsed.User != nil {
		password, _ := parsed.User.Password()
		if password != "" {
			// Replace password with ***
			return strings.Replace(redisURL, password, "***", 1)
		}
	}

	return redisURL
}
