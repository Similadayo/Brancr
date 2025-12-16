package core

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/analytics"
	"github.com/similadayo/brancr/internal/analytics/platforms"
	"github.com/similadayo/brancr/internal/bot"
	"github.com/similadayo/brancr/internal/cache"
	"github.com/similadayo/brancr/internal/circuitbreaker"
	"github.com/similadayo/brancr/internal/config"
	"github.com/similadayo/brancr/internal/email"
	"github.com/similadayo/brancr/internal/industry"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/order"
	"github.com/similadayo/brancr/internal/queue"
	"github.com/similadayo/brancr/internal/quota"
	"github.com/similadayo/brancr/internal/security"
	"github.com/similadayo/brancr/internal/social"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/tenantapi"
	"github.com/similadayo/brancr/internal/worker"
	"github.com/similadayo/brancr/internal/workflows"
	"github.com/similadayo/brancr/pkg/metrics"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Services holds all initialized services with proper separation
type Services struct {
	DB      *gorm.DB
	Redis   *redis.Client
	Logger  *zap.Logger
	Config  *config.Config
	Metrics *metrics.Metrics

	// Production Infrastructure
	JobQueue        *queue.JobQueue
	Cache           *cache.Cache
	RateLimiter     *middleware.RateLimiter
	CircuitBreakers map[string]*circuitbreaker.CircuitBreaker
	QuotaService    *quota.QuotaService
	WorkerPool      *worker.Pool

	// AI Services
	AIClient          *ai.AIClient
	MemoryService     *ai.MemoryService
	IntentDetector    *ai.IntentDetector
	CustomerResponder *ai.CustomerResponder
	CaptionOptimizer  *CaptionOptimizerImpl

	// Bot Services
	BotManager        *bot.TenantBotManager
	EscalationHandler *bot.EscalationHandler

	// Workflow Services
	WorkflowOrchestrator *workflows.WorkflowOrchestrator

	// Social Services
	MessageHandler             *social.MessageHandler
	MetaWebhookHandler         *social.MetaWebhookHandler
	RespondIOWebhookHandler    *social.RespondIOWebhookHandler
	GupshupWebhookHandler      *social.GupshupWebhookHandler
	MetaOAuthHandler           *social.MetaOAuthHandler
	InstagramLoginOAuthHandler *social.InstagramLoginOAuthHandler // Instagram Login (Business Login for Instagram)
	InstagramInsights          *social.InstagramInsightsService
	Scheduler                  *social.Scheduler
	EmbeddedSignupHandler      *social.EmbeddedSignupHandler
	TikTokOAuthHandler         *social.TikTokOAuthHandler
	TikTokWebhookHandler       *social.TikTokWebhookHandler
	GoogleOAuthHandler         *tenantapi.GoogleOAuthHandler // Google OAuth for tenant authentication

	// Analytics Services
	AnalyticsService   *analytics.Service
	AnalyticsScheduler *analytics.AnalyticsScheduler

	// Industry Services
	IndustryService *industry.Service

	// Email Service
	EmailService interface {
		SendReceiptEmail(to, customerName, orderNumber, receiptURL, portalURL string) error
		SendOrderStatusEmail(to, customerName, orderNumber, status, portalURL string) error
	}
}

// NewServices initializes all services with proper dependency injection
func NewServices(cfg *config.Config, db *gorm.DB, redis *redis.Client, logger *zap.Logger) (*Services, error) {
	services := &Services{
		DB:              db,
		Redis:           redis,
		Logger:          logger,
		Config:          cfg,
		CircuitBreakers: make(map[string]*circuitbreaker.CircuitBreaker),
	}

	// Initialize production infrastructure
	if err := services.initProductionInfra(); err != nil {
		return nil, fmt.Errorf("failed to initialize production infrastructure: %w", err)
	}

	// Initialize AI components first (no dependencies)
	if err := services.initAI(); err != nil {
		return nil, err
	}

	// Initialize Industry Service (no dependencies, needed for BotManager)
	services.initIndustry()

	// Initialize Email Service (optional, for sending receipts)
	services.initEmail()

	// Initialize Bot Manager (depends on AI and IndustryService)
	if err := services.initBotManager(); err != nil {
		return nil, err
	}

	// Initialize Escalation Handler (depends on BotManager)
	if err := services.initEscalationHandler(); err != nil {
		return nil, err
	}

	// Initialize Social Platform components (depends on AI and BotManager)
	if err := services.initSocial(); err != nil {
		return nil, err
	}

	// Initialize Analytics components (depends on AI)
	if err := services.initAnalytics(); err != nil {
		return nil, err
	}

	// Initialize Workflow Orchestrator (depends on AI, BotManager, and EscalationHandler)
	if err := services.initWorkflows(); err != nil {
		return nil, err
	}

	return services, nil
}

func (s *Services) initProductionInfra() error {
	// Initialize job queue
	if s.Redis != nil && s.Config.RedisAddr != "" {
		jobQueue, err := queue.NewJobQueue(s.Config.RedisAddr, s.Logger)
		if err != nil {
			s.Logger.Warn("failed to initialize job queue", zap.Error(err))
		} else {
			s.JobQueue = jobQueue
			s.Logger.Info("✅ Job queue initialized")
		}
	}

	// Initialize cache
	s.Cache = cache.NewCache(s.Redis, s.Logger)
	s.Logger.Info("✅ Cache layer initialized")

	// Initialize rate limiter
	s.RateLimiter = middleware.NewRateLimiter(s.Redis, s.Logger)
	s.Logger.Info("✅ Rate limiter initialized")

	// Initialize quota service (with cache support)
	s.QuotaService = quota.NewQuotaService(s.DB, s.Logger, s.Cache)
	s.Logger.Info("✅ Quota service initialized")

	// Initialize worker pool for background jobs
	poolSize := s.Config.WorkerPoolSize
	if poolSize == 0 {
		poolSize = 20 // Default
	}
	queueSize := s.Config.WorkerQueueSize
	if queueSize == 0 {
		queueSize = 100 // Default
	}
	s.WorkerPool = worker.NewPool(poolSize, queueSize, s.Logger)
	s.WorkerPool.Start()
	s.Logger.Info("✅ Worker pool initialized",
		zap.Int("size", poolSize),
		zap.Int("queue_size", queueSize))

	// Initialize circuit breakers for external APIs
	cbConfig := circuitbreaker.DefaultConfig()
	if s.Redis != nil {
		s.CircuitBreakers["tiktok"] = circuitbreaker.NewCircuitBreaker("tiktok", cbConfig, s.Redis, s.Logger)
		s.CircuitBreakers["instagram"] = circuitbreaker.NewCircuitBreaker("instagram", cbConfig, s.Redis, s.Logger)
		s.CircuitBreakers["facebook"] = circuitbreaker.NewCircuitBreaker("facebook", cbConfig, s.Redis, s.Logger)
		s.CircuitBreakers["gupshup"] = circuitbreaker.NewCircuitBreaker("gupshup", cbConfig, s.Redis, s.Logger)
		s.CircuitBreakers["openai"] = circuitbreaker.NewCircuitBreaker("openai", cbConfig, s.Redis, s.Logger)
		s.CircuitBreakers["cloudinary"] = circuitbreaker.NewCircuitBreaker("cloudinary", cbConfig, s.Redis, s.Logger)
		s.Logger.Info("✅ Circuit breakers initialized")
	} else {
		// Create in-memory circuit breakers when Redis is unavailable
		s.CircuitBreakers["tiktok"] = circuitbreaker.NewCircuitBreaker("tiktok", cbConfig, nil, s.Logger)
		s.CircuitBreakers["instagram"] = circuitbreaker.NewCircuitBreaker("instagram", cbConfig, nil, s.Logger)
		s.CircuitBreakers["facebook"] = circuitbreaker.NewCircuitBreaker("facebook", cbConfig, nil, s.Logger)
		s.CircuitBreakers["gupshup"] = circuitbreaker.NewCircuitBreaker("gupshup", cbConfig, nil, s.Logger)
		s.CircuitBreakers["openai"] = circuitbreaker.NewCircuitBreaker("openai", cbConfig, nil, s.Logger)
		s.CircuitBreakers["cloudinary"] = circuitbreaker.NewCircuitBreaker("cloudinary", cbConfig, nil, s.Logger)
		s.Logger.Info("✅ Circuit breakers initialized (in-memory mode, Redis unavailable)")
	}

	return nil
}

func (s *Services) initAI() error {
	// Initialize AI Client
	aiClient := ai.NewAIClient()
	// Add circuit breaker for OpenAI if available
	if cb, ok := s.CircuitBreakers["openai"]; ok {
		aiClient.CircuitBreaker = cb
	}
	s.AIClient = aiClient

	// Initialize Memory Service
	embedder := ai.NewEmbeddingClient()
	memoryService := &ai.MemoryService{
		DB:      s.DB,
		Encoder: embedder,
	}
	s.MemoryService = memoryService

	// Initialize Intent Detector
	intentDetector := ai.NewIntentDetector(aiClient, s.DB, s.Logger)
	s.IntentDetector = intentDetector

	// Initialize Customer Responder
	customerResponder := ai.NewCustomerResponder(aiClient, s.DB, s.Logger, memoryService)

	// Set retrieval service if IndustryService is available (will be initialized later)
	// We'll set it after IndustryService is initialized
	s.CustomerResponder = customerResponder

	// Initialize Caption Optimizer (QuotaService will be set after initProductionInfra)
	captionOptimizer := &CaptionOptimizerImpl{
		Client: aiClient,
		DB:     s.DB,
	}
	s.CaptionOptimizer = captionOptimizer

	return nil
}

func (s *Services) initBotManager() error {
	botManager := bot.NewTenantBotManager(s.DB, s.Config, s.Logger, s.Metrics)

	// Initialize IndustrySelectionHandler if IndustryService is available
	if s.IndustryService != nil {
		industryHandler := bot.NewIndustrySelectionHandler(s.DB, s.IndustryService, s.Logger)
		botManager.IndustrySelectionHandler = industryHandler
	}

	s.BotManager = botManager
	return nil
}

func (s *Services) initEscalationHandler() error {
	// Initialize Payment Security Handler
	paymentSecurity := &security.PaymentSecurityHandler{
		DB:     s.DB,
		Logger: s.Logger,
	}

	escalationHandler := &bot.EscalationHandler{
		DB:              s.DB,
		Logger:          s.Logger,
		Manager:         s.BotManager,
		PaymentSecurity: paymentSecurity,
	}
	s.EscalationHandler = escalationHandler

	// Note: MessageHandler will be set later in initSocial,
	// so we'll need to link them after both are initialized
	return nil
}

func (s *Services) initSocial() error {
	// Create escalation function that sends Telegram alerts
	escalationFunc := func(ctx context.Context, tenantID uint, interactionID uint, customerName, platform, message, suggestedReply, intent, tone string, confidence float64) error {
		// Call EscalationHandler to send Telegram alert
		if s.EscalationHandler != nil {
			return s.EscalationHandler.SendEscalation(ctx, tenantID, interactionID, customerName, platform, message, suggestedReply, intent, tone, confidence)
		}
		s.Logger.Warn("escalation handler not initialized, skipping Telegram alert",
			zap.Uint("tenant_id", tenantID),
			zap.Uint("interaction_id", interactionID))
		return nil
	}

	// Initialize Respond.io client if API key is configured
	var respondIOClient *social.RespondIOClient
	if s.Config.RespondIOAPIKey != "" {
		respondIOClient = social.NewRespondIOClient(s.Config.RespondIOAPIKey, s.Logger)
		s.Logger.Info("✅ Respond.io client initialized for WhatsApp integration")
	} else {
		s.Logger.Info("ℹ️  Respond.io API key not configured — WhatsApp will use Meta Direct API fallback")
	}

	// Initialize Gupshup client if configured
	var gupshupClient *social.GupshupClient
	if s.Config.GupshupPartnerUsername != "" && s.Config.GupshupPartnerPassword != "" {
		gupshupClient = social.NewGupshupClient(s.Config, s.Logger)
		s.Logger.Info("✅ Gupshup Partner client initialized for WhatsApp integration")
	} else {
		s.Logger.Info("ℹ️  Gupshup Partner credentials not configured — Gupshup WhatsApp integration disabled")
	}

	// Initialize Message Handler
	messageHandler := &social.MessageHandler{
		DB:             s.DB,
		Logger:         s.Logger,
		IntentDetector: s.IntentDetector,
		Responder:      s.CustomerResponder,
		SystemToken:    s.Config.BrancrWABAToken, // System token for provider-owned WhatsApp (fallback)
		RespondIO:      respondIOClient,          // Respond.io client for WhatsApp integration
		GupshupClient:  gupshupClient,            // Gupshup Partner client for WhatsApp integration
		EscalationFunc: escalationFunc,
	}
	s.MessageHandler = messageHandler

	// Link MessageHandler to EscalationHandler for sending replies
	if s.EscalationHandler != nil {
		s.EscalationHandler.SetMessageHandler(messageHandler)
		// Also link EscalationHandler and MessageHandler to BotManager for easy access
		if s.BotManager != nil {
			s.BotManager.EscalationHandler = s.EscalationHandler
			s.BotManager.MessageHandler = messageHandler
			// Set BotGetter on MessageHandler so it can send Telegram replies
			// Create a wrapper that implements BotGetter interface
			messageHandler.BotGetter = &botGetterWrapper{manager: s.BotManager}
		}
	}

	// Link MessageHandler to WorkflowOrchestrator's EscalationWorkflow for sending escalation replies
	if s.WorkflowOrchestrator != nil && s.WorkflowOrchestrator.EscalationWorkflow != nil {
		s.WorkflowOrchestrator.EscalationWorkflow.MessageSender = messageHandler
	}

	// Link MessageHandler to WorkflowOrchestrator's DashboardWorkflow for sending dashboard replies
	if s.WorkflowOrchestrator != nil && s.WorkflowOrchestrator.DashboardWorkflow != nil {
		s.WorkflowOrchestrator.DashboardWorkflow.MessageSender = messageHandler
	}

	// Link MessageHandler to WorkflowOrchestrator's ConversationWorkflow for sending conversation replies
	if s.WorkflowOrchestrator != nil && s.WorkflowOrchestrator.ConversationWorkflow != nil {
		s.WorkflowOrchestrator.ConversationWorkflow.MessageSender = messageHandler
	}

	// Initialize Meta Webhook Handler
	metaWebhookHandler := social.NewMetaWebhookHandler(s.DB, s.Logger, messageHandler)
	s.MetaWebhookHandler = metaWebhookHandler

	// Initialize Respond.io Webhook Handler (OPTIONAL - only needed if you want to receive incoming messages via webhook)
	// Note: Webhooks require Advanced plan on Respond.io. Sending messages works fine without webhooks.
	if s.Config.RespondIOAPIKey != "" {
		respondIOWebhookHandler := social.NewRespondIOWebhookHandler(s.DB, s.Logger, messageHandler)
		s.RespondIOWebhookHandler = respondIOWebhookHandler
		s.Logger.Info("✅ Respond.io webhook handler initialized (optional - webhooks are not required for sending messages)")
	}

	// Initialize Gupshup Webhook Handler
	if gupshupClient != nil {
		gupshupWebhookHandler := social.NewGupshupWebhookHandler(s.DB, s.Logger, messageHandler, gupshupClient)
		s.GupshupWebhookHandler = gupshupWebhookHandler
		s.Logger.Info("✅ Gupshup webhook handler initialized")
	}

	if oauthHandler, err := social.NewMetaOAuthHandler(s.DB, s.Logger, s.Config, s.Metrics); err != nil {
		s.Logger.Warn("failed to initialize Meta OAuth handler", zap.Error(err))
	} else {
		s.MetaOAuthHandler = oauthHandler
	}

	// Initialize Instagram Login OAuth handler (Business Login for Instagram)
	if instagramOAuthHandler, err := social.NewInstagramLoginOAuthHandler(s.DB, s.Logger, s.Config); err != nil {
		s.Logger.Warn("failed to initialize Instagram Login OAuth handler", zap.Error(err))
	} else if instagramOAuthHandler != nil {
		s.InstagramLoginOAuthHandler = instagramOAuthHandler
		s.Logger.Info("✅ Instagram Login OAuth handler initialized")
	}

	if tikTokHandler := social.NewTikTokOAuthHandler(s.DB, s.Logger, s.Config); tikTokHandler != nil {
		s.TikTokOAuthHandler = tikTokHandler
		s.Logger.Info("✅ TikTok OAuth handler initialized")
	} else {
		s.Logger.Info("ℹ️  TikTok OAuth disabled — missing TIKTOK_CLIENT_KEY, TIKTOK_CLIENT_SECRET, or TIKTOK_REDIRECT_URI")
	}

	// Initialize Google OAuth handler for tenant authentication
	if googleOAuthHandler := tenantapi.NewGoogleOAuthHandler(s.DB, s.Logger, s.Config); googleOAuthHandler != nil {
		s.GoogleOAuthHandler = googleOAuthHandler
		s.Logger.Info("✅ Google OAuth handler initialized for tenant authentication")
	} else {
		s.Logger.Info("ℹ️  Google OAuth disabled — missing GOOGLE_OAUTH_CLIENT_ID or GOOGLE_OAUTH_CLIENT_SECRET")
	}

	// Initialize TikTok Webhook Handler (ONLY if secret is configured for security)
	if s.Config.TikTokWebhookSecret != "" {
		tikTokWebhookHandler := social.NewTikTokWebhookHandler(s.DB, s.Logger, s.Config.TikTokWebhookSecret)
		s.TikTokWebhookHandler = tikTokWebhookHandler
		s.Logger.Info("✅ TikTok webhook handler initialized with signature verification")
	} else {
		s.Logger.Info("ℹ️  TikTok webhook disabled — missing TIKTOK_WEBHOOK_SECRET (webhook endpoint will not be registered for security)")
		// DO NOT create handler without secret - this prevents accepting unsigned webhooks
		s.TikTokWebhookHandler = nil
	}

	if embeddedHandler := social.NewEmbeddedSignupHandler(s.DB, s.Logger, s.Config); embeddedHandler != nil {
		s.EmbeddedSignupHandler = embeddedHandler
	}

	// Initialize Instagram Insights Service
	instagramInsights := social.NewInstagramInsightsService(s.DB, s.Logger)
	s.InstagramInsights = instagramInsights
	s.Logger.Info("✅ Instagram Insights service initialized")

	// Initialize Scheduler (uses CaptionOptimizer interface)
	scheduler := social.NewScheduler(s.DB, s.Logger, s.CaptionOptimizer)
	// Set QuotaService if available
	if s.QuotaService != nil {
		scheduler.QuotaService = s.QuotaService
	}
	// Set WorkerPool if available (wrap to match interface)
	if s.WorkerPool != nil {
		scheduler.WorkerPool = &workerPoolAdapter{pool: s.WorkerPool}
		s.Logger.Info("✅ Worker pool integrated into scheduler")
	}

	// Set scheduler on BotManager for immediate post execution
	if s.BotManager != nil {
		s.BotManager.Scheduler = scheduler
	}

	if s.BotManager != nil {
		scheduler.PostNotifier = func(ctx context.Context, tenantID uint, platform string, success bool, message string) {
			if s.BotManager == nil {
				return
			}
			if success {
				// Enhanced success message with performance preview
				preview := fmt.Sprintf(
					"✅ %s\n\n"+
						"📊 *Performance Preview*\n"+
						"Analytics will be available in:\n"+
						"• Views & Engagement (24hrs)\n"+
						"• Likes & Comments (real-time)\n"+
						"• Reach & Impressions (48hrs)\n\n"+
						"💡 _Check back tomorrow for detailed insights. Use 'analytics' to view your stats._",
					message)
				s.BotManager.NotifyOwnerWithMarkdown(tenantID, preview)
			} else {
				// Error message stays simple
				s.BotManager.NotifyOwner(tenantID, "⚠️ "+message)
			}
		}
	}
	s.Scheduler = scheduler

	// Register TikTok connector with services-aware implementation
	social.Connectors["tiktok"] = social.NewTikTokConnector(s.DB, s.Logger, s.Config)

	return nil
}

func (s *Services) initAnalytics() error {
	// Initialize analytics collector
	collector := analytics.NewCollectorService(s.DB, s.Logger)

	// Register platform analytics clients
	// Register Instagram analytics client
	instagramClient := platforms.NewInstagramAnalyticsClient()
	collector.RegisterClient(instagramClient)
	s.Logger.Info("✅ Registered Instagram analytics client")

	// Initialize trend analyzer
	trendAnalyzer := analytics.NewTrendAnalyzer(s.DB, s.Logger, s.AIClient)

	// Initialize growth advisor
	growthAdvisor := analytics.NewGrowthAdvisor(s.DB, s.Logger, s.AIClient)

	// Initialize analytics service
	analyticsService := analytics.NewService(s.DB, s.Logger, collector, trendAnalyzer, growthAdvisor)
	s.AnalyticsService = analyticsService

	// Initialize Telegram reporter
	telegramReporter := analytics.NewTelegramReporter(s.DB, s.Logger, analyticsService)

	// Initialize analytics handler for bot and attach to bot manager
	analyticsHandler := bot.NewAnalyticsHandler(s.BotManager, analyticsService, telegramReporter)
	s.BotManager.AnalyticsHandler = analyticsHandler

	// Initialize Analytics Scheduler
	s.AnalyticsScheduler = analytics.NewAnalyticsScheduler(s.DB, s.Logger, analyticsService)

	return nil
}

func (s *Services) initWorkflows() error {
	// Initialize workflow orchestrator with all required dependencies
	orchestrator := workflows.NewWorkflowOrchestrator(
		s.DB,
		s.Logger,
		s.AIClient,
		s.MemoryService,
		s.BotManager,
	)

	s.WorkflowOrchestrator = orchestrator

	// Set orchestrator on BotManager for easy access
	if s.BotManager != nil {
		s.BotManager.WorkflowOrchestrator = orchestrator
	}

	// Set bot for posting workflow after orchestrator is created
	// We'll set it when a bot is available per tenant

	s.Logger.Info("✅ Workflow orchestrator initialized")
	return nil
}

func (s *Services) initIndustry() {
	// Initialize Industry Service
	s.IndustryService = industry.NewService(s.DB, s.Logger)
	s.Logger.Info("✅ Industry service initialized")

	// Link retrieval service to CustomerResponder if available
	if s.CustomerResponder != nil && s.IndustryService != nil && s.IndustryService.Retrieval != nil {
		// Create adapter to match interface
		retrievalAdapter := &retrievalServiceAdapter{
			retrieval: s.IndustryService.Retrieval,
		}
		s.CustomerResponder.RetrievalService = retrievalAdapter
		s.Logger.Info("✅ Industry retrieval service linked to CustomerResponder")

		// Initialize Sales Conversation Manager
		salesConvManager := ai.NewSalesConversationManager(s.DB, s.IndustryService, s.Logger)
		s.CustomerResponder.SalesConversationManager = salesConvManager
		s.Logger.Info("✅ Sales conversation manager initialized")

		// Initialize Payment Instruction Generator
		paymentGen := ai.NewPaymentInstructionGenerator(s.DB, s.Logger)
		s.CustomerResponder.PaymentInstructionGen = paymentGen
		s.Logger.Info("✅ Payment instruction generator initialized")

		// Initialize Order Service
		orderService := order.NewOrderService(s.DB, s.Logger)
		orderServiceAdapter := &orderServiceAdapter{
			orderService: orderService,
		}
		s.CustomerResponder.OrderService = orderServiceAdapter
		s.Logger.Info("✅ Order service initialized and linked to CustomerResponder")

		// Initialize Customer Info Extractor
		customerExtractor := ai.NewCustomerInfoExtractor(s.DB, s.Logger)
		s.CustomerResponder.CustomerExtractor = customerExtractor
		s.Logger.Info("✅ Customer info extractor initialized")

		// Link IndustryService to CustomerResponder
		industryAdapter := &industryServiceAdapter{
			industry: s.IndustryService,
		}
		s.CustomerResponder.IndustryService = industryAdapter
		s.Logger.Info("✅ Industry service linked to CustomerResponder")

		// Link TenantBotManager for order notifications
		if s.BotManager != nil {
			tenantNotifierAdapter := &tenantNotifierAdapter{
				botManager: s.BotManager,
			}
			s.CustomerResponder.TenantNotifier = tenantNotifierAdapter
			s.Logger.Info("✅ Tenant notifier linked to CustomerResponder")
		}
	}
}

// orderServiceAdapter adapts OrderService to CustomerResponder interface
type orderServiceAdapter struct {
	orderService *order.OrderService
}

func (a *orderServiceAdapter) CreateOrderFromSalesConversation(ctx context.Context, tenantID, customerID, conversationID uint, salesConv *storage.SalesConversation, customerName, customerPhone, customerEmail, platform string) (*storage.Order, error) {
	return a.orderService.CreateOrderFromSalesConversation(ctx, tenantID, customerID, conversationID, salesConv, customerName, customerPhone, customerEmail, platform)
}

func (a *orderServiceAdapter) ExtractOrderItems(salesConv *storage.SalesConversation) ([]storage.OrderItem, float64, error) {
	return a.orderService.ExtractOrderItems(salesConv)
}

// industryServiceAdapter adapts IndustryService to CustomerResponder interface
type industryServiceAdapter struct {
	industry *industry.Service
}

func (a *industryServiceAdapter) GetTenantIndustry(ctx context.Context, tenantID uint) (interface{}, error) {
	return a.industry.GetTenantIndustry(ctx, tenantID)
}

func (a *industryServiceAdapter) GetIndustrySettings(ctx context.Context, tenantID uint) (map[string]interface{}, error) {
	return a.industry.GetIndustrySettings(ctx, tenantID)
}

// retrievalServiceAdapter adapts RetrievalService to CustomerResponder interface
type retrievalServiceAdapter struct {
	retrieval *industry.RetrievalService
}

func (a *retrievalServiceAdapter) SearchItems(ctx context.Context, tenantID uint, query string, limit int) (interface{}, error) {
	return a.retrieval.SearchItems(ctx, tenantID, query, limit)
}

func (a *retrievalServiceAdapter) GetAllActive(ctx context.Context, tenantID uint, limit int) (interface{}, error) {
	return a.retrieval.GetAllActive(ctx, tenantID, limit)
}

func (a *retrievalServiceAdapter) FormatForAI(items interface{}) string {
	if results, ok := items.([]industry.RetrievalResult); ok {
		return a.retrieval.FormatForAI(results)
	}
	return ""
}

func (a *retrievalServiceAdapter) GetItemsWithImages(ctx context.Context, tenantID uint, message string) ([]string, error) {
	return a.retrieval.GetItemsWithImages(ctx, tenantID, message)
}

func (s *Services) initEmail() {
	// Initialize Email Service (optional - will be nil if SMTP not configured)
	emailService := email.NewEmailService(s.Logger)
	// Check if SMTP is configured
	if emailService.SMTPHost == "" || emailService.SMTPUser == "" || emailService.SMTPPass == "" {
		s.Logger.Info("ℹ️  Email service not configured (SMTP settings missing) - receipt emails will be skipped")
		s.EmailService = nil
	} else {
		s.EmailService = emailService
		s.Logger.Info("✅ Email service initialized")
	}
}

// tenantNotifierAdapter adapts TenantBotManager to CustomerResponder interface
type tenantNotifierAdapter struct {
	botManager *bot.TenantBotManager
}

func (a *tenantNotifierAdapter) NotifyOwnerWithMarkdown(tenantID uint, message string) {
	if a.botManager != nil {
		a.botManager.NotifyOwnerWithMarkdown(tenantID, message)
	}
}

// botGetterWrapper wraps TenantBotManager to implement social.BotGetter interface
type botGetterWrapper struct {
	manager *bot.TenantBotManager
}

func (w *botGetterWrapper) GetBot(tenantID uint) (interface{}, bool) {
	bot, ok := w.manager.GetBot(tenantID)
	if !ok {
		return nil, false
	}
	return bot, true
}

// workerPoolAdapter adapts worker.Pool to scheduler's interface
type workerPoolAdapter struct {
	pool *worker.Pool
}

func (a *workerPoolAdapter) Submit(job interface {
	Execute(ctx context.Context) error
}) error {
	return a.pool.Submit(worker.JobFunc(job.Execute))
}
