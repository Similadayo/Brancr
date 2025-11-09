package core

import (
	"context"

	"github.com/redis/go-redis/v9"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/analytics"
	"github.com/similadayo/brancr/internal/bot"
	"github.com/similadayo/brancr/internal/config"
	"github.com/similadayo/brancr/internal/social"
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
	BotManager *bot.TenantBotManager

	// Social Services
	MessageHandler        *social.MessageHandler
	MetaWebhookHandler    *social.MetaWebhookHandler
	MetaOAuthHandler      *social.MetaOAuthHandler
	Scheduler             *social.Scheduler
	EmbeddedSignupHandler *social.EmbeddedSignupHandler
	TikTokOAuthHandler    *social.TikTokOAuthHandler

	// Analytics Services
	AnalyticsService *analytics.Service
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

	// Initialize Social Platform components (depends on AI)
	if err := services.initSocial(); err != nil {
		return nil, err
	}

	// Initialize Analytics components (depends on AI)
	if err := services.initAnalytics(); err != nil {
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

func (s *Services) initSocial() error {
	// Create escalation function
	escalationFunc := func(ctx context.Context, tenantID uint, interactionID uint, customerName, platform, message, suggestedReply, intent, tone string, confidence float64) error {
		s.Logger.Info("📤 Escalation requested",
			zap.Uint("tenant_id", tenantID),
			zap.Uint("interaction_id", interactionID),
			zap.String("platform", platform))
		return nil
	}

	// Initialize Message Handler
	messageHandler := &social.MessageHandler{
		DB:             s.DB,
		Logger:         s.Logger,
		IntentDetector: s.IntentDetector,
		Responder:      s.CustomerResponder,
		EscalationFunc: escalationFunc,
	}
	s.MessageHandler = messageHandler

	// Initialize Meta Webhook Handler
	metaWebhookHandler := social.NewMetaWebhookHandler(s.DB, s.Logger, messageHandler)
	s.MetaWebhookHandler = metaWebhookHandler

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
	s.Scheduler = scheduler

	// Register TikTok connector with services-aware implementation
	social.Connectors["tiktok"] = social.NewTikTokConnector(s.DB, s.Logger, s.Config)

	return nil
}

func (s *Services) initAnalytics() error {
	// Initialize analytics collector
	collector := analytics.NewCollectorService(s.DB, s.Logger)

	// Register platform analytics clients
	// Note: Platform clients will be implemented and registered as needed
	// For now, we'll skip registration until clients are fully implemented

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

	return nil
}
