package core

import (
	"context"
	"fmt"
	"os"
	"time"

	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"io"

	"github.com/similadayo/brancr/internal/admin"
	"github.com/similadayo/brancr/internal/ai"
	v3 "github.com/similadayo/brancr/internal/ai/v3"
	"github.com/similadayo/brancr/internal/ai_v4"
	"github.com/similadayo/brancr/internal/ai_v4/llm"
	"github.com/similadayo/brancr/internal/ai_v4/router"
	"github.com/similadayo/brancr/internal/ai_v4/validation"
	"github.com/similadayo/brancr/internal/alerts"
	"github.com/similadayo/brancr/internal/analytics"
	"github.com/similadayo/brancr/internal/analytics/platforms"
	"github.com/similadayo/brancr/internal/billing"
	"github.com/similadayo/brancr/internal/blobstore"
	"github.com/similadayo/brancr/internal/bot"
	"github.com/similadayo/brancr/internal/cache"
	"github.com/similadayo/brancr/internal/checkpoint"
	"github.com/similadayo/brancr/internal/circuitbreaker"
	"github.com/similadayo/brancr/internal/config"
	"github.com/similadayo/brancr/internal/email"
	"github.com/similadayo/brancr/internal/fulfillment"
	"github.com/similadayo/brancr/internal/industry"
	"github.com/similadayo/brancr/internal/logger"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/newsletter"
	"github.com/similadayo/brancr/internal/order"
	"github.com/similadayo/brancr/internal/queue"
	"github.com/similadayo/brancr/internal/quota"
	"github.com/similadayo/brancr/internal/receipt"
	"github.com/similadayo/brancr/internal/security"
	"github.com/similadayo/brancr/internal/social"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/storage/repositories"
	"github.com/similadayo/brancr/internal/subscription"
	"github.com/similadayo/brancr/internal/tenantapi"
	"github.com/similadayo/brancr/internal/worker"
	"github.com/similadayo/brancr/internal/workflows"
	"github.com/similadayo/brancr/pkg/metrics"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Services holds all initialized services with proper separation
type Services struct {
	DB             *gorm.DB
	Redis          *redis.Client
	Logger         *zap.Logger
	Config         *config.Config
	Metrics        *metrics.Metrics
	LogBroadcaster *logger.LogBroadcaster // For admin streaming

	// Production Infrastructure
	JobQueue          *queue.JobQueue
	Cache             *cache.Cache
	RateLimiter       *middleware.RateLimiter
	CircuitBreakers   map[string]*circuitbreaker.CircuitBreaker
	QuotaService      *quota.QuotaService
	WorkerPool        *worker.Pool
	BlobStore         blobstore.Store
	CheckpointService *checkpoint.Service

	// AI Services
	AIClient          *ai.AIClient
	MemoryService     *ai.MemoryService
	IntentDetector    *ai.IntentDetector
	V3Pipeline        v3.Pipeline           // V3 AI Pipeline (direct usage)
	V4Pipeline        *ai_v4.Pipeline       // V4 AI Pipeline (factory)
	V4Router          *router.AIRouter      // V4 Router (traffic controller)
	CustomerResponder *ai.CustomerResponder // DEPRECATED: Use V4Router adapter
	CaptionOptimizer  *CaptionOptimizerImpl

	// Bot Services
	BotManager        *bot.TenantBotManager
	EscalationHandler *bot.EscalationHandler

	// Receipt Services
	ReceiptService *receipt.ReceiptService

	// Workflow Services
	WorkflowOrchestrator *workflows.WorkflowOrchestrator

	// Social Services
	MessageHandler             *social.MessageHandler
	MetaWebhookHandler         *social.MetaWebhookHandler
	RespondIOWebhookHandler    *social.RespondIOWebhookHandler
	GupshupWebhookHandler      *social.GupshupWebhookHandler
	GupshupClient              *social.GupshupClient // Gupshup Partner API client
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
		SendEmail(to, subject, body string, attachments ...string) error
		SendPasswordResetEmail(to, resetLink string) error
		SendEmailVerification(to, verifyLink string) error
	}

	// Alert & Newsletter Services
	AlertService      interface{}
	AlertTriggers     *alerts.TenantAlertTriggers
	NewsletterService interface{}

	// Subscription Service
	SubscriptionService *subscription.Service

	// Order Service
	OrderService *order.OrderService

	// Billing Services
	BillingService     *billing.BillingService
	UsageService       *billing.UsageService
	UsageSyncService   *billing.GupshupUsageSyncService // Gupshup usage sync service
	EnforcementService *billing.EnforcementService
	PaystackService    *billing.PaystackService

	// Fulfillment Service
	FulfillmentService *fulfillment.Service
}

// NewServices initializes all services with proper dependency injection
func NewServices(cfg *config.Config, db *gorm.DB, redis *redis.Client, logger *zap.Logger, logBroadcaster *logger.LogBroadcaster) (*Services, error) {
	services := &Services{
		DB:              db,
		Redis:           redis,
		Logger:          logger,
		Config:          cfg,
		CircuitBreakers: make(map[string]*circuitbreaker.CircuitBreaker),
		LogBroadcaster:  logBroadcaster,
	}

	// Initialize production infrastructure
	if err := services.initProductionInfra(); err != nil {
		return nil, fmt.Errorf("failed to initialize production infrastructure: %w", err)
	}
	// Register background job handlers (parse imports)
	if services.JobQueue != nil {
		// Handler now accepts jobID so it can record status in Redis and reference the blobstore object
		queue.RegisterParseImportHandler(services.JobQueue, services.DB, services.Logger, func(ctx context.Context, p *queue.ParseImportPayload, jobID string) error {
			rdb := services.JobQueue.GetRedisClient()
			statusKey := fmt.Sprintf("parse:job:%s", jobID)
			// mark pending
			if rdb != nil {
				rdb.Set(ctx, statusKey, `{"status":"pending"}`, 24*time.Hour)
			}

			// download object from blobstore
			if services.BlobStore == nil {
				if rdb != nil {
					rdb.Set(ctx, statusKey, `{"status":"failed","error":"blobstore not configured"}`, 24*time.Hour)
				}
				services.Logger.Error("blobstore not configured for parse job", zap.Stringer("tenant_id", p.TenantID))
				return fmt.Errorf("blobstore not configured")
			}
			rc, err := services.BlobStore.Download(ctx, p.Bucket, p.Key)
			if err != nil {
				if rdb != nil {
					rdb.Set(ctx, statusKey, fmt.Sprintf(`{"status":"failed","error":"%s"}`, err.Error()), 24*time.Hour)
				}
				services.Logger.Error("failed to download parse import object", zap.Error(err), zap.Stringer("tenant_id", p.TenantID))
				return err
			}
			defer rc.Close()
			b, err := io.ReadAll(rc)
			if err != nil {
				if rdb != nil {
					rdb.Set(ctx, statusKey, fmt.Sprintf(`{"status":"failed","error":"%s"}`, err.Error()), 24*time.Hour)
				}
				services.Logger.Error("failed to read import object", zap.Error(err), zap.Stringer("tenant_id", p.TenantID))
				return err
			}

			// detect by filename
			var raw string
			if strings.HasSuffix(strings.ToLower(p.Filename), ".csv") || strings.HasSuffix(strings.ToLower(p.Filename), ".tsv") {
				rows, err := tenantapi.ParseCSVFromBytes(b)
				if err != nil {
					if rdb != nil {
						rdb.Set(ctx, statusKey, fmt.Sprintf(`{"status":"failed","error":"%s"}`, err.Error()), 24*time.Hour)
					}
					services.Logger.Error("failed to parse csv in job", zap.Error(err), zap.Stringer("tenant_id", p.TenantID))
					return err
				}
				raw = strings.Join(rows, "\n")
			} else if strings.HasSuffix(strings.ToLower(p.Filename), ".docx") {
				text, err := tenantapi.ExtractDocxFromBytes(b)
				if err != nil {
					if rdb != nil {
						rdb.Set(ctx, statusKey, fmt.Sprintf(`{"status":"failed","error":"%s"}`, err.Error()), 24*time.Hour)
					}
					services.Logger.Error("failed to extract docx in job", zap.Error(err), zap.Stringer("tenant_id", p.TenantID))
					return err
				}
				raw = text
			} else {
				raw = string(b)
			}

			// choose parser
			var items []tenantapi.ParsedItem
			if services.AIClient != nil && len(raw) > 200 {
				aiParser := tenantapi.NewAIParser(services.AIClient)
				items, err = aiParser.Parse(raw, "")
				if err != nil {
					services.Logger.Warn("ai-parse-job-failed", zap.Error(err), zap.Stringer("tenant_id", p.TenantID))
					// fallback
					parser := tenantapi.NewDefaultParser()
					items, err = parser.Parse(raw)
					if err != nil {
						if rdb != nil {
							rdb.Set(ctx, statusKey, fmt.Sprintf(`{"status":"failed","error":"%s"}`, err.Error()), 24*time.Hour)
						}
						services.Logger.Error("parse-job-failed", zap.Error(err), zap.Stringer("tenant_id", p.TenantID))
						return err
					}
				}
			} else {
				parser := tenantapi.NewDefaultParser()
				items, err = parser.Parse(raw)
				if err != nil {
					if rdb != nil {
						rdb.Set(ctx, statusKey, fmt.Sprintf(`{"status":"failed","error":"%s"}`, err.Error()), 24*time.Hour)
					}
					services.Logger.Error("parse-job-failed", zap.Error(err), zap.Stringer("tenant_id", p.TenantID))
					return err
				}
			}

			// save as draft
			draftRepo := repositories.NewDraftRepo(services.DB)
			contentBytes, _ := json.Marshal(items)
			d := storage.Draft{TenantID: p.TenantID, Key: "import.products", Content: string(contentBytes)}
			if err := draftRepo.Create(&d); err != nil {
				if rdb != nil {
					rdb.Set(ctx, statusKey, fmt.Sprintf(`{"status":"failed","error":"%s"}`, err.Error()), 24*time.Hour)
				}
				services.Logger.Error("failed to save import draft", zap.Error(err), zap.Stringer("tenant_id", p.TenantID))
				return err
			}

			// mark done with draft id
			if rdb != nil {
				res := fmt.Sprintf(`{"status":"done","draft_id":%d,"items":%d}`, d.ID, len(items))
				rdb.Set(ctx, statusKey, res, 24*time.Hour)
			}
			services.Logger.Info("parse import job completed", zap.Stringer("tenant_id", p.TenantID), zap.Int("items", len(items)))
			return nil
		})
	}
	// Initialize Fulfillment Service early so AI can resume pending requirements
	if err := services.initFulfillmentService(); err != nil {
		return nil, err
	}

	// Initialize AI components
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

	// Set QuotaService on BotManager's Responder after both are initialized
	if services.BotManager != nil && services.BotManager.Responder != nil && services.QuotaService != nil {
		services.BotManager.Responder.QuotaService = services.QuotaService
		services.Logger.Info("✅ QuotaService set on BotManager Responder for cost tracking")
	}

	// Initialize Alert Service (depends on EmailService and BotManager)
	services.initAlerts()

	// Link AlertService to OrderService for "New Order" alerts
	if services.OrderService != nil && services.AlertService != nil {
		// Type assert to ensure it matches the expected interface
		if alertSvc, ok := services.AlertService.(interface {
			SendAlert(ctx context.Context, tenantID uuid.UUID, alertType, severity, title, message, actionURL, actionLabel string) (*storage.TenantAlert, error)
		}); ok {
			services.OrderService.SetAlertService(alertSvc)
			services.Logger.Info("✅ AlertService linked to OrderService")
		} else {
			services.Logger.Warn("⚠️ AlertService does not implement required interface for OrderService")
		}
	}

	// Link UsageService to CustomerResponder for usage tracking (initialized in initBilling via initAlerts)
	if services.CustomerResponder != nil && services.UsageService != nil {
		services.CustomerResponder.UsageRecorder = services.UsageService
		services.Logger.Info("✅ UsageService linked to CustomerResponder")
	}

	// Initialize Receipt Service (needed for Escalation Handler)
	if err := services.initReceiptService(); err != nil {
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

	// Initialize Checkpoint Service
	if err := services.initCheckpointService(); err != nil {
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

	// Initialize BlobStore (MinIO/S3) when configured
	if s.Config.StorageEndpoint != "" {
		minioStore, err := blobstore.NewMinioStore(s.Config.StorageEndpoint, s.Config.StorageAccessKey, s.Config.StorageSecretKey, s.Config.StorageUseSSL, s.Config.StorageRegion)
		if err != nil {
			s.Logger.Warn("failed to initialize blobstore", zap.Error(err))
		} else {
			s.BlobStore = minioStore
			s.Logger.Info("✅ Blob storage initialized")
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

	// 1. Initialize Customer Responder (V3 Legacy Endpoint)
	customerResponder := ai.NewCustomerResponder(aiClient, s.DB, s.Logger, memoryService, s.Metrics, nil)
	// CRITICAL: Set PublicURL from config so receipt links are correct
	if s.Config.PublicURL != "" {
		customerResponder.SetPublicURL(s.Config.PublicURL)
	}
	s.CustomerResponder = customerResponder

	// 2. Initialize V3 Executor (Required for V4 Adapter)
	// We create a new instance to ensure clean dependency injection for V4
	negotiator := ai.NewNegotiationAdapter() // Standard V3 negotiator
	v3Executor := v3.NewActionExecutor(s.DB, s.Logger, negotiator)

	// 3. Initialize V4 Pipeline (NOW DEFAULT)
	brancr_ai_url := os.Getenv("AI_SERVICE_URL")
	if brancr_ai_url == "" {
		brancr_ai_url = "http://localhost:8001"
	}
	llmClient := llm.NewClient(brancr_ai_url)

	s.V4Pipeline = ai_v4.NewPipeline(ai_v4.PipelineConfig{
		DB:                 s.DB,
		Logger:             s.Logger,
		LLMClient:          llmClient,
		V3Executor:         v3Executor,
		AIServiceURL:       s.Config.AIServiceURL, // Pass configured URL
		FulfillmentService: s.FulfillmentService,
	})
	s.Logger.Info("v4_pipeline_initialized",
		zap.String("status", "ready"),
		zap.String("llm_url", brancr_ai_url),
	)

	// 4. Initialize V4 Router (Traffic Controller - V4 ONLY)
	s.V4Router = router.NewAIRouter(
		s.V4Pipeline.Runner, // V4 Primary
		s.Logger,
	)
	s.Logger.Info("v4_router_initialized", zap.String("mode", "authoritative_v4_only"))

	// 5. Startup Self-Check (Safety)
	validation.RunStartupCheck(s.V4Pipeline.Runner, s.Logger)

	// Initialize Caption Optimizer
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

	// Initialize Escalation Behavior Service
	behaviorService := bot.NewEscalationBehaviorService(s.DB, s.Logger)

	escalationHandler := &bot.EscalationHandler{
		DB:                s.DB,
		Logger:            s.Logger,
		Manager:           s.BotManager,
		PaymentSecurity:   paymentSecurity,
		BehaviorService:   behaviorService,
		CheckpointService: s.CheckpointService,
		ReceiptService:    s.ReceiptService,
	}
	s.EscalationHandler = escalationHandler

	// Note: MessageHandler will be set later in initSocial,
	// so we'll need to link them after both are initialized
	return nil
}

func (s *Services) initSocial() error {
	// Create escalation function that sends Telegram alerts
	escalationFunc := func(ctx context.Context, tenantID uuid.UUID, interactionID uuid.UUID, customerName, platform, message, suggestedReply, intent, tone string, confidence float64, reason string) error {
		// Call EscalationHandler to send Telegram alert (include reason)
		if s.EscalationHandler != nil {
			return s.EscalationHandler.SendEscalation(ctx, tenantID, interactionID, customerName, platform, message, suggestedReply, intent, tone, confidence, reason)
		}
		s.Logger.Warn("escalation handler not initialized, skipping Telegram alert",
			zap.Stringer("tenant_id", tenantID),
			zap.Stringer("interaction_id", interactionID),
			zap.String("reason", reason))
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
		s.GupshupClient = gupshupClient // Store in Services for use in billing
		s.Logger.Info("✅ Gupshup Partner client initialized for WhatsApp integration")
	} else {
		s.Logger.Info("ℹ️  Gupshup Partner credentials not configured — Gupshup WhatsApp integration disabled")
	}

	// Initialize Production Architecture components
	productionBuffer := social.NewProductionMessageBuffer(s.Logger, s.Config.MaxAICallsPerMin)
	conversationLock := social.NewConversationLock(s.Logger)
	replySequencer := social.NewReplySequencer(s.Logger)
	costControlFallback := social.NewCostControlFallback(s.Logger)

	// Initialize Escalation Service (optional, for future use)
	var negotiationMgr interface{}
	escalationService := social.NewEscalationService(s.DB, s.Logger, negotiationMgr)

	// Initialize Message Handler FIRST (needed for escalation function)
	// We'll set ProductionProcessor on it after creating the processor
	// V4 is now the DEFAULT pipeline - controlled by V4 Router (Feature Flag)
	responder := NewV4PipelineAdapter(s.V4Router, s.Logger, s.DB)
	s.Logger.Info("🚀 AI V4 Router is now active (V3/V4 Hybrid)")
	if s.UsageService != nil {
		responder.SetUsageService(s.UsageService)
		s.Logger.Info("✅ UsageService wired to V4 adapter (v4 quota/cost metering enabled)")
	}

	messageHandler := &social.MessageHandler{
		DB:                  s.DB,
		Logger:              s.Logger,
		IntentDetector:      s.IntentDetector,
		Responder:           responder,
		SystemToken:         s.Config.BrancrWABAToken, // System token for provider-owned WhatsApp (fallback)
		RespondIO:           respondIOClient,          // Respond.io client for WhatsApp integration
		GupshupClient:       gupshupClient,            // Gupshup Partner client for WhatsApp integration
		EscalationFunc:      escalationFunc,
		UsageService:        s.UsageService,                      // For usage tracking
		MessageBuffer:       social.NewMessageBuffer(s.Logger),   // Legacy message buffering (fallback)
		MessageSplitter:     social.NewMessageSplitter(s.Logger), // Legacy message splitting (fallback)
		ProductionBuffer:    productionBuffer,                    // Production-grade message buffer
		ConversationLock:    conversationLock,                    // Conversation lock
		ReplySequencer:      replySequencer,                      // Reply sequencer
		CostControlFallback: costControlFallback,                 // Cost control fallback
		CheckpointService:   s.CheckpointService,
	}
	s.MessageHandler = messageHandler

	// 🆕 WIRE SEND REPLY FUNCTION TO V4 ADAPTER
	// This enables the adapter to actually send menus/offerings via WhatsApp
	responder.SetSendReplyFunc(func(ctx context.Context, tenantID uuid.UUID, platform, threadID, customerID, reply string) error {
		return messageHandler.SendReply(ctx, tenantID, platform, threadID, customerID, reply)
	})
	s.Logger.Info("✅ V4 Adapter wired to SendReply (menu sending enabled)")

	// Get NegotiationManager from MessageHandler if available (will be set later)
	// For now, pass nil - the old escalation logic in message_handler.go will handle negotiation checks
	var negotiationManager interface{}
	// Note: NegotiationManager is accessed via MessageHandler.NegotiationManager in the old logic
	// We'll set it on messageHandler if needed, but the old escalation logic doesn't require it upfront

	// Create wrapper function for OLD PROVEN escalation logic (shouldEscalateToHuman)
	shouldEscalateFunc := func(ctx context.Context, msg social.IncomingMessage, customer *storage.Customer, intent *ai.DetectedIntent, conversationID uuid.UUID, events []ai.Event, salesConv *storage.SalesConversation) (bool, string) {
		return messageHandler.ShouldEscalateToHuman(ctx, msg, customer, intent, conversationID, events, salesConv)
	}

	// Create sendReply function wrapper
	// 🆕 FIX: Call sendReply DIRECTLY to bypass structured reply system for simple sends
	// This is critical for menu sending and other direct actions that shouldn't go through
	// the message processor's sequencing/splitting logic
	sendReplyFunc := func(ctx context.Context, msg social.IncomingMessage, reply string) error {
		// Call sendReply directly on MessageHandler, NOT through the processor
		// This bypasses the structured reply sequencing for simple one-off messages
		return messageHandler.SendReply(ctx, msg.TenantID, msg.Platform, msg.ThreadID, msg.CustomerID, reply)
	}

	// Initialize Production Message Processor with OLD PROVEN escalation logic
	productionProcessor := social.NewProductionMessageProcessor(
		s.DB,
		s.Logger,
		s.IntentDetector,
		responder, // Use same responder as MessageHandler (V3 or V4)
		conversationLock,
		replySequencer,
		escalationService,
		escalationFunc,
		shouldEscalateFunc, // OLD PROVEN ESCALATION LOGIC
		negotiationManager, // For pricing approval checks
		sendReplyFunc,
	)

	// Set ProductionProcessor on MessageHandler
	messageHandler.ProductionProcessor = productionProcessor

	s.Logger.Info("✅ Production message buffer initialized")
	s.Logger.Info("✅ Conversation lock initialized")
	s.Logger.Info("✅ Reply sequencer initialized")
	s.Logger.Info("✅ Cost control fallback initialized")
	s.Logger.Info("✅ Production message processor initialized")
	s.Logger.Info("✅ Legacy message buffer initialized (fallback)")
	s.Logger.Info("✅ Legacy message splitter initialized (fallback)")

	// Start periodic cleanup for message buffer (every 5 minutes)
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if messageHandler.MessageBuffer != nil {
				messageHandler.MessageBuffer.Cleanup()
			}
		}
	}()

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
	scheduler := social.NewScheduler(s.DB, s.Logger, s.CaptionOptimizer, s.Config.SchedulerConcurrency)
	// Set QuotaService if available
	if s.QuotaService != nil {
		scheduler.QuotaService = s.QuotaService
		// Set Metrics on QuotaService for cost tracking (if available)
		if s.Metrics != nil {
			s.QuotaService.Metrics = s.Metrics
		}
		// Set QuotaService on CustomerResponder for cost tracking
		if s.CustomerResponder != nil {
			s.CustomerResponder.QuotaService = s.QuotaService
			// V3 CONSOLIDATION: ResponseComposer is now a stub, skip SetQuotaService
			// if s.CustomerResponder.ResponseComposer != nil {
			// 	s.CustomerResponder.ResponseComposer.SetQuotaService(s.QuotaService)
			// }
		}
	}
	// Set UsageService for billing tracking
	if s.UsageService != nil {
		scheduler.UsageService = s.UsageService
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
		scheduler.PostNotifier = func(ctx context.Context, tenantID uuid.UUID, platform string, success bool, message string) {
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
		s.OrderService = orderService // Store in Services struct
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

func (a *orderServiceAdapter) CreateOrderFromSalesConversation(ctx context.Context, tenantID, customerID, conversationID uuid.UUID, salesConv *storage.SalesConversation, customerName, customerPhone, customerEmail, platform string) (*storage.Order, error) {
	return a.orderService.CreateOrderFromSalesConversation(ctx, tenantID, customerID, conversationID, salesConv, customerName, customerPhone, customerEmail, platform)
}

func (a *orderServiceAdapter) ExtractOrderItems(salesConv *storage.SalesConversation) ([]storage.OrderItem, float64, error) {
	return a.orderService.ExtractOrderItems(salesConv)
}

// industryServiceAdapter adapts IndustryService to CustomerResponder interface
type industryServiceAdapter struct {
	industry *industry.Service
}

func (a *industryServiceAdapter) GetTenantIndustry(ctx context.Context, tenantID uuid.UUID) (interface{}, error) {
	return a.industry.GetTenantIndustry(ctx, tenantID)
}

func (a *industryServiceAdapter) GetIndustrySettings(ctx context.Context, tenantID uuid.UUID) (map[string]interface{}, error) {
	return a.industry.GetIndustrySettings(ctx, tenantID)
}

func (a *industryServiceAdapter) GetTenantFlow(ctx context.Context, tenantID uuid.UUID) (string, error) {
	return a.industry.GetTenantFlow(ctx, tenantID)
}

func (a *industryServiceAdapter) BuildKnowledgeContext(ctx context.Context, tenantID uuid.UUID) (string, error) {
	return a.industry.BuildKnowledgeContext(ctx, tenantID)
}

// retrievalServiceAdapter adapts RetrievalService to CustomerResponder interface
type retrievalServiceAdapter struct {
	retrieval *industry.RetrievalService
}

func (a *retrievalServiceAdapter) SearchItems(ctx context.Context, tenantID uuid.UUID, query string, limit int) (interface{}, error) {
	return a.retrieval.SearchItems(ctx, tenantID, query, limit)
}

func (a *retrievalServiceAdapter) GetAllActive(ctx context.Context, tenantID uuid.UUID, limit int) (interface{}, error) {
	return a.retrieval.GetAllActive(ctx, tenantID, limit)
}

func (a *retrievalServiceAdapter) FormatForAI(items interface{}) string {
	if results, ok := items.([]industry.RetrievalResult); ok {
		return a.retrieval.FormatForAI(results)
	}
	return ""
}

func (a *retrievalServiceAdapter) GetItemsWithImages(ctx context.Context, tenantID uuid.UUID, message string) ([]string, error) {
	return a.retrieval.GetItemsWithImages(ctx, tenantID, message)
}

func (s *Services) initEmail() {
	// Initialize Email Service (optional - will be nil if SMTP not configured)
	emailService := email.NewEmailService(s.Logger)
	// Check if SMTP is configured
	if emailService.SMTPHost == "" || emailService.SMTPUser == "" || emailService.SMTPPass == "" {
		s.Logger.Info("ℹ️  Email service not configured (SMTP settings missing) - receipt emails will be skipped")
		s.EmailService = nil
		s.AlertService = nil
		s.NewsletterService = nil
	} else {
		s.EmailService = emailService
		s.Logger.Info("✅ Email service initialized")

		// Initialize Alert Service (BotManager will be set later in initBotManager)
		// We'll re-initialize it after BotManager is ready

		// Initialize Newsletter Service
		publicURL := s.Config.PublicURL
		if publicURL == "" {
			publicURL = "https://api.brancr.com"
		}
		newsletterSvc := newsletter.NewNewsletterService(s.DB, s.Logger, emailService, publicURL)
		s.NewsletterService = newsletterSvc

		s.Logger.Info("✅ Newsletter service initialized")
	}
}

// initAlerts initializes Alert Service after BotManager is ready
func (s *Services) initAlerts() {
	if s.AlertService != nil {
		return // Already initialized
	}

	// Initialize AlertService even if EmailService is nil
	// Alerts can still work via in-app notifications and Telegram
	alertSvc := alerts.NewAlertService(s.DB, s.Logger, s.EmailService, s.BotManager)
	s.AlertService = alertSvc
	s.Logger.Info("✅ Alert service initialized")

	// Initialize AlertTriggers for automated user action alerts
	s.AlertTriggers = alerts.NewTenantAlertTriggers(alertSvc, s.DB, s.Logger)
	s.Logger.Info("✅ Alert triggers initialized")

	// Seed default alert templates (admin-editable)
	if err := admin.SeedAlertTemplates(s.DB, s.Logger); err != nil {
		s.Logger.Warn("failed to seed alert templates", zap.Error(err))
	} else {
		s.Logger.Info("✅ Alert templates seeded")
	}

	// Initialize subscription service after alert service is ready
	subscriptionSvc := subscription.NewService(s.DB, s.Logger, alertSvc)
	s.SubscriptionService = subscriptionSvc
	s.Logger.Info("✅ Subscription service initialized")

	// Initialize billing services
	s.initBilling()
}

// initBilling initializes billing services
func (s *Services) initBilling() {
	// Initialize billing service
	billingSvc := billing.NewBillingService(s.DB, s.Logger)
	s.BillingService = billingSvc

	// Initialize Gupshup usage sync service (if Gupshup client available)
	var usageSyncSvc *billing.GupshupUsageSyncService
	if s.GupshupClient != nil {
		usageSyncSvc = billing.NewGupshupUsageSyncService(s.DB, s.Logger, s.GupshupClient)
		s.UsageSyncService = usageSyncSvc // Store in Services
		s.Logger.Info("✅ Gupshup usage sync service initialized")
	}

	// Initialize usage service
	usageSvc := billing.NewUsageService(s.DB, s.Logger)
	if usageSyncSvc != nil {
		usageSvc.UsageSyncService = usageSyncSvc
	}
	s.UsageService = usageSvc

	// Initialize enforcement service
	enforcementSvc := billing.NewEnforcementService(s.DB, s.Logger, billingSvc, usageSvc)
	s.EnforcementService = enforcementSvc

	// Initialize Paystack service (if keys are configured)
	if s.Config.PaystackSecretKey != "" && s.Config.PaystackPublicKey != "" {
		paystackClient := billing.NewPaystackClient(
			s.Config.PaystackSecretKey,
			s.Config.PaystackPublicKey,
			s.Logger,
		)
		paystackSvc := billing.NewPaystackService(paystackClient, s.DB, s.Logger, billingSvc)
		s.PaystackService = paystackSvc
		s.Logger.Info("✅ Paystack service initialized")
	} else {
		s.Logger.Info("ℹ️  Paystack not configured (PAYSTACK_SECRET_KEY or PAYSTACK_PUBLIC_KEY missing)")
	}

	s.Logger.Info("✅ Billing services initialized")
}

// tenantNotifierAdapter adapts TenantBotManager to CustomerResponder interface
type tenantNotifierAdapter struct {
	botManager *bot.TenantBotManager
}

func (a *tenantNotifierAdapter) NotifyOwnerWithMarkdown(tenantID uuid.UUID, message string) {
	if a.botManager != nil {
		a.botManager.NotifyOwnerWithMarkdown(tenantID, message)
	}
}

// botGetterWrapper wraps TenantBotManager to implement social.BotGetter interface
type botGetterWrapper struct {
	manager *bot.TenantBotManager
}

func (w *botGetterWrapper) GetBot(tenantID uuid.UUID) (interface{}, bool) {
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
func (s *Services) initCheckpointService() error {
	s.CheckpointService = checkpoint.NewService(s.DB, s.Logger)
	s.Logger.Info("✅ Checkpoint service initialized")
	return nil
}

func (s *Services) initReceiptService() error {
	// Initialize Receipt Service
	emailSvc, _ := s.EmailService.(interface {
		SendReceiptEmail(to, customerName, orderNumber, receiptURL, portalURL string) error
		SendOrderStatusEmail(to, customerName, orderNumber, status, portalURL string) error
	})

	s.ReceiptService = receipt.NewReceiptService(
		s.DB,
		s.Logger,
		"",                 // Let service build from env vars
		s.Config.PublicURL, // Config has PublicURL
		emailSvc,
	)
	s.Logger.Info("✅ Receipt service initialized")
	return nil
}

func (s *Services) initFulfillmentService() error {
	s.FulfillmentService = fulfillment.NewService(s.DB, s.Logger)
	s.Logger.Info("✅ Fulfillment service initialized")
	return nil
}
