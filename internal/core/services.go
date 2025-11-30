package core

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/analytics"
	"github.com/similadayo/brancr/internal/analytics/platforms"
	"github.com/similadayo/brancr/internal/bot"
	"github.com/similadayo/brancr/internal/config"
	"github.com/similadayo/brancr/internal/security"
	"github.com/similadayo/brancr/internal/social"
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
	MessageHandler          *social.MessageHandler
	MetaWebhookHandler      *social.MetaWebhookHandler
	RespondIOWebhookHandler *social.RespondIOWebhookHandler
	GupshupWebhookHandler   *social.GupshupWebhookHandler
	GupshupPartnerClient    *social.GupshupPartnerClient
	MetaOAuthHandler        *social.MetaOAuthHandler
	Scheduler               *social.Scheduler
	EmbeddedSignupHandler   *social.EmbeddedSignupHandler
	TikTokOAuthHandler      *social.TikTokOAuthHandler

	// Analytics Services
	AnalyticsService   *analytics.Service
	AnalyticsScheduler *analytics.AnalyticsScheduler
}

// NewServices initializes all services with proper dependency injection
func NewServices(cfg *config.Config, db *gorm.DB, redis *redis.Client, logger *zap.Logger) (*Services, error) {
	services := &Services{
		DB:     db,
		Redis:  redis,
		Logger: logger,
		Config: cfg,
	}

	// Initialize AI components first (no dependencies)
	if err := services.initAI(); err != nil {
		return nil, err
	}

	// Initialize Bot Manager (depends on AI)
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

func (s *Services) initAI() error {
	// Initialize AI Client
	aiClient := ai.NewAIClient()
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
	s.CustomerResponder = customerResponder

	// Initialize Caption Optimizer
	captionOptimizer := &CaptionOptimizerImpl{
		Client: aiClient,
		DB:     s.DB,
	}
	s.CaptionOptimizer = captionOptimizer

	return nil
}

func (s *Services) initBotManager() error {
	botManager := bot.NewTenantBotManager(s.DB, s.Config, s.Logger)
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

	// Initialize Gupshup Partner client (multi-tenant) if configured
	var gupshupPartnerClient *social.GupshupPartnerClient
	var whatsappProvider social.WhatsAppProvider
	if s.Config.GupshupPartnerAPIKey != "" && s.Config.GupshupPartnerID != "" {
		gupshupPartnerClient = social.NewGupshupPartnerClient(
			s.Config.GupshupPartnerBaseURL,
			s.Config.GupshupPartnerAPIKey,
			s.Logger,
		)
		s.GupshupPartnerClient = gupshupPartnerClient
		whatsappProvider = social.NewGupshupWhatsAppProvider(s.DB, s.Logger, gupshupPartnerClient)
		s.Logger.Info("✅ Gupshup Partner client initialized for multi-tenant WhatsApp integration")
	} else {
		s.Logger.Info("ℹ️  Gupshup Partner API credentials not configured — Gupshup Partner multi-tenant will be disabled")
	}

	// Initialize Gupshup legacy client if API credentials are configured (for backward compatibility)
	// Note: SourceNumber is optional - can be set later after Go Live is complete
	var gupshupClient *social.GupshupClient
	if s.Config.GupshupAPIKey != "" && s.Config.GupshupAppName != "" {
		gupshupClient = social.NewGupshupClient(
			s.Config.GupshupAPIKey,
			s.Config.GupshupAppName,
			s.Config.GupshupSourceNumber, // Can be empty - will log warning when sending
			s.Logger,
		)
		if s.Config.GupshupSourceNumber != "" {
			s.Logger.Info("✅ Gupshup legacy client initialized for WhatsApp integration")
		} else {
			s.Logger.Info("✅ Gupshup legacy client initialized (API key and app name configured, but Source Number not set yet - complete Go Live to add it)")
		}
	}

	// Initialize Respond.io client if API key is configured
	var respondIOClient *social.RespondIOClient
	if s.Config.RespondIOAPIKey != "" {
		respondIOClient = social.NewRespondIOClient(s.Config.RespondIOAPIKey, s.Logger)
		s.Logger.Info("✅ Respond.io client initialized for WhatsApp integration")
	} else {
		s.Logger.Info("ℹ️  Respond.io API key not configured — WhatsApp will use Meta Direct API fallback")
	}

	// Initialize Message Handler
	messageHandler := &social.MessageHandler{
		DB:               s.DB,
		Logger:           s.Logger,
		IntentDetector:   s.IntentDetector,
		Responder:        s.CustomerResponder,
		SystemToken:      s.Config.BrancrWABAToken, // System token for provider-owned WhatsApp (fallback)
		RespondIO:        respondIOClient,          // Respond.io client for WhatsApp integration
		Gupshup:          gupshupClient,            // Gupshup legacy client (backward compatibility)
		WhatsAppProvider: whatsappProvider,         // Gupshup Partner Provider (multi-tenant, Priority 1)
		EscalationFunc:   escalationFunc,
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

	// Initialize Gupshup Webhook Handler (works for both Partner and legacy)
	if (s.Config.GupshupPartnerAPIKey != "" && s.Config.GupshupPartnerID != "") ||
		(s.Config.GupshupAPIKey != "" && s.Config.GupshupAppName != "") {
		gupshupWebhookHandler := social.NewGupshupWebhookHandler(s.DB, s.Logger, messageHandler)
		s.GupshupWebhookHandler = gupshupWebhookHandler
		s.Logger.Info("✅ Gupshup webhook handler initialized")
	}

	if oauthHandler, err := social.NewMetaOAuthHandler(s.DB, s.Logger, s.Config); err != nil {
		s.Logger.Warn("failed to initialize Meta OAuth handler", zap.Error(err))
	} else {
		s.MetaOAuthHandler = oauthHandler
	}

	if tikTokHandler := social.NewTikTokOAuthHandler(s.DB, s.Logger, s.Config); tikTokHandler != nil {
		s.TikTokOAuthHandler = tikTokHandler
	}

	if embeddedHandler := social.NewEmbeddedSignupHandler(s.DB, s.Logger, s.Config); embeddedHandler != nil {
		s.EmbeddedSignupHandler = embeddedHandler
	}

	// Initialize Scheduler (uses CaptionOptimizer interface)
	scheduler := social.NewScheduler(s.DB, s.Logger, s.CaptionOptimizer)

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
