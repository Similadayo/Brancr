package core

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/similadayo/brancr/internal/admin/cron"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/billing"
	"github.com/similadayo/brancr/internal/bot"
	"github.com/similadayo/brancr/internal/config"
	"github.com/similadayo/brancr/internal/industry"
	"github.com/similadayo/brancr/internal/logger"
	"github.com/similadayo/brancr/internal/quota"
	"github.com/similadayo/brancr/internal/social"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/websockets"
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

	// Increment quota cost usage with actual token usage on success
	if c.QuotaService != nil {
		if result.TokenUsage != nil {
			model := "default"
			if c.Client != nil && c.Client.Model != "" {
				model = c.Client.Model
			}
			if model == "" {
				model = "default"
			}
			quotaTokenUsage := quota.TokenUsageFromAI(result.TokenUsage.PromptTokens, result.TokenUsage.CompletionTokens, result.TokenUsage.TotalTokens)
			if incErr := c.QuotaService.IncrementAICallCost(ctx, req.TenantID, model, quotaTokenUsage); incErr != nil {
				// Log but don't fail - quota was already checked
				// This is a safety net in case of race conditions
			}
		} else {
			// Fallback to usage increment if no token usage available
			if incErr := c.QuotaService.IncrementAICallUsage(ctx, req.TenantID); incErr != nil {
				// Log but don't fail - quota was already checked
			}
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
	LifecycleCtx    context.Context
}

// ✅ NewApp initializes all components using the Services pattern
func NewApp(cfg *config.Config) (*App, error) {
	// Validate required configuration before proceeding
	if err := cfg.ValidateRequired(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Initialize LogBroadcaster (keep last 1000 lines)
	logBroadcaster := logger.NewLogBroadcaster(1000)

	// Configure Zap to write to both stderr and LogBroadcaster
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoder := zapcore.NewJSONEncoder(encoderConfig)

	// High-priority logs (Info/Error) go to stderr + broadcaster
	core := zapcore.NewTee(
		zapcore.NewCore(encoder, zapcore.AddSync(os.Stderr), zapcore.InfoLevel),
		zapcore.NewCore(encoder, zapcore.AddSync(logBroadcaster), zapcore.InfoLevel),
	)

	logger := zap.New(core)

	// 🗄️ Database with connection pooling and timeout configuration
	// Add connection timeout parameters to DSN if not already present
	dsn := cfg.PostgresDSN
	connectTimeout := 30 // seconds (default)
	maxRetries := 3      // default retries

	if dsn != "" {
		// Check if DSN already has timeout parameters
		if !strings.Contains(dsn, "connect_timeout") {
			// Use longer timeout for production (Railway can be slower during startup)
			if cfg.AppEnv == "production" {
				connectTimeout = 60 // 60 seconds for production (Railway)
				maxRetries = 5      // More retries for production
			}
			separator := "?"
			if strings.Contains(dsn, "?") {
				separator = "&"
			}
			// Add connection timeout, statement timeout, and SSL mode
			dsn = dsn + separator + fmt.Sprintf("connect_timeout=%d&statement_timeout=60000&sslmode=require", connectTimeout)
			logger.Info("database connection configured",
				zap.Int("connect_timeout", connectTimeout),
				zap.Int("max_retries", maxRetries),
				zap.String("env", cfg.AppEnv))
		} else if cfg.AppEnv == "production" {
			// If timeout already in DSN, still increase retries for production
			maxRetries = 5
		}
	}

	// Retry connection with exponential backoff
	var db *gorm.DB
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		// Configure GORM with query logging in development
		gormConfig := &gorm.Config{
			PrepareStmt: true, // Enable prepared statements for better performance
		}

		// Enable query logging in development
		if cfg.AppEnv == "development" {
			// Will be set after logger is created
		}

		// Attempt connection with timeout context
		connectStart := time.Now()
		db, lastErr = gorm.Open(postgres.Open(dsn), gormConfig)
		connectDuration := time.Since(connectStart)

		if lastErr == nil {
			logger.Info("✅ Database connected successfully",
				zap.Duration("connect_duration", connectDuration),
				zap.Int("attempt", i+1))
			break
		}

		if i < maxRetries-1 {
			// Exponential backoff: 2s, 4s, 6s, 8s, 10s
			waitTime := time.Duration(i+1) * 2 * time.Second
			logger.Warn("database connection failed, retrying",
				zap.Int("attempt", i+1),
				zap.Int("max_retries", maxRetries),
				zap.Duration("wait_time", waitTime),
				zap.Duration("connect_duration", connectDuration),
				zap.Error(lastErr))
			time.Sleep(waitTime)
		} else {
			// Last attempt failed
			logger.Error("database connection failed on final attempt",
				zap.Int("attempt", i+1),
				zap.Int("max_retries", maxRetries),
				zap.Duration("connect_duration", connectDuration),
				zap.Error(lastErr))
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

		sqlDB2.SetMaxOpenConns(maxOpen)             // Maximum open connections
		sqlDB2.SetMaxIdleConns(maxIdle)             // Maximum idle connections
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
	// CRITICAL: This must succeed - tables won't exist without it
	logger.Info("🚀 Starting GORM AutoMigrate - creating/updating all database tables",
		zap.String("env", cfg.AppEnv))

	if err := storage.AutoMigrate(db); err != nil {
		// In production, this is fatal - the app cannot function without tables
		logger.Error("❌ GORM AutoMigrate FAILED - this is critical",
			zap.Error(err),
			zap.String("env", cfg.AppEnv))
		return nil, fmt.Errorf("gorm auto-migration failed (critical): %w", err)
	}

	logger.Info("✅ GORM AutoMigrate completed successfully - all tables are ready",
		zap.String("env", cfg.AppEnv))

	// Seed default plan templates
	reqCtx := context.Background()
	if err := seedDefaultPlans(reqCtx, db, logger); err != nil {
		logger.Warn("failed to seed default plans", zap.Error(err))
		// Non-fatal - continue startup
	}

	// Then run SQL migrations for schema changes, indexes, etc.
	// Try to find migrations directory (works in both local dev and Railway deployments)
	migrationsPath := "migrations"
	if _, err := os.Stat(migrationsPath); err != nil {
		// If migrations not found relative to current dir, try executable location
		if exe, err := os.Executable(); err == nil {
			exeDir := filepath.Dir(exe)
			projectRoot := filepath.Join(exeDir, "..")
			candidate := filepath.Join(projectRoot, "migrations")
			if _, err := os.Stat(candidate); err == nil {
				migrationsPath = candidate
				logger.Info("Found migrations directory relative to executable", zap.String("path", migrationsPath))
			}
		}
	}

	if err := storage.RunMigrations(db, migrationsPath, logger); err != nil {
		if cfg.AppEnv == "production" {
			return nil, fmt.Errorf("sql migrations failed: %w", err)
		}
		logger.Warn("sql migrations failed", zap.Error(err))
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

	// 🔄 Migrate BusinessMenuItem to industry-specific types (Product/MenuItem/Service)
	// This runs automatically on deployment and is safe to run multiple times
	if count, err := migrateBusinessMenuItems(reqCtx, db, logger); err != nil {
		logger.Warn("BusinessMenuItem migration failed", zap.Error(err))
		// Non-fatal - continue startup (migration can be run manually later)
	} else if count > 0 {
		logger.Info("✅ BusinessMenuItem migration completed",
			zap.Int("migrated_count", count))
	}

	// 🧠 Redis (optional) - with enhanced retry settings for Railway sleep/wake cycle
	var rdb *redis.Client
	if addr := strings.TrimSpace(cfg.RedisAddr); addr != "" {
		// Parse Redis URL if it's a full URL (Railway format: redis://default:password@host:port)
		var client *redis.Client

		// Common options for handling Railway's sleep/wake cycle
		commonOpts := func(opt *redis.Options) {
			opt.DisableIdentity = true // Disable auto-detection to avoid maint_notifications errors
			opt.MaxRetries = 5         // Retry up to 5 times (helps when Redis is waking up)
			opt.MinRetryBackoff = 500 * time.Millisecond
			opt.MaxRetryBackoff = 5 * time.Second
			opt.DialTimeout = 30 * time.Second // Allow 30s for Redis to wake up
			opt.ReadTimeout = 10 * time.Second
			opt.WriteTimeout = 10 * time.Second
			opt.PoolSize = 50                     // Increased for higher concurrency
			opt.MinIdleConns = 5                  // Keep more connections warm
			opt.PoolTimeout = 30 * time.Second    // Wait up to 30s for a connection from pool
			opt.ConnMaxIdleTime = 5 * time.Minute // Close idle connections after 5 min
		}

		if strings.HasPrefix(addr, "redis://") || strings.HasPrefix(addr, "rediss://") {
			// Use ParseURL for full Redis URLs
			opt, err := redis.ParseURL(addr)
			if err != nil {
				logger.Warn("failed to parse Redis URL, trying as address", zap.Error(err), zap.String("addr", addr))
				// Fallback to direct address
				fallbackOpt := &redis.Options{Addr: addr}
				commonOpts(fallbackOpt)
				client = redis.NewClient(fallbackOpt)
			} else {
				// Apply common options to parsed URL options
				commonOpts(opt)
				// Ensure TLS is enabled for rediss:// URLs (Upstash, etc.)
				if strings.HasPrefix(addr, "rediss://") && opt.TLSConfig == nil {
					opt.TLSConfig = &tls.Config{
						MinVersion: tls.VersionTLS12,
					}
				}
				client = redis.NewClient(opt)
			}
		} else {
			// Direct address format (host:port)
			opt := &redis.Options{Addr: addr}
			commonOpts(opt)
			client = redis.NewClient(opt)
		}

		// Test connection with retry (Redis might be waking up)
		var pingErr error
		for attempt := 0; attempt < 3; attempt++ {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			pingErr = client.Ping(ctx).Err()
			cancel()
			if pingErr == nil {
				break
			}
			if attempt < 2 {
				logger.Info("redis ping failed, retrying...",
					zap.Int("attempt", attempt+1),
					zap.Error(pingErr))
				time.Sleep(2 * time.Second)
			}
		}

		if pingErr != nil {
			logger.Warn("redis unavailable after retries, proceeding without it",
				zap.Error(pingErr),
				zap.String("note", "App will continue but some features may be limited"))
			// Still set the client - it might reconnect later
			// This allows features to gracefully degrade
			rdb = client
		} else {
			rdb = client
			logger.Info("✅ redis connected with enhanced retry settings", zap.String("addr", maskRedisURL(addr)))
		}
	} else {
		logger.Info("redis not configured; queueing and cache features running in-memory")
	}

	// ✅ Initialize all services with proper dependency injection
	services, err := NewServices(cfg, db, rdb, logger, logBroadcaster)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize services: %w", err)
	}

	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())

	app := &App{
		Config:          cfg,
		Services:        services,
		SchedulerCancel: lifecycleCancel,
		LifecycleCtx:    lifecycleCtx,
	}

	// Sanity check: ensure migrations that support new features (drafts) are applied.
	if services.DB != nil {
		if !services.DB.Migrator().HasTable(&storage.Draft{}) {
			logger.Warn("database missing drafts table - run migrations: migrations/000002_add_drafts_table.sql")
		}
	}

	// --- API router (Chi) ---
	apiRouter := chi.NewRouter()
	log.Println(">> apply middleware (SetupMiddleware) to apiRouter")
	SetupMiddleware(app, apiRouter)
	log.Println(">> now register routes (RegisterRoutes) on apiRouter")
	RegisterRoutes(app, apiRouter)

	// --- WebSocket handler (plain net/http, NO chi middleware) ---
	// Use the raw handler to ensure http.Hijacker isn't lost to middleware wrappers
	wsHandler := http.HandlerFunc(websockets.NewWSHandler(app.Services.DB, app.Services.Logger, app.Config.TenantJWTSecret))

	// --- Top-level ServeMux routes ---
	rootMux := http.NewServeMux()
	rootMux.Handle("/ws", wsHandler)
	rootMux.Handle("/", apiRouter) // all other traffic handled by Chi

	log.Println(">> starting server with root mux (ws + api)")
	app.Server = &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: rootMux,
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

			// Create HTTP client with longer timeout for Telegram API
			telegramClient := &http.Client{
				Timeout: 70 * time.Second, // 70 second timeout to accommodate 60s long polling
			}

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

				// Create bot API - it will use default client, but we can set custom client after
				adminBot, err = tgbotapi.NewBotAPI(adminToken)
				if err == nil {
					// Set custom HTTP client with longer timeout
					adminBot.Client = telegramClient

					// Test connection (GetMe will use the custom client with 30s timeout)
					_, err = adminBot.GetMe()

					if err == nil {
						// Success!
						services.BotManager.AdminBot = adminBot
						logger.Info("🚀 Admin Onboarding Bot started",
							zap.String("username", adminBot.Self.UserName))

						// Launch onboarding handler
						go startOnboardingBot(db, adminBot, services.BotManager, logger)
						return
					}
				}

				// Log error with more context
				errMsg := ""
				if err != nil {
					errMsg = err.Error()
				}
				isTimeout := strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "i/o timeout")
				isNetwork := strings.Contains(errMsg, "dial") || strings.Contains(errMsg, "connection")

				if isTimeout || isNetwork {
					logger.Warn("❌ Failed to start admin bot (network/timeout)",
						zap.Int("attempt", attempt+1),
						zap.Int("max_retries", maxRetries),
						zap.Bool("is_timeout", isTimeout),
						zap.Bool("is_network", isNetwork),
						zap.Error(err))
				} else {
					logger.Warn("❌ Failed to start admin bot (authentication/other)",
						zap.Int("attempt", attempt+1),
						zap.Int("max_retries", maxRetries),
						zap.Error(err))
				}
			}

			logger.Error("❌ Failed to start admin bot after all retries",
				zap.Int("max_retries", maxRetries),
				zap.Error(err))
		}()
	}

	// ✅ Start tenant bots (after admin bot readiness)
	go func() {
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		select {
		case <-lifecycleCtx.Done():
			return
		case <-timer.C:
		}

		if err := services.BotManager.InitBots(); err != nil {
			logger.Warn("bot manager init failed", zap.Error(err))
		} else {
			logger.Info("✅ Tenant bots initialized successfully")
		}

		// Start background retry worker for failed bots
		go services.BotManager.StartRetryWorker(lifecycleCtx)
		logger.Info("🔄 Bot retry worker started")
	}()

	// ✅ Start Analytics Scheduler (periodic aggregation jobs)
	if services.AnalyticsScheduler != nil {
		go services.AnalyticsScheduler.Run(lifecycleCtx)
		logger.Info("📊 Analytics scheduler started")
	}

	// ✅ Start Gupshup Polling Worker (checks pending WhatsApp onboarding apps)
	if services.GupshupWebhookHandler != nil {
		// Poll every 10 minutes for apps stuck in pending_onboarding
		go services.GupshupWebhookHandler.StartPollingWorker(lifecycleCtx, 10*time.Minute)
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
	ctxSched := a.LifecycleCtx
	if ctxSched == nil {
		ctxSched, a.SchedulerCancel = context.WithCancel(context.Background())
		a.LifecycleCtx = ctxSched
	}
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
		timer := time.NewTimer(time.Until(nextRun))
		select {
		case <-ctxSched.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		// Then run daily at 9 AM UTC
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()

		// Run immediately on startup if it's close to 9 AM
		if time.Until(nextRun) < 1*time.Hour {
			go func() {
				ctx := ctxSched
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
				ctx := ctxSched
				if err := a.Services.EscalationHandler.SendDailyReminders(ctx); err != nil {
					a.Services.Logger.Error("daily reminder failed", zap.Error(err))
				}
			}
		}
	}()
	a.Services.Logger.Info("📋 Daily escalation reminders scheduled (9 AM UTC)")

	// Daily Trial Expiration Check
	go func() {
		// Calculate time until next 10 AM UTC
		now := time.Now().UTC()
		nextRun := time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, time.UTC)
		if now.After(nextRun) {
			nextRun = nextRun.Add(24 * time.Hour)
		}

		// Wait until 10 AM UTC
		timer := time.NewTimer(time.Until(nextRun))
		select {
		case <-ctxSched.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		// Then run daily at 10 AM UTC
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()

		// Run immediately on startup if it's close to 10 AM
		if time.Until(nextRun) < 1*time.Hour {
			go func() {
				if a.Services.AlertTriggers != nil {
					ctx := ctxSched
					a.Services.AlertTriggers.CheckTrialExpirations(ctx)
				}
			}()
		}

		for {
			select {
			case <-ctxSched.Done():
				return
			case <-ticker.C:
				if a.Services.AlertTriggers != nil {
					ctx := ctxSched
					a.Services.AlertTriggers.CheckTrialExpirations(ctx)
				}
			}
		}
	}()
	a.Services.Logger.Info("⏳ Daily trial expiration check scheduled (10 AM UTC)")

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

	// 📊 Admin metrics aggregation (hourly + daily)
	go func() {
		cron.StartMetricsAggregationCron(ctxSched, a.Services.DB, a.Services.Logger)
	}()

	// 💳 Subscription expiration monitoring (daily at 8 AM UTC)
	go func() {
		// Calculate time until next 8 AM UTC
		now := time.Now().UTC()
		nextRun := time.Date(now.Year(), now.Month(), now.Day(), 8, 0, 0, 0, time.UTC)
		if nextRun.Before(now) {
			nextRun = nextRun.Add(24 * time.Hour)
		}
		initialDelay := nextRun.Sub(now)

		// Wait for initial delay
		timer := time.NewTimer(initialDelay)
		select {
		case <-ctxSched.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		// Run immediately on first tick
		ctx := ctxSched
		if a.Services.SubscriptionService != nil {
			if err := a.Services.SubscriptionService.CheckExpiringSubscriptions(ctx); err != nil {
				a.Services.Logger.Error("subscription expiration check failed", zap.Error(err))
			} else {
				a.Services.Logger.Info("✅ Subscription expiration check completed")
			}
		}

		// Then run every 24 hours
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctxSched.Done():
				return
			case <-ticker.C:
				if a.Services.SubscriptionService != nil {
					if err := a.Services.SubscriptionService.CheckExpiringSubscriptions(ctx); err != nil {
						a.Services.Logger.Error("subscription expiration check failed", zap.Error(err))
					} else {
						a.Services.Logger.Debug("✅ Periodic subscription expiration check completed")
					}
				}
			}
		}
	}()
	a.Services.Logger.Info("💳 Subscription expiration monitoring started (checking daily at 8 AM UTC)")

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
		timer := time.NewTimer(initialDelay)
		select {
		case <-ctxSched.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		// Run immediately on first tick
		ctx := ctxSched
		if a.Services.AnalyticsService != nil {
			var tenants []storage.Tenant
			if err := a.Services.DB.Find(&tenants).Error; err == nil {
				for _, tenant := range tenants {
					if err := a.Services.AnalyticsService.CollectMetricsForTenant(ctx, tenant.ID, 7); err != nil {
						a.Services.Logger.Warn("periodic analytics refresh failed",
							zap.Stringer("tenant_id", tenant.ID),
							zap.Error(err))
					} else {
						a.Services.Logger.Info("✅ Refreshed analytics",
							zap.Stringer("tenant_id", tenant.ID))
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
				ctx := ctxSched
				if a.Services.AnalyticsService != nil {
					var tenants []storage.Tenant
					if err := a.Services.DB.Find(&tenants).Error; err == nil {
						for _, tenant := range tenants {
							if err := a.Services.AnalyticsService.CollectMetricsForTenant(ctx, tenant.ID, 7); err != nil {
								a.Services.Logger.Warn("periodic analytics refresh failed",
									zap.Stringer("tenant_id", tenant.ID),
									zap.Error(err))
							} else {
								// Trigger insights generation after analytics collection
								go func(tID uuid.UUID) {
									periodEnd := time.Now()
									periodStart := periodEnd.AddDate(0, 0, -30)
									if _, err := a.Services.AnalyticsService.GenerateInsights(ctx, tID, periodStart, periodEnd); err != nil {
										a.Services.Logger.Warn("auto insights generation failed",
											zap.Stringer("tenant_id", tID),
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

// seedDefaultPlans seeds the default plan templates (Trial, Starter, Pro)
func seedDefaultPlans(ctx context.Context, db *gorm.DB, logger *zap.Logger) error {
	return billing.SeedDefaultPlans(ctx, db, logger)
}

// migrateBusinessMenuItems migrates BusinessMenuItem records to industry-specific types
// This is safe to run multiple times (idempotent) - it only migrates records that exist
func migrateBusinessMenuItems(ctx context.Context, db *gorm.DB, logger *zap.Logger) (int, error) {
	// Initialize industry service
	industryService := industry.NewService(db, logger)

	// Get all BusinessMenuItem records
	var businessMenuItems []storage.BusinessMenuItem
	if err := db.WithContext(ctx).Find(&businessMenuItems).Error; err != nil {
		return 0, fmt.Errorf("failed to query BusinessMenuItem records: %w", err)
	}

	if len(businessMenuItems) == 0 {
		logger.Info("✅ No BusinessMenuItem records found - migration already complete or not needed")
		return 0, nil
	}

	logger.Info("🔄 Starting BusinessMenuItem migration",
		zap.Int("count", len(businessMenuItems)))

	migratedCount := 0
	errorCount := 0

	for _, bmi := range businessMenuItems {
		// Get tenant's industry capabilities
		capabilities, err := industryService.GetIndustryCapabilities(ctx, bmi.TenantID)
		if err != nil {
			logger.Warn("Failed to get industry capabilities for tenant",
				zap.Stringer("tenant_id", bmi.TenantID),
				zap.Error(err))
			errorCount++
			continue
		}

		// Determine target type based on industry capabilities
		var targetType string
		if capabilities.HasServices {
			targetType = "service"
		} else if capabilities.HasProducts {
			targetType = "product"
		} else if capabilities.HasMenu {
			targetType = "menu"
		} else {
			// Default to menu if no capabilities (backward compatibility)
			targetType = "menu"
			logger.Info("No industry capabilities found, defaulting to menu",
				zap.Stringer("tenant_id", bmi.TenantID))
		}

		// Parse price from string
		price := float64(0)
		if bmi.Price != "" {
			priceStr := strings.ReplaceAll(bmi.Price, "₦", "")
			priceStr = strings.ReplaceAll(priceStr, ",", "")
			priceStr = strings.ReplaceAll(priceStr, " ", "")
			if parsed, err := strconv.ParseFloat(priceStr, 64); err == nil {
				price = parsed
			}
		}

		// Parse tags
		var tags []string
		if bmi.Tags != "" {
			tags = strings.Split(bmi.Tags, ",")
			for i := range tags {
				tags[i] = strings.TrimSpace(tags[i])
			}
		}
		tagsJSON, _ := json.Marshal(tags)
		category := ""
		if len(tags) > 0 {
			category = tags[0] // Use first tag as category
		}

		// Create the appropriate type
		switch targetType {
		case "product":
			product := storage.Product{
				TenantID:        bmi.TenantID,
				Name:            bmi.Name,
				Description:     bmi.Description,
				Price:           price,
				Currency:        "NGN",
				Category:        category,
				Tags:            string(tagsJSON),
				IsActive:        true,
				Availability:    "in_stock",
				NegotiationMode: bmi.NegotiationMode,
			}
			if bmi.NegotiationMinPrice > 0 {
				product.NegotiationMinPrice = bmi.NegotiationMinPrice
			}
			if bmi.NegotiationMaxPrice > 0 {
				product.NegotiationMaxPrice = bmi.NegotiationMaxPrice
			} else {
				product.NegotiationMaxPrice = price
			}

			if err := db.WithContext(ctx).Create(&product).Error; err != nil {
				logger.Error("Failed to create Product",
					zap.Stringer("business_menu_item_id", bmi.ID),
					zap.Stringer("tenant_id", bmi.TenantID),
					zap.Error(err))
				errorCount++
				continue
			}
			migratedCount++
			logger.Debug("✅ Migrated BusinessMenuItem to Product",
				zap.Stringer("business_menu_item_id", bmi.ID),
				zap.Stringer("product_id", product.ID),
				zap.Stringer("tenant_id", bmi.TenantID))

		case "service":
			// For services, create fixed pricing
			pricing := map[string]interface{}{
				"type":   "fixed",
				"amount": price,
			}
			pricingJSON, err := json.Marshal(pricing)
			if err != nil {
				logger.Warn("Failed to marshal pricing JSON, using empty object",
					zap.Stringer("business_menu_item_id", bmi.ID),
					zap.Error(err))
				pricingJSON = []byte("{}")
			}

			// Ensure Packages field has valid JSON (empty array if no packages)
			packagesJSON := []byte("[]")

			service := storage.Service{
				TenantID:    bmi.TenantID,
				Name:        bmi.Name,
				Description: bmi.Description,
				Category:    category,
				Pricing:     string(pricingJSON),
				Packages:    string(packagesJSON), // Required JSONB field - set to valid JSON
				IsActive:    true,
			}

			if err := db.WithContext(ctx).Create(&service).Error; err != nil {
				logger.Error("Failed to create Service",
					zap.Stringer("business_menu_item_id", bmi.ID),
					zap.Stringer("tenant_id", bmi.TenantID),
					zap.Error(err))
				errorCount++
				continue
			}
			migratedCount++
			logger.Debug("✅ Migrated BusinessMenuItem to Service",
				zap.Stringer("business_menu_item_id", bmi.ID),
				zap.Stringer("service_id", service.ID),
				zap.Stringer("tenant_id", bmi.TenantID))

		case "menu":
			fallthrough
		default:
			// Parse dietary info from tags
			dietaryJSON, _ := json.Marshal(tags)

			menuItem := storage.MenuItem{
				TenantID:        bmi.TenantID,
				Name:            bmi.Name,
				Description:     bmi.Description,
				Price:           price,
				Currency:        "NGN",
				Category:        category,
				DietaryInfo:     string(dietaryJSON),
				IsActive:        true,
				Availability:    "available",
				NegotiationMode: bmi.NegotiationMode,
			}
			if bmi.NegotiationMinPrice > 0 {
				menuItem.NegotiationMinPrice = bmi.NegotiationMinPrice
			}
			if bmi.NegotiationMaxPrice > 0 {
				menuItem.NegotiationMaxPrice = bmi.NegotiationMaxPrice
			} else {
				menuItem.NegotiationMaxPrice = price
			}

			if err := db.WithContext(ctx).Create(&menuItem).Error; err != nil {
				logger.Error("Failed to create MenuItem",
					zap.Stringer("business_menu_item_id", bmi.ID),
					zap.Stringer("tenant_id", bmi.TenantID),
					zap.Error(err))
				errorCount++
				continue
			}
			migratedCount++
			logger.Debug("✅ Migrated BusinessMenuItem to MenuItem",
				zap.Stringer("business_menu_item_id", bmi.ID),
				zap.Stringer("menu_item_id", menuItem.ID),
				zap.Stringer("tenant_id", bmi.TenantID))
		}
	}

	if errorCount > 0 {
		logger.Warn("BusinessMenuItem migration completed with errors",
			zap.Int("total", len(businessMenuItems)),
			zap.Int("migrated", migratedCount),
			zap.Int("errors", errorCount))
	} else {
		logger.Info("✅ BusinessMenuItem migration completed successfully",
			zap.Int("total", len(businessMenuItems)),
			zap.Int("migrated", migratedCount))
	}

	return migratedCount, nil
}
