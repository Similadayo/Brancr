package bot

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/config"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"github.com/similadayo/brancr/pkg/metrics"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TenantBotManager orchestrates all tenant bots & their AI brains
type TenantBotManager struct {
	DB                *gorm.DB
	Bots              map[uuid.UUID]*tgbotapi.BotAPI
	Owners            map[uuid.UUID]int64
	Mutex             sync.RWMutex
	Logger            *zap.Logger
	Groups            *MediaGroupCollector
	AIClient          *ai.AIClient
	Memory            *ai.MemoryService
	Classifier        *ai.Classifier
	Emotion           *ai.EmotionAnalyzer
	Policy            *ai.PolicyEngine
	Responder         *ai.Responder
	AdminBot          *tgbotapi.BotAPI
	AnalyticsHandler  *AnalyticsHandler  // Analytics handler for tenant requests
	EscalationHandler *EscalationHandler // Escalation handler for managing customer escalations
	MessageHandler    interface {        // MessageHandler interface to avoid circular dependency
		ProcessMessage(ctx context.Context, msg interface{}) error
		SendReply(ctx context.Context, tenantID uuid.UUID, platform, threadID, customerID, reply string) error
	}
	Scheduler interface { // Interface to avoid circular dependency
		ExecutePostImmediately(postID uuid.UUID) error
	}
	Config                   *config.Config
	Metrics                  *metrics.Metrics
	sessionFinalizers        sync.Map
	WorkflowOrchestrator     interface{}                // Workflow orchestrator for managing workflows
	IndustrySelectionHandler *IndustrySelectionHandler  // Industry selection handler
	SessionManager           *SessionManager            // Database-backed session manager
	TaskManager              *ConversationalTaskManager // Conversational task orchestrator
	ProactiveTriggers        *ProactiveTriggers         // Proactive assistance triggers
}

// StartPostFlowWithWorkflow starts post flow using the new workflow orchestrator if available, falls back to old method
func (m *TenantBotManager) StartPostFlowWithWorkflow(bot *tgbotapi.BotAPI, chatID int64, assetID uuid.UUID) {
	if m.WorkflowOrchestrator != nil {
		// Try to use new workflow orchestrator
		if orchestrator, ok := m.WorkflowOrchestrator.(interface {
			StartPostingFlow(ctx context.Context, tenantID uuid.UUID, chatID int64, mediaAssetID uuid.UUID) error
			SetBotForPosting(bot *tgbotapi.BotAPI)
		}); ok {
			// Get tenant ID from asset
			var asset storage.MediaAsset
			if err := m.DB.First(&asset, assetID).Error; err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "I couldn't find that upload. Mind sending it again?"))
				return
			}

			// Set bot for posting workflow
			if botSetter, ok := orchestrator.(interface{ SetBotForPosting(*tgbotapi.BotAPI) }); ok {
				botSetter.SetBotForPosting(bot)
			}

			// Start workflow
			ctx := context.Background()
			if err := orchestrator.StartPostingFlow(ctx, asset.TenantID, chatID, assetID); err != nil {
				m.Logger.Error("failed to start posting workflow", zap.Error(err))
				// Fallback to old method
				m.StartPostFlow(bot, chatID, assetID)
			}
			return
		}
	}

	// Fallback to old post flow
	m.StartPostFlow(bot, chatID, assetID)
}

// ✅ Constructor — now initializes AI components too
func NewTenantBotManager(db *gorm.DB, cfg *config.Config, logger *zap.Logger, metricsSvc *metrics.Metrics) *TenantBotManager {
	var rdb *redis.Client
	addr := strings.TrimSpace(cfg.RedisAddr)
	if addr != "" {
		// Common options for handling Railway's sleep/wake cycle
		commonOpts := func(opt *redis.Options) {
			opt.DisableIdentity = true // Disable auto-detection to avoid maint_notifications errors
			opt.MaxRetries = 5         // Retry up to 5 times (helps when Redis is waking up)
			opt.MinRetryBackoff = 500 * time.Millisecond
			opt.MaxRetryBackoff = 5 * time.Second
			opt.DialTimeout = 30 * time.Second // Allow 30s for Redis to wake up
			opt.ReadTimeout = 10 * time.Second
			opt.WriteTimeout = 10 * time.Second
			opt.PoolSize = 10
			opt.MinIdleConns = 1
			opt.PoolTimeout = 30 * time.Second
			opt.ConnMaxIdleTime = 5 * time.Minute
		}

		// Parse Redis URL if it's a full URL (Railway format)
		var client *redis.Client
		if strings.HasPrefix(addr, "redis://") || strings.HasPrefix(addr, "rediss://") {
			opt, err := redis.ParseURL(addr)
			if err == nil {
				commonOpts(opt)
				// Ensure TLS is enabled for rediss:// URLs (Upstash, etc.)
				if strings.HasPrefix(addr, "rediss://") && opt.TLSConfig == nil {
					opt.TLSConfig = &tls.Config{
						MinVersion: tls.VersionTLS12,
					}
				}
				client = redis.NewClient(opt)
			} else {
				// Fallback to direct address
				fallbackOpt := &redis.Options{Addr: addr}
				commonOpts(fallbackOpt)
				client = redis.NewClient(fallbackOpt)
			}
		} else {
			// Direct address format (host:port)
			opt := &redis.Options{Addr: addr}
			commonOpts(opt)
			client = redis.NewClient(opt)
		}

		if err := client.Ping(context.Background()).Err(); err != nil {
			logger.Warn("redis unavailable, falling back to in-memory collectors",
				zap.Error(err))
			// Still set client - it might reconnect later
			rdb = client
		} else {
			rdb = client
		}
	} else if logger != nil {
		logger.Info("Redis not configured — using in-memory collectors for bot media.")
	}

	// ✅ Initialize AI Embedding engine
	embedder := ai.NewEmbeddingClient()

	// ✅ Initialize AI core stack
	client := ai.NewAIClient()
	memory := &ai.MemoryService{
		DB:      db,
		Encoder: embedder, // 🧠 FIXED: plug in embedding engine
	}
	classifier := &ai.Classifier{Client: client}
	emotion := &ai.EmotionAnalyzer{Client: client}
	policy := ai.NewPolicyEngine()

	// ✅ Responder now has full context (no nils)
	responder := &ai.Responder{
		Client:     client,
		Memory:     memory,
		Persona:    storage.Persona{BotName: "Brancr Assistant", Tone: "friendly and professional"},
		Classifier: classifier,
		Emotion:    emotion,
		Policy:     policy,
	}

	manager := &TenantBotManager{
		DB:             db,
		Bots:           make(map[uuid.UUID]*tgbotapi.BotAPI),
		Owners:         make(map[uuid.UUID]int64),
		Logger:         logger,
		Groups:         NewMediaGroupCollector(rdb),
		AIClient:       client,
		Memory:         memory,
		Classifier:     classifier,
		SessionManager: NewSessionManager(db, logger),
		Emotion:        emotion,
		Policy:         policy,
		Responder:      responder,
		Config:         cfg,
		Metrics:        metricsSvc,
	}

	// Initialize conversational task manager
	manager.TaskManager = NewConversationalTaskManager(db, logger, memory, manager)
	manager.ProactiveTriggers = NewProactiveTriggers(db, logger)

	return manager
}

// ✅ Initialize all bots for active tenants
func (m *TenantBotManager) InitBots() error {
	publicURL, err := utils.GetPublicURL()
	if err != nil {
		log.Printf("⚠️ Could not detect public tunnel: %v", err)
	} else {
		log.Printf("🌐 Detected public URL: %s", publicURL)
	}

	var tenants []storage.Tenant
	// Load tenants with active status (case-insensitive: "active", "Active", "ACTIVE")
	if err := m.DB.Where("LOWER(status) = ?", "active").Find(&tenants).Error; err != nil {
		return fmt.Errorf("failed to fetch tenants: %w", err)
	}
	if len(tenants) == 0 {
		log.Println("⚠️ No active tenants found — skipping bot startup.")
		return nil
	}

	for _, t := range tenants {
		time.Sleep(2 * time.Second)

		// Try initial startup with retry logic
		ctx := context.Background()
		err := m.retryBotStartup(ctx, &t, 3) // 3 retries with exponential backoff
		if err != nil {
			// Check if it's a permanent error
			if isRetryableError(err) {
				// Retryable error - will be picked up by retry worker
				if m.Logger != nil {
					m.Logger.Warn("❌ Failed to start bot (will retry)",
						zap.Stringer("tenant_id", t.ID),
						zap.String("tenant_name", t.Name),
						zap.Error(err))
				} else {
					log.Printf("❌ Failed to start bot for tenant %s (will retry): %v", t.Name, err)
				}
			} else {
				// Permanent error - don't retry
				if m.Logger != nil {
					m.Logger.Error("❌ Failed to start bot (permanent error, won't retry)",
						zap.Stringer("tenant_id", t.ID),
						zap.String("tenant_name", t.Name),
						zap.Error(err))
				} else {
					log.Printf("❌ Failed to start bot for tenant %s (permanent error): %v", t.Name, err)
				}
			}
			continue
		}

		bot, ok := m.GetBot(t.ID)
		if !ok || bot == nil {
			log.Printf("⚠️ Bot instance not found after startup for tenant %s", t.ID.String())
			continue
		}

		m.Mutex.Lock()
		m.Owners[t.ID] = t.OwnerTelegramID
		m.Mutex.Unlock()

		bot.Request(tgbotapi.DeleteWebhookConfig{})
		if publicURL != "" {
			webhook := fmt.Sprintf("%s/bot/webhook/%s", publicURL, t.ID.String())
			cfg, err := tgbotapi.NewWebhook(webhook)
			if err != nil {
				log.Printf("❌ Invalid webhook URL for tenant %s: %v", t.ID.String(), err)
				continue
			}
			if _, err = bot.Request(cfg); err != nil {
				log.Printf("❌ Failed to set webhook for tenant %s: %v", t.ID.String(), err)
			} else {
				log.Printf("✅ Webhook set for tenant %s → %s", t.ID.String(), webhook)
			}
		} else {
			log.Printf("⚠️ No public URL detected — webhook skipped for tenant %s.", t.ID.String())
		}

	}

	go m.selfHealWebhooks()
	log.Printf("🤖 Initialized %d active tenant bots (webhook mode)", len(tenants))
	return nil
}

// ✅ Self-healing webhook refresher
func (m *TenantBotManager) selfHealWebhooks() {
	for {
		time.Sleep(10 * time.Minute)
		publicURL, err := utils.GetPublicURL()
		if err != nil {
			log.Printf("⚠️ Could not refresh tunnel URL: %v", err)
			continue
		}
		for id, bot := range m.Bots {
			// CRITICAL: Use id.String() for UUID formatting and correct path format
			expected := fmt.Sprintf("%s/bot/webhook/%s", publicURL, id.String())
			info, _ := bot.GetWebhookInfo()
			if info.URL != expected {
				bot.Request(tgbotapi.DeleteWebhookConfig{})
				wh, _ := tgbotapi.NewWebhook(expected)
				if _, err := bot.Request(wh); err != nil {
					log.Printf("⚠️ Failed to reset webhook for tenant %s: %v", id.String(), err)
				} else {
					log.Printf("🔄 Refreshed webhook for tenant %s → %s", id.String(), expected)
				}
			}
			if info.LastErrorMessage != "" {
				log.Printf("⚠️ Webhook error for tenant %s: %s", id.String(), info.LastErrorMessage)
			}
		}
	}
}

// ✅ Start a bot instance per tenant
func (m *TenantBotManager) startTenantBot(t *storage.Tenant) error {
	m.Mutex.Lock()
	defer m.Mutex.Unlock()

	if _, exists := m.Bots[t.ID]; exists {
		return nil
	}

	// Validate token exists and is not empty
	if t.TelegramBotToken == "" {
		return fmt.Errorf("bot token is empty for tenant %s", t.ID)
	}

	// Attempt to decrypt token
	token, err := utils.DecryptToken(t.TelegramBotToken)
	if err != nil {
		// Log detailed error for debugging
		if m.Logger != nil {
			m.Logger.Error("failed to decrypt bot token",
				zap.Stringer("tenant_id", t.ID),
				zap.String("tenant_name", t.Name),
				zap.Int("token_length", len(t.TelegramBotToken)),
				zap.Error(err))
		}
		return fmt.Errorf("decrypt bot token for tenant %s (%s): %w", t.ID, t.Name, err)
	}

	// Validate decrypted token is not empty
	if token == "" {
		return fmt.Errorf("decrypted bot token is empty for tenant %s (%s)", t.ID, t.Name)
	}

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		if m.Logger != nil {
			m.Logger.Error("invalid bot token after decryption",
				zap.Stringer("tenant_id", t.ID),
				zap.String("tenant_name", t.Name),
				zap.Error(err))
		}
		return fmt.Errorf("invalid bot token for tenant %s (%s): %w", t.ID, t.Name, err)
	}

	m.Bots[t.ID] = bot
	m.Owners[t.ID] = t.OwnerTelegramID

	log.Printf("🤖 Tenant bot loaded → ID %s | Owner %d", t.ID, t.OwnerTelegramID)
	return nil
}

// ✅ Safe bot retrieval
func (m *TenantBotManager) GetBot(id uuid.UUID) (*tgbotapi.BotAPI, bool) {
	m.Mutex.RLock()
	defer m.Mutex.RUnlock()
	b, ok := m.Bots[id]
	return b, ok
}

// ✅ Owner lookup cache
func (m *TenantBotManager) GetOwnerID(tenantID uuid.UUID) int64 {
	m.Mutex.RLock()
	defer m.Mutex.RUnlock()
	return m.Owners[tenantID]
}

// NotifyOwner sends a direct message to the tenant owner via Telegram.
func (m *TenantBotManager) NotifyOwner(tenantID uuid.UUID, message string) {
	if m == nil {
		return
	}
	if strings.TrimSpace(message) == "" {
		return
	}
	bot := m.GetBotInstance(tenantID)
	if bot == nil {
		if m.Logger != nil {
			m.Logger.Warn("notify owner: bot instance missing", zap.Stringer("tenant_id", tenantID))
		}
		return
	}
	ownerID := m.GetOwnerID(tenantID)
	if ownerID == 0 {
		var tenant storage.Tenant
		if err := m.DB.WithContext(context.Background()).First(&tenant, tenantID).Error; err != nil {
			if m.Logger != nil {
				m.Logger.Warn("notify owner: failed to resolve owner", zap.Stringer("tenant_id", tenantID), zap.Error(err))
			}
			return
		}
		if tenant.OwnerTelegramID == 0 {
			if m.Logger != nil {
				m.Logger.Warn("notify owner: tenant has no owner telegram id", zap.Stringer("tenant_id", tenantID))
			}
			return
		}
		ownerID = tenant.OwnerTelegramID
		m.Mutex.Lock()
		m.Owners[tenantID] = ownerID
		m.Mutex.Unlock()
	}
	msg := tgbotapi.NewMessage(ownerID, message)
	if _, err := bot.Send(msg); err != nil && m.Logger != nil {
		m.Logger.Warn("notify owner: failed to send message", zap.Stringer("tenant_id", tenantID), zap.Error(err))
	}
}

// NotifyOwnerWithMarkdown sends a message with markdown formatting
func (m *TenantBotManager) NotifyOwnerWithMarkdown(tenantID uuid.UUID, message string) {
	if m == nil {
		return
	}
	if strings.TrimSpace(message) == "" {
		return
	}
	bot := m.GetBotInstance(tenantID)
	if bot == nil {
		if m.Logger != nil {
			m.Logger.Warn("notify owner: bot instance missing", zap.Stringer("tenant_id", tenantID))
		}
		return
	}
	ownerID := m.GetOwnerID(tenantID)
	if ownerID == 0 {
		var tenant storage.Tenant
		if err := m.DB.WithContext(context.Background()).First(&tenant, tenantID).Error; err != nil {
			if m.Logger != nil {
				m.Logger.Warn("notify owner: failed to resolve owner", zap.Stringer("tenant_id", tenantID), zap.Error(err))
			}
			return
		}
		if tenant.OwnerTelegramID == 0 {
			if m.Logger != nil {
				m.Logger.Warn("notify owner: tenant has no owner telegram id", zap.Stringer("tenant_id", tenantID))
			}
			return
		}
		ownerID = tenant.OwnerTelegramID
		m.Mutex.Lock()
		m.Owners[tenantID] = ownerID
		m.Mutex.Unlock()
	}
	msg := tgbotapi.NewMessage(ownerID, message)
	msg.ParseMode = "Markdown"
	if _, err := bot.Send(msg); err != nil && m.Logger != nil {
		m.Logger.Warn("notify owner: failed to send message", zap.Stringer("tenant_id", tenantID), zap.Error(err))
	}
}

// ✅ Stop bot
func (m *TenantBotManager) StopBot(tenantID uuid.UUID) {
	m.Mutex.Lock()
	defer m.Mutex.Unlock()
	if _, ok := m.Bots[tenantID]; ok {
		delete(m.Bots, tenantID)
		delete(m.Owners, tenantID)
		log.Printf("🛑 Bot stopped for tenant ID %s", tenantID.String())
	}
}

// ✅ Generate AI Response for messages
func (m *TenantBotManager) GenerateAIResponse(ctx context.Context, tenantID uuid.UUID, userMsg string) (string, error) {
	persona := ai.GetPersona(m.DB, tenantID)
	responder := &ai.Responder{
		Client:     m.AIClient,
		Memory:     m.Memory,
		Persona:    persona,
		Classifier: m.Classifier,
		Emotion:    m.Emotion,
		Policy:     m.Policy,
	}
	return responder.GenerateConversationalReply(ctx, tenantID, userMsg)
}

// ✅ GetBotInstance returns a bot instance if it exists
func (m *TenantBotManager) GetBotInstance(tenantID uuid.UUID) *tgbotapi.BotAPI {
	m.Mutex.RLock()
	defer m.Mutex.RUnlock()
	if bot, ok := m.Bots[tenantID]; ok {
		return bot
	}
	return nil
}

// ✅ StartBotForTenant starts a bot instance + registers webhook
func (m *TenantBotManager) StartBotForTenant(tenant *storage.Tenant) error {
	// Start bot in memory
	if err := m.startTenantBot(tenant); err != nil {
		return fmt.Errorf("failed to start tenant bot: %w", err)
	}

	// ✅ Retrieve bot instance
	bot, ok := m.GetBot(tenant.ID)
	if !ok || bot == nil {
		return fmt.Errorf("bot instance not found after creation for tenant %s", tenant.ID.String())
	}

	// Detect public URL
	publicURL, err := utils.GetPublicURL()
	if err != nil || publicURL == "" {
		m.Logger.Warn("no public URL detected for webhook registration",
			zap.Stringer("tenant_id", tenant.ID))
		return nil
	}

	// ✅ Webhook setup with retry
	webhook := fmt.Sprintf("%s/bot/webhook/%s", publicURL, tenant.ID.String())
	if err := m.setWebhookWithRetry(bot, tenant.ID, webhook, 3); err != nil {
		m.Logger.Error("failed to set webhook after retries",
			zap.Stringer("tenant_id", tenant.ID),
			zap.Error(err))
		return err
	}

	m.Logger.Info("✅ Tenant bot started successfully",
		zap.Stringer("tenant_id", tenant.ID),
		zap.String("webhook", webhook),
		zap.String("bot_username", bot.Self.UserName))

	return nil
}

// ✅ setWebhookWithRetry safely retries webhook setup when Telegram rate-limits
func (m *TenantBotManager) setWebhookWithRetry(bot *tgbotapi.BotAPI, tenantID uuid.UUID, webhook string, maxRetries int) error {
	var err error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		cfg, _ := tgbotapi.NewWebhook(webhook)
		_, err = bot.Request(cfg)
		if err == nil {
			m.Logger.Info("✅ Webhook set successfully",
				zap.Stringer("tenant_id", tenantID),
				zap.String("webhook", webhook))
			return nil
		}

		// Check for Telegram rate-limit hint
		if strings.Contains(err.Error(), "Too Many Requests") {
			m.Logger.Warn("⏳ Telegram rate limit hit, retrying...",
				zap.Int("attempt", attempt),
				zap.Stringer("tenant_id", tenantID),
				zap.Error(err))
			time.Sleep(time.Duration(attempt) * time.Second) // exponential-ish backoff
			continue
		}

		m.Logger.Warn("⚠️ Webhook registration attempt failed",
			zap.Int("attempt", attempt),
			zap.Stringer("tenant_id", tenantID),
			zap.Error(err))
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("failed to set webhook after %d retries: %w", maxRetries, err)
}

func (m *TenantBotManager) WaitForBot(tenantID uuid.UUID, timeout time.Duration) *tgbotapi.BotAPI {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		bot, ok := m.GetBot(tenantID)
		if ok && bot != nil {
			return bot
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil
}

// TrackOrCreateCustomer finds or creates a cross-platform customer record
func (m *TenantBotManager) TrackOrCreateCustomer(
	ctx context.Context,
	tenantID uuid.UUID,
	channel, platformID, displayName, username, profilePic string,
) (*storage.Customer, error) {

	customer := &storage.Customer{
		TenantID:      tenantID,
		Channel:       channel,
		PlatformID:    platformID,
		DisplayName:   displayName,
		Username:      username,
		ProfilePicURL: profilePic,
		LastMessageAt: time.Now(),
	}

	err := m.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "channel"}, {Name: "platform_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_message_at", "display_name", "profile_pic_url"}),
	}).Create(customer).Error

	if err != nil {
		m.Logger.Error("failed to track customer",
			zap.Stringer("tenant_id", tenantID),
			zap.String("channel", channel),
			zap.String("platform_id", platformID),
			zap.Error(err))
		return nil, err
	}

	return customer, nil
}
