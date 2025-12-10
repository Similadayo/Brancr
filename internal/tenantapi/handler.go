package tenantapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudinary/cloudinary-go/v2"
	"github.com/cloudinary/cloudinary-go/v2/api/uploader"
	"github.com/go-chi/chi/v5"
	redis "github.com/redis/go-redis/v9"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/cache"
	"github.com/similadayo/brancr/internal/config"
	"github.com/similadayo/brancr/internal/industry"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/social"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"github.com/similadayo/brancr/internal/workflows"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	SessionCookie = "brancr_tenant_session"
	sessionTTL    = 7 * 24 * time.Hour
)

// sanitizeForAppName removes special characters from a string, keeping only alphanumeric
// This ensures app names meet Gupshup's requirements (alphanumeric only)
func sanitizeForAppName(name string) string {
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			result.WriteRune(r)
		}
	}
	sanitized := result.String()
	if sanitized == "" {
		return "App" // Fallback if name becomes empty
	}
	return sanitized
}

type Handler struct {
	DB                   *gorm.DB
	Logger               *zap.Logger
	Config               *config.Config
	Redis                *redis.Client
	Cache                *cache.Cache // Redis cache for sessions and tenant data
	WorkflowOrchestrator *workflows.WorkflowOrchestrator
	Scheduler            *social.Scheduler
	AIClient             *ai.AIClient
	MessageHandler       *social.MessageHandler           // For sending replies via MessageHandler
	IndustryService      *industry.Service                // Industry service for industry-aware features
	InstagramInsights    *social.InstagramInsightsService // Instagram insights service
	EmailService         interface {
		SendReceiptEmail(to, customerName, orderNumber, receiptURL, portalURL string) error
	} // Optional email service for sending receipts

	signupCache sync.Map
	tokenCache  sync.Map
}

func NewHandler(db *gorm.DB, logger *zap.Logger, cfg *config.Config, redis *redis.Client, orchestrator *workflows.WorkflowOrchestrator, scheduler *social.Scheduler, aiClient *ai.AIClient, messageHandler *social.MessageHandler, industryService *industry.Service, instagramInsights *social.InstagramInsightsService, emailService interface {
	SendReceiptEmail(to, customerName, orderNumber, receiptURL, portalURL string) error
}, cacheService *cache.Cache) *Handler {
	return &Handler{
		DB:                   db,
		Logger:               logger,
		Config:               cfg,
		Redis:                redis,
		Cache:                cacheService,
		WorkflowOrchestrator: orchestrator,
		Scheduler:            scheduler,
		AIClient:             aiClient,
		MessageHandler:       messageHandler,
		IndustryService:      industryService,
		InstagramInsights:    instagramInsights,
		EmailService:         emailService,
	}
}

// getCloudinaryURL gets Cloudinary URL from config or environment
func (h *Handler) getCloudinaryURL() string {
	// First try CLOUDINARY_URL env var
	if url := os.Getenv("CLOUDINARY_URL"); url != "" {
		return url
	}
	// If not available, try constructing from config components
	if h.Config.CloudName != "" && h.Config.CloudAPIKey != "" && h.Config.CloudAPISecret != "" {
		return fmt.Sprintf("cloudinary://%s:%s@%s", h.Config.CloudAPIKey, h.Config.CloudAPISecret, h.Config.CloudName)
	}
	return ""
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	// Note: OAuth endpoints are registered in core/router.go:
	// - GET /api/oauth/meta/start -> MetaOAuthHandler.StartOAuth
	// - GET /api/oauth/meta/callback -> MetaOAuthHandler.Callback
	// - GET /api/oauth/tiktok/start -> TikTokOAuthHandler.StartOAuth
	// - GET /api/oauth/tiktok/callback -> TikTokOAuthHandler.Callback

	r.Route("/auth", func(r chi.Router) {
		r.Post("/signup", h.signup)
		r.Post("/login", h.login)
		r.Post("/logout", h.logout)
		r.Group(func(r chi.Router) {
			r.Use(middleware.TenantAuth([]byte(h.Config.TenantJWTSecret), SessionCookie))
			r.Get("/me", h.me)
		})
	})

	// Customer Portal (public, token-based access)
	r.Route("/portal", func(r chi.Router) {
		r.Get("/order", h.getOrderByPortalToken)
		r.Get("/receipt", h.getReceiptByPortalToken)
		r.Get("/orders", h.listCustomerOrders)
	})

	r.Group(func(r chi.Router) {
		r.Use(middleware.TenantAuth([]byte(h.Config.TenantJWTSecret), SessionCookie))

		// Onboarding endpoints (exempt from onboarding check - users need to access these to complete onboarding)
		r.Get("/tenant/onboarding/status", h.getOnboardingStatus)
		r.Post("/tenant/onboarding/business-profile", h.updateBusinessProfile)
		r.Post("/tenant/onboarding/persona", h.updatePersona)
		r.Post("/tenant/onboarding/business-details", h.updateBusinessDetails)
		r.Post("/tenant/onboarding/industry", h.setOnboardingIndustry)
		r.Post("/tenant/onboarding/complete", h.completeOnboarding)

		// All other routes require onboarding to be complete
		r.Group(func(r chi.Router) {
			r.Use(middleware.OnboardingCheck(h.DB, h.Logger))

			// Post-onboarding settings
			r.Put("/tenant/settings/business-profile", h.updateBusinessProfileSettings)
			r.Put("/tenant/settings/persona", h.updatePersonaSettings)
			r.Put("/tenant/settings/business-details", h.updateBusinessDetailsSettings)

			r.Get("/tenant/social-accounts", h.listIntegrations) // backward-compatible

			// Integration CRUD
			r.Get("/tenant/integrations", h.listIntegrations)
			r.Get("/tenant/integrations/{platform}", h.getIntegration)
			r.Delete("/tenant/integrations/{platform}", h.deleteIntegration)
			r.Post("/tenant/integrations/{platform}/verify", h.verifyIntegration)
			r.Post("/tenant/integrations/{platform}/repair", h.repairIntegration)
			r.Get("/tenant/integrations/{platform}/debug", h.debugIntegration) // Debug endpoint to check account details
			// Manual Page subscription endpoint (for Instagram)
			r.Post("/tenant/integrations/instagram/subscribe", h.subscribeInstagramPage)

			// Instagram Insights
			r.Get("/tenant/instagram/insights/account", h.getInstagramAccountInsights)
			r.Get("/tenant/instagram/insights/media/{mediaId}", h.getInstagramMediaInsights)

			// Scheduled posts CRUD
			r.Get("/tenant/scheduled-posts", h.listScheduledPosts)
			r.Get("/tenant/scheduled-posts/{id}", h.getScheduledPost)
			r.Put("/tenant/scheduled-posts/{id}", h.updateScheduledPost)
			r.Delete("/tenant/scheduled-posts/{id}", h.deleteScheduledPost)
			// Calendar endpoints
			r.Get("/tenant/calendar", h.getCalendar)
			r.Get("/tenant/calendar/{date}", h.getCalendarDay)
			r.Post("/tenant/calendar", h.createCalendarEntry)
			r.Put("/tenant/calendar/{date}", h.updateCalendarEntry)

			// Overview and stats
			r.Get("/tenant/overview", h.overview)

			// Intent catalog management
			r.Get("/tenant/intent-catalog", h.listIntentCatalog)
			r.Post("/tenant/intent-catalog", h.createIntentCatalog)
			r.Put("/tenant/intent-catalog/{id}", h.updateIntentCatalog)
			r.Delete("/tenant/intent-catalog/{id}", h.deleteIntentCatalog)

			// Industry management
			r.Get("/tenant/industries", h.listIndustries)
			r.Get("/tenant/industry", h.getTenantIndustry)
			r.Post("/tenant/industry", h.setTenantIndustry)

			// Product management (E-commerce)
			r.Get("/tenant/products", h.listProducts)
			r.Post("/tenant/products", h.createProduct)
			r.Get("/tenant/products/{id}", h.getProduct)
			r.Put("/tenant/products/{id}", h.updateProduct)
			r.Delete("/tenant/products/{id}", h.deleteProduct)

			// Menu item management (Restaurants)
			r.Get("/tenant/menu-items", h.listMenuItems)
			r.Post("/tenant/menu-items", h.createMenuItem)
			r.Get("/tenant/menu-items/{id}", h.getMenuItem)
			r.Put("/tenant/menu-items/{id}", h.updateMenuItem)
			r.Delete("/tenant/menu-items/{id}", h.deleteMenuItem)

			// Service management (Consultants/Agencies)
			r.Get("/tenant/services", h.listServices)
			r.Post("/tenant/services", h.createService)
			r.Get("/tenant/services/{id}", h.getService)
			r.Put("/tenant/services/{id}", h.updateService)
			r.Delete("/tenant/services/{id}", h.deleteService)

			// Payment Account management
			r.Get("/tenant/payment-accounts", h.listPaymentAccounts)
			r.Post("/tenant/payment-accounts", h.createPaymentAccount)
			r.Get("/tenant/payment-accounts/{id}", h.getPaymentAccount)
			r.Put("/tenant/payment-accounts/{id}", h.updatePaymentAccount)
			r.Delete("/tenant/payment-accounts/{id}", h.deletePaymentAccount)
			r.Put("/tenant/payment-accounts/{id}/set-default", h.setDefaultPaymentAccount)

			// Order management
			r.Get("/tenant/orders", h.listOrders)
			r.Get("/tenant/orders/{id}", h.getOrder)
			r.Put("/tenant/orders/{id}", h.updateOrder)
			r.Put("/tenant/orders/{id}/confirm-payment", h.confirmOrderPayment)
			r.Get("/tenant/orders/{orderId}/portal-token", h.generatePortalToken)
			r.Get("/tenant/orders/stats", h.getOrderStats)

			// Payment management
			r.Get("/tenant/payments", h.listPayments)
			r.Get("/tenant/payments/stats", h.getPaymentStats)
			r.Get("/tenant/payments/{id}", h.getPayment)
			r.Put("/tenant/payments/{id}/verify", h.verifyPayment)
			r.Post("/tenant/payments/{id}/generate-receipt", h.generateReceipt)
			r.Put("/tenant/payments/{id}/dispute", h.disputePayment)

			// Webhook management
			r.Get("/tenant/webhooks", h.listWebhooks)
			r.Post("/tenant/webhooks", h.addWebhook)
			r.Delete("/tenant/webhooks", h.removeWebhook)
			r.Post("/tenant/webhooks/regenerate-secret", h.regenerateWebhookSecret)

			// WhatsApp provider-owned model (new)
			r.Get("/tenant/whatsapp/numbers", h.listAvailablePhoneNumbers)
			r.Post("/tenant/whatsapp/numbers/{id}/assign", h.assignPhoneNumber)
			r.Post("/tenant/whatsapp/select-number", h.selectWhatsAppNumber)
			r.Get("/tenant/whatsapp/current", h.getAssignedPhoneNumber)
			r.Post("/tenant/whatsapp/disconnect", h.disconnectPhoneNumber)
			r.Get("/tenant/whatsapp/webhook-status", h.checkWebhookStatus)
			r.Post("/tenant/whatsapp/resubscribe", h.resubscribeWebhook)
			r.Get("/tenant/whatsapp/diagnostics", h.comprehensiveDiagnostics)

			// Tenant-provided phone numbers (for Meta direct)
			r.Post("/tenant/whatsapp/request-number", h.requestTenantPhoneNumber)
			r.Post("/tenant/whatsapp/verify-number", h.verifyTenantPhoneNumber)
			r.Post("/tenant/whatsapp/check-number", h.checkTenantPhoneNumber) // Check if number is ready to verify

			// New simplified WhatsApp connection (Respond.io)
			r.Post("/tenant/whatsapp/connect", h.connectWhatsApp)
			r.Get("/tenant/whatsapp/connection-status", h.getWhatsAppConnectionStatus)
			r.Post("/tenant/whatsapp/refresh-status", h.refreshWhatsAppStatus) // Manual refresh - calls GetAppDetails

			// Legacy embedded signup (deprecated - keeping for migration)
			r.Route("/internal/meta/whatsapp", func(r chi.Router) {
				r.Post("/code", h.handleWhatsAppExchangeCode)
				r.Post("/session", h.handleWhatsAppSession)
			})

			// Templates CRUD
			r.Get("/tenant/templates", h.listTemplates)
			r.Post("/tenant/templates", h.createTemplate)
			r.Get("/tenant/templates/{id}", h.getTemplate)
			r.Put("/tenant/templates/{id}", h.updateTemplate)
			r.Delete("/tenant/templates/{id}", h.deleteTemplate)

			// Conversations API
			r.Get("/tenant/conversations", h.listConversations)
			r.Get("/tenant/conversations/{id}", h.getConversation)
			r.Post("/tenant/conversations/{id}/replies", h.sendReply)
			r.Put("/tenant/conversations/{id}", h.updateConversation)
			r.Post("/tenant/conversations/{id}/suggest-replies", h.suggestReplies)

			// Escalations API
			r.Get("/tenant/escalations", h.listEscalations)
			r.Get("/tenant/escalations/stats", h.getEscalationStats)
			r.Get("/tenant/escalations/{id}", h.getEscalation)

			// Campaigns/Posts Stats API
			r.Get("/tenant/campaigns/stats", h.getCampaignStats)

			// Caption AI Suggestions API
			r.Post("/tenant/captions/generate-from-keywords", h.generateCaptionFromKeywords)
			r.Post("/tenant/captions/rephrase", h.rephraseCaption)
			r.Post("/tenant/captions/fine-tune", h.fineTuneCaption)
			r.Post("/tenant/escalations/{id}/approve", h.approveEscalation)
			r.Post("/tenant/escalations/{id}/reply", h.replyToEscalation)
			r.Post("/tenant/escalations/{id}/ignore", h.ignoreEscalation)
			r.Post("/tenant/escalations/{id}/resolve", h.resolveEscalation)

			// Analytics API
			r.Get("/tenant/analytics", h.getAnalytics)
			r.Get("/tenant/analytics/performance", h.getPerformanceSummary)

			// Media Library
			r.Get("/tenant/media", h.listMedia)
			r.Get("/tenant/media/{id}", h.getMedia)
			r.Post("/tenant/media", h.createMedia)
			r.Put("/tenant/media/{id}", h.updateMedia)
			r.Delete("/tenant/media/{id}", h.deleteMedia)

			// Post creation
			r.Post("/tenant/posts", h.createPost)
			r.Post("/tenant/posts/generate-caption", h.generateCaption)
			r.Post("/tenant/posts/{id}/publish", h.publishPost)
			r.Get("/tenant/posts/optimal-times", h.getOptimalTimes)

			// Bulk uploads
			r.Get("/tenant/bulk-uploads", h.listBulkUploads)
			r.Get("/tenant/bulk-uploads/{id}", h.getBulkUpload)
			r.Post("/tenant/bulk-uploads", h.createBulkUpload)
			r.Put("/tenant/bulk-uploads/{id}", h.updateBulkUploadStatus)
			r.Post("/tenant/bulk-uploads/{id}/cancel", h.cancelBulkUpload)

			// Team Management API (coming soon - placeholder)
			r.Get("/tenant/team/members", h.listTeamMembers)
			r.Post("/tenant/team/invite", h.inviteTeamMember)
			r.Get("/tenant/team/members/{id}", h.getTeamMember)
			r.Put("/tenant/team/members/{id}", h.updateTeamMember)
			r.Delete("/tenant/team/members/{id}", h.deleteTeamMember)

			// TikTok Video Management API
			r.Route("/tenant/tiktok", func(r chi.Router) {
				// Video listing and details
				r.Get("/videos", h.listTikTokVideos)
				r.Get("/videos/{video_id}", h.getTikTokVideo)
				r.Get("/videos/status/{publish_id}", h.getTikTokVideoStatus)
				r.Delete("/videos/{video_id}", h.deleteTikTokVideo)

				// Analytics endpoints removed - TikTok analytics functionality disabled

				// Comment management
				r.Get("/videos/{video_id}/comments", h.listTikTokComments)
				r.Post("/videos/{video_id}/comments/{comment_id}/reply", h.replyToTikTokComment)
			})
		}) // End onboarding check middleware group
	}) // End tenant auth middleware group
}

// OAuth handlers are implemented in internal/social/meta_oauth.go and internal/social/tiktok_oauth.go
// and registered in internal/core/router.go as:
// - GET /api/oauth/meta/start -> MetaOAuthHandler.StartOAuth
// - GET /api/oauth/meta/callback -> MetaOAuthHandler.Callback
// - GET /api/oauth/tiktok/start -> TikTokOAuthHandler.StartOAuth
// - GET /api/oauth/tiktok/callback -> TikTokOAuthHandler.Callback

// Auth handlers (signup, login, logout, me) moved to auth_handlers.go

func (h *Handler) listSocialAccounts(w http.ResponseWriter, r *http.Request) {
	h.listIntegrations(w, r)
}

func (h *Handler) listIntegrations(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var accounts []storage.SocialAccount
	if err := h.DB.Where("tenant_id = ?", claims.TenantID).Find(&accounts).Error; err != nil {
		h.Logger.Error("fetch social accounts", zap.Error(err))
		http.Error(w, "could not fetch accounts", http.StatusInternalServerError)
		return
	}

	type integration struct {
		ID              uint       `json:"id"`
		Platform        string     `json:"platform"`
		Connected       bool       `json:"connected"`
		Username        string     `json:"username"`
		ExternalID      string     `json:"external_id"`
		PageID          string     `json:"page_id"`
		PageName        string     `json:"page_name,omitempty"`        // Facebook Page name (for Facebook and Instagram)
		InstagramHandle string     `json:"instagram_handle,omitempty"` // Instagram username/handle (for Instagram)
		Mode            string     `json:"mode"`
		ExpiresAt       *time.Time `json:"expires_at"`
		CreatedAt       time.Time  `json:"created_at"`
		UpdatedAt       time.Time  `json:"updated_at"`
	}

	resp := make([]integration, 0, len(accounts)+1) // +1 for Telegram

	// Add Telegram bot integration status
	var tenant storage.Tenant
	if err := h.DB.First(&tenant, claims.TenantID).Error; err == nil {
		// Telegram is considered connected if bot token exists
		// OwnerTelegramID is optional - can be set later during onboarding
		telegramConnected := tenant.TelegramBotToken != ""

		// If we have a bot token but no username, try to get it from bot instance
		botUsername := tenant.BotUsername
		if botUsername == "" && telegramConnected {
			// Try to get bot username from token (optional enhancement)
			// For now, just use what's stored
		}

		telegramIg := integration{
			ID:        0, // Telegram doesn't have a SocialAccount ID
			Platform:  "telegram",
			Connected: telegramConnected,
			Username:  botUsername,
			CreatedAt: tenant.CreatedAt,
			UpdatedAt: tenant.UpdatedAt,
		}
		resp = append(resp, telegramIg)
	}

	for _, acc := range accounts {
		// Determine connection status: account is connected if:
		// 1. Connected flag is true, OR
		// 2. Has a valid access token (not empty) and not expired
		isConnected := acc.Connected
		if !isConnected && acc.AccessToken != "" {
			// Check if token is not expired (if ExpiresAt is set)
			if acc.ExpiresAt == nil || acc.ExpiresAt.After(time.Now()) {
				isConnected = true
				// Auto-fix: Update the Connected flag in database if it's wrong
				if acc.Connected != isConnected {
					h.Logger.Info("Auto-fixing connection status",
						zap.Uint("account_id", acc.ID),
						zap.String("platform", acc.Platform),
						zap.Uint("tenant_id", claims.TenantID))
					acc.Connected = true
					if err := h.DB.Model(&acc).Update("connected", true).Error; err != nil {
						h.Logger.Warn("failed to auto-fix connection status", zap.Error(err))
					}
				}
			}
		}

		ig := integration{
			ID:         acc.ID,
			Platform:   acc.Platform,
			Connected:  isConnected,
			Username:   acc.Username,
			ExternalID: acc.ExternalID,
			PageID:     acc.PageID,
			Mode:       acc.Mode,
			ExpiresAt:  acc.ExpiresAt,
			CreatedAt:  acc.CreatedAt,
			UpdatedAt:  acc.UpdatedAt,
		}

		// For Facebook: Username IS the page name
		if acc.Platform == "facebook" {
			ig.PageName = acc.Username
		}

		// For Instagram: Username is Instagram handle, fetch Facebook Page name
		if acc.Platform == "instagram" {
			ig.InstagramHandle = acc.Username // Instagram username/handle (@username)
			// Fetch Facebook Page name if PageID is available
			if acc.PageID != "" && acc.AccessToken != "" {
				decryptedToken, err := utils.DecryptToken(acc.AccessToken)
				if err == nil {
					if pageName, err := h.fetchFacebookPageName(r.Context(), acc.PageID, decryptedToken); err == nil {
						ig.PageName = pageName
					}
				}
			}
		}

		resp = append(resp, ig)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"integrations": resp,
	})
}

// fetchFacebookPageName fetches the Facebook Page name from Graph API
func (h *Handler) fetchFacebookPageName(ctx context.Context, pageID, accessToken string) (string, error) {
	pageURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s?fields=name&access_token=%s", pageID, url.QueryEscape(accessToken))

	req, err := http.NewRequestWithContext(ctx, "GET", pageURL, nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		// Don't log error - just return empty string if page fetch fails
		return "", fmt.Errorf("failed to fetch page name: status %d", resp.StatusCode)
	}

	var pageData struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pageData); err != nil {
		return "", err
	}

	return pageData.Name, nil
}

func (h *Handler) overview(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var convoCount int64
	h.DB.Model(&storage.Conversation{}).Where("tenant_id = ?", claims.TenantID).Count(&convoCount)

	var scheduledCount int64
	h.DB.Model(&storage.ScheduledPost{}).Where("tenant_id = ?", claims.TenantID).Count(&scheduledCount)

	writeJSON(w, http.StatusOK, map[string]any{
		"conversations":  convoCount,
		"scheduledPosts": scheduledCount,
	})
}

// listScheduledPosts lists scheduled posts with optional filtering
// @Summary      List scheduled posts
// @Description  Get a list of scheduled posts, optionally filtered by status
// @Tags         posts
// @Accept       json
// @Produce      json
// @Param        status   query     string  false  "Filter by status (scheduled, posted, draft)"
// @Param        limit    query     int     false  "Limit number of results (default: 100)"
// @Security     BearerAuth
// @Success      200      {object}  map[string]interface{}  "List of scheduled posts"
// @Failure      401      {object}  map[string]interface{}  "Unauthorized"
// @Failure      500      {object}  map[string]interface{}  "Internal server error"
// @Router       /tenant/scheduled-posts [get]
func (h *Handler) listScheduledPosts(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		h.writeUnauthorizedError(w, r, "unauthorized")
		return
	}

	// Optional status filter from query parameter
	statusFilter := r.URL.Query().Get("status")

	query := h.DB.Where("tenant_id = ?", claims.TenantID)
	if statusFilter != "" {
		switch statusFilter {
		case "scheduled":
			// For "scheduled" filter, include both "scheduled" and "posting" statuses
			query = query.Where("status IN ?", []string{"scheduled", "posting"})
		case "published", "posted":
			// For "published" filter, use "posted" status
			query = query.Where("status = ?", "posted")
		case "draft":
			// For "draft" filter, match the same logic as getCampaignStats:
			// status = "draft" OR (scheduled_at IS NULL AND status != "posted")
			query = query.Where("(status = ? OR (scheduled_at IS NULL AND status != ?))", "draft", "posted")
		default:
			// For other status filters, use exact match
			query = query.Where("status = ?", statusFilter)
		}
	}

	// Parse pagination parameters
	pagination := ParsePagination(r)

	var posts []storage.ScheduledPost
	var total int64

	// Count total matching records (before pagination)
	if err := query.Model(&storage.ScheduledPost{}).Count(&total).Error; err != nil {
		h.Logger.Error("count scheduled posts", zap.Error(err))
		h.writeInternalError(w, r, err)
		return
	}

	// Apply pagination and fetch posts
	if err := ApplyPagination(query.
		Order("scheduled_at ASC, created_at ASC"), pagination).
		Find(&posts).Error; err != nil {
		h.Logger.Error("fetch scheduled posts", zap.Error(err))
		h.writeInternalError(w, r, err)
		return
	}

	type scheduledPost struct {
		ID            uint       `json:"id"`
		Name          string     `json:"name"`
		Caption       string     `json:"caption"`
		Status        string     `json:"status"`
		ScheduledAt   *time.Time `json:"scheduled_at"`
		Platforms     []string   `json:"platforms"`
		MediaAssetIDs []uint     `json:"media_asset_ids"`
		Attempts      int        `json:"attempts"`
		LastError     string     `json:"last_error"`
		CreatedAt     time.Time  `json:"created_at"`
		PostedAt      *time.Time `json:"posted_at"`
	}

	resp := make([]scheduledPost, 0, len(posts))
	for _, post := range posts {
		var platforms []string
		if strings.TrimSpace(post.Platforms) != "" {
			_ = json.Unmarshal([]byte(post.Platforms), &platforms)
		}

		var mediaIDs []uint
		if strings.TrimSpace(post.MediaAssetIDs) != "" {
			_ = json.Unmarshal([]byte(post.MediaAssetIDs), &mediaIDs)
		}

		resp = append(resp, scheduledPost{
			ID:            post.ID,
			Name:          post.Name,
			Caption:       post.Caption,
			Status:        post.Status,
			ScheduledAt:   post.ScheduledAt,
			Platforms:     platforms,
			MediaAssetIDs: mediaIDs,
			Attempts:      post.Attempts,
			LastError:     post.LastError,
			CreatedAt:     post.CreatedAt,
			PostedAt:      post.PostedAt,
		})
	}

	// Return paginated response
	response := NewPaginatedResponse(resp, total, pagination)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, tenantID uint, email string) error {
	token, err := middleware.CreateTenantToken([]byte(h.Config.TenantJWTSecret), tenantID, email, sessionTTL)
	if err != nil {
		return err
	}

	// Cache session in Redis for faster validation (optional, falls back to JWT validation)
	if h.Cache != nil {
		ctx := context.Background() // Use background context for cache operations
		sessionKey := fmt.Sprintf("session:tenant:%d:%s", tenantID, email)
		sessionData := map[string]interface{}{
			"tenant_id":  tenantID,
			"email":      email,
			"token":      token,
			"created_at": time.Now().Unix(),
		}
		// Cache for same duration as cookie
		if err := h.Cache.SetWithTTL(ctx, sessionKey, sessionData, sessionTTL); err != nil {
			// Log but don't fail - JWT validation will still work
			h.Logger.Warn("failed to cache session", zap.Error(err), zap.Uint("tenant_id", tenantID))
		}
	}

	cookie := &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),  // Use MaxAge for better browser compatibility
		Expires:  time.Now().Add(sessionTTL), // Also set Expires for older browsers
		Domain:   h.cookieDomain(),
		HttpOnly: true,
		SameSite: http.SameSiteNoneMode, // Required for cross-site requests (OAuth redirects)
		Secure:   true,                  // Required when SameSite=None
	}

	http.SetCookie(w, cookie)
	return nil
}

func (h *Handler) cookieDomain() string {
	if h.Config.TenantCookieDomain != "" {
		return h.Config.TenantCookieDomain
	}

	if h.Config.PublicURL != "" {
		if u, err := url.Parse(h.Config.PublicURL); err == nil && u.Host != "" {
			host := u.Hostname()
			if strings.Count(host, ".") >= 2 {
				parts := strings.Split(host, ".")
				host = "." + strings.Join(parts[len(parts)-2:], ".")
			} else {
				host = "." + host
			}
			return host
		}
	}
	return ""
}

func (h *Handler) getIntegration(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	platform := chi.URLParam(r, "platform")
	if platform == "" {
		http.Error(w, "platform required", http.StatusBadRequest)
		return
	}

	var account storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ?", claims.TenantID, platform).First(&account).Error; err != nil {
		http.Error(w, "integration not found", http.StatusNotFound)
		return
	}

	// Determine connection status: account is connected if:
	// 1. Connected flag is true, OR
	// 2. Has a valid access token (not empty) and not expired
	isConnected := account.Connected
	if !isConnected && account.AccessToken != "" {
		// Check if token is not expired (if ExpiresAt is set)
		if account.ExpiresAt == nil || account.ExpiresAt.After(time.Now()) {
			isConnected = true
			// Auto-fix: Update the Connected flag in database if it's wrong
			if account.Connected != isConnected {
				h.Logger.Info("Auto-fixing connection status",
					zap.Uint("account_id", account.ID),
					zap.String("platform", account.Platform),
					zap.Uint("tenant_id", claims.TenantID))
				account.Connected = true
				if err := h.DB.Model(&account).Update("connected", true).Error; err != nil {
					h.Logger.Warn("failed to auto-fix connection status", zap.Error(err))
				}
			}
		}
	}

	type integrationDetail struct {
		ID         uint       `json:"id"`
		Platform   string     `json:"platform"`
		Connected  bool       `json:"connected"`
		Username   string     `json:"username"`
		ExternalID string     `json:"external_id"`
		PageID     string     `json:"page_id"`
		Mode       string     `json:"mode"`
		ExpiresAt  *time.Time `json:"expires_at"`
		CreatedAt  time.Time  `json:"created_at"`
		UpdatedAt  time.Time  `json:"updated_at"`
	}

	writeJSON(w, http.StatusOK, integrationDetail{
		ID:         account.ID,
		Platform:   account.Platform,
		Connected:  isConnected,
		Username:   account.Username,
		ExternalID: account.ExternalID,
		PageID:     account.PageID,
		Mode:       account.Mode,
		ExpiresAt:  account.ExpiresAt,
		CreatedAt:  account.CreatedAt,
		UpdatedAt:  account.UpdatedAt,
	})
}

func (h *Handler) deleteIntegration(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	platform := chi.URLParam(r, "platform")
	if platform == "" {
		http.Error(w, "platform required", http.StatusBadRequest)
		return
	}

	var account storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ?", claims.TenantID, platform).First(&account).Error; err != nil {
		http.Error(w, "integration not found", http.StatusNotFound)
		return
	}

	// Log before deletion for debugging
	h.Logger.Info("Disconnecting integration",
		zap.Uint("tenant_id", claims.TenantID),
		zap.String("platform", platform),
		zap.Uint("account_id", account.ID),
		zap.String("external_id", account.ExternalID),
		zap.Bool("was_connected", account.Connected))

	// Delete the account record entirely to ensure clean reconnection
	// This prevents any stale data from interfering with reconnection
	if err := h.DB.Delete(&account).Error; err != nil {
		h.Logger.Error("failed to delete integration", zap.Error(err), zap.String("platform", platform))
		http.Error(w, "failed to disconnect", http.StatusInternalServerError)
		return
	}

	h.Logger.Info("✅ Integration deleted successfully",
		zap.Uint("tenant_id", claims.TenantID),
		zap.String("platform", platform),
		zap.Uint("account_id", account.ID))

	writeJSON(w, http.StatusOK, map[string]string{
		"message":  "integration disconnected successfully",
		"platform": platform,
	})
}

func (h *Handler) verifyIntegration(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	platform := chi.URLParam(r, "platform")
	if platform == "" {
		http.Error(w, "platform required", http.StatusBadRequest)
		return
	}

	var account storage.SocialAccount
	// Find account by tenant_id + platform (handles both connected and reconnection scenarios)
	err := h.DB.Where("tenant_id = ? AND platform = ?", claims.TenantID, platform).First(&account).Error
	if err != nil {
		h.Logger.Warn("integration not found for verification",
			zap.Error(err),
			zap.Uint("tenant_id", claims.TenantID),
			zap.String("platform", platform))
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error":    "integration_not_found",
			"message":  fmt.Sprintf("No %s integration found. Please connect your %s account first.", platform, platform),
			"platform": platform,
		})
		return
	}

	// Check if account has access token - if not, can't verify
	if account.AccessToken == "" {
		h.Logger.Warn("integration found but access token is missing",
			zap.Uint("tenant_id", claims.TenantID),
			zap.String("platform", platform),
			zap.Bool("connected", account.Connected))
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":    "token_missing",
			"message":  fmt.Sprintf("Your %s account is not properly connected. Please complete the OAuth flow to reconnect.", platform),
			"platform": platform,
			"hint":     "If you just reconnected, wait a few seconds and try again.",
		})
		return
	}

	// Log verification attempt for debugging with detailed account info
	h.Logger.Info("verifying integration",
		zap.Uint("tenant_id", claims.TenantID),
		zap.Uint("account_id", account.ID),
		zap.String("platform", platform),
		zap.String("mode", account.Mode),
		zap.String("login_method", account.LoginMethod),
		zap.Bool("connected", account.Connected),
		zap.Bool("has_access_token", account.AccessToken != ""),
		zap.Int("access_token_length", len(account.AccessToken)),
		zap.String("page_id", account.PageID),
		zap.String("external_id", account.ExternalID),
		zap.Time("created_at", account.CreatedAt),
		zap.Time("updated_at", account.UpdatedAt))

	// Verify the connection using the social package
	message, err := social.VerifySocialConnection(r.Context(), &account, h.DB)
	if err != nil {
		h.Logger.Error("integration verification failed",
			zap.Error(err),
			zap.String("platform", platform),
			zap.Uint("tenant_id", claims.TenantID),
			zap.String("mode", account.Mode),
			zap.Bool("has_access_token", account.AccessToken != ""),
			zap.String("page_id", account.PageID),
			zap.String("external_id", account.ExternalID))
		// Provide better error message based on error type
		errorMessage := err.Error()
		hint := "Please reconnect your account via OAuth to get a fresh access token."

		if strings.Contains(errorMessage, "encryption key") ||
			strings.Contains(errorMessage, "Cannot parse access token") ||
			strings.Contains(errorMessage, "Invalid OAuth access token") {
			hint = "Your access token appears to be invalid or encrypted incorrectly. Please reconnect your account via OAuth to refresh the token."
		} else if strings.Contains(errorMessage, "missing") {
			if platform == "instagram" {
				if account.LoginMethod == "instagram_login" {
					hint = "Your Instagram account is missing an access token. Please reconnect via Instagram Login to refresh the token."
				} else {
					hint = "Instagram via Facebook OAuth is no longer supported. Please reconnect using Instagram Login (Business Login for Instagram) instead."
				}
			} else {
				hint = "Your integration is missing required information. Please reconnect your account via OAuth."
			}
		} else if strings.Contains(errorMessage, "Instagram via Facebook OAuth is no longer supported") {
			hint = "Instagram via Facebook OAuth is no longer supported. Please reconnect using Instagram Login (Business Login for Instagram) instead."
		}

		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":    "verification_failed",
			"message":  errorMessage,
			"platform": platform,
			"hint":     hint,
		})
		return
	}

	// Reload the account to get updated fields
	h.DB.Where("tenant_id = ? AND platform = ?", claims.TenantID, platform).First(&account)

	// Log successful verification with account details
	h.Logger.Info("✅ Integration verified successfully",
		zap.Uint("tenant_id", claims.TenantID),
		zap.String("platform", platform),
		zap.Uint("account_id", account.ID),
		zap.String("external_id", account.ExternalID),
		zap.String("page_id", account.PageID),
		zap.Bool("connected", account.Connected),
		zap.String("login_method", account.LoginMethod))

	writeJSON(w, http.StatusOK, map[string]any{
		"message":     message,
		"platform":    platform,
		"connected":   account.Connected,
		"username":    account.Username,
		"external_id": account.ExternalID,
		"page_id":     account.PageID,
	})
}

// debugIntegration returns debug information about an integration (useful for troubleshooting)
func (h *Handler) debugIntegration(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	platform := chi.URLParam(r, "platform")
	if platform == "" {
		http.Error(w, "platform required", http.StatusBadRequest)
		return
	}

	var account storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ?", claims.TenantID, platform).First(&account).Error; err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error":    "integration_not_found",
			"message":  fmt.Sprintf("No %s integration found.", platform),
			"platform": platform,
		})
		return
	}

	// Return debug info (don't include sensitive data like tokens)
	writeJSON(w, http.StatusOK, map[string]any{
		"platform":         platform,
		"connected":        account.Connected,
		"mode":             account.Mode,
		"username":         account.Username,
		"external_id":      account.ExternalID, // This is used for webhook tenant resolution
		"page_id":          account.PageID,     // Fallback for tenant resolution
		"has_access_token": account.AccessToken != "",
		"created_at":       account.CreatedAt,
		"updated_at":       account.UpdatedAt,
		"hint":             "external_id must match instagram_business_account.id from Meta webhooks",
	})
}

func (h *Handler) repairIntegration(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	platform := chi.URLParam(r, "platform")
	if platform == "" {
		http.Error(w, "platform required", http.StatusBadRequest)
		return
	}

	var account storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ?", claims.TenantID, platform).First(&account).Error; err != nil {
		http.Error(w, "integration not found", http.StatusNotFound)
		return
	}

	// For now, just mark as needing repair
	// In the future, this could trigger re-verification or token refresh
	message := "⚠️ Repair functionality will be implemented based on platform requirements"

	writeJSON(w, http.StatusOK, map[string]string{
		"message":  message,
		"platform": platform,
	})
}

// subscribeInstagramPage manually subscribes the Facebook Page to receive Instagram message webhooks
// This is useful for debugging and fixing webhook issues
func (h *Handler) subscribeInstagramPage(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Get Instagram account
	var account storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ?", claims.TenantID, "instagram").First(&account).Error; err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error":   "instagram_not_found",
			"message": "Instagram account not connected. Please connect Instagram first.",
		})
		return
	}

	// Get the connected Facebook Page ID
	ctx := r.Context()
	client := &http.Client{Timeout: 10 * time.Second}

	// Decrypt token
	token := account.AccessToken
	if decrypted, err := utils.DecryptToken(account.AccessToken); err == nil {
		token = decrypted
	}

	// Get Instagram Business Account ID
	igAccountID := account.ExternalID
	if igAccountID == "" {
		igAccountID = account.PageID
	}

	if igAccountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "instagram_id_missing",
			"message": "Instagram Business Account ID not found. Please verify Instagram first.",
		})
		return
	}

	// Try to use stored Facebook Page ID first (from OAuth), then fallback to querying
	pageID := account.PageID // This should be the Facebook Page ID if connected via OAuth
	if pageID == "" {
		// Fallback: Query pages to find the one connected to this Instagram account
		// Note: This only works with User Access Tokens, not Page Access Tokens
		var err error
		pageID, err = social.FindAndSubscribeInstagramPage(ctx, client, igAccountID, token, "")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "no_connected_page",
				"message": fmt.Sprintf("No Facebook Page ID stored and failed to query: %v. Please reconnect Instagram via OAuth to store the Page ID.", err),
			})
			return
		}
	}

	// Subscribe Page to receive Instagram message webhooks
	subscribeErr := social.SubscribePageToInstagramMessages(ctx, client, pageID, token)
	if subscribeErr != nil {
		h.Logger.Warn("failed to subscribe Instagram Page",
			zap.Uint("tenant_id", claims.TenantID),
			zap.String("page_id", pageID),
			zap.String("instagram_id", igAccountID),
			zap.Error(subscribeErr))
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "subscription_failed",
			"message": fmt.Sprintf("Failed to subscribe Page to Instagram messages: %v", subscribeErr),
			"page_id": pageID,
			"hint":    "This might be normal if already subscribed. Check Meta Dashboard webhook configuration.",
		})
		return
	}

	h.Logger.Info("✅ Successfully subscribed Instagram Page",
		zap.Uint("tenant_id", claims.TenantID),
		zap.String("page_id", pageID),
		zap.String("instagram_id", igAccountID))

	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"message":      "Page successfully subscribed to receive Instagram message webhooks",
		"page_id":      pageID,
		"instagram_id": igAccountID,
		"next_steps": []string{
			"1. Verify webhook URL in Meta Dashboard points to Railway",
			"2. Ensure 'messages' subscription is enabled in Meta Dashboard",
			"3. Test webhook by sending a DM to your Instagram account",
		},
	})
}

// getScheduledPost retrieves a single scheduled post by ID
// @Summary      Get scheduled post
// @Description  Get details of a specific scheduled post by ID
// @Tags         posts
// @Accept       json
// @Produce      json
// @Param        id   path      int  true  "Post ID"
// @Security     BearerAuth
// @Success      200  {object}  map[string]interface{}  "Scheduled post details"
// @Failure      400  {object}  map[string]interface{}  "Invalid request"
// @Failure      401  {object}  map[string]interface{}  "Unauthorized"
// @Failure      404  {object}  map[string]interface{}  "Post not found"
// @Router       /tenant/scheduled-posts/{id} [get]
func (h *Handler) getScheduledPost(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		h.writeUnauthorizedError(w, r, "unauthorized")
		return
	}

	postID := chi.URLParam(r, "id")
	if postID == "" {
		h.writeValidationError(w, r, "post id required", nil)
		return
	}

	var post storage.ScheduledPost
	if err := h.DB.Where("tenant_id = ? AND id = ?", claims.TenantID, postID).First(&post).Error; err != nil {
		h.writeNotFoundError(w, r, "scheduled post")
		return
	}

	var platforms []string
	if strings.TrimSpace(post.Platforms) != "" {
		_ = json.Unmarshal([]byte(post.Platforms), &platforms)
	}

	var mediaIDs []uint
	if strings.TrimSpace(post.MediaAssetIDs) != "" {
		_ = json.Unmarshal([]byte(post.MediaAssetIDs), &mediaIDs)
	}

	type scheduledPostDetail struct {
		ID            uint       `json:"id"`
		Name          string     `json:"name"`
		Caption       string     `json:"caption"`
		Status        string     `json:"status"`
		ScheduledAt   *time.Time `json:"scheduled_at"`
		Platforms     []string   `json:"platforms"`
		MediaAssetIDs []uint     `json:"media_asset_ids"`
		Attempts      int        `json:"attempts"`
		LastError     string     `json:"last_error"`
		CreatedAt     time.Time  `json:"created_at"`
		UpdatedAt     time.Time  `json:"updated_at"`
		PostedAt      *time.Time `json:"posted_at"`
	}

	writeJSON(w, http.StatusOK, scheduledPostDetail{
		ID:            post.ID,
		Name:          post.Name,
		Caption:       post.Caption,
		Status:        post.Status,
		ScheduledAt:   post.ScheduledAt,
		Platforms:     platforms,
		MediaAssetIDs: mediaIDs,
		Attempts:      post.Attempts,
		LastError:     post.LastError,
		CreatedAt:     post.CreatedAt,
		UpdatedAt:     post.UpdatedAt,
		PostedAt:      post.PostedAt,
	})
}

// deleteScheduledPost cancels a scheduled post (soft delete)
// @Summary      Delete scheduled post
// @Description  Cancel a scheduled post by marking it as cancelled. Cannot delete already posted content.
// @Tags         posts
// @Accept       json
// @Produce      json
// @Param        id   path      int  true  "Post ID"
// @Security     BearerAuth
// @Success      200  {object}  map[string]interface{}  "Post cancelled successfully"
// @Failure      400  {object}  map[string]interface{}  "Invalid request or post already posted"
// @Failure      401  {object}  map[string]interface{}  "Unauthorized"
// @Failure      404  {object}  map[string]interface{}  "Post not found"
// @Failure      500  {object}  map[string]interface{}  "Internal server error"
// @Router       /tenant/scheduled-posts/{id} [delete]
func (h *Handler) deleteScheduledPost(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		h.writeUnauthorizedError(w, r, "unauthorized")
		return
	}

	postID := chi.URLParam(r, "id")
	if postID == "" {
		h.writeValidationError(w, r, "post id required", nil)
		return
	}

	var post storage.ScheduledPost
	if err := h.DB.Where("tenant_id = ? AND id = ?", claims.TenantID, postID).First(&post).Error; err != nil {
		h.writeNotFoundError(w, r, "scheduled post")
		return
	}

	// Only allow cancellation of posts that haven't been posted yet
	if post.Status == "posted" {
		h.writeValidationError(w, r, "cannot delete already posted content", nil)
		return
	}

	// Mark as cancelled instead of deleting (soft delete)
	post.Status = "cancelled"
	if err := h.DB.Save(&post).Error; err != nil {
		h.Logger.Error("failed to cancel scheduled post", zap.Error(err), zap.Uint("post_id", post.ID))
		h.writeInternalError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "scheduled post cancelled successfully",
		"post_id": postID,
	})
}

func (h *Handler) listAvailablePhoneNumbers(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Get tenant's current assignment first
	var currentAssignment storage.WhatsAppPhoneNumber
	if err := h.DB.Where("tenant_id = ? AND status = ?", claims.TenantID, "assigned").First(&currentAssignment).Error; err != nil {
		currentAssignment.ID = 0 // Not found
	}

	// Get all available numbers (not assigned to anyone)
	var available []storage.WhatsAppPhoneNumber
	h.DB.Where("status = ? AND (tenant_id IS NULL OR tenant_id = 0)", "available").Find(&available)

	type phoneNumber struct {
		ID            uint   `json:"id"`
		PhoneNumberID string `json:"phone_number_id"`
		PhoneNumber   string `json:"phone_number"`
		DisplayName   string `json:"display_name"`
		VerifiedName  string `json:"verified_name"`
		Status        string `json:"status"`
	}

	availableResp := make([]phoneNumber, 0, len(available))
	for _, num := range available {
		availableResp = append(availableResp, phoneNumber{
			ID:            num.ID,
			PhoneNumberID: num.PhoneNumberID,
			PhoneNumber:   num.PhoneNumber,
			DisplayName:   num.DisplayName,
			VerifiedName:  num.VerifiedName,
			Status:        num.Status,
		})
	}

	var current *phoneNumber
	if currentAssignment.ID != 0 {
		current = &phoneNumber{
			ID:            currentAssignment.ID,
			PhoneNumberID: currentAssignment.PhoneNumberID,
			PhoneNumber:   currentAssignment.PhoneNumber,
			DisplayName:   currentAssignment.DisplayName,
			VerifiedName:  currentAssignment.VerifiedName,
			Status:        currentAssignment.Status,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"current":           current,
		"available_numbers": availableResp,
	})
}

func (h *Handler) assignPhoneNumber(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	phoneNumberID := chi.URLParam(r, "id")
	if phoneNumberID == "" {
		http.Error(w, "phone number id required", http.StatusBadRequest)
		return
	}

	// Check if tenant already has a number assigned
	var existingAssignment storage.WhatsAppPhoneNumber
	if err := h.DB.Where("tenant_id = ? AND status = ?", claims.TenantID, "assigned").First(&existingAssignment).Error; err == nil {
		http.Error(w, "you already have a WhatsApp number assigned", http.StatusBadRequest)
		return
	}

	// Get the phone number from pool
	var phoneNum storage.WhatsAppPhoneNumber
	if err := h.DB.Where("id = ? OR phone_number_id = ?", phoneNumberID, phoneNumberID).First(&phoneNum).Error; err != nil {
		http.Error(w, "phone number not found", http.StatusNotFound)
		return
	}

	if phoneNum.Status != "available" || (phoneNum.TenantID != nil && *phoneNum.TenantID != 0) {
		http.Error(w, "phone number is not available", http.StatusBadRequest)
		return
	}

	// Get tenant info for display name
	var tenant storage.Tenant
	if err := h.DB.First(&tenant, claims.TenantID).Error; err != nil {
		http.Error(w, "tenant not found", http.StatusNotFound)
		return
	}

	// Assign to tenant
	now := time.Now()
	phoneNum.TenantID = &claims.TenantID
	phoneNum.DisplayName = tenant.Name
	phoneNum.Status = "assigned"
	phoneNum.AssignedAt = &now

	if err := h.DB.Save(&phoneNum).Error; err != nil {
		h.Logger.Error("failed to assign phone number", zap.Error(err))
		http.Error(w, "failed to assign phone number", http.StatusInternalServerError)
		return
	}

	// Create/update social account for WhatsApp (provider-owned model)
	// Note: No AccessToken stored - we use Brancr's system token
	var account storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ?", claims.TenantID, "whatsapp").First(&account).Error; err != nil {
		// Create new account (no token needed for provider-owned model)
		account = storage.SocialAccount{
			TenantID:   claims.TenantID,
			Platform:   "whatsapp",
			PageID:     phoneNum.PhoneNumberID, // This is phone_number_id
			ExternalID: phoneNum.PhoneNumberID, // Used for webhook routing
			Username:   phoneNum.PhoneNumber,
			Connected:  true,
			Mode:       "provider_owned", // Marks it as using Brancr's system token
		}
		if err := h.DB.Create(&account).Error; err != nil {
			h.Logger.Error("failed to create social account", zap.Error(err))
		}
	} else {
		// Update existing account
		account.PageID = phoneNum.PhoneNumberID
		account.ExternalID = phoneNum.PhoneNumberID
		account.Username = phoneNum.PhoneNumber
		account.Connected = true
		account.Mode = "provider_owned" // Switch to provider-owned mode
		// Don't clear AccessToken for backward compatibility, but Mode=provider_owned will use system token
		if err := h.DB.Save(&account).Error; err != nil {
			h.Logger.Error("failed to update social account", zap.Error(err))
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message":         "WhatsApp number assigned successfully",
		"phone_number":    phoneNum.PhoneNumber,
		"phone_number_id": phoneNum.PhoneNumberID,
	})
}

func (h *Handler) getAssignedPhoneNumber(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var phoneNum storage.WhatsAppPhoneNumber
	if err := h.DB.Where("tenant_id = ? AND status = ?", claims.TenantID, "assigned").First(&phoneNum).Error; err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"assigned": false,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"assigned":        true,
		"id":              phoneNum.ID,
		"phone_number_id": phoneNum.PhoneNumberID,
		"phone_number":    phoneNum.PhoneNumber,
		"display_name":    phoneNum.DisplayName,
		"verified_name":   phoneNum.VerifiedName,
		"quality_rating":  phoneNum.QualityRating,
		"assigned_at":     phoneNum.AssignedAt,
	})
}

func (h *Handler) disconnectPhoneNumber(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Step 1: Disconnect SocialAccount (for Respond.io connections)
	var account storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ? AND connected = ?", claims.TenantID, "whatsapp", true).First(&account).Error; err == nil {
		// Found connected WhatsApp account - disconnect it
		account.Connected = false
		// Keep phone number and channel ID for potential reconnection
		// account.PageID = "" // Keep phone number
		// account.ExternalID = "" // Keep channel ID

		if err := h.DB.Save(&account).Error; err != nil {
			h.Logger.Error("failed to disconnect social account", zap.Error(err))
			http.Error(w, "failed to disconnect", http.StatusInternalServerError)
			return
		}

		h.Logger.Info("✅ WhatsApp disconnected (SocialAccount)",
			zap.Uint("tenant_id", claims.TenantID),
			zap.String("mode", account.Mode),
			zap.String("phone_number", account.PageID))

		writeJSON(w, http.StatusOK, map[string]any{
			"success":  true,
			"message":  "WhatsApp disconnected successfully",
			"provider": account.Mode,
		})
		return
	}

	// Step 2: Try provider-owned model (legacy - WhatsAppPhoneNumber)
	var phoneNum storage.WhatsAppPhoneNumber
	if err := h.DB.Where("tenant_id = ? AND status = ?", claims.TenantID, "assigned").First(&phoneNum).Error; err == nil {
		// Unassign from tenant
		phoneNum.TenantID = nil
		phoneNum.Status = "available"
		phoneNum.AssignedAt = nil

		if err := h.DB.Save(&phoneNum).Error; err != nil {
			h.Logger.Error("failed to disconnect phone number", zap.Error(err))
			http.Error(w, "failed to disconnect", http.StatusInternalServerError)
			return
		}

		// Also update social account if it exists
		var socialAccount storage.SocialAccount
		if err := h.DB.Where("tenant_id = ? AND platform = ?", claims.TenantID, "whatsapp").First(&socialAccount).Error; err == nil {
			socialAccount.Connected = false
			h.DB.Save(&socialAccount)
		}

		h.Logger.Info("✅ WhatsApp disconnected (Provider-owned)",
			zap.Uint("tenant_id", claims.TenantID),
			zap.String("phone_number", phoneNum.PhoneNumber))

		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"message": "WhatsApp number disconnected successfully",
		})
		return
	}

	// No WhatsApp connection found
	http.Error(w, "no WhatsApp connection found to disconnect", http.StatusNotFound)
}

func (h *Handler) checkWebhookStatus(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Get tenant's assigned number
	var phoneNum storage.WhatsAppPhoneNumber
	if err := h.DB.Where("tenant_id = ? AND status = ?", claims.TenantID, "assigned").First(&phoneNum).Error; err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"assigned": false,
			"message":  "No WhatsApp number assigned",
		})
		return
	}

	// Check webhook subscription status
	systemToken := h.Config.MetaSystemUserToken
	if systemToken == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"assigned":        true,
			"phone_number_id": phoneNum.PhoneNumberID,
			"subscribed":      false,
			"error":           "System token not configured",
		})
		return
	}

	subscribed, err := social.CheckPhoneNumberWebhookSubscription(h.Logger, phoneNum.PhoneNumberID, systemToken)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"assigned":        true,
			"phone_number_id": phoneNum.PhoneNumberID,
			"subscribed":      false,
			"error":           err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"assigned":        true,
		"phone_number_id": phoneNum.PhoneNumberID,
		"phone_number":    phoneNum.PhoneNumber,
		"subscribed":      subscribed,
		"message":         map[bool]string{true: "Webhook is subscribed", false: "Webhook is NOT subscribed - messages won't be received"}[subscribed],
	})
}

func (h *Handler) resubscribeWebhook(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Get tenant's assigned number
	var phoneNum storage.WhatsAppPhoneNumber
	if err := h.DB.Where("tenant_id = ? AND status = ?", claims.TenantID, "assigned").First(&phoneNum).Error; err != nil {
		http.Error(w, "no WhatsApp number assigned", http.StatusNotFound)
		return
	}

	systemToken := h.Config.MetaSystemUserToken
	if systemToken == "" {
		http.Error(w, "system token not configured", http.StatusInternalServerError)
		return
	}

	// Resubscribe (async)
	go func() {
		social.SubscribePhoneNumberToWebhooks(h.Logger, phoneNum.PhoneNumberID, systemToken)
	}()

	writeJSON(w, http.StatusOK, map[string]any{
		"message":         "Webhook resubscription initiated",
		"phone_number_id": phoneNum.PhoneNumberID,
		"note":            "Check webhook status in a few seconds",
	})
}

func (h *Handler) comprehensiveDiagnostics(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	diagnostics := map[string]any{
		"timestamp": time.Now().Format(time.RFC3339),
		"tenant_id": claims.TenantID,
		"errors":    []string{},
		"warnings":  []string{},
	}

	// 1. Check if number is assigned
	var phoneNum storage.WhatsAppPhoneNumber
	if err := h.DB.Where("tenant_id = ? AND status = ?", claims.TenantID, "assigned").First(&phoneNum).Error; err != nil {
		diagnostics["assigned"] = false
		diagnostics["error"] = "No WhatsApp number assigned"
		writeJSON(w, http.StatusOK, diagnostics)
		return
	}

	diagnostics["assigned"] = true
	diagnostics["phone_number_id"] = phoneNum.PhoneNumberID
	diagnostics["phone_number"] = phoneNum.PhoneNumber
	diagnostics["verified_name"] = phoneNum.VerifiedName
	diagnostics["quality_rating"] = phoneNum.QualityRating

	// 2. Check system token
	systemToken := h.Config.MetaSystemUserToken
	if systemToken == "" {
		diagnostics["system_token_configured"] = false
		errors := diagnostics["errors"].([]string)
		errors = append(errors, "META_SYSTEM_USER_TOKEN not configured")
		diagnostics["errors"] = errors
		writeJSON(w, http.StatusOK, diagnostics)
		return
	}
	diagnostics["system_token_configured"] = true

	// 3. Get phone number status from Meta
	metaStatus, err := social.GetPhoneNumberStatus(h.Logger, phoneNum.PhoneNumberID, systemToken)
	if err != nil {
		diagnostics["meta_status_check"] = map[string]any{
			"success": false,
			"error":   err.Error(),
		}
	} else {
		diagnostics["meta_status_check"] = map[string]any{
			"success":                  true,
			"verified_name":            metaStatus["verified_name"],
			"quality_rating":           metaStatus["quality_rating"],
			"code_verification_status": metaStatus["code_verification_status"],
			"status":                   metaStatus["status"],
		}

		// Check if verification is complete
		verificationStatus, _ := metaStatus["code_verification_status"].(string)
		if verificationStatus != "VERIFIED" {
			warnings := diagnostics["warnings"].([]string)
			warnings = append(warnings, fmt.Sprintf("Phone number verification status: %s (should be VERIFIED)", verificationStatus))
			diagnostics["warnings"] = warnings
		}
	}

	// 4. Check phone number-level webhook subscription
	phoneSubscribed, err := social.CheckPhoneNumberWebhookSubscription(h.Logger, phoneNum.PhoneNumberID, systemToken)
	if err != nil {
		diagnostics["phone_webhook_subscription"] = map[string]any{
			"subscribed": false,
			"error":      err.Error(),
		}
	} else {
		diagnostics["phone_webhook_subscription"] = map[string]any{
			"subscribed": phoneSubscribed,
		}
		if !phoneSubscribed {
			errors := diagnostics["errors"].([]string)
			errors = append(errors, "Phone number is NOT subscribed to webhooks - inbound messages won't be received")
			diagnostics["errors"] = errors
		}
	}

	// 5. Check WABA-level subscription
	wabaID := h.Config.BrancrWABAID
	if wabaID != "" {
		wabaSubscribed, fields, err := social.CheckWABASubscription(h.Logger, wabaID, systemToken)
		if err != nil {
			diagnostics["waba_webhook_subscription"] = map[string]any{
				"subscribed": false,
				"error":      err.Error(),
			}
		} else {
			diagnostics["waba_webhook_subscription"] = map[string]any{
				"subscribed":        wabaSubscribed,
				"subscribed_fields": fields,
			}
			if !wabaSubscribed {
				warnings := diagnostics["warnings"].([]string)
				warnings = append(warnings, "WABA is NOT subscribed to webhooks - this may prevent message delivery")
				diagnostics["warnings"] = warnings
			}
		}
	} else {
		diagnostics["waba_webhook_subscription"] = map[string]any{
			"error": "BRANCR_WABA_ID not configured",
		}
	}

	// 6. Check social account
	var account storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ?", claims.TenantID, "whatsapp").First(&account).Error; err == nil {
		diagnostics["social_account"] = map[string]any{
			"exists":      true,
			"connected":   account.Connected,
			"mode":        account.Mode,
			"page_id":     account.PageID,
			"external_id": account.ExternalID,
		}
		if !account.Connected {
			errors := diagnostics["errors"].([]string)
			errors = append(errors, "Social account is not marked as connected")
			diagnostics["errors"] = errors
		}
		if account.Mode != "provider_owned" {
			warnings := diagnostics["warnings"].([]string)
			warnings = append(warnings, fmt.Sprintf("Account mode is '%s' but should be 'provider_owned'", account.Mode))
			diagnostics["warnings"] = warnings
		}
	} else {
		diagnostics["social_account"] = map[string]any{
			"exists": false,
			"error":  "Social account not found",
		}
		errors := diagnostics["errors"].([]string)
		errors = append(errors, "Social account not found - webhook routing may fail")
		diagnostics["errors"] = errors
	}

	// 7. Overall health check
	errors := diagnostics["errors"].([]string)
	warnings := diagnostics["warnings"].([]string)

	diagnostics["health"] = map[string]any{
		"status":   map[bool]string{true: "healthy", false: "unhealthy"}[len(errors) == 0],
		"errors":   errors,
		"warnings": warnings,
		"ready":    len(errors) == 0 && phoneSubscribed,
	}

	writeJSON(w, http.StatusOK, diagnostics)
}

func (h *Handler) requestTenantPhoneNumber(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		PhoneNumber string `json:"phone_number"` // e.g., "+1234567890"
		DisplayName string `json:"display_name"` // Optional: defaults to tenant name
		CodeMethod  string `json:"code_method"`  // Optional: "SMS" or "VOICE" (defaults to "SMS")
		Locale      string `json:"locale"`       // Optional: locale code (defaults to "en_US")
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.PhoneNumber == "" {
		http.Error(w, "phone_number is required", http.StatusBadRequest)
		return
	}

	// Get tenant name for display name if not provided
	if req.DisplayName == "" {
		var tenant storage.Tenant
		if err := h.DB.First(&tenant, claims.TenantID).Error; err == nil {
			req.DisplayName = tenant.Name
		} else {
			req.DisplayName = "Business"
		}
	}

	// Format phone number (ensure + prefix)
	phoneNumber := strings.TrimSpace(req.PhoneNumber)
	if !strings.HasPrefix(phoneNumber, "+") {
		phoneNumber = "+" + phoneNumber
	}

	// Check if number is already assigned to this tenant
	var existingNumber storage.WhatsAppPhoneNumber
	if err := h.DB.Where("phone_number = ? AND tenant_id = ?", phoneNumber, claims.TenantID).First(&existingNumber).Error; err == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "number_already_assigned",
			"message": "This phone number is already assigned to your account.",
		})
		return
	}

	// 🚀 METHOD 1 AUTOMATED FLOW: Check if number exists in WABA pool (pre-provisioned)
	var phoneNum storage.WhatsAppPhoneNumber
	if err := h.DB.Where("phone_number = ? AND tenant_id IS NULL", phoneNumber).First(&phoneNum).Error; err == nil {
		// Number exists in pool and is available - AUTOMATE the flow!
		h.Logger.Info("phone number found in WABA pool, starting automated onboarding",
			zap.String("phone_number", phoneNumber),
			zap.String("phone_number_id", phoneNum.PhoneNumberID),
			zap.Uint("tenant_id", claims.TenantID))

		// Step 1: Register the number (if needed)
		if phoneNum.Status == "pending" || phoneNum.Status == "pending_verification" {
			if err := social.RegisterPhoneNumber(h.Logger, h.Config, phoneNum.PhoneNumberID); err != nil {
				h.Logger.Error("failed to register phone number", zap.Error(err))
				writeJSON(w, http.StatusInternalServerError, map[string]any{
					"error":   "registration_failed",
					"message": "Failed to register phone number: " + err.Error(),
				})
				return
			}
			h.Logger.Info("phone number registered successfully", zap.String("phone_number_id", phoneNum.PhoneNumberID))
		}

		// Step 2: Request verification code (Meta will send SMS/Voice to tenant's phone)
		codeMethod := req.CodeMethod
		if codeMethod == "" {
			codeMethod = "SMS" // Default to SMS
		}
		if codeMethod != "SMS" && codeMethod != "VOICE" {
			codeMethod = "SMS" // Fallback to SMS if invalid
		}
		locale := req.Locale
		if locale == "" {
			locale = "en_US" // Default locale
		}

		if err := social.RequestPhoneNumberVerificationCode(h.Logger, h.Config, phoneNum.PhoneNumberID, codeMethod, locale); err != nil {
			h.Logger.Error("failed to request verification code", zap.Error(err))
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error":   "code_request_failed",
				"message": "Failed to request verification code: " + err.Error(),
			})
			return
		}

		// Step 3: Create a request record to track the verification flow
		request := storage.WhatsAppPhoneNumberRequest{
			TenantID:      claims.TenantID,
			PhoneNumber:   phoneNumber,
			DisplayName:   req.DisplayName,
			PhoneNumberID: phoneNum.PhoneNumberID,
			Status:        "verification_code_sent", // Code has been sent
		}

		if err := h.DB.Create(&request).Error; err != nil {
			h.Logger.Error("failed to create phone number request", zap.Error(err))
			// Don't fail - code was already sent
		}

		h.Logger.Info("verification code sent to tenant",
			zap.Uint("tenant_id", claims.TenantID),
			zap.String("phone_number", phoneNumber),
			zap.String("phone_number_id", phoneNum.PhoneNumberID))

		methodText := "SMS"
		if codeMethod == "VOICE" {
			methodText = "voice call"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success":         true,
			"request_id":      request.ID,
			"phone_number_id": phoneNum.PhoneNumberID,
			"phone_number":    phoneNumber,
			"code_method":     codeMethod,
			"message":         fmt.Sprintf("✅ Verification code sent via %s to %s! Please check your phone and enter the code.", methodText, phoneNumber),
			"next_step":       "verify_number",
			"automated":       true,
		})
		return
	}

	// Number not in pool - try to add it to WABA first, then sync
	h.Logger.Info("phone number not in database pool, attempting to add to WABA",
		zap.String("phone_number", phoneNumber),
		zap.Uint("tenant_id", claims.TenantID))

	// Step 1: Try to add phone number to WABA via Meta API
	phoneNumberID, err := social.RequestPhoneNumberFromMeta(h.Logger, h.Config, phoneNumber, req.DisplayName)
	if err != nil {
		h.Logger.Warn("failed to add phone number to WABA via API, will sync to check if it exists",
			zap.String("phone_number", phoneNumber),
			zap.Error(err))
		// Continue to sync - number might already exist
	} else if phoneNumberID != "" {
		h.Logger.Info("phone number added to WABA successfully",
			zap.String("phone_number", phoneNumber),
			zap.String("phone_number_id", phoneNumberID))
		// Sync to get the number into our database
		if err := social.SyncPhoneNumbersFromMeta(h.DB, h.Logger, h.Config); err != nil {
			h.Logger.Warn("failed to sync after adding number", zap.Error(err))
		}
	}

	// Step 2: Sync phone numbers from Meta to ensure DB is up-to-date
	if err := social.SyncPhoneNumbersFromMeta(h.DB, h.Logger, h.Config); err != nil {
		h.Logger.Warn("failed to sync phone numbers from Meta, proceeding with fallback", zap.Error(err))
	} else {
		// Check again after sync
		var phoneNumAfterSync storage.WhatsAppPhoneNumber
		if err := h.DB.Where("phone_number = ? AND tenant_id IS NULL", phoneNumber).First(&phoneNumAfterSync).Error; err == nil {
			// Number found after sync! Proceed with automated flow
			h.Logger.Info("phone number found in Meta after sync, starting automated onboarding",
				zap.String("phone_number", phoneNumber),
				zap.String("phone_number_id", phoneNumAfterSync.PhoneNumberID),
				zap.Uint("tenant_id", claims.TenantID))

			// Step 1: Register the number (if needed)
			if phoneNumAfterSync.Status == "pending" || phoneNumAfterSync.Status == "pending_verification" {
				if err := social.RegisterPhoneNumber(h.Logger, h.Config, phoneNumAfterSync.PhoneNumberID); err != nil {
					h.Logger.Error("failed to register phone number", zap.Error(err))
					writeJSON(w, http.StatusInternalServerError, map[string]any{
						"error":   "registration_failed",
						"message": "Failed to register phone number: " + err.Error(),
					})
					return
				}
				h.Logger.Info("phone number registered successfully", zap.String("phone_number_id", phoneNumAfterSync.PhoneNumberID))
			}

			// Step 2: Request verification code
			codeMethod := req.CodeMethod
			if codeMethod == "" {
				codeMethod = "SMS" // Default to SMS
			}
			if codeMethod != "SMS" && codeMethod != "VOICE" {
				codeMethod = "SMS" // Fallback to SMS if invalid
			}
			locale := req.Locale
			if locale == "" {
				locale = "en_US" // Default locale
			}

			if err := social.RequestPhoneNumberVerificationCode(h.Logger, h.Config, phoneNumAfterSync.PhoneNumberID, codeMethod, locale); err != nil {
				h.Logger.Error("failed to request verification code", zap.Error(err))
				writeJSON(w, http.StatusInternalServerError, map[string]any{
					"error":   "code_request_failed",
					"message": "Failed to request verification code: " + err.Error(),
				})
				return
			}

			// Step 3: Create a request record
			request := storage.WhatsAppPhoneNumberRequest{
				TenantID:      claims.TenantID,
				PhoneNumber:   phoneNumber,
				DisplayName:   req.DisplayName,
				PhoneNumberID: phoneNumAfterSync.PhoneNumberID,
				Status:        "verification_code_sent",
			}

			if err := h.DB.Create(&request).Error; err != nil {
				h.Logger.Error("failed to create phone number request", zap.Error(err))
				// Don't fail - code was already sent
			}

			methodText := "SMS"
			if codeMethod == "VOICE" {
				methodText = "voice call"
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"success":         true,
				"request_id":      request.ID,
				"phone_number_id": phoneNumAfterSync.PhoneNumberID,
				"phone_number":    phoneNumber,
				"code_method":     codeMethod,
				"message":         fmt.Sprintf("✅ Verification code sent via %s to %s! Please check your phone and enter the code.", methodText, phoneNumber),
				"next_step":       "verify_number",
				"automated":       true,
			})
			return
		}
	}

	// Still not in pool - check if tenant already has a pending request for this number
	var existingRequest storage.WhatsAppPhoneNumberRequest
	if err := h.DB.Where("tenant_id = ? AND phone_number = ? AND status IN (?)", claims.TenantID, phoneNumber, []string{"pending", "added_to_meta", "verification_code_sent"}).First(&existingRequest).Error; err == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":      "request_already_exists",
			"message":    fmt.Sprintf("You already have a pending request for %s. Status: %s.", phoneNumber, existingRequest.Status),
			"request_id": existingRequest.ID,
			"status":     existingRequest.Status,
		})
		return
	}

	// Create admin request (for numbers not in pool - admin needs to add to Meta BM first)
	request := storage.WhatsAppPhoneNumberRequest{
		TenantID:    claims.TenantID,
		PhoneNumber: phoneNumber,
		DisplayName: req.DisplayName,
		Status:      "pending",
	}

	if err := h.DB.Create(&request).Error; err != nil {
		h.Logger.Error("failed to create phone number request", zap.Error(err), zap.Uint("tenant_id", claims.TenantID))
		http.Error(w, "failed to create request", http.StatusInternalServerError)
		return
	}

	h.Logger.Info("tenant created phone number request (not in pool)",
		zap.Uint("tenant_id", claims.TenantID),
		zap.String("phone_number", phoneNumber),
		zap.Uint("request_id", request.ID))

	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"request_id":   request.ID,
		"phone_number": phoneNumber,
		"message":      "📋 Phone number request submitted! Our admin team will add your number to WhatsApp Business Manager. You'll receive an SMS verification code automatically once it's added (usually within 1-2 business hours). You'll be notified via email when it's ready.",
		"automated":    false,
		"fallback":     "admin_request",
		"next_steps":   "We'll add your number to our WhatsApp Business Account, sync it to our system, then send you a verification code. Check back in 1-2 hours or wait for email notification.",
	})
}

func (h *Handler) verifyTenantPhoneNumber(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		RequestID        uint   `json:"request_id"`        // Request ID from request-number
		VerificationCode string `json:"verification_code"` // Code sent via SMS/voice from Meta
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.RequestID == 0 || req.VerificationCode == "" {
		http.Error(w, "request_id and verification_code are required", http.StatusBadRequest)
		return
	}

	// Find the request
	var phoneRequest storage.WhatsAppPhoneNumberRequest
	if err := h.DB.Where("id = ? AND tenant_id = ?", req.RequestID, claims.TenantID).First(&phoneRequest).Error; err != nil {
		http.Error(w, "request not found", http.StatusNotFound)
		return
	}

	// Check if request is in the right state
	if phoneRequest.Status != "added_to_meta" && phoneRequest.Status != "verification_code_sent" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_request_status",
			"message": fmt.Sprintf("Request is in status '%s'. Verification code can only be provided when admin has added the number to Meta.", phoneRequest.Status),
		})
		return
	}

	// 🚀 AUTOMATED FLOW: If phone_number_id exists, verify automatically
	if phoneRequest.PhoneNumberID != "" {
		h.Logger.Info("automated verification flow - verifying with Meta",
			zap.String("phone_number_id", phoneRequest.PhoneNumberID),
			zap.Uint("tenant_id", claims.TenantID))

		// Verify the code with Meta
		if err := social.VerifyPhoneNumberWithCode(h.Logger, h.Config, phoneRequest.PhoneNumberID, req.VerificationCode); err != nil {
			h.Logger.Error("verification failed", zap.Error(err))
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "verification_failed",
				"message": "Invalid verification code: " + err.Error(),
			})
			return
		}

		// Sync phone numbers to update status
		if err := social.SyncPhoneNumbersFromMeta(h.DB, h.Logger, h.Config); err != nil {
			h.Logger.Warn("verification succeeded but sync failed", zap.Error(err))
		}

		// Assign the number to the tenant automatically
		var phoneNum storage.WhatsAppPhoneNumber
		if err := h.DB.Where("phone_number_id = ?", phoneRequest.PhoneNumberID).First(&phoneNum).Error; err != nil {
			h.Logger.Error("phone number not found after verification", zap.Error(err))
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error":   "assignment_failed",
				"message": "Number verified but could not be assigned. Please contact support.",
			})
			return
		}

		// Assign to tenant
		phoneNum.TenantID = &claims.TenantID
		now := time.Now()
		phoneNum.AssignedAt = &now
		if err := h.DB.Save(&phoneNum).Error; err != nil {
			h.Logger.Error("failed to assign phone number", zap.Error(err))
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error":   "assignment_failed",
				"message": "Number verified but assignment failed. Please contact support.",
			})
			return
		}

		// Subscribe to webhooks
		social.SubscribePhoneNumberToWebhooks(h.Logger, phoneNum.PhoneNumberID, h.Config.BrancrWABAToken)

		// Update request status
		phoneRequest.VerificationCode = req.VerificationCode
		phoneRequest.Status = "verified"
		phoneRequest.ProcessedAt = &now
		h.DB.Save(&phoneRequest)

		h.Logger.Info("✅ phone number verified and assigned automatically",
			zap.Uint("tenant_id", claims.TenantID),
			zap.String("phone_number", phoneRequest.PhoneNumber),
			zap.String("phone_number_id", phoneRequest.PhoneNumberID))

		writeJSON(w, http.StatusOK, map[string]any{
			"success":         true,
			"phone_number_id": phoneNum.PhoneNumberID,
			"phone_number":    phoneNum.PhoneNumber,
			"message":         fmt.Sprintf("✅ Success! Your WhatsApp number %s is now connected and ready to receive messages.", phoneNum.PhoneNumber),
			"automated":       true,
		})
		return
	}

	// Manual flow: Update request with verification code (admin will verify)
	phoneRequest.VerificationCode = req.VerificationCode
	phoneRequest.Status = "verification_code_sent"

	if err := h.DB.Save(&phoneRequest).Error; err != nil {
		h.Logger.Error("failed to save verification code", zap.Error(err), zap.Uint("tenant_id", claims.TenantID))
		http.Error(w, "failed to save verification code", http.StatusInternalServerError)
		return
	}

	h.Logger.Info("tenant provided verification code (manual flow)",
		zap.Uint("tenant_id", claims.TenantID),
		zap.Uint("request_id", req.RequestID),
		zap.String("phone_number", phoneRequest.PhoneNumber))

	writeJSON(w, http.StatusOK, map[string]any{
		"success":   true,
		"message":   "Verification code received. Admin will verify it with Meta and complete the setup. You'll be notified once your number is ready.",
		"automated": false,
	})
}

func (h *Handler) checkTenantPhoneNumber(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		PhoneNumber string `json:"phone_number"` // e.g., "+1234567890"
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.PhoneNumber == "" {
		http.Error(w, "phone_number is required", http.StatusBadRequest)
		return
	}

	// Format phone number (ensure + prefix)
	phoneNumber := strings.TrimSpace(req.PhoneNumber)
	if !strings.HasPrefix(phoneNumber, "+") {
		phoneNumber = "+" + phoneNumber
	}

	// Check if phone number exists in database (already synced)
	var phoneNum storage.WhatsAppPhoneNumber
	if err := h.DB.Where("phone_number = ?", phoneNumber).First(&phoneNum).Error; err == nil {
		// Number exists - ready to verify
		writeJSON(w, http.StatusOK, map[string]any{
			"found":           true,
			"phone_number_id": phoneNum.PhoneNumberID,
			"phone_number":    phoneNum.PhoneNumber,
			"status":          phoneNum.Status,
			"message":         "Phone number found! You can now request a verification code.",
			"ready":           true,
		})
		return
	}

	// Number not found - needs to be added by admin
	h.Logger.Info("tenant checked for phone number - not found",
		zap.String("phone_number", phoneNumber),
		zap.Uint("tenant_id", claims.TenantID))

	writeJSON(w, http.StatusOK, map[string]any{
		"found":           false,
		"phone_number":    phoneNumber,
		"message":         "Phone number not found in our system. Please contact support to add it first. Admin will add it in Meta Business Manager, then sync it to our system.",
		"ready":           false,
		"action_required": "contact_support",
	})
}

// =======================
// ONBOARDING HANDLERS
// =======================

func (h *Handler) getOnboardingStatus(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var tenant storage.Tenant
	if err := h.DB.First(&tenant, claims.TenantID).Error; err != nil {
		http.Error(w, "tenant not found", http.StatusNotFound)
		return
	}

	// Get business profile (or create empty one if doesn't exist)
	var profile storage.BusinessProfile
	if err := h.DB.Where("tenant_id = ?", tenant.ID).First(&profile).Error; err != nil {
		// Profile doesn't exist, create empty one
		profile = storage.BusinessProfile{
			TenantID: tenant.ID,
		}
	}

	// Get persona (or create empty one if doesn't exist)
	var persona storage.Persona
	if err := h.DB.Where("tenant_id = ?", tenant.ID).First(&persona).Error; err != nil {
		// Persona doesn't exist, create empty one
		persona = storage.Persona{
			TenantID: tenant.ID,
		}
	}

	// Get business menu items
	var menuItems []storage.BusinessMenuItem
	h.DB.Where("tenant_id = ?", tenant.ID).Find(&menuItems)

	// Get business FAQs
	var faqs []storage.BusinessFAQ
	h.DB.Where("tenant_id = ?", tenant.ID).Find(&faqs)

	// Auto-complete onboarding if tenant has sufficient data
	h.checkAndAutoCompleteOnboarding(&tenant)

	// Format menu items for response
	menuItemsResp := make([]map[string]any, len(menuItems))
	for i, item := range menuItems {
		menuItemsResp[i] = map[string]any{
			"id":          item.ID,
			"name":        item.Name,
			"category":    item.Category,
			"price":       item.Price,
			"description": item.Description,
		}
	}

	// Format FAQs for response
	faqsResp := make([]map[string]any, len(faqs))
	for i, faq := range faqs {
		faqsResp[i] = map[string]any{
			"id":       faq.ID,
			"question": faq.Question,
			"answer":   faq.Answer,
		}
	}

	// Format response with all business details
	writeJSON(w, http.StatusOK, map[string]any{
		"step":        tenant.OnboardingStep,
		"complete":    tenant.OnboardingComplete,
		"tenant_name": tenant.Name,
		"business_profile": map[string]any{
			"name":            getStringOrEmpty(profile.Name),
			"industry":        getStringOrEmpty(profile.Industry),
			"description":     getStringOrEmpty(profile.Description),
			"location":        getStringOrEmpty(profile.Location),
			"website":         getStringOrEmpty(profile.Website),
			"operating_hours": getStringOrEmpty(profile.OperatingHours),
		},
		"persona": map[string]any{
			"bot_name":    getStringOrEmpty(persona.BotName),
			"tone":        getStringOrEmpty(persona.Tone),
			"language":    getStringOrEmpty(persona.Language),
			"humor":       persona.Humor,
			"style_notes": getStringOrEmpty(persona.StyleNotes),
		},
		"business_details": map[string]any{
			"menu_items":     menuItemsResp,
			"faqs":           faqsResp,
			"keywords":       getStringOrEmpty(profile.Keywords),
			"knowledge_base": getStringOrEmpty(profile.KnowledgeBase),
		},
		"has_telegram_bot": tenant.TelegramBotToken != "",
	})
}

// Helper function to return empty string instead of empty value
func getStringOrEmpty(s string) string {
	if s == "" {
		return ""
	}
	return s
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0f seconds", d.Seconds())
	}
	if d < time.Hour {
		minutes := d.Minutes()
		if minutes < 1 {
			return "less than a minute"
		}
		return fmt.Sprintf("%.0f minutes", minutes)
	}
	if d < 24*time.Hour {
		hours := d.Hours()
		return fmt.Sprintf("%.1f hours", hours)
	}
	days := d.Hours() / 24
	return fmt.Sprintf("%.1f days", days)
}

// checkAndAutoCompleteOnboarding checks if tenant has sufficient data and auto-completes onboarding if needed
func (h *Handler) checkAndAutoCompleteOnboarding(tenant *storage.Tenant) {
	if tenant.OnboardingComplete {
		return // Already complete
	}

	// Get business profile
	var profile storage.BusinessProfile
	h.DB.Where("tenant_id = ?", tenant.ID).First(&profile)

	// Get persona
	var persona storage.Persona
	h.DB.Where("tenant_id = ?", tenant.ID).First(&persona)

	// Get business menu items
	var menuItemsCount int64
	h.DB.Model(&storage.BusinessMenuItem{}).Where("tenant_id = ?", tenant.ID).Count(&menuItemsCount)

	// Get business FAQs
	var faqsCount int64
	h.DB.Model(&storage.BusinessFAQ{}).Where("tenant_id = ?", tenant.ID).Count(&faqsCount)

	hasBusinessProfile := profile.Name != "" || profile.Industry != "" || profile.Description != ""
	hasPersona := persona.BotName != "" && persona.Tone != ""
	hasBusinessDetails := menuItemsCount > 0 || faqsCount > 0 || profile.Keywords != "" || profile.KnowledgeBase != ""

	// Check if tenant has been using the system (has posts, media, or social accounts)
	var postCount int64
	h.DB.Model(&storage.ScheduledPost{}).Where("tenant_id = ?", tenant.ID).Count(&postCount)

	var mediaCount int64
	h.DB.Model(&storage.MediaAsset{}).Where("tenant_id = ?", tenant.ID).Count(&mediaCount)

	var socialAccountCount int64
	h.DB.Model(&storage.SocialAccount{}).Where("tenant_id = ? AND connected = ?", tenant.ID, true).Count(&socialAccountCount)

	hasActivity := postCount > 0 || mediaCount > 0 || socialAccountCount > 0

	// Auto-complete if tenant has:
	// 1. Business profile AND persona (minimum required)
	// 2. OR has been actively using the system (posts/media/social accounts)
	if (hasBusinessProfile && hasPersona) || hasActivity {
		// Store original values before modifying in case we need to revert
		originalStep := tenant.OnboardingStep
		originalComplete := tenant.OnboardingComplete

		tenant.OnboardingStep = "complete"
		tenant.OnboardingComplete = true
		if err := h.DB.Save(tenant).Error; err != nil {
			h.Logger.Warn("failed to auto-complete onboarding", zap.Error(err), zap.Uint("tenant_id", tenant.ID))
			// Reload tenant from database to ensure we return the actual persisted state
			// This prevents returning modified in-memory state when save fails
			if reloadErr := h.DB.First(tenant, tenant.ID).Error; reloadErr != nil {
				// Both save and reload failed - revert in-memory state to original values
				h.Logger.Error("failed to reload tenant after save failure, reverting in-memory state", zap.Error(reloadErr), zap.Uint("tenant_id", tenant.ID))
				tenant.OnboardingStep = originalStep
				tenant.OnboardingComplete = originalComplete
			}
		} else {
			h.Logger.Info("auto-completed onboarding for existing tenant",
				zap.Uint("tenant_id", tenant.ID),
				zap.Bool("has_business_profile", hasBusinessProfile),
				zap.Bool("has_persona", hasPersona),
				zap.Bool("has_business_details", hasBusinessDetails),
				zap.Bool("has_activity", hasActivity))
		}
	}
}

func (h *Handler) updateBusinessProfile(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Name           string `json:"name"`
		Industry       string `json:"industry"`
		Description    string `json:"description"`
		Location       string `json:"location"`
		Website        string `json:"website"`
		OperatingHours string `json:"operating_hours"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	var profile storage.BusinessProfile
	if err := h.DB.Where("tenant_id = ?", claims.TenantID).First(&profile).Error; err != nil {
		// Create if doesn't exist
		profile = storage.BusinessProfile{
			TenantID: claims.TenantID,
		}
	}

	profile.Name = req.Name
	profile.Industry = req.Industry
	profile.Description = req.Description
	profile.Location = req.Location
	profile.Website = req.Website
	profile.OperatingHours = req.OperatingHours

	if err := h.DB.Save(&profile).Error; err != nil {
		h.Logger.Error("failed to update business profile", zap.Error(err))
		http.Error(w, "failed to update", http.StatusInternalServerError)
		return
	}

	// Update tenant step
	var tenant storage.Tenant
	h.DB.First(&tenant, claims.TenantID)
	if tenant.OnboardingStep == "business_profile" {
		tenant.OnboardingStep = "persona"
		h.DB.Save(&tenant)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":   true,
		"message":   "Business profile updated",
		"next_step": "persona",
	})
}

func (h *Handler) updatePersona(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		BotName    string `json:"bot_name"`
		Tone       string `json:"tone"`
		Language   string `json:"language"`
		Humor      bool   `json:"humor"`
		StyleNotes string `json:"style_notes"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	var persona storage.Persona
	if err := h.DB.Where("tenant_id = ?", claims.TenantID).First(&persona).Error; err != nil {
		// Create if doesn't exist
		persona = storage.Persona{
			TenantID: claims.TenantID,
		}
	}

	persona.BotName = req.BotName
	persona.Tone = req.Tone
	persona.Language = req.Language
	persona.Humor = req.Humor
	persona.StyleNotes = req.StyleNotes

	if err := h.DB.Save(&persona).Error; err != nil {
		h.Logger.Error("failed to update persona", zap.Error(err))
		http.Error(w, "failed to update", http.StatusInternalServerError)
		return
	}

	// Update tenant step
	var tenant storage.Tenant
	h.DB.First(&tenant, claims.TenantID)
	if tenant.OnboardingStep == "persona" {
		tenant.OnboardingStep = "business_details"
		h.DB.Save(&tenant)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":   true,
		"message":   "Persona updated",
		"next_step": "business_details",
	})
}

func (h *Handler) updateBusinessDetails(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		MenuItems []struct {
			Name        string `json:"name"`
			Category    string `json:"category"`
			Price       string `json:"price"`
			Description string `json:"description"`
		} `json:"menu_items"`
		FAQs []struct {
			Question string `json:"question"`
			Answer   string `json:"answer"`
		} `json:"faqs"`
		Keywords      string `json:"keywords"`
		KnowledgeBase string `json:"knowledge_base"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Update business profile keywords and knowledge base
	var profile storage.BusinessProfile
	if err := h.DB.Where("tenant_id = ?", claims.TenantID).First(&profile).Error; err == nil {
		profile.Keywords = req.Keywords
		profile.KnowledgeBase = req.KnowledgeBase
		h.DB.Save(&profile)
	}

	// Delete existing menu items and FAQs (optional - can keep or replace)
	// h.DB.Where("tenant_id = ?", claims.TenantID).Delete(&storage.BusinessMenuItem{})
	// h.DB.Where("tenant_id = ?", claims.TenantID).Delete(&storage.BusinessFAQ{})

	// Create menu items
	for _, item := range req.MenuItems {
		menuItem := storage.BusinessMenuItem{
			TenantID:    claims.TenantID,
			Name:        item.Name,
			Category:    item.Category,
			Price:       item.Price,
			Description: item.Description,
		}
		h.DB.Create(&menuItem)
	}

	// Create FAQs
	for _, faq := range req.FAQs {
		businessFAQ := storage.BusinessFAQ{
			TenantID: claims.TenantID,
			Question: faq.Question,
			Answer:   faq.Answer,
		}
		h.DB.Create(&businessFAQ)
	}

	// Update tenant step
	var tenant storage.Tenant
	h.DB.First(&tenant, claims.TenantID)
	if tenant.OnboardingStep == "business_details" {
		tenant.OnboardingStep = "social_connect"
		h.DB.Save(&tenant)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":   true,
		"message":   "Business details updated",
		"next_step": "social_connect",
	})
}

func (h *Handler) completeOnboarding(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var tenant storage.Tenant
	if err := h.DB.First(&tenant, claims.TenantID).Error; err != nil {
		http.Error(w, "tenant not found", http.StatusNotFound)
		return
	}

	tenant.OnboardingStep = "complete"
	tenant.OnboardingComplete = true

	if err := h.DB.Save(&tenant).Error; err != nil {
		h.Logger.Error("failed to complete onboarding", zap.Error(err))
		http.Error(w, "failed to complete", http.StatusInternalServerError)
		return
	}

	h.Logger.Info("onboarding completed", zap.Uint("tenant_id", claims.TenantID))

	writeJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"message":     "Onboarding completed! You can now connect your social media accounts.",
		"redirect_to": "/dashboard",
	})
}

// =======================
// Settings (post-onboarding)
// =======================
func (h *Handler) updateBusinessProfileSettings(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		Name           *string `json:"name"`
		Industry       *string `json:"industry"`
		Description    *string `json:"description"`
		Location       *string `json:"location"`
		Website        *string `json:"website"`
		OperatingHours *string `json:"operating_hours"`
		Timezone       *string `json:"timezone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	var profile storage.BusinessProfile
	if err := h.DB.Where("tenant_id = ?", claims.TenantID).First(&profile).Error; err != nil {
		profile = storage.BusinessProfile{TenantID: claims.TenantID}
	}
	if req.Name != nil {
		profile.Name = *req.Name
	}
	if req.Industry != nil {
		profile.Industry = *req.Industry
	}
	if req.Description != nil {
		profile.Description = *req.Description
	}
	if req.Location != nil {
		profile.Location = *req.Location
	}
	if req.Website != nil {
		profile.Website = *req.Website
	}
	if req.OperatingHours != nil {
		profile.OperatingHours = *req.OperatingHours
	}
	if err := h.DB.Save(&profile).Error; err != nil {
		h.Logger.Error("failed to update business profile settings", zap.Error(err))
		http.Error(w, "failed to update", http.StatusInternalServerError)
		return
	}
	if req.Timezone != nil && *req.Timezone != "" {
		var tenant storage.Tenant
		if err := h.DB.First(&tenant, claims.TenantID).Error; err == nil {
			tenant.Timezone = *req.Timezone
			_ = h.DB.Save(&tenant).Error
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Business profile updated"})
}

func (h *Handler) updatePersonaSettings(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		BotName    *string `json:"bot_name"`
		Tone       *string `json:"tone"`
		Language   *string `json:"language"`
		Humor      *bool   `json:"humor"`
		StyleNotes *string `json:"style_notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	var persona storage.Persona
	if err := h.DB.Where("tenant_id = ?", claims.TenantID).First(&persona).Error; err != nil {
		persona = storage.Persona{TenantID: claims.TenantID}
	}
	if req.BotName != nil {
		persona.BotName = *req.BotName
	}
	if req.Tone != nil {
		persona.Tone = *req.Tone
	}
	if req.Language != nil {
		persona.Language = *req.Language
	}
	if req.Humor != nil {
		persona.Humor = *req.Humor
	}
	if req.StyleNotes != nil {
		persona.StyleNotes = *req.StyleNotes
	}
	if err := h.DB.Save(&persona).Error; err != nil {
		h.Logger.Error("failed to update persona settings", zap.Error(err))
		http.Error(w, "failed to update", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Persona updated"})
}

func (h *Handler) updateBusinessDetailsSettings(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		MenuItems []struct {
			ID          *uint  `json:"id,omitempty"` // nil = new item, provided = update existing
			Name        string `json:"name"`
			Category    string `json:"category"`
			Price       string `json:"price"`
			Description string `json:"description"`
		} `json:"menu_items"`
		FAQs []struct {
			ID       *uint  `json:"id,omitempty"` // nil = new FAQ, provided = update existing
			Question string `json:"question"`
			Answer   string `json:"answer"`
		} `json:"faqs"`
		Keywords      *string `json:"keywords"`
		KnowledgeBase *string `json:"knowledge_base"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Update keywords and knowledge base if provided
	var profile storage.BusinessProfile
	if err := h.DB.Where("tenant_id = ?", claims.TenantID).First(&profile).Error; err != nil {
		profile = storage.BusinessProfile{TenantID: claims.TenantID}
		if err := h.DB.Create(&profile).Error; err != nil {
			h.Logger.Error("failed to create business profile", zap.Error(err))
		}
	}

	if req.Keywords != nil {
		profile.Keywords = *req.Keywords
	}
	if req.KnowledgeBase != nil {
		profile.KnowledgeBase = *req.KnowledgeBase
	}
	if req.Keywords != nil || req.KnowledgeBase != nil {
		if err := h.DB.Save(&profile).Error; err != nil {
			h.Logger.Error("failed to update business profile", zap.Error(err))
		}
	}

	// Handle menu items: update existing, create new
	for _, item := range req.MenuItems {
		if item.ID != nil {
			// Update existing menu item
			var menuItem storage.BusinessMenuItem
			if err := h.DB.Where("id = ? AND tenant_id = ?", *item.ID, claims.TenantID).First(&menuItem).Error; err == nil {
				menuItem.Name = item.Name
				menuItem.Category = item.Category
				menuItem.Price = item.Price
				menuItem.Description = item.Description
				h.DB.Save(&menuItem)
			}
		} else {
			// Create new menu item
			menuItem := storage.BusinessMenuItem{
				TenantID:    claims.TenantID,
				Name:        item.Name,
				Category:    item.Category,
				Price:       item.Price,
				Description: item.Description,
			}
			h.DB.Create(&menuItem)
		}
	}

	// Handle FAQs: update existing, create new
	for _, faq := range req.FAQs {
		if faq.ID != nil {
			// Update existing FAQ
			var businessFAQ storage.BusinessFAQ
			if err := h.DB.Where("id = ? AND tenant_id = ?", *faq.ID, claims.TenantID).First(&businessFAQ).Error; err == nil {
				businessFAQ.Question = faq.Question
				businessFAQ.Answer = faq.Answer
				h.DB.Save(&businessFAQ)
			}
		} else {
			// Create new FAQ
			businessFAQ := storage.BusinessFAQ{
				TenantID: claims.TenantID,
				Question: faq.Question,
				Answer:   faq.Answer,
			}
			h.DB.Create(&businessFAQ)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Business details updated",
	})
}

// =======================
// Scheduled posts updates & calendar
// =======================
func (h *Handler) updateScheduledPost(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	postID := chi.URLParam(r, "id")
	if postID == "" {
		http.Error(w, "post id required", http.StatusBadRequest)
		return
	}
	var req struct {
		ScheduledAt *string  `json:"scheduled_at"` // RFC3339
		Caption     *string  `json:"caption"`
		Platforms   []string `json:"platforms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	var post storage.ScheduledPost
	if err := h.DB.Where("tenant_id = ? AND id = ?", claims.TenantID, postID).First(&post).Error; err != nil {
		http.Error(w, "scheduled post not found", http.StatusNotFound)
		return
	}
	if req.Caption != nil {
		post.Caption = *req.Caption
	}
	if req.ScheduledAt != nil && *req.ScheduledAt != "" {
		if t, err := time.Parse(time.RFC3339, *req.ScheduledAt); err == nil {
			post.ScheduledAt = &t
		} else {
			http.Error(w, "invalid scheduled_at", http.StatusBadRequest)
			return
		}
	}
	if req.Platforms != nil {
		if b, err := json.Marshal(req.Platforms); err == nil {
			post.Platforms = string(b)
		}
	}

	// Store old scheduled_at BEFORE updating
	oldScheduledAt := post.ScheduledAt
	var oldDate *time.Time
	if oldScheduledAt != nil {
		normalized := time.Date(oldScheduledAt.Year(), oldScheduledAt.Month(), oldScheduledAt.Day(), 0, 0, 0, 0, time.UTC)
		oldDate = &normalized
	}

	if err := h.DB.Save(&post).Error; err != nil {
		h.Logger.Error("failed to update scheduled post", zap.Error(err))
		http.Error(w, "failed to update", http.StatusInternalServerError)
		return
	}

	// Update calendar: remove from old date, add to new date
	if post.ScheduledAt != nil {
		newDate := time.Date(post.ScheduledAt.Year(), post.ScheduledAt.Month(), post.ScheduledAt.Day(), 0, 0, 0, 0, time.UTC)

		// Remove from old date if date changed
		if oldDate != nil && !oldDate.Equal(newDate) {
			h.removePostFromCalendar(claims.TenantID, post.ID, *oldDate)
		}

		// Add to new date (or update if same date)
		h.updateCalendarForPost(claims.TenantID, post.ID, *post.ScheduledAt)
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// getCalendar returns calendar entries with full ContentCalendar data (themes, notes, posts)
func (h *Handler) getCalendar(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")
	if startStr == "" || endStr == "" {
		http.Error(w, "start and end required (YYYY-MM-DD or RFC3339)", http.StatusBadRequest)
		return
	}
	// Parse dates (accept YYYY-MM-DD)
	parseDay := func(s string) (time.Time, error) {
		if len(s) == 10 {
			return time.Parse("2006-01-02", s)
		}
		return time.Parse(time.RFC3339, s)
	}
	start, err1 := parseDay(startStr)
	end, err2 := parseDay(endStr)
	if err1 != nil || err2 != nil {
		http.Error(w, "invalid date range", http.StatusBadRequest)
		return
	}
	// Normalize dates to start of day for calendar entries
	startDate := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	endDate := time.Date(end.Year(), end.Month(), end.Day(), 23, 59, 59, 999999999, time.UTC)

	// Fetch calendar entries
	var entries []storage.ContentCalendar
	if err := h.DB.Where("tenant_id = ? AND date >= ? AND date <= ?", claims.TenantID, startDate, endDate).
		Order("date ASC").Find(&entries).Error; err != nil {
		h.Logger.Error("failed to fetch calendar entries", zap.Error(err))
		http.Error(w, "failed to fetch calendar", http.StatusInternalServerError)
		return
	}

	// Also fetch posts for dates without calendar entries to auto-create entries
	var posts []storage.ScheduledPost
	if err := h.DB.Where("tenant_id = ? AND scheduled_at >= ? AND scheduled_at <= ?", claims.TenantID, startDate, endDate).
		Order("scheduled_at ASC").Find(&posts).Error; err != nil {
		h.Logger.Warn("failed to fetch posts for calendar", zap.Error(err))
	}

	// Create map of existing entries by date
	entryMap := make(map[string]*storage.ContentCalendar)
	for i := range entries {
		dateKey := entries[i].Date.Format("2006-01-02")
		entryMap[dateKey] = &entries[i]
	}

	// Auto-create calendar entries for posts on dates without entries
	postsByDate := make(map[string][]uint)
	for _, post := range posts {
		if post.ScheduledAt == nil {
			continue
		}
		dateKey := post.ScheduledAt.Format("2006-01-02")
		if _, exists := entryMap[dateKey]; !exists {
			// Create entry for this date
			normalizedDate := time.Date(post.ScheduledAt.Year(), post.ScheduledAt.Month(), post.ScheduledAt.Day(), 0, 0, 0, 0, time.UTC)
			entry := storage.ContentCalendar{
				TenantID: claims.TenantID,
				Date:     normalizedDate,
				Status:   "scheduled",
				Posts:    "[]",
			}
			if err := h.DB.Create(&entry).Error; err == nil {
				entryMap[dateKey] = &entry
				entries = append(entries, entry)
			}
		}
		postsByDate[dateKey] = append(postsByDate[dateKey], post.ID)
	}

	// Build response with post counts
	type calendarEntry struct {
		ID        uint      `json:"id"`
		Date      time.Time `json:"date"`
		Theme     string    `json:"theme"`
		Notes     string    `json:"notes"`
		Status    string    `json:"status"`
		PostIDs   []uint    `json:"post_ids"`
		PostCount int       `json:"post_count"`
	}

	resp := make([]calendarEntry, 0, len(entries))
	for _, entry := range entries {
		dateKey := entry.Date.Format("2006-01-02")

		// Parse post IDs from entry
		var postIDs []uint
		if entry.Posts != "" && entry.Posts != "[]" {
			if err := json.Unmarshal([]byte(entry.Posts), &postIDs); err != nil {
				postIDs = []uint{}
			}
		}

		// Merge with posts found for this date
		if datePosts, ok := postsByDate[dateKey]; ok {
			// Add posts that aren't already in the entry
			existingMap := make(map[uint]bool)
			for _, id := range postIDs {
				existingMap[id] = true
			}
			for _, id := range datePosts {
				if !existingMap[id] {
					postIDs = append(postIDs, id)
				}
			}
		}

		resp = append(resp, calendarEntry{
			ID:        entry.ID,
			Date:      entry.Date,
			Theme:     entry.Theme,
			Notes:     entry.Notes,
			Status:    entry.Status,
			PostIDs:   postIDs,
			PostCount: len(postIDs),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"entries": resp})
}

// getCalendarDay returns a single day's calendar entry with full post details
func (h *Handler) getCalendarDay(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	dateStr := chi.URLParam(r, "date")
	if dateStr == "" {
		http.Error(w, "date required (YYYY-MM-DD)", http.StatusBadRequest)
		return
	}

	// Parse date
	var targetDate time.Time
	var err error
	if len(dateStr) == 10 {
		targetDate, err = time.Parse("2006-01-02", dateStr)
	} else {
		targetDate, err = time.Parse(time.RFC3339, dateStr)
	}
	if err != nil {
		http.Error(w, "invalid date format", http.StatusBadRequest)
		return
	}

	// Normalize to start of day
	normalizedDate := time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(), 0, 0, 0, 0, time.UTC)

	// Get or create calendar entry
	var entry storage.ContentCalendar
	err = h.DB.Where("tenant_id = ? AND date = ?", claims.TenantID, normalizedDate).First(&entry).Error

	if err != nil {
		// Create entry if doesn't exist
		entry = storage.ContentCalendar{
			TenantID: claims.TenantID,
			Date:     normalizedDate,
			Status:   "planned",
			Posts:    "[]",
		}
		if err := h.DB.Create(&entry).Error; err != nil {
			h.Logger.Error("failed to create calendar entry", zap.Error(err))
			http.Error(w, "failed to create calendar entry", http.StatusInternalServerError)
			return
		}
	}

	// Parse post IDs
	var postIDs []uint
	if entry.Posts != "" && entry.Posts != "[]" {
		if err := json.Unmarshal([]byte(entry.Posts), &postIDs); err != nil {
			postIDs = []uint{}
		}
	}

	// Also fetch posts scheduled for this day that might not be in the entry
	var dayPosts []storage.ScheduledPost
	startOfDay := normalizedDate
	endOfDay := normalizedDate.Add(24 * time.Hour)
	h.DB.Where("tenant_id = ? AND scheduled_at >= ? AND scheduled_at < ?", claims.TenantID, startOfDay, endOfDay).
		Order("scheduled_at ASC").Find(&dayPosts)

	// Merge post IDs
	existingMap := make(map[uint]bool)
	for _, id := range postIDs {
		existingMap[id] = true
	}
	for _, post := range dayPosts {
		if !existingMap[post.ID] {
			postIDs = append(postIDs, post.ID)
		}
	}

	// Fetch full post details
	var posts []storage.ScheduledPost
	if len(postIDs) > 0 {
		h.DB.Where("tenant_id = ? AND id IN ?", claims.TenantID, postIDs).
			Order("scheduled_at ASC").Find(&posts)
	}

	// Build post details
	type postDetail struct {
		ID          uint       `json:"id"`
		Name        string     `json:"name"`
		Caption     string     `json:"caption"`
		Status      string     `json:"status"`
		ScheduledAt *time.Time `json:"scheduled_at"`
		Platforms   []string   `json:"platforms"`
		MediaIDs    []uint     `json:"media_ids"`
	}

	postDetails := make([]postDetail, 0, len(posts))
	for _, post := range posts {
		var plats []string
		if strings.TrimSpace(post.Platforms) != "" {
			_ = json.Unmarshal([]byte(post.Platforms), &plats)
		}
		var mediaIDs []uint
		if strings.TrimSpace(post.MediaAssetIDs) != "" {
			_ = json.Unmarshal([]byte(post.MediaAssetIDs), &mediaIDs)
		}
		postDetails = append(postDetails, postDetail{
			ID:          post.ID,
			Name:        post.Name,
			Caption:     post.Caption,
			Status:      post.Status,
			ScheduledAt: post.ScheduledAt,
			Platforms:   plats,
			MediaIDs:    mediaIDs,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"entry": map[string]any{
			"id":         entry.ID,
			"date":       entry.Date,
			"theme":      entry.Theme,
			"notes":      entry.Notes,
			"status":     entry.Status,
			"posts":      postDetails,
			"post_count": len(postDetails),
		},
	})
}

// createCalendarEntry creates a new calendar entry
func (h *Handler) createCalendarEntry(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		Date   string `json:"date"` // YYYY-MM-DD
		Theme  string `json:"theme"`
		Notes  string `json:"notes"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Date == "" {
		http.Error(w, "date required", http.StatusBadRequest)
		return
	}

	// Parse date
	var targetDate time.Time
	var err error
	if len(req.Date) == 10 {
		targetDate, err = time.Parse("2006-01-02", req.Date)
	} else {
		targetDate, err = time.Parse(time.RFC3339, req.Date)
	}
	if err != nil {
		http.Error(w, "invalid date format", http.StatusBadRequest)
		return
	}

	// Normalize to start of day
	normalizedDate := time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(), 0, 0, 0, 0, time.UTC)

	// Check if entry already exists
	var existing storage.ContentCalendar
	if err := h.DB.Where("tenant_id = ? AND date = ?", claims.TenantID, normalizedDate).First(&existing).Error; err == nil {
		http.Error(w, "calendar entry already exists for this date", http.StatusConflict)
		return
	}

	status := req.Status
	if status == "" {
		status = "planned"
	}

	entry := storage.ContentCalendar{
		TenantID: claims.TenantID,
		Date:     normalizedDate,
		Theme:    req.Theme,
		Notes:    req.Notes,
		Status:   status,
		Posts:    "[]",
	}

	if err := h.DB.Create(&entry).Error; err != nil {
		h.Logger.Error("failed to create calendar entry", zap.Error(err))
		http.Error(w, "failed to create calendar entry", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"success": true,
		"entry": map[string]any{
			"id":       entry.ID,
			"date":     entry.Date,
			"theme":    entry.Theme,
			"notes":    entry.Notes,
			"status":   entry.Status,
			"post_ids": []uint{},
		},
	})
}

// updateCalendarEntry updates a calendar entry's theme, notes, or status
func (h *Handler) updateCalendarEntry(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	dateStr := chi.URLParam(r, "date")
	if dateStr == "" {
		http.Error(w, "date required (YYYY-MM-DD)", http.StatusBadRequest)
		return
	}

	var req struct {
		Theme  *string `json:"theme"`
		Notes  *string `json:"notes"`
		Status *string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Parse date
	var targetDate time.Time
	var err error
	if len(dateStr) == 10 {
		targetDate, err = time.Parse("2006-01-02", dateStr)
	} else {
		targetDate, err = time.Parse(time.RFC3339, dateStr)
	}
	if err != nil {
		http.Error(w, "invalid date format", http.StatusBadRequest)
		return
	}

	// Normalize to start of day
	normalizedDate := time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(), 0, 0, 0, 0, time.UTC)

	// Get or create entry
	var entry storage.ContentCalendar
	err = h.DB.Where("tenant_id = ? AND date = ?", claims.TenantID, normalizedDate).First(&entry).Error

	if err != nil {
		// Create if doesn't exist
		entry = storage.ContentCalendar{
			TenantID: claims.TenantID,
			Date:     normalizedDate,
			Status:   "planned",
			Posts:    "[]",
		}
	}

	// Update fields
	if req.Theme != nil {
		entry.Theme = *req.Theme
	}
	if req.Notes != nil {
		entry.Notes = *req.Notes
	}
	if req.Status != nil {
		entry.Status = *req.Status
	}

	if err := h.DB.Save(&entry).Error; err != nil {
		h.Logger.Error("failed to update calendar entry", zap.Error(err))
		http.Error(w, "failed to update calendar entry", http.StatusInternalServerError)
		return
	}

	// Parse post IDs for response
	var postIDs []uint
	if entry.Posts != "" && entry.Posts != "[]" {
		_ = json.Unmarshal([]byte(entry.Posts), &postIDs)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"entry": map[string]any{
			"id":         entry.ID,
			"date":       entry.Date,
			"theme":      entry.Theme,
			"notes":      entry.Notes,
			"status":     entry.Status,
			"post_ids":   postIDs,
			"post_count": len(postIDs),
		},
	})
}

// removePostFromCalendar removes a post from a calendar entry
func (h *Handler) removePostFromCalendar(tenantID uint, postID uint, date time.Time) {
	normalizedDate := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)

	var entry storage.ContentCalendar
	if err := h.DB.Where("tenant_id = ? AND date = ?", tenantID, normalizedDate).First(&entry).Error; err != nil {
		return // Entry doesn't exist, nothing to remove
	}

	// Parse existing posts
	var postIDs []uint
	if entry.Posts != "" && entry.Posts != "[]" {
		if err := json.Unmarshal([]byte(entry.Posts), &postIDs); err != nil {
			return
		}
	}

	// Remove post ID
	newPostIDs := make([]uint, 0, len(postIDs))
	for _, id := range postIDs {
		if id != postID {
			newPostIDs = append(newPostIDs, id)
		}
	}

	postsJSON, _ := json.Marshal(newPostIDs)
	entry.Posts = string(postsJSON)

	// Update status if no posts left
	if len(newPostIDs) == 0 && entry.Theme == "" && entry.Notes == "" {
		entry.Status = "planned"
	}

	if err := h.DB.Save(&entry).Error; err != nil {
		h.Logger.Error("failed to remove post from calendar", zap.Error(err), zap.Uint("tenant_id", tenantID), zap.Uint("post_id", postID))
	}
}

// updateCalendarForPost is a helper function that automatically updates the calendar when a post is created/updated
func (h *Handler) updateCalendarForPost(tenantID uint, postID uint, scheduledAt time.Time) {
	// Normalize date to start of day
	normalizedDate := time.Date(scheduledAt.Year(), scheduledAt.Month(), scheduledAt.Day(), 0, 0, 0, 0, time.UTC)

	// Get or create calendar entry
	var entry storage.ContentCalendar
	err := h.DB.Where("tenant_id = ? AND date = ?", tenantID, normalizedDate).First(&entry).Error

	if err != nil {
		// Create entry if it doesn't exist
		entry = storage.ContentCalendar{
			TenantID: tenantID,
			Date:     normalizedDate,
			Status:   "scheduled",
			Posts:    "[]",
		}
		if err := h.DB.Create(&entry).Error; err != nil {
			h.Logger.Error("failed to create calendar entry for post", zap.Error(err), zap.Uint("tenant_id", tenantID), zap.Uint("post_id", postID))
			return
		}
	}

	// Parse existing posts
	var postIDs []uint
	if entry.Posts != "" && entry.Posts != "[]" {
		if err := json.Unmarshal([]byte(entry.Posts), &postIDs); err != nil {
			postIDs = []uint{}
		}
	}

	// Check if post already exists
	for _, id := range postIDs {
		if id == postID {
			return // Already added
		}
	}

	// Add new post ID
	postIDs = append(postIDs, postID)
	postsJSON, _ := json.Marshal(postIDs)
	entry.Posts = string(postsJSON)
	entry.Status = "scheduled"

	if err := h.DB.Save(&entry).Error; err != nil {
		h.Logger.Error("failed to update calendar entry with post", zap.Error(err), zap.Uint("tenant_id", tenantID), zap.Uint("post_id", postID))
	}
}

// =======================
// WhatsApp helpers
// =======================
func (h *Handler) selectWhatsAppNumber(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		NumberID string `json:"number_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.NumberID) == "" {
		http.Error(w, "number_id required", http.StatusBadRequest)
		return
	}
	// Reuse assignPhoneNumber by setting URL param is cumbersome; directly implement here
	var phoneNum storage.WhatsAppPhoneNumber
	if err := h.DB.Where("id = ? OR phone_number_id = ?", req.NumberID, req.NumberID).First(&phoneNum).Error; err != nil {
		http.Error(w, "phone number not found", http.StatusNotFound)
		return
	}
	if phoneNum.Status != "available" || (phoneNum.TenantID != nil && *phoneNum.TenantID != 0) {
		http.Error(w, "phone number is not available", http.StatusBadRequest)
		return
	}
	var tenant storage.Tenant
	if err := h.DB.First(&tenant, claims.TenantID).Error; err != nil {
		http.Error(w, "tenant not found", http.StatusNotFound)
		return
	}
	now := time.Now()
	phoneNum.TenantID = &claims.TenantID
	phoneNum.DisplayName = tenant.Name
	phoneNum.Status = "assigned"
	phoneNum.AssignedAt = &now
	if err := h.DB.Save(&phoneNum).Error; err != nil {
		http.Error(w, "failed to assign number", http.StatusInternalServerError)
		return
	}
	// Ensure social account exists/updated
	var account storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ?", claims.TenantID, "whatsapp").First(&account).Error; err != nil {
		account = storage.SocialAccount{
			TenantID:   claims.TenantID,
			Platform:   "whatsapp",
			PageID:     phoneNum.PhoneNumberID,
			ExternalID: phoneNum.PhoneNumberID,
			Username:   phoneNum.PhoneNumber,
			Connected:  true,
			Mode:       "provider_owned",
		}
		_ = h.DB.Create(&account).Error
	} else {
		account.PageID = phoneNum.PhoneNumberID
		account.ExternalID = phoneNum.PhoneNumberID
		account.Username = phoneNum.PhoneNumber
		account.Connected = true
		account.Mode = "provider_owned"
		_ = h.DB.Save(&account).Error
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message":         "WhatsApp number assigned successfully",
		"phone_number":    phoneNum.PhoneNumber,
		"phone_number_id": phoneNum.PhoneNumberID,
	})
}

// =======================
// Conversations enhancements
// =======================
func (h *Handler) updateConversation(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conversationID := chi.URLParam(r, "id")
	if conversationID == "" {
		http.Error(w, "conversation id required", http.StatusBadRequest)
		return
	}
	var req struct {
		Status string   `json:"status"`
		Tags   []string `json:"tags"`
		Notes  string   `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	var conv storage.Conversation
	if err := h.DB.Where("tenant_id = ? AND id = ?", claims.TenantID, conversationID).
		First(&conv).Error; err != nil {
		http.Error(w, "conversation not found", http.StatusNotFound)
		return
	}
	if req.Status != "" {
		conv.Status = req.Status
	}
	if err := h.DB.Save(&conv).Error; err != nil {
		http.Error(w, "failed to update", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *Handler) suggestReplies(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conversationID := chi.URLParam(r, "id")
	if conversationID == "" {
		http.Error(w, "conversation id required", http.StatusBadRequest)
		return
	}
	// Fetch last interaction content to base suggestions on
	var last storage.Interaction
	if err := h.DB.Where("tenant_id = ? AND conversation_id = ?", claims.TenantID, conversationID).
		Order("created_at DESC").First(&last).Error; err != nil {
		http.Error(w, "no conversation messages found", http.StatusNotFound)
		return
	}
	// Very basic heuristic suggestions (placeholder). Can integrate AI client.
	suggestions := []string{
		"Thanks for reaching out! Could you share a bit more detail?",
		"Happy to help with that. Would you prefer Instagram or Facebook for this post?",
		"I can schedule this for you. Do you have a preferred date/time?",
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestions": suggestions})
}

// =======================
// Media Library
// =======================
func (h *Handler) listMedia(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	query := h.DB.Where("tenant_id = ?", claims.TenantID)
	if t := r.URL.Query().Get("type"); t != "" {
		query = query.Where("type = ?", t)
	}
	if tag := r.URL.Query().Get("tag"); tag != "" {
		query = query.Where("tags LIKE ?", "%"+tag+"%")
	}
	if campaign := r.URL.Query().Get("campaign"); campaign != "" {
		query = query.Where("campaign = ?", campaign)
	}
	limit := 50
	if s := r.URL.Query().Get("limit"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 && v <= 200 {
			limit = v
		}
	}
	var assets []storage.MediaAsset
	if err := query.Order("created_at DESC").Limit(limit).Find(&assets).Error; err != nil {
		h.Logger.Error("failed to list media", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "failed to list media",
			"message": "An error occurred while fetching media assets",
		})
		return
	}

	// Transform assets to frontend-friendly format
	type MediaAssetResponse struct {
		ID           uint       `json:"id"`
		Type         string     `json:"type"`
		Name         string     `json:"name"`
		CarouselName string     `json:"carousel_name,omitempty"`
		Caption      string     `json:"caption,omitempty"`
		URLs         []string   `json:"urls"`
		Status       string     `json:"status"`
		ScheduledAt  *time.Time `json:"scheduled_at,omitempty"`
		Platforms    []string   `json:"platforms"`
		Tags         []string   `json:"tags"`
		Campaign     string     `json:"campaign,omitempty"`
		CreatedAt    time.Time  `json:"created_at"`
		UpdatedAt    time.Time  `json:"updated_at"`
	}

	response := make([]MediaAssetResponse, 0, len(assets))
	for _, asset := range assets {
		// Parse JSON strings into arrays
		var urls []string
		if asset.URLs != "" && asset.URLs != "[]" {
			if err := json.Unmarshal([]byte(asset.URLs), &urls); err != nil {
				h.Logger.Warn("failed to parse URLs", zap.Uint("asset_id", asset.ID), zap.Error(err))
				urls = []string{}
			}
		}

		var platforms []string
		if asset.Platforms != "" && asset.Platforms != "[]" {
			if err := json.Unmarshal([]byte(asset.Platforms), &platforms); err != nil {
				h.Logger.Warn("failed to parse platforms", zap.Uint("asset_id", asset.ID), zap.Error(err))
				platforms = []string{}
			}
		}

		var tags []string
		if asset.Tags != "" && asset.Tags != "[]" {
			if err := json.Unmarshal([]byte(asset.Tags), &tags); err != nil {
				h.Logger.Warn("failed to parse tags", zap.Uint("asset_id", asset.ID), zap.Error(err))
				tags = []string{}
			}
		}

		response = append(response, MediaAssetResponse{
			ID:           asset.ID,
			Type:         asset.Type,
			Name:         asset.Name,
			CarouselName: asset.CarouselName,
			Caption:      asset.Caption,
			URLs:         urls,
			Status:       asset.Status,
			ScheduledAt:  asset.ScheduledAt,
			Platforms:    platforms,
			Tags:         tags,
			Campaign:     asset.Campaign,
			CreatedAt:    asset.CreatedAt,
			UpdatedAt:    asset.UpdatedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": response,
		"count": len(response),
	})
}

func (h *Handler) getMedia(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id := chi.URLParam(r, "id")
	var asset storage.MediaAsset
	if err := h.DB.Where("tenant_id = ? AND id = ?", claims.TenantID, id).First(&asset).Error; err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error":   "media not found",
			"message": fmt.Sprintf("Media asset with ID %s not found", id),
		})
		return
	}

	// Parse JSON strings into arrays (same format as listMedia)
	var urls []string
	if asset.URLs != "" && asset.URLs != "[]" {
		_ = json.Unmarshal([]byte(asset.URLs), &urls)
	}

	var platforms []string
	if asset.Platforms != "" && asset.Platforms != "[]" {
		_ = json.Unmarshal([]byte(asset.Platforms), &platforms)
	}

	var tags []string
	if asset.Tags != "" && asset.Tags != "[]" {
		_ = json.Unmarshal([]byte(asset.Tags), &tags)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":            asset.ID,
		"type":          asset.Type,
		"name":          asset.Name,
		"carousel_name": asset.CarouselName,
		"caption":       asset.Caption,
		"urls":          urls,
		"status":        asset.Status,
		"scheduled_at":  asset.ScheduledAt,
		"platforms":     platforms,
		"tags":          tags,
		"campaign":      asset.Campaign,
		"created_at":    asset.CreatedAt,
		"updated_at":    asset.UpdatedAt,
	})
}

func (h *Handler) createMedia(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Get content type for logging (but don't fail if missing - let ParseMultipartForm handle it)
	contentType := r.Header.Get("Content-Type")
	if contentType != "" && !strings.HasPrefix(contentType, "multipart/form-data") {
		h.Logger.Warn("unexpected content type for media upload",
			zap.String("content_type", contentType),
			zap.String("method", r.Method),
			zap.String("url", r.URL.String()),
			zap.String("note", "attempting to parse anyway"))
		// Don't fail here - let ParseMultipartForm try to handle it
	}

	// Parse multipart form (max 50MB per file)
	// Note: ParseMultipartForm will auto-detect Content-Type even if boundary is missing
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		h.Logger.Error("failed to parse multipart form",
			zap.Error(err),
			zap.String("content_type", contentType),
			zap.String("method", r.Method))
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "failed to parse multipart form",
			"details": err.Error(),
		})
		return
	}

	// Try different possible field names
	var file multipart.File
	var fileHeader *multipart.FileHeader
	var err error

	fileNames := []string{"file", "media", "upload", "image", "video"}
	for _, fieldName := range fileNames {
		file, fileHeader, err = r.FormFile(fieldName)
		if err == nil {
			break
		}
	}

	if err != nil || file == nil {
		// List available form fields for debugging
		var availableFileFields []string
		var availableValueFields []string
		if r.MultipartForm != nil {
			for key := range r.MultipartForm.File {
				availableFileFields = append(availableFileFields, key)
			}
			for key := range r.MultipartForm.Value {
				availableValueFields = append(availableValueFields, key)
			}
		}

		// Log all form fields for debugging
		h.Logger.Warn("no file found in form",
			zap.String("content_type", contentType),
			zap.String("method", r.Method),
			zap.String("url", r.URL.String()),
			zap.Strings("available_file_fields", availableFileFields),
			zap.Strings("available_value_fields", availableValueFields),
			zap.Error(err))

		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":                  "file is required",
			"message":                "Please include a file with form field name: 'file', 'media', 'upload', 'image', or 'video'",
			"available_file_fields":  availableFileFields,
			"available_value_fields": availableValueFields,
			"tried_fields":           fileNames,
		})
		return
	}
	defer file.Close()

	// Get optional metadata
	name := r.FormValue("name")
	tags := r.FormValue("tags") // Comma-separated
	campaign := r.FormValue("campaign")

	// Get TikTok posting options (if provided)
	tiktokDisableDuet := r.FormValue("tiktok_disable_duet") == "true"
	tiktokDisableStitch := r.FormValue("tiktok_disable_stitch") == "true"
	tiktokDisableComment := r.FormValue("tiktok_disable_comment") == "true"
	tiktokScheduleTime := r.FormValue("tiktok_schedule_time") // RFC3339 format

	// Determine resource type from file
	mimeType := fileHeader.Header.Get("Content-Type")
	resourceType := "image"
	if strings.HasPrefix(mimeType, "video/") {
		resourceType = "video"
	}

	// Upload to Cloudinary
	cloudURL := os.Getenv("CLOUDINARY_URL")
	if cloudURL == "" {
		h.Logger.Error("CLOUDINARY_URL not set")
		http.Error(w, "media storage not configured", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	url, err := h.uploadToCloudinary(ctx, cloudURL, claims.TenantID, file, resourceType)
	if err != nil {
		h.Logger.Error("failed to upload to cloudinary", zap.Error(err))
		http.Error(w, fmt.Sprintf("upload failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Parse tags
	var tagsArray []string
	if tags != "" {
		tagsArray = strings.Split(tags, ",")
		for i := range tagsArray {
			tagsArray[i] = strings.TrimSpace(tagsArray[i])
		}
	}

	// Create MediaAsset
	urlsJSON, _ := json.Marshal([]string{url})
	tagsJSON, _ := json.Marshal(tagsArray)

	// Build metadata JSON with TikTok posting options if provided
	metadata := make(map[string]interface{})
	if tiktokDisableDuet || tiktokDisableStitch || tiktokDisableComment || tiktokScheduleTime != "" {
		metadata["tiktok_disable_duet"] = tiktokDisableDuet
		metadata["tiktok_disable_stitch"] = tiktokDisableStitch
		metadata["tiktok_disable_comment"] = tiktokDisableComment
		if tiktokScheduleTime != "" {
			if scheduleTime, err := time.Parse(time.RFC3339, tiktokScheduleTime); err == nil {
				metadata["tiktok_schedule_time"] = scheduleTime.Format(time.RFC3339)
			}
		}
	}
	metadataJSON, _ := json.Marshal(metadata)

	asset := storage.MediaAsset{
		TenantID:  claims.TenantID,
		Type:      resourceType,
		Name:      name,
		URLs:      string(urlsJSON),
		Status:    "uploaded",
		Platforms: "[]",
		Tags:      string(tagsJSON),
		Campaign:  campaign,
		Metadata:  string(metadataJSON),
	}

	if err := h.DB.Create(&asset).Error; err != nil {
		h.Logger.Error("failed to create media asset", zap.Error(err))
		http.Error(w, "failed to save media", http.StatusInternalServerError)
		return
	}

	h.Logger.Info("media uploaded via API",
		zap.Uint("tenant_id", claims.TenantID),
		zap.Uint("asset_id", asset.ID),
		zap.String("type", resourceType))

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":   asset.ID,
		"url":  url,
		"type": resourceType,
		"name": asset.Name,
	})
}

// uploadToCloudinary uploads a file to Cloudinary and returns the URL
func (h *Handler) uploadToCloudinary(ctx context.Context, cloudURL string, tenantID uint, file io.Reader, resourceType string) (string, error) {
	cld, err := cloudinary.NewFromURL(cloudURL)
	if err != nil {
		return "", fmt.Errorf("cloudinary init failed: %w", err)
	}

	// Build folder structure using existing function
	folder := social.BuildCloudinaryFolder(tenantID, resourceType, "")
	if folder == "" {
		folder = fmt.Sprintf("brancr/uploads/tenant_%d/misc", tenantID)
	}

	// Generate unique publicID
	randomBytes := make([]byte, 4)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	randomStr := base64.URLEncoding.EncodeToString(randomBytes)[:6]
	timestamp := time.Now().UnixNano()
	publicID := fmt.Sprintf("%s_%d_%s", resourceType, timestamp, randomStr)

	// Upload to Cloudinary
	res, err := cld.Upload.Upload(ctx, file, uploader.UploadParams{
		PublicID:     publicID,
		Folder:       folder,
		ResourceType: resourceType,
		Timestamp:    time.Now().Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("cloudinary upload failed: %w", err)
	}

	if res.SecureURL == "" {
		return "", fmt.Errorf("cloudinary returned empty url")
	}

	h.Logger.Info("✅ Uploaded to Cloudinary via API",
		zap.Uint("tenant_id", tenantID),
		zap.String("url", res.SecureURL),
		zap.String("folder", folder),
		zap.String("resource_type", resourceType))

	return res.SecureURL, nil
}

func (h *Handler) updateMedia(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id := chi.URLParam(r, "id")
	var asset storage.MediaAsset
	if err := h.DB.Where("tenant_id = ? AND id = ?", claims.TenantID, id).First(&asset).Error; err != nil {
		http.Error(w, "media not found", http.StatusNotFound)
		return
	}
	var req struct {
		Tags    []string `json:"tags"`
		Caption *string  `json:"caption"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Tags != nil {
		if b, err := json.Marshal(req.Tags); err == nil {
			asset.Tags = string(b)
		}
	}
	if req.Caption != nil {
		asset.Caption = *req.Caption
	}
	if err := h.DB.Save(&asset).Error; err != nil {
		http.Error(w, "failed to update media", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *Handler) deleteMedia(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.DB.Where("tenant_id = ? AND id = ?", claims.TenantID, id).Delete(&storage.MediaAsset{}).Error; err != nil {
		http.Error(w, "failed to delete", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// =======================
// Post creation
// =======================
// createPost creates a new scheduled post
// @Summary      Create scheduled post
// @Description  Create a new scheduled post with media, caption, and platforms. Can be published immediately or scheduled for later.
// @Tags         posts
// @Accept       json
// @Produce      json
// @Param        request  body      object  true  "Post creation request"  example({"media_ids":[1,2,3],"caption":"Check out our new product!","platforms":["instagram","facebook"],"scheduled_at":"2025-01-15T10:00:00Z","name":"Product Launch","enhance_caption":false})
// @Security     BearerAuth
// @Success      201     {object}  map[string]interface{}  "Post created successfully"
// @Failure      400     {object}  map[string]interface{}  "Invalid request"
// @Failure      401     {object}  map[string]interface{}  "Unauthorized"
// @Failure      500     {object}  map[string]interface{}  "Internal server error"
// @Router       /tenant/posts [post]
func (h *Handler) createPost(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		h.writeUnauthorizedError(w, r, "unauthorized")
		return
	}
	var req struct {
		MediaIDs       []uint   `json:"media_ids" example:"[1,2,3]" binding:"required"`
		Caption        string   `json:"caption" example:"Check out our new product!"`
		Platforms      []string `json:"platforms" example:"[\"instagram\",\"facebook\"]" binding:"required"`
		ScheduledAt    string   `json:"scheduled_at" example:"2025-01-15T10:00:00Z"` // RFC3339
		Name           string   `json:"name" example:"Product Launch"`
		EnhanceCaption bool     `json:"enhance_caption" example:"false"` // If true, enhance existing caption with AI (Mode 3); if false, use caption as-is (Mode 2)
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.MediaIDs) == 0 {
		h.writeValidationError(w, r, "media_ids required", nil)
		return
	}
	var scheduledAt *time.Time
	publishNow := false
	if req.ScheduledAt != "" {
		// Check if "now" or empty string means publish immediately
		if strings.ToLower(req.ScheduledAt) == "now" || req.ScheduledAt == "" {
			publishNow = true
			now := time.Now()
			scheduledAt = &now
		} else {
			if t, err := time.Parse(time.RFC3339, req.ScheduledAt); err == nil {
				scheduledAt = &t
			} else {
				h.writeValidationError(w, r, "scheduled_at must be RFC3339 format or 'now'", nil)
				return
			}
		}
	} else {
		// No scheduled_at means publish now
		publishNow = true
		now := time.Now()
		scheduledAt = &now
	}

	// Validate platforms
	if len(req.Platforms) == 0 {
		h.writeValidationError(w, r, "At least one platform must be selected", nil)
		return
	}

	mediaJSON, _ := json.Marshal(req.MediaIDs)
	platsJSON, _ := json.Marshal(req.Platforms)
	post := storage.ScheduledPost{
		TenantID:       claims.TenantID,
		Name:           req.Name,
		Caption:        req.Caption,
		MediaAssetIDs:  string(mediaJSON),
		Platforms:      string(platsJSON),
		ScheduledAt:    scheduledAt,
		Status:         "scheduled",
		EnhanceCaption: req.EnhanceCaption, // Store AI enhancement preference
	}
	if err := h.DB.Create(&post).Error; err != nil {
		h.Logger.Error("failed to create post", zap.Error(err))
		h.writeInternalError(w, r, err)
		return
	}

	// Auto-update calendar if post has scheduled_at
	if post.ScheduledAt != nil {
		h.updateCalendarForPost(claims.TenantID, post.ID, *post.ScheduledAt)
	}

	// Publish immediately if requested
	if publishNow && h.Scheduler != nil {
		go func() {
			if err := h.Scheduler.ExecutePostImmediately(post.ID); err != nil {
				h.Logger.Error("failed to execute post immediately",
					zap.Uint("post_id", post.ID),
					zap.Error(err))
				// Post will be picked up by scheduler on next poll
			}
		}()
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":             post.ID,
		"status":         post.Status,
		"publishing_now": publishNow,
	})
}

// publishPost publishes a scheduled post immediately
func (h *Handler) publishPost(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	postIDStr := chi.URLParam(r, "id")
	if postIDStr == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "post id required",
		})
		return
	}

	postID, err := strconv.ParseUint(postIDStr, 10, 32)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "invalid post id",
		})
		return
	}

	// Verify post belongs to tenant
	var post storage.ScheduledPost
	if err := h.DB.Where("tenant_id = ? AND id = ?", claims.TenantID, uint(postID)).First(&post).Error; err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "post not found",
		})
		return
	}

	// Check if post can be published
	if post.Status != "scheduled" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":          "post cannot be published",
			"message":        fmt.Sprintf("Post status is '%s'. Only 'scheduled' posts can be published.", post.Status),
			"current_status": post.Status,
		})
		return
	}

	// Execute post immediately
	if h.Scheduler == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "scheduler not available",
			"message": "Post publishing service is not available",
		})
		return
	}

	// Execute in background
	go func() {
		if err := h.Scheduler.ExecutePostImmediately(uint(postID)); err != nil {
			h.Logger.Error("failed to execute post immediately",
				zap.Uint("post_id", uint(postID)),
				zap.Uint("tenant_id", claims.TenantID),
				zap.Error(err))
		}
	}()

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Post is being published now",
		"post_id": postID,
		"status":  "posting",
	})
}

func (h *Handler) generateCaption(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := middleware.TenantFromContext(ctx)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		MediaID         *uint  `json:"media_id,omitempty"`         // Optional: to determine media type
		Platform        string `json:"platform"`                   // Required: instagram, facebook, tiktok, youtube, whatsapp
		MediaType       string `json:"media_type,omitempty"`       // Optional: image, video, carousel (if not provided, inferred from media_id)
		Context         string `json:"context,omitempty"`          // Optional: description/hint for caption generation
		OriginalCaption string `json:"original_caption,omitempty"` // Optional: existing caption to optimize
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.Logger.Error("failed to decode generate caption request", zap.Error(err))
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Set defaults
	if req.Platform == "" {
		req.Platform = "instagram"
	}
	req.Platform = strings.ToLower(req.Platform)

	// Determine media type and extract image URLs - either from request, or from media asset
	mediaType := req.MediaType
	var imageURLs []string

	if req.MediaID != nil {
		var asset storage.MediaAsset
		if err := h.DB.Where("id = ? AND tenant_id = ?", *req.MediaID, claims.TenantID).First(&asset).Error; err == nil {
			if mediaType == "" {
				mediaType = asset.Type
			}
			// Extract image URLs from media asset
			if asset.URLs != "" {
				var urls []string
				if err := json.Unmarshal([]byte(asset.URLs), &urls); err == nil {
					imageURLs = urls
				}
			}
		}
	}
	if mediaType == "" {
		mediaType = "image" // Default fallback
	}

	// Create caption optimizer
	if h.AIClient == nil {
		h.Logger.Error("AI client not available for caption generation")
		http.Error(w, "AI service unavailable", http.StatusServiceUnavailable)
		return
	}

	optimizer := &ai.CaptionOptimizer{
		Client: h.AIClient,
		DB:     h.DB,
	}

	// Use context as original caption hint if provided, otherwise use original caption field
	originalCaption := req.OriginalCaption
	if originalCaption == "" && req.Context != "" {
		originalCaption = req.Context
	}

	// Generate caption using AI with image analysis
	aiReq := ai.OptimizeCaptionRequest{
		OriginalCaption: originalCaption,
		Platform:        req.Platform,
		MediaType:       mediaType,
		TenantID:        claims.TenantID,
		ImageURLs:       imageURLs, // Pass image URLs for analysis
	}

	result, err := optimizer.OptimizeCaption(ctx, aiReq)
	if err != nil {
		h.Logger.Error("failed to generate caption", zap.Error(err), zap.Uint("tenant_id", claims.TenantID))
		http.Error(w, fmt.Sprintf("failed to generate caption: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"caption":    result.OptimizedCaption,
		"hashtags":   result.Hashtags,
		"platform":   req.Platform,
		"media_type": mediaType,
	})
}

func (h *Handler) getOptimalTimes(w http.ResponseWriter, r *http.Request) {
	// Basic placeholder with next few hours
	now := time.Now().UTC().Truncate(time.Hour)
	result := []map[string]any{
		{"at": now.Add(6 * time.Hour), "score": 0.78},
		{"at": now.Add(24 * time.Hour).Add(10 * time.Hour), "score": 0.82},
		{"at": now.Add(48 * time.Hour).Add(9 * time.Hour), "score": 0.80},
	}
	writeJSON(w, http.StatusOK, map[string]any{"times": result})
}

// =======================
// Bulk uploads (basic)
// =======================
func (h *Handler) listBulkUploads(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var sessions []storage.BulkUploadSession
	if err := h.DB.Where("tenant_id = ?", claims.TenantID).Order("created_at DESC").Limit(50).Find(&sessions).Error; err != nil {
		http.Error(w, "failed to list bulk uploads", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": sessions})
}

func (h *Handler) getBulkUpload(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id := chi.URLParam(r, "id")
	var sess storage.BulkUploadSession
	if err := h.DB.Where("tenant_id = ? AND id = ?", claims.TenantID, id).First(&sess).Error; err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (h *Handler) createBulkUpload(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":       "not_implemented",
		"message":     "Bulk upload creation via dashboard not implemented yet",
		"coming_soon": true,
		"feature":     "bulk_uploads",
	})
}

func (h *Handler) updateBulkUploadStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":       "not_implemented",
		"message":     "Pause/Resume not implemented yet",
		"coming_soon": true,
		"feature":     "bulk_uploads",
	})
}

func (h *Handler) cancelBulkUpload(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":       "not_implemented",
		"message":     "Cancel not implemented yet",
		"coming_soon": true,
		"feature":     "bulk_uploads",
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// =======================
// TEMPLATES API
// =======================

func (h *Handler) listTemplates(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var templates []storage.RecurringTemplate
	if err := h.DB.Where("tenant_id = ?", claims.TenantID).
		Order("created_at DESC").
		Find(&templates).Error; err != nil {
		h.Logger.Error("fetch templates", zap.Error(err))
		http.Error(w, "could not fetch templates", http.StatusInternalServerError)
		return
	}

	type templateResp struct {
		ID              uint       `json:"id"`
		Name            string     `json:"name"`
		Frequency       string     `json:"frequency"`
		Schedule        string     `json:"schedule"`
		MediaTemplate   string     `json:"media_template"`
		CaptionTemplate string     `json:"caption_template"`
		Platforms       []string   `json:"platforms"`
		Active          bool       `json:"active"`
		NextRun         *time.Time `json:"next_run"`
		LastRun         *time.Time `json:"last_run"`
		CreatedAt       time.Time  `json:"created_at"`
		UpdatedAt       time.Time  `json:"updated_at"`
	}

	resp := make([]templateResp, 0, len(templates))
	for _, t := range templates {
		var platforms []string
		if strings.TrimSpace(t.Platforms) != "" {
			_ = json.Unmarshal([]byte(t.Platforms), &platforms)
		}

		resp = append(resp, templateResp{
			ID:              t.ID,
			Name:            t.Name,
			Frequency:       t.Frequency,
			Schedule:        t.Schedule,
			MediaTemplate:   t.MediaTemplate,
			CaptionTemplate: t.CaptionTemplate,
			Platforms:       platforms,
			Active:          t.Active,
			NextRun:         t.NextRun,
			LastRun:         t.LastRun,
			CreatedAt:       t.CreatedAt,
			UpdatedAt:       t.UpdatedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"templates": resp,
	})
}

func (h *Handler) createTemplate(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Name            string   `json:"name"`
		Frequency       string   `json:"frequency"`
		Schedule        string   `json:"schedule"`
		MediaTemplate   string   `json:"media_template"`
		CaptionTemplate string   `json:"caption_template"`
		Platforms       []string `json:"platforms"`
		Active          bool     `json:"active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	platformsJSON, _ := json.Marshal(req.Platforms)
	template := storage.RecurringTemplate{
		TenantID:        claims.TenantID,
		Name:            req.Name,
		Frequency:       req.Frequency,
		Schedule:        req.Schedule,
		MediaTemplate:   req.MediaTemplate,
		CaptionTemplate: req.CaptionTemplate,
		Platforms:       string(platformsJSON),
		Active:          req.Active,
	}

	if err := h.DB.Create(&template).Error; err != nil {
		h.Logger.Error("failed to create template", zap.Error(err))
		http.Error(w, "failed to create template", http.StatusInternalServerError)
		return
	}

	var platforms []string
	_ = json.Unmarshal([]byte(template.Platforms), &platforms)

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":               template.ID,
		"name":             template.Name,
		"frequency":        template.Frequency,
		"schedule":         template.Schedule,
		"media_template":   template.MediaTemplate,
		"caption_template": template.CaptionTemplate,
		"platforms":        platforms,
		"active":           template.Active,
		"created_at":       template.CreatedAt,
		"updated_at":       template.UpdatedAt,
	})
}

func (h *Handler) getTemplate(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	templateID := chi.URLParam(r, "id")
	if templateID == "" {
		http.Error(w, "template id required", http.StatusBadRequest)
		return
	}

	var template storage.RecurringTemplate
	if err := h.DB.Where("tenant_id = ? AND id = ?", claims.TenantID, templateID).
		First(&template).Error; err != nil {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	}

	var platforms []string
	if strings.TrimSpace(template.Platforms) != "" {
		_ = json.Unmarshal([]byte(template.Platforms), &platforms)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":               template.ID,
		"name":             template.Name,
		"frequency":        template.Frequency,
		"schedule":         template.Schedule,
		"media_template":   template.MediaTemplate,
		"caption_template": template.CaptionTemplate,
		"platforms":        platforms,
		"active":           template.Active,
		"next_run":         template.NextRun,
		"last_run":         template.LastRun,
		"created_at":       template.CreatedAt,
		"updated_at":       template.UpdatedAt,
	})
}

func (h *Handler) updateTemplate(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	templateID := chi.URLParam(r, "id")
	if templateID == "" {
		http.Error(w, "template id required", http.StatusBadRequest)
		return
	}

	var template storage.RecurringTemplate
	if err := h.DB.Where("tenant_id = ? AND id = ?", claims.TenantID, templateID).
		First(&template).Error; err != nil {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	}

	var req struct {
		Name            *string   `json:"name"`
		Frequency       *string   `json:"frequency"`
		Schedule        *string   `json:"schedule"`
		MediaTemplate   *string   `json:"media_template"`
		CaptionTemplate *string   `json:"caption_template"`
		Platforms       *[]string `json:"platforms"`
		Active          *bool     `json:"active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name != nil {
		template.Name = *req.Name
	}
	if req.Frequency != nil {
		template.Frequency = *req.Frequency
	}
	if req.Schedule != nil {
		template.Schedule = *req.Schedule
	}
	if req.MediaTemplate != nil {
		template.MediaTemplate = *req.MediaTemplate
	}
	if req.CaptionTemplate != nil {
		template.CaptionTemplate = *req.CaptionTemplate
	}
	if req.Platforms != nil {
		platformsJSON, _ := json.Marshal(*req.Platforms)
		template.Platforms = string(platformsJSON)
	}
	if req.Active != nil {
		template.Active = *req.Active
	}

	if err := h.DB.Save(&template).Error; err != nil {
		h.Logger.Error("failed to update template", zap.Error(err))
		http.Error(w, "failed to update template", http.StatusInternalServerError)
		return
	}

	var platforms []string
	_ = json.Unmarshal([]byte(template.Platforms), &platforms)

	writeJSON(w, http.StatusOK, map[string]any{
		"id":               template.ID,
		"name":             template.Name,
		"frequency":        template.Frequency,
		"schedule":         template.Schedule,
		"media_template":   template.MediaTemplate,
		"caption_template": template.CaptionTemplate,
		"platforms":        platforms,
		"active":           template.Active,
		"created_at":       template.CreatedAt,
		"updated_at":       template.UpdatedAt,
	})
}

func (h *Handler) deleteTemplate(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	templateID := chi.URLParam(r, "id")
	if templateID == "" {
		http.Error(w, "template id required", http.StatusBadRequest)
		return
	}

	var template storage.RecurringTemplate
	if err := h.DB.Where("tenant_id = ? AND id = ?", claims.TenantID, templateID).
		First(&template).Error; err != nil {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	}

	if err := h.DB.Delete(&template).Error; err != nil {
		h.Logger.Error("failed to delete template", zap.Error(err))
		http.Error(w, "failed to delete template", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "template deleted successfully",
	})
}

// =======================
// CONVERSATIONS API
// =======================

func (h *Handler) listConversations(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	userID := claims.TenantID

	// Query parameters for filtering
	platform := r.URL.Query().Get("platform")
	status := r.URL.Query().Get("status")
	search := r.URL.Query().Get("search")

	query := h.DB.Where("tenant_id = ?", claims.TenantID)

	if platform != "" {
		query = query.Where("platform = ?", platform)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if search != "" {
		// Search in interactions content
		query = query.Joins("JOIN interactions ON interactions.conversation_id = conversations.id").
			Where("interactions.content ILIKE ?", "%"+search+"%")
	}

	var conversations []storage.Conversation
	if err := query.Order("last_message_at DESC").Limit(100).Find(&conversations).Error; err != nil {
		h.Logger.Error("fetch conversations", zap.Error(err))
		http.Error(w, "could not fetch conversations", http.StatusInternalServerError)
		return
	}

	// Prefetch per-user conversation states to avoid repeated lookups
	stateByConversation := make(map[uint]storage.ConversationUserState)
	if userID != 0 && len(conversations) > 0 {
		conversationIDs := make([]uint, 0, len(conversations))
		for _, conv := range conversations {
			conversationIDs = append(conversationIDs, conv.ID)
		}

		var states []storage.ConversationUserState
		if err := h.DB.Where("tenant_id = ? AND user_id = ? AND conversation_id IN (?)", claims.TenantID, userID, conversationIDs).
			Find(&states).Error; err != nil {
			h.Logger.Warn("prefetch conversation states failed", zap.Error(err))
		} else {
			for _, state := range states {
				stateByConversation[state.ConversationID] = state
			}
		}
	}

	// Get customer info and last message for each conversation
	type conversationResp struct {
		ID                  uint              `json:"id"`
		CustomerID          uint              `json:"customer_id"`
		CustomerName        string            `json:"customer_name"`
		CustomerAvatar      string            `json:"customer_avatar"`
		CustomerPhone       string            `json:"customer_phone,omitempty"`        // Phone number (for WhatsApp)
		CustomerCountryCode string            `json:"customer_country_code,omitempty"` // Country code (e.g., "234")
		CustomerDialCode    string            `json:"customer_dial_code,omitempty"`    // Dial code (e.g., "8137369592")
		Platform            string            `json:"platform"`
		Status              string            `json:"status"`
		LastMessage         string            `json:"last_message"`
		LastMessageAt       time.Time         `json:"last_message_at"`
		UnreadCount         int               `json:"unread_count"`
		CreatedAt           time.Time         `json:"created_at"`
		UpdatedAt           time.Time         `json:"updated_at"`
		LastMessageMedia    *InteractionMedia `json:"last_message_media,omitempty"` // Media preview for last message
	}

	resp := make([]conversationResp, 0, len(conversations))
	for _, conv := range conversations {
		var customer storage.Customer
		h.DB.Where("id = ?", conv.CustomerID).First(&customer)

		// Extract customer phone, country_code, and dial_code from first interaction metadata
		var customerPhone, countryCode, dialCode string
		if conv.Platform == "whatsapp" {
			// For WhatsApp, phone is stored in PlatformID
			customerPhone = customer.PlatformID

			// Try to get country_code and dial_code from first interaction metadata
			var firstInteraction storage.Interaction
			if err := h.DB.Where("conversation_id = ? AND direction = ?", conv.ID, "incoming").
				Order("created_at ASC").
				First(&firstInteraction).Error; err == nil {
				if firstInteraction.Metadata != "" && firstInteraction.Metadata != "{}" {
					var metadata map[string]interface{}
					if err := json.Unmarshal([]byte(firstInteraction.Metadata), &metadata); err == nil {
						if cc, ok := metadata["customer_country_code"].(string); ok {
							countryCode = cc
						}
						if dc, ok := metadata["customer_dial_code"].(string); ok {
							dialCode = dc
						}
						// Also check for phone in metadata if PlatformID is empty
						if customerPhone == "" {
							if cp, ok := metadata["customer_phone"].(string); ok {
								customerPhone = cp
							}
						}
					}
				}
			}
		}

		// Compute unread count using per-user state; fallback to legacy logic if user is not identified
		var unreadCount int64
		if userID != 0 {
			if state, ok := stateByConversation[conv.ID]; ok {
				countQuery := h.DB.Model(&storage.Interaction{}).
					Where("conversation_id = ? AND direction = ?", conv.ID, "incoming")
				if state.LastReadAt != nil {
					countQuery = countQuery.Where("created_at > ?", *state.LastReadAt)
				}

				if err := countQuery.Count(&unreadCount).Error; err != nil {
					h.Logger.Warn("unread count lookup failed", zap.Error(err), zap.Uint("conversation_id", conv.ID))
					unreadCount = int64(state.UnreadCount)
				} else if int(unreadCount) != state.UnreadCount {
					h.DB.Model(&state).Update("unread_count", int(unreadCount))
				}
			} else {
				if err := h.DB.Model(&storage.Interaction{}).
					Where("conversation_id = ? AND direction = ?", conv.ID, "incoming").
					Count(&unreadCount).Error; err != nil {
					h.Logger.Warn("unread count init failed", zap.Error(err), zap.Uint("conversation_id", conv.ID))
					unreadCount = 0
				} else {
					state := storage.ConversationUserState{
						TenantID:       claims.TenantID,
						ConversationID: conv.ID,
						UserID:         userID,
						UnreadCount:    int(unreadCount),
					}
					if err := h.DB.Create(&state).Error; err != nil {
						h.Logger.Warn("create conversation state failed", zap.Error(err), zap.Uint("conversation_id", conv.ID))
					}
				}
			}
		} else {
			h.DB.Model(&storage.Interaction{}).
				Where("conversation_id = ? AND direction = ? AND response_status != ?", conv.ID, "incoming", "sent").
				Count(&unreadCount)
		}

		// Get last interaction (from ANY party - customer or tenant)
		var lastInteraction storage.Interaction
		if err := h.DB.Where("conversation_id = ?", conv.ID).
			Order("created_at DESC").
			First(&lastInteraction).Error; err == nil {
			// Interaction found - determine display text based on direction
			lastMessageText := lastInteraction.Content
			if lastInteraction.Direction == "outgoing" {
				// For outgoing messages (from tenant/AI), use FinalReply if available
				if lastInteraction.FinalReply != "" {
					lastMessageText = lastInteraction.FinalReply
				} else if lastInteraction.Content != "" {
					lastMessageText = lastInteraction.Content
				}
			}
			// For incoming messages, Content is already correct

			// Extract media information from last interaction
			lastMessageMedia := parseMediaFromMetadata(lastInteraction.Metadata)

			resp = append(resp, conversationResp{
				ID:                  conv.ID,
				CustomerID:          conv.CustomerID,
				CustomerName:        customer.DisplayName,
				CustomerAvatar:      customer.ProfilePicURL,
				CustomerPhone:       customerPhone,
				CustomerCountryCode: countryCode,
				CustomerDialCode:    dialCode,
				Platform:            conv.Platform,
				Status:              conv.Status,
				LastMessage:         lastMessageText, // Show message from any party
				LastMessageAt:       conv.LastMessageAt,
				UnreadCount:         int(unreadCount),
				CreatedAt:           conv.CreatedAt,
				UpdatedAt:           conv.UpdatedAt,
				LastMessageMedia:    lastMessageMedia, // Include media preview if present
			})
		} else {
			// No interactions yet - still create conversation entry
			resp = append(resp, conversationResp{
				ID:                  conv.ID,
				CustomerID:          conv.CustomerID,
				CustomerName:        customer.DisplayName,
				CustomerAvatar:      customer.ProfilePicURL,
				CustomerPhone:       customerPhone,
				CustomerCountryCode: countryCode,
				CustomerDialCode:    dialCode,
				Platform:            conv.Platform,
				Status:              conv.Status,
				LastMessage:         "",
				LastMessageAt:       conv.LastMessageAt,
				UnreadCount:         int(unreadCount),
				CreatedAt:           conv.CreatedAt,
				UpdatedAt:           conv.UpdatedAt,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"conversations": resp,
	})
}

func (h *Handler) getConversation(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conversationID := chi.URLParam(r, "id")
	if conversationID == "" {
		http.Error(w, "conversation id required", http.StatusBadRequest)
		return
	}

	var conversation storage.Conversation
	if err := h.DB.Where("tenant_id = ? AND id = ?", claims.TenantID, conversationID).
		First(&conversation).Error; err != nil {
		http.Error(w, "conversation not found", http.StatusNotFound)
		return
	}

	// Get customer info
	var customer storage.Customer
	h.DB.Where("id = ?", conversation.CustomerID).First(&customer)

	// Extract customer phone, country_code, and dial_code from first interaction metadata
	var customerPhone, countryCode, dialCode string
	if conversation.Platform == "whatsapp" {
		// For WhatsApp, phone is stored in PlatformID
		customerPhone = customer.PlatformID

		// Try to get country_code and dial_code from first interaction metadata
		var firstInteraction storage.Interaction
		if err := h.DB.Where("conversation_id = ? AND direction = ?", conversation.ID, "incoming").
			Order("created_at ASC").
			First(&firstInteraction).Error; err == nil {
			if firstInteraction.Metadata != "" && firstInteraction.Metadata != "{}" {
				var metadata map[string]interface{}
				if err := json.Unmarshal([]byte(firstInteraction.Metadata), &metadata); err == nil {
					if cc, ok := metadata["customer_country_code"].(string); ok {
						countryCode = cc
					}
					if dc, ok := metadata["customer_dial_code"].(string); ok {
						dialCode = dc
					}
					// Also check for phone in metadata if PlatformID is empty
					if customerPhone == "" {
						if cp, ok := metadata["customer_phone"].(string); ok {
							customerPhone = cp
						}
					}
				}
			}
		}
	}

	// Get all interactions from ALL parties in the conversation
	// This includes:
	// - Incoming messages from customers (direction='incoming')
	// - Outgoing messages from tenant/AI (direction='outgoing')
	// No filtering by direction - we want to show the complete conversation history from all parties
	var interactions []storage.Interaction

	// Use explicit query to ensure we get ALL interactions
	query := h.DB.Model(&storage.Interaction{}).Where("conversation_id = ?", conversation.ID)
	if err := query.Order("created_at ASC").Find(&interactions).Error; err != nil {
		h.Logger.Error("failed to fetch interactions",
			zap.Error(err),
			zap.Uint("conversation_id", conversation.ID))
		// Continue with empty messages array
		interactions = []storage.Interaction{}
	}

	// DEBUG: Also query separately for outgoing to verify they exist
	var outgoingInteractions []storage.Interaction
	h.DB.Where("conversation_id = ? AND direction = ?", conversation.ID, "outgoing").
		Order("created_at ASC").
		Find(&outgoingInteractions)

	if len(outgoingInteractions) > 0 {
		h.Logger.Info("🔍 DEBUG: Found outgoing interactions separately",
			zap.Uint("conversation_id", conversation.ID),
			zap.Int("outgoing_count", len(outgoingInteractions)),
			zap.Int("total_in_main_query", len(interactions)))

		// Check if outgoing interactions are in main query
		outgoingIDs := make(map[uint]bool)
		for _, out := range outgoingInteractions {
			outgoingIDs[out.ID] = true
		}

		inMainQueryCount := 0
		for _, inter := range interactions {
			if outgoingIDs[inter.ID] {
				inMainQueryCount++
			}
		}

		if inMainQueryCount != len(outgoingInteractions) {
			h.Logger.Warn("⚠️ Some outgoing interactions are missing from main query - adding them manually",
				zap.Int("expected_outgoing", len(outgoingInteractions)),
				zap.Int("found_in_main", inMainQueryCount))

			// Add missing outgoing interactions to the list
			existingIDs := make(map[uint]bool)
			for _, inter := range interactions {
				existingIDs[inter.ID] = true
			}

			for _, out := range outgoingInteractions {
				if !existingIDs[out.ID] {
					interactions = append(interactions, out)
					h.Logger.Info("➕ Added missing outgoing interaction to response",
						zap.Uint("interaction_id", out.ID),
						zap.String("content_preview", func() string {
							preview := out.FinalReply
							if preview == "" {
								preview = out.Content
							}
							if len(preview) > 50 {
								return preview[:50] + "..."
							}
							return preview
						}()))
				}
			}

			// Re-sort by created_at after adding
			sort.Slice(interactions, func(i, j int) bool {
				return interactions[i].CreatedAt.Before(interactions[j].CreatedAt)
			})
		}
	}

	// Verify we're getting all interactions - double check with a separate query for outgoing
	var outgoingCheck []storage.Interaction
	h.DB.Where("conversation_id = ? AND direction = ?", conversation.ID, "outgoing").
		Find(&outgoingCheck)
	if len(outgoingCheck) > 0 && len(interactions) > 0 {
		// Count outgoing in main query
		outgoingInMain := 0
		for _, i := range interactions {
			if i.Direction == "outgoing" {
				outgoingInMain++
			}
		}
		if outgoingInMain != len(outgoingCheck) {
			h.Logger.Warn("outgoing interaction count mismatch",
				zap.Uint("conversation_id", conversation.ID),
				zap.Int("outgoing_in_main_query", outgoingInMain),
				zap.Int("outgoing_in_separate_query", len(outgoingCheck)))
		}
	}

	// Log for debugging - count incoming vs outgoing
	incomingCount := 0
	outgoingCount := 0
	for _, i := range interactions {
		if i.Direction == "incoming" {
			incomingCount++
		} else if i.Direction == "outgoing" {
			outgoingCount++
		}
	}
	h.Logger.Info("fetched conversation interactions",
		zap.Uint("conversation_id", conversation.ID),
		zap.Int("total", len(interactions)),
		zap.Int("incoming", incomingCount),
		zap.Int("outgoing", outgoingCount))

	type interactionResp struct {
		ID             uint              `json:"id"`
		Direction      string            `json:"direction"`
		MessageType    string            `json:"message_type"`
		Content        string            `json:"content"`
		DetectedIntent string            `json:"detected_intent"`
		DetectedTone   string            `json:"detected_tone"`
		ResponseType   string            `json:"response_type"`
		ResponseStatus string            `json:"response_status"`
		SuggestedReply string            `json:"suggested_reply"`
		FinalReply     string            `json:"final_reply"`
		CreatedAt      time.Time         `json:"created_at"`
		Media          *InteractionMedia `json:"media,omitempty"` // Media information (if message has media)
	}

	messages := make([]interactionResp, 0, len(interactions))
	for _, i := range interactions {
		// Determine display content: for outgoing messages, use final_reply (what was sent)
		// For incoming messages, use content (what customer sent)
		displayContent := i.Content
		if i.Direction == "outgoing" {
			// Outgoing messages: prefer final_reply, fallback to content
			if i.FinalReply != "" {
				displayContent = i.FinalReply
			} else if i.Content != "" {
				displayContent = i.Content
			}
		}
		// For incoming messages, displayContent already equals i.Content, which is correct

		// Extract media information from metadata
		media := parseMediaFromMetadata(i.Metadata)

		// Log each message being added (for debugging)
		if i.Direction == "outgoing" {
			h.Logger.Info("including outgoing message in response",
				zap.Uint("interaction_id", i.ID),
				zap.String("content_preview", func() string {
					preview := displayContent
					if len(preview) > 50 {
						return preview[:50] + "..."
					}
					return preview
				}()))
		}

		messages = append(messages, interactionResp{
			ID:             i.ID,
			Direction:      i.Direction,
			MessageType:    i.MessageType,
			Content:        displayContent, // Display text for the message
			DetectedIntent: i.DetectedIntent,
			DetectedTone:   i.DetectedTone,
			ResponseType:   i.ResponseType,
			ResponseStatus: i.ResponseStatus,
			SuggestedReply: i.SuggestedReply,
			FinalReply:     i.FinalReply, // Always include final_reply for reference
			CreatedAt:      i.CreatedAt,
			Media:          media, // Include media information if present
		})
	}

	// Log final message count breakdown
	outgoingInResponse := 0
	incomingInResponse := 0
	for _, msg := range messages {
		if msg.Direction == "outgoing" {
			outgoingInResponse++
		} else if msg.Direction == "incoming" {
			incomingInResponse++
		}
	}
	h.Logger.Info("conversation response prepared",
		zap.Uint("conversation_id", conversation.ID),
		zap.Int("total_messages", len(messages)),
		zap.Int("incoming_messages", incomingInResponse),
		zap.Int("outgoing_messages", outgoingInResponse))

	// Ensure we're returning ALL messages - log if any are missing
	if len(messages) != len(interactions) {
		h.Logger.Warn("message count mismatch",
			zap.Int("interactions_fetched", len(interactions)),
			zap.Int("messages_in_response", len(messages)))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":                    conversation.ID,
		"customer_id":           conversation.CustomerID,
		"customer_name":         customer.DisplayName,
		"customer_avatar":       customer.ProfilePicURL,
		"customer_phone":        customerPhone,
		"customer_country_code": countryCode,
		"customer_dial_code":    dialCode,
		"platform":              conversation.Platform,
		"status":                conversation.Status,
		"messages":              messages, // This includes ALL messages (incoming + outgoing)
		"created_at":            conversation.CreatedAt,
		"updated_at":            conversation.UpdatedAt,
	})
}

func (h *Handler) sendReply(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conversationID := chi.URLParam(r, "id")
	if conversationID == "" {
		http.Error(w, "conversation id required", http.StatusBadRequest)
		return
	}

	var req struct {
		Message string `json:"message"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	var conversation storage.Conversation
	if err := h.DB.Where("tenant_id = ? AND id = ?", claims.TenantID, conversationID).
		First(&conversation).Error; err != nil {
		http.Error(w, "conversation not found", http.StatusNotFound)
		return
	}

	// Get customer
	var customer storage.Customer
	if err := h.DB.Where("id = ?", conversation.CustomerID).First(&customer).Error; err != nil {
		http.Error(w, "customer not found", http.StatusNotFound)
		return
	}

	ctx := r.Context()
	var err error

	// Handle Telegram differently - it uses bot directly, not SocialAccount
	if conversation.Platform == "telegram" {
		// Get customer's Telegram chat ID from conversation or customer metadata
		var chatID int64
		threadID := conversation.PlatformThreadID
		if threadID != "" {
			// Parse threadID as chat ID
			if parsed, err := strconv.ParseInt(threadID, 10, 64); err == nil {
				chatID = parsed
			}
		}

		if chatID == 0 {
			// Try to get from customer metadata in last interaction
			var lastInteraction storage.Interaction
			if h.DB.Where("conversation_id = ? AND direction = ?", conversation.ID, "incoming").
				Order("created_at DESC").First(&lastInteraction).Error == nil {
				if metadataStr := lastInteraction.Metadata; metadataStr != "" {
					var metadata map[string]interface{}
					if json.Unmarshal([]byte(metadataStr), &metadata) == nil {
						if id, ok := metadata["telegram_chat_id"].(float64); ok {
							chatID = int64(id)
						}
					}
				}
			}
		}

		if chatID == 0 {
			// Use customer PlatformID as fallback (Telegram user ID stored there)
			if parsed, err := strconv.ParseInt(customer.PlatformID, 10, 64); err == nil {
				chatID = parsed
			}
		}

		if chatID == 0 {
			http.Error(w, "could not determine Telegram chat ID for customer", http.StatusBadRequest)
			return
		}

		// Use MessageHandler to send Telegram reply (it has access to bot)
		if h.MessageHandler == nil {
			http.Error(w, "message handler not available", http.StatusInternalServerError)
			return
		}

		// Use MessageHandler's SendReply method (public method with different signature)
		err = h.MessageHandler.SendReply(ctx, claims.TenantID, conversation.Platform, fmt.Sprintf("%d", chatID), customer.PlatformID, req.Message)
		if err != nil {
			h.Logger.Error("failed to send telegram reply", zap.Error(err))
			http.Error(w, fmt.Sprintf("failed to send reply: %v", err), http.StatusInternalServerError)
			return
		}
	} else {
		// For other platforms, use SocialAccount
		var account storage.SocialAccount
		if err := h.DB.Where("tenant_id = ? AND platform = ? AND connected = ?", claims.TenantID, conversation.Platform, true).
			First(&account).Error; err != nil {
			http.Error(w, fmt.Sprintf("%s account not connected", conversation.Platform), http.StatusBadRequest)
			return
		}

		// Check 24-hour window for WhatsApp
		// WhatsApp only allows free-form (session) messages within 24 hours of customer's last message
		// After 24 hours, you must use pre-approved template messages
		if conversation.Platform == "whatsapp" {
			// Find the last incoming message from the customer
			var lastIncoming storage.Interaction
			if err := h.DB.Where("conversation_id = ? AND direction = ?", conversation.ID, "incoming").
				Order("created_at DESC").First(&lastIncoming).Error; err == nil {
				// Check if last message was within 24 hours
				timeSinceLastMessage := time.Since(lastIncoming.CreatedAt)
				if timeSinceLastMessage > 24*time.Hour {
					h.Logger.Warn("Cannot send message - outside 24-hour window",
						zap.String("platform", conversation.Platform),
						zap.Uint("conversation_id", conversation.ID),
						zap.Duration("time_since_last_message", timeSinceLastMessage))

					writeJSON(w, http.StatusBadRequest, map[string]any{
						"error": "outside_24_hour_window",
						"message": fmt.Sprintf("Cannot send message: Customer's last message was %v ago. %s only allows free-form messages within 24 hours of the customer's last message. Please use a template message or wait for the customer to message again.",
							formatDuration(timeSinceLastMessage), strings.Title(conversation.Platform)),
						"time_since_last_message": timeSinceLastMessage.String(),
					})
					return
				}
			} else {
				// No incoming messages found - check conversation LastMessageAt as fallback
				if !conversation.LastMessageAt.IsZero() {
					timeSinceLastMessage := time.Since(conversation.LastMessageAt)
					if timeSinceLastMessage > 24*time.Hour {
						h.Logger.Warn("Cannot send message - outside 24-hour window (using conversation LastMessageAt)",
							zap.String("platform", conversation.Platform),
							zap.Uint("conversation_id", conversation.ID),
							zap.Duration("time_since_last_message", timeSinceLastMessage))

						writeJSON(w, http.StatusBadRequest, map[string]any{
							"error": "outside_24_hour_window",
							"message": fmt.Sprintf("Cannot send message: Last activity was %v ago. %s only allows free-form messages within 24 hours of the customer's last message.",
								formatDuration(timeSinceLastMessage), strings.Title(conversation.Platform)),
							"time_since_last_message": timeSinceLastMessage.String(),
						})
						return
					}
				}
			}
		}

		// Use platform-specific reply sending
		err = h.sendPlatformReply(ctx, conversation.Platform, &account, &customer, req.Message)
		if err != nil {
			h.Logger.Error("failed to send reply", zap.Error(err))
			http.Error(w, fmt.Sprintf("failed to send reply: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// Reload conversation to ensure we have the latest data (conversation might have been updated)
	// This is critical to ensure conversation_id is correct
	var refreshedConversation storage.Conversation
	if err := h.DB.Where("tenant_id = ? AND id = ?", claims.TenantID, conversation.ID).
		First(&refreshedConversation).Error; err != nil {
		h.Logger.Error("conversation disappeared after sending message - this should not happen",
			zap.Uint("conversation_id", conversation.ID),
			zap.Uint("tenant_id", claims.TenantID),
			zap.Error(err))
		http.Error(w, "conversation not found", http.StatusNotFound)
		return
	}
	conversation = refreshedConversation

	// Verify conversation ID is valid (non-zero)
	if conversation.ID == 0 {
		h.Logger.Error("⚠️ CRITICAL: Conversation ID is 0 after reload",
			zap.Uint("tenant_id", claims.TenantID),
			zap.Uint("customer_id", conversation.CustomerID),
			zap.String("platform", conversation.Platform))
		http.Error(w, "invalid conversation state", http.StatusInternalServerError)
		return
	}

	// Create interaction record - CRITICAL: Must have valid conversation_id
	interaction := storage.Interaction{
		TenantID:       claims.TenantID,
		CustomerID:     conversation.CustomerID,
		ConversationID: conversation.ID, // MUST be non-zero for message to appear in conversation
		Platform:       conversation.Platform,
		Direction:      "outgoing",
		MessageType:    "text",
		Content:        req.Message,
		ResponseType:   "manual",
		ResponseStatus: "sent",
		FinalReply:     req.Message,
		Metadata:       "{}", // Initialize as empty JSON object to prevent "invalid input syntax for type json" error
	}

	if err := h.DB.Create(&interaction).Error; err != nil {
		h.Logger.Error("failed to create interaction",
			zap.Error(err),
			zap.Uint("conversation_id", conversation.ID),
			zap.Uint("tenant_id", claims.TenantID),
			zap.Uint("customer_id", conversation.CustomerID),
			zap.String("message_preview", func() string {
				if len(req.Message) > 50 {
					return req.Message[:50] + "..."
				}
				return req.Message
			}()))
		http.Error(w, "failed to save interaction", http.StatusInternalServerError)
		return
	}

	// CRITICAL: Verify interaction was created with correct conversation_id
	// Reload interaction to ensure we have the database-generated ID and all fields
	var reloadedInteraction storage.Interaction
	if err := h.DB.First(&reloadedInteraction, interaction.ID).Error; err != nil {
		h.Logger.Error("failed to reload interaction after creation", zap.Error(err))
		// Continue with original interaction
		reloadedInteraction = interaction
	} else {
		interaction = reloadedInteraction
	}

	// Double-check conversation_id is correct
	if interaction.ConversationID == 0 || interaction.ConversationID != conversation.ID {
		h.Logger.Error("⚠️ CRITICAL: Interaction has wrong conversation_id, fixing now",
			zap.Uint("interaction_id", interaction.ID),
			zap.Uint("current_conversation_id", interaction.ConversationID),
			zap.Uint("expected_conversation_id", conversation.ID))

		// Fix it immediately
		interaction.ConversationID = conversation.ID
		if err := h.DB.Model(&storage.Interaction{}).Where("id = ?", interaction.ID).
			Update("conversation_id", conversation.ID).Error; err != nil {
			h.Logger.Error("failed to fix interaction conversation_id", zap.Error(err))
		} else {
			h.Logger.Info("✅ Fixed interaction conversation_id",
				zap.Uint("interaction_id", interaction.ID),
				zap.Uint("conversation_id", conversation.ID))
			// Reload to get fixed version
			h.DB.First(&interaction, interaction.ID)
		}
	}

	// Log final state for debugging
	h.Logger.Info("✅ Outgoing interaction created and verified",
		zap.Uint("interaction_id", interaction.ID),
		zap.Uint("conversation_id", interaction.ConversationID),
		zap.Uint("tenant_id", claims.TenantID),
		zap.String("direction", interaction.Direction),
		zap.String("content_preview", func() string {
			preview := req.Message
			if len(preview) > 50 {
				return preview[:50] + "..."
			}
			return preview
		}()))

	// Verify the interaction was created with correct values
	msgPreview := req.Message
	if len(msgPreview) > 50 {
		msgPreview = msgPreview[:50] + "..."
	}
	h.Logger.Info("✅ created outgoing interaction",
		zap.Uint("interaction_id", interaction.ID),
		zap.Uint("conversation_id", conversation.ID),
		zap.Uint("tenant_id", claims.TenantID),
		zap.String("direction", interaction.Direction),
		zap.String("response_type", interaction.ResponseType),
		zap.String("response_status", interaction.ResponseStatus),
		zap.String("content_preview", msgPreview))

	// Reload interaction to get all populated fields (timestamps, etc.)
	if err := h.DB.First(&interaction, interaction.ID).Error; err != nil {
		h.Logger.Error("failed to reload interaction", zap.Error(err))
		// Continue anyway - we have the basic fields
	}

	// Update conversation last message time
	conversation.LastMessageAt = time.Now()
	h.DB.Save(&conversation)

	// Return the created interaction in the same format as getConversation
	type interactionResp struct {
		ID             uint              `json:"id"`
		Direction      string            `json:"direction"`
		MessageType    string            `json:"message_type"`
		Content        string            `json:"content"`
		DetectedIntent string            `json:"detected_intent"`
		DetectedTone   string            `json:"detected_tone"`
		ResponseType   string            `json:"response_type"`
		ResponseStatus string            `json:"response_status"`
		SuggestedReply string            `json:"suggested_reply"`
		FinalReply     string            `json:"final_reply"`
		CreatedAt      time.Time         `json:"created_at"`
		Media          *InteractionMedia `json:"media,omitempty"` // Media information (if message has media)
	}

	// Use same display logic as getConversation - for outgoing, use final_reply
	displayContent := interaction.Content
	if interaction.Direction == "outgoing" && interaction.FinalReply != "" {
		displayContent = interaction.FinalReply
	}

	// Extract media information from metadata
	media := parseMediaFromMetadata(interaction.Metadata)

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Reply sent successfully",
		"interaction": interactionResp{
			ID:             interaction.ID,
			Direction:      interaction.Direction,
			MessageType:    interaction.MessageType,
			Content:        displayContent, // Use final_reply for outgoing messages
			DetectedIntent: interaction.DetectedIntent,
			DetectedTone:   interaction.DetectedTone,
			ResponseType:   interaction.ResponseType,
			ResponseStatus: interaction.ResponseStatus,
			SuggestedReply: interaction.SuggestedReply,
			FinalReply:     interaction.FinalReply,
			CreatedAt:      interaction.CreatedAt,
			Media:          media, // Include media information if present
		},
	})
}

// =======================
// ESCALATIONS API
// =======================

// listEscalations returns list of pending escalations
func (h *Handler) listEscalations(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if h.WorkflowOrchestrator == nil {
		http.Error(w, "workflow orchestrator not available", http.StatusInternalServerError)
		return
	}

	// Query parameters
	priorityStr := r.URL.Query().Get("priority")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	var priority workflows.EscalationPriority
	if priorityStr != "" {
		priority = workflows.EscalationPriority(priorityStr)
	}

	ctx := r.Context()
	interactions, err := h.WorkflowOrchestrator.GetPendingEscalations(ctx, claims.TenantID, priority, limit)
	if err != nil {
		h.Logger.Error("failed to get escalations", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Load customer and conversation details for each escalation
	escalations := make([]map[string]interface{}, 0, len(interactions))
	for _, interaction := range interactions {
		var customer storage.Customer
		h.DB.First(&customer, interaction.CustomerID)

		var conversation storage.Conversation
		if interaction.ConversationID > 0 {
			h.DB.First(&conversation, interaction.ConversationID)
		}

		esc := map[string]interface{}{
			"id":                interaction.ID,
			"interaction_id":    interaction.ID,
			"customer_id":       customer.ID,
			"customer_name":     customer.DisplayName,
			"customer_username": customer.Username,
			"platform":          interaction.Platform,
			"message":           interaction.Content,
			"intent":            interaction.DetectedIntent,
			"tone":              interaction.DetectedTone,
			"confidence":        interaction.Confidence,
			"suggested_reply":   interaction.SuggestedReply,
			"created_at":        interaction.CreatedAt,
			"conversation_id":   interaction.ConversationID,
			"priority":          interaction.DetectedTone, // Use tone as priority indicator
		}

		if conversation.ID > 0 {
			esc["conversation_status"] = conversation.Status
		}

		escalations = append(escalations, esc)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"escalations": escalations,
		"count":       len(escalations),
	})
}

// getEscalation returns details of a specific escalation
func (h *Handler) getEscalation(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid escalation id", http.StatusBadRequest)
		return
	}

	var interaction storage.Interaction
	if err := h.DB.Where("tenant_id = ? AND id = ?", claims.TenantID, id).First(&interaction).Error; err != nil {
		http.Error(w, "escalation not found", http.StatusNotFound)
		return
	}

	// Load customer
	var customer storage.Customer
	h.DB.First(&customer, interaction.CustomerID)

	// Load conversation and related interactions
	var conversation storage.Conversation
	var conversationInteractions []storage.Interaction
	if interaction.ConversationID > 0 {
		h.DB.First(&conversation, interaction.ConversationID)
		h.DB.Where("conversation_id = ?", conversation.ID).
			Order("created_at ASC").
			Find(&conversationInteractions)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"escalation": map[string]interface{}{
			"id":                   interaction.ID,
			"customer":             customer,
			"platform":             interaction.Platform,
			"message":              interaction.Content,
			"intent":               interaction.DetectedIntent,
			"tone":                 interaction.DetectedTone,
			"confidence":           interaction.Confidence,
			"suggested_reply":      interaction.SuggestedReply,
			"created_at":           interaction.CreatedAt,
			"conversation":         conversation,
			"conversation_history": conversationInteractions,
		},
	})
}

// approveEscalation approves and sends the suggested reply
func (h *Handler) approveEscalation(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if h.WorkflowOrchestrator == nil {
		http.Error(w, "workflow orchestrator not available", http.StatusInternalServerError)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid escalation id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	err = h.WorkflowOrchestrator.HandleEscalationAction(ctx, claims.TenantID, uint(id), workflows.EscalationActionApprove, nil)
	if err != nil {
		h.Logger.Error("failed to approve escalation", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Escalation approved and reply sent",
	})
}

// replyToEscalation sends a custom reply to escalation
func (h *Handler) replyToEscalation(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if h.WorkflowOrchestrator == nil {
		http.Error(w, "workflow orchestrator not available", http.StatusInternalServerError)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid escalation id", http.StatusBadRequest)
		return
	}

	var req struct {
		Reply string `json:"reply"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Reply == "" {
		http.Error(w, "reply is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	params := map[string]interface{}{
		"reply": req.Reply,
	}

	// Check if editing or custom reply
	editStr := r.URL.Query().Get("edit")
	action := workflows.EscalationActionCustom
	if editStr == "true" {
		action = workflows.EscalationActionEdit
	}

	err = h.WorkflowOrchestrator.HandleEscalationAction(ctx, claims.TenantID, uint(id), action, params)
	if err != nil {
		h.Logger.Error("failed to reply to escalation", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Reply sent successfully",
	})
}

// ignoreEscalation ignores an escalation
func (h *Handler) ignoreEscalation(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if h.WorkflowOrchestrator == nil {
		http.Error(w, "workflow orchestrator not available", http.StatusInternalServerError)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid escalation id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	err = h.WorkflowOrchestrator.HandleEscalationAction(ctx, claims.TenantID, uint(id), workflows.EscalationActionIgnore, nil)
	if err != nil {
		h.Logger.Error("failed to ignore escalation", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Escalation ignored",
	})
}

// resolveEscalation marks escalation as resolved
func (h *Handler) resolveEscalation(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if h.WorkflowOrchestrator == nil {
		http.Error(w, "workflow orchestrator not available", http.StatusInternalServerError)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid escalation id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	err = h.WorkflowOrchestrator.HandleEscalationAction(ctx, claims.TenantID, uint(id), workflows.EscalationActionResolve, nil)
	if err != nil {
		h.Logger.Error("failed to resolve escalation", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Escalation resolved",
	})
}

// getEscalationStats returns escalation statistics
func (h *Handler) getEscalationStats(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if h.WorkflowOrchestrator == nil {
		http.Error(w, "workflow orchestrator not available", http.StatusInternalServerError)
		return
	}

	// Parse date range (optional - if not provided, get all-time stats)
	startDateStr := r.URL.Query().Get("start_date")
	endDateStr := r.URL.Query().Get("end_date")

	var startDate, endDate time.Time
	useDateRange := false

	if startDateStr != "" && endDateStr != "" {
		if parsedStart, err := time.Parse("2006-01-02", startDateStr); err == nil {
			if parsedEnd, err := time.Parse("2006-01-02", endDateStr); err == nil {
				startDate = parsedStart
				endDate = parsedEnd
				useDateRange = true
			}
		}
	}

	// If no date range provided, use zero times (will be treated as all-time)
	if !useDateRange {
		startDate = time.Time{}
		endDate = time.Time{}
	}

	ctx := r.Context()
	stats, err := h.WorkflowOrchestrator.GetEscalationStats(ctx, claims.TenantID, startDate, endDate)
	if err != nil {
		h.Logger.Error("failed to get escalation stats", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, stats)
}

// getCampaignStats returns statistics about scheduled, published, and draft posts
// @Summary      Get campaign statistics
// @Description  Get counts for scheduled, published, and draft posts
// @Tags         campaigns
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  map[string]interface{}  "Campaign statistics"
// @Failure      401  {object}  map[string]interface{}  "Unauthorized"
// @Router       /tenant/campaigns/stats [get]
func (h *Handler) getCampaignStats(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		h.writeUnauthorizedError(w, r, "unauthorized")
		return
	}

	// Count scheduled posts (status: scheduled, posting)
	var scheduledCount int64
	h.DB.Model(&storage.ScheduledPost{}).
		Where("tenant_id = ? AND status IN ?", claims.TenantID, []string{"scheduled", "posting"}).
		Count(&scheduledCount)

	// Count published posts (status: posted)
	var publishedCount int64
	h.DB.Model(&storage.ScheduledPost{}).
		Where("tenant_id = ? AND status = ?", claims.TenantID, "posted").
		Count(&publishedCount)

	// Count draft posts (status: draft, or posts with no scheduled_at and status not posted)
	var draftCount int64
	h.DB.Model(&storage.ScheduledPost{}).
		Where("tenant_id = ? AND (status = ? OR (scheduled_at IS NULL AND status != ?))", claims.TenantID, "draft", "posted").
		Count(&draftCount)

	stats := map[string]interface{}{
		"scheduled": scheduledCount,
		"published": publishedCount,
		"draft":     draftCount,
	}

	writeJSON(w, http.StatusOK, stats)
}

// getPerformanceSummary returns performance metrics for the overview dashboard
// @Summary      Get performance summary
// @Description  Get performance metrics including engagement rate, impressions, reach, and top performing post
// @Tags         analytics
// @Accept       json
// @Produce      json
// @Param        period  query     string  false  "Time period (7d, 30d, 90d, all)"  default(7d)
// @Security     BearerAuth
// @Success      200     {object}  map[string]interface{}  "Performance summary"
// @Failure      401     {object}  map[string]interface{}  "Unauthorized"
// @Router       /tenant/analytics/performance [get]
func (h *Handler) getPerformanceSummary(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		h.writeUnauthorizedError(w, r, "unauthorized")
		return
	}

	// Parse period parameter (7d, 30d, 90d, all)
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "7d"
	}

	var startDate time.Time
	now := time.Now()
	switch period {
	case "7d":
		startDate = now.AddDate(0, 0, -7)
	case "30d":
		startDate = now.AddDate(0, 0, -30)
	case "90d":
		startDate = now.AddDate(0, 0, -90)
	case "all":
		startDate = time.Time{} // Zero time means all time
	default:
		startDate = now.AddDate(0, 0, -7)
	}

	// Query PostAnalytics for the period
	var analytics []storage.PostAnalytics
	query := h.DB.Model(&storage.PostAnalytics{}).Where("tenant_id = ?", claims.TenantID)
	if !startDate.IsZero() {
		query = query.Where("posted_at >= ? AND posted_at <= ?", startDate, now)
	} else {
		query = query.Where("posted_at <= ?", now)
	}

	if err := query.Find(&analytics).Error; err != nil {
		h.Logger.Error("failed to fetch performance analytics", zap.Error(err))
		// Return empty response instead of error
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"period":              period,
			"engagement_rate":     nil,
			"total_impressions":   0,
			"total_reach":         0,
			"total_likes":         0,
			"total_comments":      0,
			"total_shares":        0,
			"total_posts":         0,
			"top_performing_post": nil,
			"platform_breakdown":  map[string]interface{}{},
		})
		return
	}

	if len(analytics) == 0 {
		// No analytics data available
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"period":              period,
			"engagement_rate":     nil,
			"total_impressions":   0,
			"total_reach":         0,
			"total_likes":         0,
			"total_comments":      0,
			"total_shares":        0,
			"total_posts":         0,
			"top_performing_post": nil,
			"platform_breakdown":  map[string]interface{}{},
		})
		return
	}

	// Aggregate metrics
	var totalImpressions, totalReach, totalLikes, totalComments, totalShares int64
	var totalEngagement int64
	var topPost *storage.PostAnalytics
	var topEngagement int64

	platformBreakdown := make(map[string]map[string]interface{})

	for i := range analytics {
		anal := &analytics[i]
		totalImpressions += anal.Impressions
		totalReach += anal.Reach
		totalLikes += anal.Likes
		totalComments += anal.Comments
		totalShares += anal.Shares
		totalEngagement += anal.Likes + anal.Comments + anal.Shares

		// Track top performing post
		postEngagement := anal.Likes + anal.Comments + anal.Shares
		if postEngagement > topEngagement {
			topEngagement = postEngagement
			topPost = anal
		}

		// Platform breakdown
		if platformBreakdown[anal.Platform] == nil {
			platformBreakdown[anal.Platform] = map[string]interface{}{
				"posts":       0,
				"impressions": int64(0),
				"reach":       int64(0),
				"likes":       int64(0),
				"comments":    int64(0),
			}
		}
		plat := platformBreakdown[anal.Platform]
		plat["posts"] = plat["posts"].(int) + 1
		plat["impressions"] = plat["impressions"].(int64) + anal.Impressions
		plat["reach"] = plat["reach"].(int64) + anal.Reach
		plat["likes"] = plat["likes"].(int64) + anal.Likes
		plat["comments"] = plat["comments"].(int64) + anal.Comments
	}

	// Calculate engagement rate
	var engagementRate *float64
	if totalReach > 0 {
		rate := float64(totalEngagement) / float64(totalReach) * 100
		engagementRate = &rate
	}

	// Get top performing post details
	var topPerformingPost map[string]interface{}
	if topPost != nil {
		// Get the scheduled post name
		var scheduledPost storage.ScheduledPost
		h.DB.First(&scheduledPost, topPost.ScheduledPostID)

		topEngagementRate := float64(0)
		if topPost.Reach > 0 {
			topEngagementRate = float64(topEngagement) / float64(topPost.Reach) * 100
		}

		topPerformingPost = map[string]interface{}{
			"id":              topPost.ScheduledPostID,
			"name":            scheduledPost.Name,
			"platform":        topPost.Platform,
			"impressions":     topPost.Impressions,
			"reach":           topPost.Reach,
			"likes":           topPost.Likes,
			"comments":        topPost.Comments,
			"shares":          topPost.Shares,
			"engagement_rate": topEngagementRate,
			"posted_at":       topPost.PostedAt.Format(time.RFC3339),
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"period":              period,
		"engagement_rate":     engagementRate,
		"total_impressions":   totalImpressions,
		"total_reach":         totalReach,
		"total_likes":         totalLikes,
		"total_comments":      totalComments,
		"total_shares":        totalShares,
		"total_posts":         len(analytics),
		"top_performing_post": topPerformingPost,
		"platform_breakdown":  platformBreakdown,
	})
}

// =======================
// CAPTION AI SUGGESTIONS API
// =======================

// generateCaptionFromKeywords generates a caption from keywords provided by tenant
func (h *Handler) generateCaptionFromKeywords(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Keywords  string   `json:"keywords"`   // Keywords to generate caption from
		Platform  string   `json:"platform"`   // instagram, facebook, tiktok, etc.
		MediaType string   `json:"media_type"` // image, video, carousel
		ImageURLs []string `json:"image_urls"` // Optional: URLs of images to analyze
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Keywords == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "keywords required",
			"message": "Please provide keywords to generate a caption",
		})
		return
	}

	if req.Platform == "" {
		req.Platform = "instagram" // Default platform
	}

	if req.MediaType == "" {
		req.MediaType = "image" // Default media type
	}

	// Create caption optimizer
	optimizer := &ai.CaptionOptimizer{
		Client: h.AIClient,
		DB:     h.DB,
	}

	ctx := r.Context()
	caption, err := optimizer.GenerateCaptionFromKeywords(ctx, claims.TenantID, req.Keywords, req.Platform, req.MediaType, req.ImageURLs)
	if err != nil {
		h.Logger.Error("failed to generate caption from keywords", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "failed to generate caption",
			"message": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"caption": caption,
	})
}

// rephraseCaption rephrases an existing caption
func (h *Handler) rephraseCaption(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Caption   string   `json:"caption"`    // Original caption to rephrase
		Platform  string   `json:"platform"`   // instagram, facebook, tiktok, etc.
		MediaType string   `json:"media_type"` // image, video, carousel
		ImageURLs []string `json:"image_urls"` // Optional: URLs of images
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Caption == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "caption required",
			"message": "Please provide a caption to rephrase",
		})
		return
	}

	if req.Platform == "" {
		req.Platform = "instagram" // Default platform
	}

	if req.MediaType == "" {
		req.MediaType = "image" // Default media type
	}

	// Create caption optimizer
	optimizer := &ai.CaptionOptimizer{
		Client: h.AIClient,
		DB:     h.DB,
	}

	ctx := r.Context()
	caption, err := optimizer.RephraseCaption(ctx, claims.TenantID, req.Caption, req.Platform, req.MediaType, req.ImageURLs)
	if err != nil {
		h.Logger.Error("failed to rephrase caption", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "failed to rephrase caption",
			"message": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"caption": caption,
	})
}

// fineTuneCaption fine-tunes an existing caption to improve it
func (h *Handler) fineTuneCaption(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Caption   string   `json:"caption"`    // Original caption to fine-tune
		Platform  string   `json:"platform"`   // instagram, facebook, tiktok, etc.
		MediaType string   `json:"media_type"` // image, video, carousel
		ImageURLs []string `json:"image_urls"` // Optional: URLs of images to analyze
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Caption == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "caption required",
			"message": "Please provide a caption to fine-tune",
		})
		return
	}

	if req.Platform == "" {
		req.Platform = "instagram" // Default platform
	}

	if req.MediaType == "" {
		req.MediaType = "image" // Default media type
	}

	// Create caption optimizer
	optimizer := &ai.CaptionOptimizer{
		Client: h.AIClient,
		DB:     h.DB,
	}

	ctx := r.Context()
	caption, err := optimizer.FineTuneCaption(ctx, claims.TenantID, req.Caption, req.Platform, req.MediaType, req.ImageURLs)
	if err != nil {
		h.Logger.Error("failed to fine-tune caption", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "failed to fine-tune caption",
			"message": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"caption": caption,
	})
}

// =======================
// ANALYTICS API
// =======================

func (h *Handler) getAnalytics(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Query parameters for filtering
	platform := r.URL.Query().Get("platform")
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")

	// Parse date range
	var start, end time.Time
	if startDate != "" {
		if parsed, err := time.Parse("2006-01-02", startDate); err == nil {
			start = parsed
		}
	} else {
		start = time.Now().AddDate(0, 0, -30) // Default: last 30 days
	}

	if endDate != "" {
		if parsed, err := time.Parse("2006-01-02", endDate); err == nil {
			end = parsed
		}
	} else {
		end = time.Now()
	}

	// Get scheduled posts count
	var scheduledCount int64
	query := h.DB.Model(&storage.ScheduledPost{}).Where("tenant_id = ? AND created_at >= ? AND created_at <= ?", claims.TenantID, start, end)
	if platform != "" {
		query = query.Where("platforms LIKE ?", "%"+platform+"%")
	}
	query.Count(&scheduledCount)

	// Get posted count
	var postedCount int64
	postQuery := h.DB.Model(&storage.ScheduledPost{}).Where("tenant_id = ? AND status = ? AND posted_at >= ? AND posted_at <= ?", claims.TenantID, "posted", start, end)
	if platform != "" {
		postQuery = postQuery.Where("platforms LIKE ?", "%"+platform+"%")
	}
	postQuery.Count(&postedCount)

	// Get conversations count
	var conversationsCount int64
	h.DB.Model(&storage.Conversation{}).Where("tenant_id = ? AND created_at >= ? AND created_at <= ?", claims.TenantID, start, end).Count(&conversationsCount)

	// Get interactions count
	var interactionsCount int64
	interactionQuery := h.DB.Model(&storage.Interaction{}).Where("tenant_id = ? AND created_at >= ? AND created_at <= ?", claims.TenantID, start, end)
	if platform != "" {
		interactionQuery = interactionQuery.Where("platform = ?", platform)
	}
	interactionQuery.Count(&interactionsCount)

	// Get response distribution (auto_reply, manual, escalated)
	var autoReplyCount, manualCount, escalatedCount int64
	responseQuery := h.DB.Model(&storage.Interaction{}).
		Where("tenant_id = ? AND direction = ? AND created_at >= ? AND created_at <= ?", claims.TenantID, "outgoing", start, end)
	if platform != "" {
		responseQuery = responseQuery.Where("platform = ?", platform)
	}
	responseQuery.Where("response_type = ?", "auto_reply").Count(&autoReplyCount)
	responseQuery.Where("response_type = ?", "manual").Count(&manualCount)
	responseQuery.Where("response_type = ?", "escalated").Count(&escalatedCount)

	// Get engagement metrics from PostAnalytics (if available)
	var totalImpressions, totalReach, totalLikes, totalComments, totalShares int64
	var avgEngagementRate float64
	var analyticsCount int64
	analyticsQuery := h.DB.Model(&storage.PostAnalytics{}).
		Where("tenant_id = ? AND posted_at >= ? AND posted_at <= ?", claims.TenantID, start, end)
	if platform != "" {
		analyticsQuery = analyticsQuery.Where("platform = ?", platform)
	}

	var postAnalytics []storage.PostAnalytics
	if err := analyticsQuery.Find(&postAnalytics).Error; err == nil {
		analyticsCount = int64(len(postAnalytics))
		for _, a := range postAnalytics {
			totalImpressions += a.Impressions
			totalReach += a.Reach
			totalLikes += a.Likes
			totalComments += a.Comments
			totalShares += a.Shares
			avgEngagementRate += a.EngagementRate
		}
		if len(postAnalytics) > 0 {
			avgEngagementRate = avgEngagementRate / float64(len(postAnalytics))
		}
	}

	// Get platform breakdown with engagement metrics
	type platformStats struct {
		Platform       string  `json:"platform"`
		Posts          int64   `json:"posts"`
		Conversations  int64   `json:"conversations"`
		Impressions    int64   `json:"impressions"`
		Reach          int64   `json:"reach"`
		Likes          int64   `json:"likes"`
		Comments       int64   `json:"comments"`
		Shares         int64   `json:"shares"`
		EngagementRate float64 `json:"engagement_rate"`
	}

	var platformBreakdown []platformStats
	platforms := []string{"instagram", "facebook", "whatsapp", "tiktok", "youtube"}
	for _, p := range platforms {
		if platform != "" && platform != p {
			continue
		}

		var posts int64
		h.DB.Model(&storage.ScheduledPost{}).
			Where("tenant_id = ? AND platforms LIKE ? AND created_at >= ? AND created_at <= ?", claims.TenantID, "%"+p+"%", start, end).
			Count(&posts)

		var convos int64
		h.DB.Model(&storage.Conversation{}).
			Where("tenant_id = ? AND platform = ? AND created_at >= ? AND created_at <= ?", claims.TenantID, p, start, end).
			Count(&convos)

		// Get engagement metrics for this platform
		var platImpressions, platReach, platLikes, platComments, platShares int64
		var platEngagementRate float64
		var platAnalytics []storage.PostAnalytics
		if err := h.DB.Model(&storage.PostAnalytics{}).
			Where("tenant_id = ? AND platform = ? AND posted_at >= ? AND posted_at <= ?", claims.TenantID, p, start, end).
			Find(&platAnalytics).Error; err == nil {
			for _, a := range platAnalytics {
				platImpressions += a.Impressions
				platReach += a.Reach
				platLikes += a.Likes
				platComments += a.Comments
				platShares += a.Shares
				platEngagementRate += a.EngagementRate
			}
			if len(platAnalytics) > 0 {
				platEngagementRate = platEngagementRate / float64(len(platAnalytics))
			}
		}

		platformBreakdown = append(platformBreakdown, platformStats{
			Platform:       p,
			Posts:          posts,
			Conversations:  convos,
			Impressions:    platImpressions,
			Reach:          platReach,
			Likes:          platLikes,
			Comments:       platComments,
			Shares:         platShares,
			EngagementRate: platEngagementRate,
		})
	}

	// Calculate response distribution percentage
	var totalResponses = autoReplyCount + manualCount + escalatedCount
	var responseDistribution map[string]any
	if totalResponses > 0 {
		responseDistribution = map[string]any{
			"auto_reply": map[string]any{
				"count":      autoReplyCount,
				"percentage": float64(autoReplyCount) / float64(totalResponses) * 100,
			},
			"manual": map[string]any{
				"count":      manualCount,
				"percentage": float64(manualCount) / float64(totalResponses) * 100,
			},
			"escalated": map[string]any{
				"count":      escalatedCount,
				"percentage": float64(escalatedCount) / float64(totalResponses) * 100,
			},
		}
	} else {
		responseDistribution = map[string]any{
			"auto_reply": map[string]any{"count": 0, "percentage": 0},
			"manual":     map[string]any{"count": 0, "percentage": 0},
			"escalated":  map[string]any{"count": 0, "percentage": 0},
		}
	}

	// Calculate response time distribution (< 1 min, 1-5 min, 5-15 min, > 15 min)
	// Get all incoming interactions with their first response time
	type responseTimeStats struct {
		Under1Min   int64 `json:"under_1_min"`
		OneTo5Min   int64 `json:"1_to_5_min"`
		FiveTo15Min int64 `json:"5_to_15_min"`
		Over15Min   int64 `json:"over_15_min"`
	}

	var responseTimeDist responseTimeStats
	var responseTimeInteractions []struct {
		IncomingTime time.Time
		OutgoingTime time.Time
	}

	// Get conversations with their first incoming and outgoing interactions
	// This calculates REAL response times by finding the time between each incoming message
	// and the first outgoing response in the same conversation
	rows, err := h.DB.Raw(`
		SELECT 
			i1.created_at as incoming_time,
			MIN(i2.created_at) as outgoing_time
		FROM interactions i1
		INNER JOIN interactions i2 ON 
			i2.conversation_id = i1.conversation_id 
			AND i2.direction = 'outgoing' 
			AND i2.created_at > i1.created_at
		WHERE i1.tenant_id = ? 
			AND i1.direction = 'incoming'
			AND i1.created_at >= ? 
			AND i1.created_at <= ?
		GROUP BY i1.id, i1.created_at
	`, claims.TenantID, start, end).Rows()

	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var rt struct {
				IncomingTime time.Time
				OutgoingTime time.Time
			}
			if err := rows.Scan(&rt.IncomingTime, &rt.OutgoingTime); err == nil {
				duration := rt.OutgoingTime.Sub(rt.IncomingTime)
				minutes := duration.Minutes()

				if minutes < 1 {
					responseTimeDist.Under1Min++
				} else if minutes < 5 {
					responseTimeDist.OneTo5Min++
				} else if minutes < 15 {
					responseTimeDist.FiveTo15Min++
				} else {
					responseTimeDist.Over15Min++
				}
			}
		}
	}

	for _, rt := range responseTimeInteractions {
		duration := rt.OutgoingTime.Sub(rt.IncomingTime)
		minutes := duration.Minutes()

		if minutes < 1 {
			responseTimeDist.Under1Min++
		} else if minutes < 5 {
			responseTimeDist.OneTo5Min++
		} else if minutes < 15 {
			responseTimeDist.FiveTo15Min++
		} else {
			responseTimeDist.Over15Min++
		}
	}

	var totalResponseTimes = responseTimeDist.Under1Min + responseTimeDist.OneTo5Min + responseTimeDist.FiveTo15Min + responseTimeDist.Over15Min
	var responseTimeDistribution map[string]any
	if totalResponseTimes > 0 {
		responseTimeDistribution = map[string]any{
			"under_1_min": map[string]any{
				"count":      responseTimeDist.Under1Min,
				"percentage": float64(responseTimeDist.Under1Min) / float64(totalResponseTimes) * 100,
			},
			"1_to_5_min": map[string]any{
				"count":      responseTimeDist.OneTo5Min,
				"percentage": float64(responseTimeDist.OneTo5Min) / float64(totalResponseTimes) * 100,
			},
			"5_to_15_min": map[string]any{
				"count":      responseTimeDist.FiveTo15Min,
				"percentage": float64(responseTimeDist.FiveTo15Min) / float64(totalResponseTimes) * 100,
			},
			"over_15_min": map[string]any{
				"count":      responseTimeDist.Over15Min,
				"percentage": float64(responseTimeDist.Over15Min) / float64(totalResponseTimes) * 100,
			},
		}
	} else {
		responseTimeDistribution = map[string]any{
			"under_1_min": map[string]any{"count": 0, "percentage": 0},
			"1_to_5_min":  map[string]any{"count": 0, "percentage": 0},
			"5_to_15_min": map[string]any{"count": 0, "percentage": 0},
			"over_15_min": map[string]any{"count": 0, "percentage": 0},
		}
	}

	// Calculate volume by channel (percentage distribution)
	var totalChannelVolume int64
	for _, p := range platformBreakdown {
		totalChannelVolume += p.Conversations
	}

	var volumeByChannel []map[string]any
	for _, p := range platformBreakdown {
		var percentage float64
		if totalChannelVolume > 0 {
			percentage = float64(p.Conversations) / float64(totalChannelVolume) * 100
		}
		volumeByChannel = append(volumeByChannel, map[string]any{
			"platform":   p.Platform,
			"count":      p.Conversations,
			"percentage": percentage,
		})
	}

	// Determine if data is available (not just zeros)
	hasData := scheduledCount > 0 || postedCount > 0 || conversationsCount > 0 || interactionsCount > 0
	hasPlatformData := totalChannelVolume > 0
	hasResponseTimeData := totalResponseTimes > 0

	writeJSON(w, http.StatusOK, map[string]any{
		"summary": map[string]any{
			"scheduled_posts": scheduledCount,
			"posted":          postedCount,
			"conversations":   conversationsCount,
			"interactions":    interactionsCount,
			"has_data":        hasData,
		},
		"engagement": map[string]any{
			"total_impressions":    totalImpressions,
			"total_reach":          totalReach,
			"total_likes":          totalLikes,
			"total_comments":       totalComments,
			"total_shares":         totalShares,
			"avg_engagement_rate":  avgEngagementRate,
			"posts_with_analytics": analyticsCount,
		},
		"response_distribution": responseDistribution,
		"response_time_distribution": map[string]any{
			"data":     responseTimeDistribution,
			"has_data": hasResponseTimeData,
			"total":    totalResponseTimes,
		},
		"volume_by_channel": map[string]any{
			"data":     volumeByChannel,
			"has_data": hasPlatformData,
			"total":    totalChannelVolume,
		},
		"platforms": platformBreakdown,
		"date_range": map[string]string{
			"start": start.Format("2006-01-02"),
			"end":   end.Format("2006-01-02"),
		},
	})
}

// =======================
// TEAM MANAGEMENT API (Placeholder - Coming Soon)
// =======================

func (h *Handler) listTeamMembers(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// For now, return just the tenant owner
	var tenant storage.Tenant
	if err := h.DB.First(&tenant, claims.TenantID).Error; err != nil {
		http.Error(w, "tenant not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"members": []map[string]any{
			{
				"id":         tenant.ID,
				"name":       tenant.OwnerName,
				"email":      tenant.Email,
				"role":       "owner",
				"created_at": tenant.CreatedAt,
			},
		},
		"message":     "Team management coming soon",
		"coming_soon": true,
		"feature":     "team_management",
	})
}

func (h *Handler) inviteTeamMember(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":       "not_implemented",
		"message":     "Team invitations coming soon",
		"coming_soon": true,
		"feature":     "team_management",
	})
}

func (h *Handler) getTeamMember(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":       "not_implemented",
		"message":     "Team management coming soon",
		"coming_soon": true,
		"feature":     "team_management",
	})
}

func (h *Handler) updateTeamMember(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":       "not_implemented",
		"message":     "Team management coming soon",
		"coming_soon": true,
		"feature":     "team_management",
	})
}

func (h *Handler) deleteTeamMember(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":       "not_implemented",
		"message":     "Team management coming soon",
		"coming_soon": true,
		"feature":     "team_management",
	})
}

// sendPlatformReply sends a reply via the appropriate platform connector
func (h *Handler) sendPlatformReply(ctx context.Context, platform string, account *storage.SocialAccount, customer *storage.Customer, message string) error {
	switch platform {
	case "instagram":
		return h.sendInstagramReply(ctx, account, customer.PlatformID, message)
	case "facebook":
		return h.sendFacebookReply(ctx, account, customer.PlatformID, message)
	case "whatsapp":
		return h.sendWhatsAppReply(ctx, account, customer.PlatformID, message)
	default:
		return fmt.Errorf("reply sending not implemented for %s", platform)
	}
}

func (h *Handler) sendInstagramReply(ctx context.Context, acc *storage.SocialAccount, recipientID, message string) error {
	// Decrypt token
	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		decryptedToken = acc.AccessToken
	}

	if decryptedToken == "" {
		return fmt.Errorf("Instagram access token is empty")
	}

	igAccountID := acc.ExternalID
	if igAccountID == "" {
		igAccountID = acc.PageID
	}
	if igAccountID == "" {
		return fmt.Errorf("Instagram Business Account ID not found")
	}

	// CRITICAL: Validate that we're not trying to send to ourselves
	if recipientID == igAccountID {
		return fmt.Errorf("CRITICAL: Cannot send message - recipient ID matches Instagram account ID")
	}

	if recipientID == "" {
		return fmt.Errorf("CRITICAL: Recipient ID is empty - cannot determine recipient for Instagram message")
	}

	// Determine API base URL and token requirements based on login method
	var apiURL string
	if acc.LoginMethod == "instagram_login" {
		// Instagram Login: Use graph.instagram.com/v24.0 with Instagram User access token
		// According to: https://developers.facebook.com/docs/instagram-platform/instagram-api-with-instagram-login/messaging-api
		apiURL = fmt.Sprintf("https://graph.instagram.com/v24.0/%s/messages", igAccountID)
	} else {
		// Facebook Login: Use graph.facebook.com/v18.0 with Page Access Token
		apiURL = fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages", igAccountID)
	}

	payload := map[string]interface{}{
		"recipient": map[string]string{"id": recipientID},
		"message":   map[string]interface{}{"text": message},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// For Instagram Login, use Bearer token in Authorization header (as per documentation)
	// For Facebook Login, use access_token query parameter (backward compatible)
	var finalURL string
	if acc.LoginMethod == "instagram_login" {
		// Instagram Login: Use Bearer token in header
		finalURL = apiURL
	} else {
		// Facebook Login: Add access token as query parameter (Meta API standard)
		finalURL = fmt.Sprintf("%s?access_token=%s", apiURL, url.QueryEscape(decryptedToken))
	}

	req, err := http.NewRequestWithContext(ctx, "POST", finalURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if acc.LoginMethod == "instagram_login" {
		req.Header.Set("Authorization", "Bearer "+decryptedToken)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Instagram API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Instagram API error %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (h *Handler) sendFacebookReply(ctx context.Context, acc *storage.SocialAccount, recipientID, message string) error {
	// Similar to Instagram
	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		decryptedToken = acc.AccessToken
	}

	pageID := acc.PageID
	if pageID == "" {
		return fmt.Errorf("Facebook Page ID not found")
	}

	apiURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages", pageID)
	payload := map[string]interface{}{
		"recipient": map[string]string{"id": recipientID},
		"message":   map[string]interface{}{"text": message},
	}

	jsonData, _ := json.Marshal(payload)
	apiURLWithToken := fmt.Sprintf("%s?access_token=%s", apiURL, url.QueryEscape(decryptedToken))

	req, _ := http.NewRequestWithContext(ctx, "POST", apiURLWithToken, strings.NewReader(string(jsonData)))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Facebook API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Facebook API error %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (h *Handler) sendWhatsAppReply(ctx context.Context, acc *storage.SocialAccount, recipientID, message string) error {
	// Since we only use Gupshup, check for Gupshup mode
	// Gupshup connections are stored in WhatsAppApp table, not SocialAccount
	// Check if account mode indicates Gupshup, or look up WhatsAppApp directly
	var app storage.WhatsAppApp
	if err := h.DB.Where("tenant_id = ? AND mode = ?", acc.TenantID, "gupshup_partner").First(&app).Error; err != nil {
		// If no Gupshup app found, try to use Meta API as fallback (for backward compatibility)
		h.Logger.Warn("No Gupshup app found, falling back to Meta API",
			zap.Uint("tenant_id", acc.TenantID),
			zap.Error(err))
		return h.sendWhatsAppReplyMeta(ctx, acc, recipientID, message)
	}

	// Verify app is live and has required data
	if app.Status != "live" {
		return fmt.Errorf("WhatsApp app is not live (status: %s)", app.Status)
	}

	if app.GupshupAppID == "" {
		return fmt.Errorf("Gupshup App ID not found for WhatsApp account")
	}

	// Create Gupshup client
	gupshupClient := social.NewGupshupClient(h.Config, h.Logger)

	// Since we're using sessions, try session message first (no template needed)
	// Session messages work within 24 hours of customer's last message
	messageID, err := gupshupClient.SendSessionMessage(app.GupshupAppID, recipientID, message)
	if err != nil {
		// Session message failed - might be outside 24-hour window
		// Fall back to template message
		h.Logger.Warn("Session message failed, falling back to template",
			zap.Uint("tenant_id", acc.TenantID),
			zap.String("app_id", app.GupshupAppID),
			zap.String("recipient", recipientID),
			zap.Error(err),
			zap.String("note", "This may happen if customer is outside 24-hour window"))

		// Get default template ID if available
		var templateID string
		if app.DefaultTemplateID != "" {
			templateID = app.DefaultTemplateID
		}

		// Try template message as fallback
		messageID, err = gupshupClient.SendMessage(app.GupshupAppID, recipientID, message, templateID)
		if err != nil {
			h.Logger.Error("Failed to send WhatsApp message via Gupshup (both session and template failed)",
				zap.Uint("tenant_id", acc.TenantID),
				zap.String("app_id", app.GupshupAppID),
				zap.String("recipient", recipientID),
				zap.Error(err))
			return fmt.Errorf("Gupshup API request failed: %w", err)
		}

		h.Logger.Info("✅ WhatsApp message sent via Gupshup (template fallback)",
			zap.Uint("tenant_id", acc.TenantID),
			zap.String("app_id", app.GupshupAppID),
			zap.String("recipient", recipientID),
			zap.String("message_id", messageID))
	} else {
		h.Logger.Info("✅ WhatsApp message sent via Gupshup (session message)",
			zap.Uint("tenant_id", acc.TenantID),
			zap.String("app_id", app.GupshupAppID),
			zap.String("recipient", recipientID),
			zap.String("message_id", messageID))
	}

	return nil
}

// sendWhatsAppReplyMeta sends a WhatsApp message via Meta API (fallback for non-Gupshup)
func (h *Handler) sendWhatsAppReplyMeta(ctx context.Context, acc *storage.SocialAccount, recipientID, message string) error {
	// WhatsApp uses phone number as recipient
	phoneNumberID := acc.PageID // This is phone_number_id for WhatsApp
	if phoneNumberID == "" {
		return fmt.Errorf("WhatsApp Phone Number ID not found")
	}

	// Use system token for provider-owned model, or decrypt tenant token
	token := os.Getenv("META_SYSTEM_TOKEN")
	if acc.Mode != "provider_owned" {
		decrypted, err := utils.DecryptToken(acc.AccessToken)
		if err == nil {
			token = decrypted
		}
	}

	if token == "" {
		return fmt.Errorf("WhatsApp access token not found")
	}

	apiURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages", phoneNumberID)
	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                recipientID,
		"type":              "text",
		"text":              map[string]string{"body": message},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal WhatsApp payload: %w", err)
	}
	apiURLWithToken := fmt.Sprintf("%s?access_token=%s", apiURL, url.QueryEscape(token))

	req, err := http.NewRequestWithContext(ctx, "POST", apiURLWithToken, strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to create WhatsApp request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("WhatsApp API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("WhatsApp API error %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// connectWhatsApp - New simplified WhatsApp connection endpoint for Respond.io
// This replaces the Meta embedded signup flow
func (h *Handler) connectWhatsApp(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		PhoneNumber string `json:"phone_number"` // e.g., "+2347039841392"
		Provider    string `json:"provider"`     // "respondio" or "auto" (default)
		ChannelID   string `json:"channel_id"`   // Optional: manual Respond.io channel ID override (e.g., "440617")
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Determine provider first (to check if phone number is required)
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider == "" || provider == "auto" {
		// Auto-detect: Use Respond.io if configured
		if h.Config.RespondIOAPIKey != "" {
			provider = "respondio"
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "no_provider_configured",
				"message": "Respond.io is not configured. Please contact support.",
			})
			return
		}
	}

	// Normalize phone number (required for Respond.io)
	phoneNumber := strings.TrimSpace(req.PhoneNumber)

	// For Respond.io, phone number is required
	if provider == "respondio" && phoneNumber == "" {
		http.Error(w, "phone_number is required for Respond.io provider", http.StatusBadRequest)
		return
	}

	// Normalize phone number format
	if phoneNumber != "" && !strings.HasPrefix(phoneNumber, "+") {
		phoneNumber = "+" + phoneNumber
	}

	// Check if tenant already has a WhatsApp account connected (check both SocialAccount and WhatsAppApp)
	var existingAccount storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ? AND connected = ?", claims.TenantID, "whatsapp", true).First(&existingAccount).Error; err == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "already_connected",
			"message": "You already have WhatsApp connected. Please disconnect first to connect a new number.",
			"account": map[string]any{
				"external_id":  existingAccount.ExternalID,
				"phone_number": existingAccount.PageID,
			},
		})
		return
	}

	// Check WhatsAppApp table (legacy, kept for future use)
	var existingApp storage.WhatsAppApp
	if err := h.DB.Where("tenant_id = ?", claims.TenantID).First(&existingApp).Error; err == nil {
		if existingApp.Status == "live" || existingApp.Status == "pending_onboarding" || existingApp.Status == "pending_verification" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "already_connected",
				"message": fmt.Sprintf("You already have a WhatsApp connection in progress (status: %s). Please wait for it to complete or disconnect first.", existingApp.Status),
				"account": map[string]any{
					"app_id":       existingApp.GupshupAppID,
					"phone_number": existingApp.PhoneNumber,
					"status":       existingApp.Status,
				},
			})
			return
		}
	}

	// Route to appropriate provider flow
	switch provider {
	case "meta_embedded", "meta", "embedded_signup":
		// Meta Embedded Signup - redirect to Embedded Signup flow
		h.connectWhatsAppMetaEmbedded(w, r, claims.TenantID)

	case "gupshup", "gupshup_partner":
		// Gupshup Partner - create app and generate embed link
		h.connectWhatsAppGupshup(w, r, claims.TenantID)

	case "respondio":
		// For Respond.io, we use the shared account
		h.connectWhatsAppRespondIO(w, r, claims.TenantID, phoneNumber, req.ChannelID)

	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_provider",
			"message": fmt.Sprintf("Invalid provider '%s'. Use 'meta_embedded', 'gupshup', 'respondio', or 'auto'.", provider),
		})
	}
}

// connectWhatsAppRespondIO - Connect WhatsApp via Respond.io (simplified - uses shared account)
func (h *Handler) connectWhatsAppRespondIO(w http.ResponseWriter, r *http.Request, tenantID uint, phoneNumber string, manualChannelID string) {
	if h.Config.RespondIOAPIKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "respondio_not_configured",
			"message": "Respond.io is not configured. Please contact support.",
		})
		return
	}

	ctx := r.Context()

	// Step 1: Get WhatsApp channel from Respond.io workspace
	respondIOClient := social.NewRespondIOClient(h.Config.RespondIOAPIKey, h.Logger)
	channels, err := respondIOClient.ListChannels(ctx)
	if err != nil {
		h.Logger.Error("failed to list Respond.io channels", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "channel_fetch_failed",
			"message": "Failed to fetch WhatsApp channel from Respond.io: " + err.Error(),
		})
		return
	}

	// Find WhatsApp channel
	var whatsappChannelID string
	var channelStatus string
	for _, channel := range channels {
		channelType, _ := channel["type"].(string)
		channelName, _ := channel["name"].(string)

		// Handle channel ID as string or number (e.g., 440617)
		var channelID string
		if idStr, ok := channel["id"].(string); ok {
			channelID = idStr
		} else if idNum, ok := channel["id"].(float64); ok {
			channelID = fmt.Sprintf("%.0f", idNum) // Convert 440617.0 to "440617"
		} else if idNum, ok := channel["id"].(int); ok {
			channelID = fmt.Sprintf("%d", idNum)
		}

		if status, ok := channel["status"].(string); ok {
			channelStatus = status
		} else if status, ok := channel["available"].(bool); ok {
			if status {
				channelStatus = "available"
			} else {
				channelStatus = "unavailable"
			}
		}

		h.Logger.Info("Found Respond.io channel",
			zap.String("type", channelType),
			zap.String("id", channelID),
			zap.String("name", channelName),
			zap.String("status", channelStatus),
			zap.Any("full_channel", channel))

		// Look for WhatsApp channel (accept even if unavailable - we can still send messages)
		// Match by type OR name contains "whatsapp" OR contains "wa" (abbreviation)
		isWhatsApp := channelType == "whatsapp" ||
			channelType == "wa" ||
			channelType == "whatsapp_business_platform" ||
			strings.Contains(strings.ToLower(channelName), "whatsapp") ||
			strings.Contains(strings.ToLower(channelName), "wa business")

		if isWhatsApp && channelID != "" {
			whatsappChannelID = channelID
			if channelStatus != "" && channelStatus != "available" {
				h.Logger.Warn("⚠️ WhatsApp channel found but status is not 'available'",
					zap.String("channel_id", channelID),
					zap.String("status", channelStatus),
					zap.String("note", "Channel may still work for sending messages, but you may need to activate it in Respond.io dashboard"))
			}
			break
		}
	}

	// If no WhatsApp channel found but we have 0 channels total,
	// the API might not be returning channels properly
	// Allow fallback: use channel ID directly if provided in request
	if whatsappChannelID == "" && manualChannelID != "" {
		h.Logger.Warn("⚠️ No WhatsApp channel found in API response, but channel_id provided in request. Using provided channel ID.",
			zap.String("channel_id", manualChannelID),
			zap.Int("channels_returned", len(channels)),
			zap.String("note", "Channel exists in dashboard but API returned empty. Using manual channel ID as workaround."))
		whatsappChannelID = manualChannelID
	}

	if whatsappChannelID == "" {
		h.Logger.Error("no WhatsApp channel found in Respond.io workspace",
			zap.Int("total_channels", len(channels)),
			zap.Any("all_channels", channels))

		// List available channel types and details for debugging
		channelTypes := []string{}
		channelDetails := []map[string]interface{}{}
		for _, ch := range channels {
			chDetail := map[string]interface{}{}
			if t, ok := ch["type"].(string); ok {
				channelTypes = append(channelTypes, t)
				chDetail["type"] = t
			}
			if id, ok := ch["id"].(string); ok {
				chDetail["id"] = id
			} else if id, ok := ch["id"].(float64); ok {
				chDetail["id"] = fmt.Sprintf("%.0f", id) // Convert float64 to string
			}
			if name, ok := ch["name"].(string); ok {
				chDetail["name"] = name
			}
			if status, ok := ch["status"].(string); ok {
				chDetail["status"] = status
			}
			channelDetails = append(channelDetails, chDetail)
		}

		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":           "no_whatsapp_channel",
			"message":         "No WhatsApp channel found in Respond.io API response. The channel exists in dashboard (ID: 440617) but API returned 0 channels.",
			"total_channels":  len(channels),
			"channel_types":   channelTypes,
			"channel_details": channelDetails,
			"workaround":      "Since channel ID 440617 exists in your dashboard, you can manually specify it by adding 'channel_id': '440617' to your request body.",
			"troubleshooting": []string{
				"1. The API returned 0 channels - this might be a permissions or endpoint issue",
				"2. Your channel (ID: 440617) exists in dashboard but isn't returned by the API",
				"3. As a workaround, add 'channel_id': '440617' to your POST request body",
				"4. Or verify API key has permission to list channels in Respond.io settings",
			},
			"example_request": map[string]any{
				"phone_number": phoneNumber,
				"provider":     "respondio",
				"channel_id":   "440617", // Manual override
			},
		})
		return
	}

	h.Logger.Info("✅ Found WhatsApp channel in Respond.io",
		zap.String("channel_id", whatsappChannelID),
		zap.Uint("tenant_id", tenantID))

	// Step 2: Create or update contact in Respond.io (using tenant's phone number)
	// This ensures the contact exists for sending/receiving messages
	contactData := map[string]interface{}{
		"name": phoneNumber, // Use phone number as name initially
		"channels": []map[string]interface{}{
			{
				"type":  "whatsapp",
				"value": phoneNumber,
			},
		},
	}

	// Create or update contact in Respond.io
	if err := respondIOClient.CreateOrUpdateContact(ctx, phoneNumber, contactData); err != nil {
		h.Logger.Warn("failed to create/update contact in Respond.io (may already exist)", zap.Error(err))
		// Don't fail - contact might already exist or be created automatically
	}

	// Step 3: Create or update SocialAccount record with channel ID
	var account storage.SocialAccount
	err = h.DB.Where("tenant_id = ? AND platform = ?", tenantID, "whatsapp").First(&account).Error
	if err == nil {
		// Update existing account
		account.Connected = true
		account.PageID = phoneNumber           // Store phone number for reference
		account.ExternalID = whatsappChannelID // Store Respond.io WhatsApp channel ID
		account.Mode = "respondio"
		if err := h.DB.Save(&account).Error; err != nil {
			h.Logger.Error("failed to update WhatsApp account", zap.Error(err))
			http.Error(w, "failed to connect", http.StatusInternalServerError)
			return
		}
	} else {
		// Create new account
		account = storage.SocialAccount{
			TenantID:   tenantID,
			Platform:   "whatsapp",
			Connected:  true,
			PageID:     phoneNumber,       // Store phone number for reference
			ExternalID: whatsappChannelID, // Store Respond.io WhatsApp channel ID (CRITICAL!)
			Mode:       "respondio",
			Username:   phoneNumber,
		}
		if err := h.DB.Create(&account).Error; err != nil {
			h.Logger.Error("failed to create WhatsApp account", zap.Error(err))
			http.Error(w, "failed to connect", http.StatusInternalServerError)
			return
		}
	}

	h.Logger.Info("✅ WhatsApp connected via Respond.io",
		zap.Uint("tenant_id", tenantID),
		zap.String("phone_number", phoneNumber),
		zap.String("channel_id", whatsappChannelID))

	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"provider":     "respondio",
		"phone_number": phoneNumber,
		"channel_id":   whatsappChannelID,
		"message":      fmt.Sprintf("✅ WhatsApp connected successfully via Respond.io! Your number %s is ready to receive messages.", phoneNumber),
		"account_id":   account.ID,
		"note":         "Respond.io uses a shared account. Messages will be sent from the configured Respond.io WhatsApp number to your customers.",
	})
}

// getWhatsAppConnectionStatus - Get current WhatsApp connection status
func (h *Handler) getWhatsAppConnectionStatus(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	tenantID := claims.TenantID

	// Check for WhatsApp connection (Respond.io, Meta Embedded Signup, or Gupshup)
	// First check Gupshup Partner (whatsapp_apps table)
	var gupshupApp storage.WhatsAppApp
	if err := h.DB.Where("tenant_id = ? AND mode = ?", tenantID, "gupshup_partner").First(&gupshupApp).Error; err == nil {
		// Gupshup Partner connection found
		// CRITICAL: Only mark as connected if status is "live" AND we have metadata (WABA ID or phone number)
		// Don't mark as connected just because subscription is active - that happens during onboarding
		hasMetadata := gupshupApp.WABAID != "" || gupshupApp.PhoneNumber != ""
		connected := gupshupApp.Status == "live" && hasMetadata

		h.Logger.Info("📊 WhatsApp connection status check",
			zap.Uint("tenant_id", tenantID),
			zap.String("app_id", gupshupApp.GupshupAppID),
			zap.String("status", gupshupApp.Status),
			zap.Bool("has_waba_id", gupshupApp.WABAID != ""),
			zap.Bool("has_phone", gupshupApp.PhoneNumber != ""),
			zap.Bool("subscription_active", gupshupApp.SubscriptionActive),
			zap.Bool("connected", connected))

		response := map[string]any{
			"connected":        connected,
			"provider":         "gupshup_partner",
			"phone_number":     gupshupApp.PhoneNumber,
			"status":           gupshupApp.Status,
			"subscription_id":  gupshupApp.SubscriptionID,
			"subscription_set": gupshupApp.SubscriptionActive,
			"app_id":           gupshupApp.GupshupAppID,
		}
		writeJSON(w, http.StatusOK, response)
		return
	}

	// Fallback: Check SocialAccount table (for Respond.io, Meta, etc.)
	var account storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ? AND connected = ?", tenantID, "whatsapp", true).First(&account).Error; err == nil {
		response := map[string]any{
			"connected":    true,
			"provider":     account.Mode,
			"phone_number": account.Username, // Phone number is stored in Username for WhatsApp
			"status":       "connected",
		}

		// Add provider-specific details
		if account.Mode == "oauth2" || account.Mode == "meta_embedded" {
			response["provider"] = "meta_embedded"
			response["phone_number_id"] = account.PageID
			response["external_id"] = account.ExternalID
		} else if account.Mode == "respondio" {
			response["channel_id"] = account.ExternalID
		} else if account.Mode == "gupshup_partner" {
			// Check WhatsAppApp table for Gupshup details
			var app storage.WhatsAppApp
			if err := h.DB.Where("tenant_id = ?", tenantID).First(&app).Error; err == nil {
				response["app_id"] = app.GupshupAppID
				response["phone_number"] = app.PhoneNumber
				response["status"] = app.Status
			}
		}
		writeJSON(w, http.StatusOK, response)
		return
	}

	// No connection found
	writeJSON(w, http.StatusOK, map[string]any{
		"connected": false,
		"status":    "not_connected",
	})
}

// refreshWhatsAppStatus - Manually refresh WhatsApp status by calling GetAppDetails
// This is useful when webhooks don't arrive - allows manual trigger to check status
func (h *Handler) refreshWhatsAppStatus(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	tenantID := claims.TenantID

	// Find Gupshup app for this tenant
	h.Logger.Info("🔄 Searching for Gupshup app",
		zap.Uint("tenant_id", tenantID))
	var app storage.WhatsAppApp
	if err := h.DB.Where("tenant_id = ? AND mode = ?", tenantID, "gupshup_partner").First(&app).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			h.Logger.Warn("⚠️ No WhatsApp app found for tenant",
				zap.Uint("tenant_id", tenantID))
			writeJSON(w, http.StatusNotFound, map[string]any{
				"error":   "no_whatsapp_app",
				"message": "No WhatsApp app found for this tenant",
			})
			return
		}
		h.Logger.Error("❌ Database error finding app",
			zap.Uint("tenant_id", tenantID),
			zap.Error(err))
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	h.Logger.Info("✅ Found WhatsApp app",
		zap.Uint("tenant_id", tenantID),
		zap.String("app_id", app.GupshupAppID),
		zap.String("status", app.Status))

	h.Logger.Info("🔄 Manual refresh WhatsApp status triggered",
		zap.Uint("tenant_id", tenantID),
		zap.String("app_id", app.GupshupAppID),
		zap.String("current_status", app.Status),
		zap.String("gupshup_app_id", app.GupshupAppID))

	// Create Gupshup client
	h.Logger.Info("🔄 Creating Gupshup client for refresh",
		zap.Uint("tenant_id", tenantID),
		zap.String("app_id", app.GupshupAppID))
	client := social.NewGupshupClient(h.Config, h.Logger)

	// Call GetAppDetails to fetch latest metadata
	h.Logger.Info("🔄 Calling GetAppDetails",
		zap.Uint("tenant_id", tenantID),
		zap.String("app_id", app.GupshupAppID),
		zap.String("gupshup_app_id", app.GupshupAppID))
	appDetails, err := client.GetAppDetails(app.GupshupAppID)
	if err != nil {
		h.Logger.Error("❌ GetAppDetails failed during refresh",
			zap.Uint("tenant_id", tenantID),
			zap.String("app_id", app.GupshupAppID),
			zap.Error(err))
	} else {
		h.Logger.Info("✅ GetAppDetails succeeded during refresh",
			zap.Uint("tenant_id", tenantID),
			zap.String("app_id", app.GupshupAppID),
			zap.Bool("has_app_details", appDetails != nil))
	}
	if err != nil {
		h.Logger.Error("failed to refresh app details",
			zap.String("app_id", app.GupshupAppID),
			zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "refresh_failed",
			"message": fmt.Sprintf("Failed to fetch app details: %v", err),
		})
		return
	}

	// Extract WhatsApp metadata from app details
	updated := false
	if whatsapp, ok := appDetails["whatsapp"].(map[string]interface{}); ok {
		// Update WABA ID
		if businessID, ok := whatsapp["businessId"].(string); ok && businessID != "" && app.WABAID != businessID {
			app.WABAID = businessID
			app.BusinessID = businessID
			updated = true
			h.Logger.Info("✅ Updated WABA ID from GetAppDetails",
				zap.String("app_id", app.GupshupAppID),
				zap.String("waba_id", businessID))
		}

		// Update phone number
		if phone, ok := whatsapp["formattedPhoneNumber"].(string); ok && phone != "" && app.PhoneNumber != phone {
			app.PhoneNumber = phone
			updated = true
			h.Logger.Info("✅ Updated phone number from GetAppDetails",
				zap.String("app_id", app.GupshupAppID),
				zap.String("phone", phone))
		} else if phone, ok := whatsapp["normalizedPhoneNumber"].(string); ok && phone != "" && app.PhoneNumber != phone {
			app.PhoneNumber = phone
			updated = true
			h.Logger.Info("✅ Updated phone number (normalized) from GetAppDetails",
				zap.String("app_id", app.GupshupAppID),
				zap.String("phone", phone))
		}

		// Update phone number ID
		if phoneNumberID, ok := whatsapp["phoneNumberId"].(string); ok && phoneNumberID != "" && app.PhoneNumberID != phoneNumberID {
			app.PhoneNumberID = phoneNumberID
			updated = true
		}

		// Update status to "live" if we have metadata
		if (app.WABAID != "" || app.PhoneNumber != "") && app.Status != "live" {
			app.Status = "live"
			updated = true
			h.Logger.Info("✅ Status updated to 'live' (has metadata)",
				zap.String("app_id", app.GupshupAppID),
				zap.String("waba_id", app.WABAID),
				zap.String("phone", app.PhoneNumber))
		}
	}

	// Check app status from response
	if status, ok := appDetails["status"].(string); ok {
		if status == "live" || status == "connected" || status == "approved" {
			if app.Status != "live" {
				app.Status = "live"
				updated = true
			}
		}
	}

	if updated {
		if err := h.DB.Save(&app).Error; err != nil {
			h.Logger.Error("failed to save updated app",
				zap.String("app_id", app.GupshupAppID),
				zap.Error(err))
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error":   "save_failed",
				"message": "Failed to save updated app details",
			})
			return
		}

		// Also update SocialAccount for compatibility
		var account storage.SocialAccount
		if err := h.DB.Where("tenant_id = ? AND platform = ?", tenantID, "whatsapp").First(&account).Error; err == nil {
			account.Username = app.PhoneNumber
			account.PageID = app.PhoneNumberID
			account.ExternalID = app.PhoneNumberID
			account.Connected = (app.Status == "live" && app.PhoneNumber != "")
			h.DB.Save(&account)
		}
	}

	// Return updated status
	hasMetadata := app.WABAID != "" || app.PhoneNumber != ""
	connected := app.Status == "live" && hasMetadata

	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"message":      "Status refreshed successfully",
		"connected":    connected,
		"provider":     "gupshup_partner",
		"phone_number": app.PhoneNumber,
		"status":       app.Status,
		"waba_id":      app.WABAID,
		"app_id":       app.GupshupAppID,
		"updated":      updated,
		"has_metadata": hasMetadata,
	})
}

// connectWhatsAppGupshup - Connect WhatsApp via Gupshup Partner
func (h *Handler) connectWhatsAppGupshup(w http.ResponseWriter, r *http.Request, tenantID uint) {
	if h.Config.GupshupPartnerUsername == "" || h.Config.GupshupPartnerPassword == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "gupshup_not_configured",
			"message": "Gupshup Partner is not configured. Please contact support.",
		})
		return
	}

	// Create Gupshup client
	client := social.NewGupshupClient(h.Config, h.Logger)

	// Check if app already exists for this tenant
	var existingApp storage.WhatsAppApp
	if err := h.DB.Where("tenant_id = ?", tenantID).First(&existingApp).Error; err == nil {
		// App exists, generate new embed link
		redirectURL := fmt.Sprintf("%s/api/webhooks/gupshup/redirect?tenant_id=%d", h.Config.PublicURL, tenantID)
		embedLink, err := client.GenerateEmbedSignedLink(existingApp.GupshupAppID, redirectURL)
		if err != nil {
			h.Logger.Error("failed to generate embed link", zap.Error(err))
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error":   "embed_link_failed",
				"message": "Failed to generate onboarding link. Please try again.",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"success":        true,
			"provider":       "gupshup_partner",
			"onboarding_url": embedLink,
			"message":        "Redirect to this URL to complete WhatsApp onboarding.",
			"app_id":         existingApp.GupshupAppID,
		})
		return
	}

	// Create new app for tenant
	// App name must be alphanumeric only (no special characters like hyphens)
	// Make it unique by including tenant ID and business name
	var tenant storage.Tenant
	var businessProfile storage.BusinessProfile
	var businessName string

	// Try to get tenant's business name
	if err := h.DB.First(&tenant, tenantID).Error; err == nil {
		// Try to get business profile name
		if err := h.DB.Where("tenant_id = ?", tenantID).First(&businessProfile).Error; err == nil && businessProfile.Name != "" {
			businessName = businessProfile.Name
		} else if tenant.Name != "" {
			businessName = tenant.Name
		}
	}

	// Sanitize business name (alphanumeric only)
	sanitizedName := sanitizeForAppName(businessName)
	if len(sanitizedName) > 15 {
		sanitizedName = sanitizedName[:15] // Truncate if too long
	}

	// Generate unique app name: Brancr{BusinessName}{TenantID}{RandomSuffix}
	// Format ensures uniqueness: business name + tenant ID + random 4-digit suffix
	randomSuffix := time.Now().Unix() % 10000 // Last 4 digits of timestamp for uniqueness
	var appName string
	if sanitizedName != "" {
		appName = fmt.Sprintf("Brancr%s%d%d", sanitizedName, tenantID, randomSuffix)
	} else {
		appName = fmt.Sprintf("BrancrTenant%d%d", tenantID, randomSuffix)
	}

	// Ensure minimum length (Gupshup requires 6 chars) and max (150 chars)
	if len(appName) < 6 {
		appName = appName + "12345"
		appName = appName[:6]
	}
	if len(appName) > 150 {
		appName = appName[:150] // Truncate if too long
	}

	appID, err := client.CreateApp(appName)
	if err != nil {
		h.Logger.Error("failed to create Gupshup app", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "app_creation_failed",
			"message": "Failed to create WhatsApp app. Please try again.",
		})
		return
	}

	// Save app to database
	app := storage.WhatsAppApp{
		TenantID:     tenantID,
		GupshupAppID: appID,
		Status:       "pending_onboarding",
		Mode:         "gupshup_partner",
	}
	if err := h.DB.Create(&app).Error; err != nil {
		h.Logger.Error("failed to save WhatsAppApp", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "database_error",
			"message": "Failed to save app details. Please try again.",
		})
		return
	}

	// Step 3: Immediately configure webhook subscription for account events
	// This allows us to receive notifications when WABA is approved/phone is verified
	// CRITICAL: Subscribe to account-event to receive Go-Live (onboarding-event) notifications
	webhookURL := fmt.Sprintf("%s/api/webhooks/gupshup", h.Config.PublicURL)
	// Subscribe to: message (incoming messages), message-event (delivery/read status), account-event (WABA status changes), and onboarding-event (Go-Live)
	// Using modes="ALL" ensures we receive all events including onboarding-event
	subID, err := client.SetSubscription(appID, webhookURL, []string{"message", "message-event", "account-event", "onboarding-event"})
	if err != nil {
		h.Logger.Warn("⚠️ Failed to set webhook subscription immediately after app creation",
			zap.String("app_id", appID),
			zap.String("webhook_url", webhookURL),
			zap.Error(err))
		// Don't fail - we can set subscription later via redirect callback
	} else {
		app.SubscriptionID = subID
		app.SubscriptionActive = true
		app.WebhookURL = webhookURL
		if err := h.DB.Save(&app).Error; err != nil {
			h.Logger.Warn("failed to save subscription details", zap.Error(err))
		} else {
			h.Logger.Info("✅ Webhook subscription configured immediately after app creation",
				zap.String("app_id", appID),
				zap.String("subscription_id", subID),
				zap.String("webhook_url", webhookURL))
		}
	}

	// Step 4: Generate embed signup link
	// Add popup=true parameter so redirect handler knows to use postMessage
	redirectURL := fmt.Sprintf("%s/api/webhooks/gupshup/redirect?tenant_id=%d&popup=true", h.Config.PublicURL, tenantID)
	embedLink, err := client.GenerateEmbedSignedLink(appID, redirectURL)
	if err != nil {
		h.Logger.Error("failed to generate embed link", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "embed_link_failed",
			"message": "Failed to generate onboarding link. Please try again.",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":        true,
		"provider":       "gupshup_partner",
		"onboarding_url": embedLink,
		"message":        "Redirect to this URL to complete WhatsApp onboarding.",
		"app_id":         appID,
	})
}

// connectWhatsAppMetaEmbedded - Connect WhatsApp via Meta Embedded Signup
func (h *Handler) connectWhatsAppMetaEmbedded(w http.ResponseWriter, r *http.Request, tenantID uint) {
	if h.Config.MetaAppID == "" || h.Config.MetaEmbeddedConfigID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "meta_not_configured",
			"message": "Meta Embedded Signup is not configured. Please contact support.",
		})
		return
	}

	// Generate Embedded Signup URL
	baseURL := h.Config.PublicURL
	if baseURL == "" {
		baseURL = "https://api.brancr.com"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	signupURL := fmt.Sprintf("%s/api/oauth/meta/whatsapp/start?tenant_id=%d", baseURL, tenantID)

	writeJSON(w, http.StatusOK, map[string]any{
		"success":        true,
		"provider":       "meta_embedded",
		"onboarding_url": signupURL,
		"message":        "Redirect to Meta Embedded Signup to connect your WhatsApp Business Account.",
		"note":           "After completing onboarding, your WhatsApp will be automatically connected. You can check status using the connection-status endpoint.",
	})
}

// ====================
// INTENT CATALOG CRUD
// ====================

// listIntentCatalog lists all intent catalog entries for the tenant
func (h *Handler) listIntentCatalog(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var intents []storage.IntentCatalog
	if err := h.DB.Where("tenant_id = ?", claims.TenantID).
		Order("name ASC").
		Find(&intents).Error; err != nil {
		h.Logger.Error("failed to fetch intent catalog", zap.Error(err))
		http.Error(w, "failed to fetch intent catalog", http.StatusInternalServerError)
		return
	}

	type intentResp struct {
		ID        uint      `json:"id"`
		Name      string    `json:"name"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}

	resp := make([]intentResp, len(intents))
	for i, intent := range intents {
		resp[i] = intentResp{
			ID:        intent.ID,
			Name:      intent.Name,
			CreatedAt: intent.CreatedAt,
			UpdatedAt: intent.UpdatedAt,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"intents": resp,
	})
}

// createIntentCatalog creates a new intent catalog entry
func (h *Handler) createIntentCatalog(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Name string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	// Check if intent with same name already exists for this tenant
	var existing storage.IntentCatalog
	if err := h.DB.Where("tenant_id = ? AND name = ?", claims.TenantID, req.Name).
		First(&existing).Error; err == nil {
		http.Error(w, "intent with this name already exists", http.StatusConflict)
		return
	}

	intent := storage.IntentCatalog{
		TenantID: claims.TenantID,
		Name:     req.Name,
	}

	if err := h.DB.Create(&intent).Error; err != nil {
		h.Logger.Error("failed to create intent catalog", zap.Error(err))
		http.Error(w, "failed to create intent", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"success": true,
		"message": "Intent catalog entry created successfully",
		"intent": map[string]any{
			"id":         intent.ID,
			"name":       intent.Name,
			"created_at": intent.CreatedAt,
			"updated_at": intent.UpdatedAt,
		},
	})
}

// updateIntentCatalog updates an existing intent catalog entry
func (h *Handler) updateIntentCatalog(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	var id uint
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		http.Error(w, "invalid intent id", http.StatusBadRequest)
		return
	}

	var req struct {
		Name string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	// Find intent and verify ownership
	var intent storage.IntentCatalog
	if err := h.DB.Where("id = ? AND tenant_id = ?", id, claims.TenantID).First(&intent).Error; err != nil {
		http.Error(w, "intent catalog entry not found", http.StatusNotFound)
		return
	}

	// Check if another intent with same name exists
	var existing storage.IntentCatalog
	if err := h.DB.Where("tenant_id = ? AND name = ? AND id != ?", claims.TenantID, req.Name, id).
		First(&existing).Error; err == nil {
		http.Error(w, "intent with this name already exists", http.StatusConflict)
		return
	}

	intent.Name = req.Name
	if err := h.DB.Save(&intent).Error; err != nil {
		h.Logger.Error("failed to update intent catalog", zap.Error(err))
		http.Error(w, "failed to update intent", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Intent catalog entry updated successfully",
		"intent": map[string]any{
			"id":         intent.ID,
			"name":       intent.Name,
			"created_at": intent.CreatedAt,
			"updated_at": intent.UpdatedAt,
		},
	})
}

// deleteIntentCatalog deletes an intent catalog entry
func (h *Handler) deleteIntentCatalog(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	var id uint
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		http.Error(w, "invalid intent id", http.StatusBadRequest)
		return
	}

	// Find intent and verify ownership
	var intent storage.IntentCatalog
	if err := h.DB.Where("id = ? AND tenant_id = ?", id, claims.TenantID).First(&intent).Error; err != nil {
		http.Error(w, "intent catalog entry not found", http.StatusNotFound)
		return
	}

	if err := h.DB.Delete(&intent).Error; err != nil {
		h.Logger.Error("failed to delete intent catalog", zap.Error(err))
		http.Error(w, "failed to delete intent", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Intent catalog entry deleted successfully",
	})
}

// ✅ TIKTOK VIDEO MANAGEMENT ENDPOINTS

// listTikTokVideos lists all published TikTok videos for the tenant
func (h *Handler) listTikTokVideos(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Get TikTok account
	var acc storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ?", claims.TenantID, "tiktok").First(&acc).Error; err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "TikTok account not connected. Please connect your TikTok account first.",
		})
		return
	}

	// Get TikTok connector
	connector, ok := social.Connectors["tiktok"]
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "TikTok connector not available",
		})
		return
	}

	tikTokConnector, ok := connector.(*social.TikTokConnector)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "TikTok connector type assertion failed",
		})
		return
	}

	// Parse query parameters
	maxCount := 20
	if mc := r.URL.Query().Get("max_count"); mc != "" {
		if parsed, err := strconv.Atoi(mc); err == nil && parsed > 0 && parsed <= 50 {
			maxCount = parsed
		}
	}
	cursor := r.URL.Query().Get("cursor")

	// Decrypt token for connector
	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		decryptedToken = acc.AccessToken
	}
	acc.AccessToken = decryptedToken

	// List videos
	videos, err := tikTokConnector.ListVideos(r.Context(), &acc, maxCount, cursor)
	if err != nil {
		// Extract detailed error information if available
		errorMsg := err.Error()
		errorDetails := map[string]any{
			"error": errorMsg,
		}

		// Try to extract enhanced error details
		if enhancedErr, ok := err.(*social.EnhancedTikTokError); ok {
			errorDetails["code"] = enhancedErr.Code
			errorDetails["type"] = string(enhancedErr.Type)
			errorDetails["log_id"] = enhancedErr.LogID
			if enhancedErr.Message != enhancedErr.UserMessage {
				errorDetails["details"] = enhancedErr.Message
			}
		}

		h.Logger.Error("failed to list TikTok videos",
			zap.Error(err),
			zap.Uint("tenant_id", claims.TenantID),
			zap.Any("error_details", errorDetails))

		writeJSON(w, http.StatusInternalServerError, errorDetails)
		return
	}

	writeJSON(w, http.StatusOK, videos)
}

// getTikTokVideo gets details of a specific TikTok video
func (h *Handler) getTikTokVideo(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	videoID := chi.URLParam(r, "video_id")
	if videoID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "video_id is required",
		})
		return
	}

	// Get TikTok account
	var acc storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ?", claims.TenantID, "tiktok").First(&acc).Error; err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "TikTok account not connected",
		})
		return
	}

	// Get TikTok connector
	connector, ok := social.Connectors["tiktok"]
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "TikTok connector not available",
		})
		return
	}

	tikTokConnector, ok := connector.(*social.TikTokConnector)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "TikTok connector type assertion failed",
		})
		return
	}

	// Decrypt token
	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		decryptedToken = acc.AccessToken
	}
	acc.AccessToken = decryptedToken

	// Get video analytics (includes video details)
	analytics, err := tikTokConnector.GetVideoAnalytics(r.Context(), &acc, videoID)
	if err != nil {
		h.Logger.Error("failed to get TikTok video", zap.Error(err), zap.String("video_id", videoID))
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": fmt.Sprintf("Failed to get video: %v", err),
		})
		return
	}

	writeJSON(w, http.StatusOK, analytics)
}

// getTikTokVideoStatus checks the publish status of a video
func (h *Handler) getTikTokVideoStatus(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	publishID := chi.URLParam(r, "publish_id")
	if publishID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "publish_id is required",
		})
		return
	}

	// Get TikTok account
	var acc storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ?", claims.TenantID, "tiktok").First(&acc).Error; err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "TikTok account not connected",
		})
		return
	}

	// Get TikTok connector
	connector, ok := social.Connectors["tiktok"]
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "TikTok connector not available",
		})
		return
	}

	tikTokConnector, ok := connector.(*social.TikTokConnector)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "TikTok connector type assertion failed",
		})
		return
	}

	// Decrypt token
	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		decryptedToken = acc.AccessToken
	}
	acc.AccessToken = decryptedToken

	// Check status
	status, err := tikTokConnector.CheckVideoStatus(r.Context(), &acc, publishID)
	if err != nil {
		h.Logger.Error("failed to check TikTok video status", zap.Error(err), zap.String("publish_id", publishID))
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": fmt.Sprintf("Failed to check status: %v", err),
		})
		return
	}

	writeJSON(w, http.StatusOK, status)
}

// deleteTikTokVideo deletes a TikTok video
func (h *Handler) deleteTikTokVideo(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	videoID := chi.URLParam(r, "video_id")
	if videoID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "video_id is required",
		})
		return
	}

	// Get TikTok account
	var acc storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ?", claims.TenantID, "tiktok").First(&acc).Error; err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "TikTok account not connected",
		})
		return
	}

	// Get TikTok connector
	connector, ok := social.Connectors["tiktok"]
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "TikTok connector not available",
		})
		return
	}

	tikTokConnector, ok := connector.(*social.TikTokConnector)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "TikTok connector type assertion failed",
		})
		return
	}

	// Decrypt token
	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		decryptedToken = acc.AccessToken
	}
	acc.AccessToken = decryptedToken

	// Delete video
	if err := tikTokConnector.DeleteVideo(r.Context(), &acc, videoID); err != nil {
		h.Logger.Error("failed to delete TikTok video", zap.Error(err), zap.String("video_id", videoID))
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": fmt.Sprintf("Failed to delete video: %v", err),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message":  "Video deleted successfully",
		"video_id": videoID,
	})
}

// getTikTokVideoAnalytics - REMOVED: TikTok analytics functionality disabled
// This endpoint has been removed. TikTok analytics collection is no longer supported.
func (h *Handler) getTikTokVideoAnalytics(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusGone, map[string]any{
		"error":   "TikTok analytics functionality has been removed",
		"message": "TikTok analytics collection is no longer supported",
	})
}

// getTikTokAnalytics - REMOVED: TikTok analytics functionality disabled
// This endpoint has been removed. TikTok analytics collection is no longer supported.
func (h *Handler) getTikTokAnalytics(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusGone, map[string]any{
		"error":   "TikTok analytics functionality has been removed",
		"message": "TikTok analytics collection is no longer supported",
	})
}

// listTikTokComments lists comments for a TikTok video
func (h *Handler) listTikTokComments(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	videoID := chi.URLParam(r, "video_id")
	if videoID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "video_id is required",
		})
		return
	}

	// Get TikTok account
	var acc storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ?", claims.TenantID, "tiktok").First(&acc).Error; err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "TikTok account not connected",
		})
		return
	}

	// Get TikTok connector
	connector, ok := social.Connectors["tiktok"]
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "TikTok connector not available",
		})
		return
	}

	tikTokConnector, ok := connector.(*social.TikTokConnector)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "TikTok connector type assertion failed",
		})
		return
	}

	// Decrypt token
	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		decryptedToken = acc.AccessToken
	}
	acc.AccessToken = decryptedToken

	// Parse query parameters
	maxCount := 20
	if mc := r.URL.Query().Get("max_count"); mc != "" {
		if parsed, err := strconv.Atoi(mc); err == nil && parsed > 0 && parsed <= 50 {
			maxCount = parsed
		}
	}
	cursor := r.URL.Query().Get("cursor")

	// List comments
	comments, err := tikTokConnector.ListComments(r.Context(), &acc, videoID, maxCount, cursor)
	if err != nil {
		h.Logger.Error("failed to list TikTok comments", zap.Error(err), zap.String("video_id", videoID))
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": fmt.Sprintf("Failed to list comments: %v", err),
		})
		return
	}

	writeJSON(w, http.StatusOK, comments)
}

// replyToTikTokComment replies to a TikTok comment
func (h *Handler) replyToTikTokComment(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	videoID := chi.URLParam(r, "video_id")
	commentID := chi.URLParam(r, "comment_id")
	if videoID == "" || commentID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "video_id and comment_id are required",
		})
		return
	}

	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Text == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "text is required",
		})
		return
	}

	// Get TikTok account
	var acc storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ?", claims.TenantID, "tiktok").First(&acc).Error; err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "TikTok account not connected",
		})
		return
	}

	// Get TikTok connector
	connector, ok := social.Connectors["tiktok"]
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "TikTok connector not available",
		})
		return
	}

	tikTokConnector, ok := connector.(*social.TikTokConnector)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "TikTok connector type assertion failed",
		})
		return
	}

	// Decrypt token
	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		decryptedToken = acc.AccessToken
	}
	acc.AccessToken = decryptedToken

	// Reply to comment
	replyID, err := tikTokConnector.ReplyToComment(r.Context(), &acc, videoID, commentID, req.Text)
	if err != nil {
		h.Logger.Error("failed to reply to TikTok comment", zap.Error(err), zap.String("video_id", videoID), zap.String("comment_id", commentID))
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": fmt.Sprintf("Failed to reply: %v", err),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message":    "Reply sent successfully",
		"reply_id":   replyID,
		"video_id":   videoID,
		"comment_id": commentID,
	})
}

// generateRandomString generates a cryptographically secure random alphanumeric string
// Used to ensure app names are unique and never collide, even across different tenants
// Length should be 6-12 characters for good uniqueness without making names too long
func generateRandomString(length int) string {
	const charset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based if crypto/rand fails (extremely rare)
		// Use nanosecond timestamp as hex string
		fallback := fmt.Sprintf("%x", time.Now().UnixNano())
		if len(fallback) > length {
			return fallback[:length]
		}
		return fallback + strings.Repeat("0", length-len(fallback))
	}
	for i, b := range bytes {
		bytes[i] = charset[b%byte(len(charset))]
	}
	return string(bytes)
}

// =======================
// INSTAGRAM INSIGHTS API
// =======================

// getInstagramAccountInsights fetches account-level insights for Instagram
// Query params: metrics (comma-separated), period (day, week, days_28, lifetime)
func (h *Handler) getInstagramAccountInsights(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if h.InstagramInsights == nil {
		http.Error(w, "Instagram insights service not available", http.StatusServiceUnavailable)
		return
	}

	// Get Instagram account
	var account storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ? AND connected = ?", claims.TenantID, "instagram", true).
		First(&account).Error; err != nil {
		http.Error(w, "Instagram account not connected", http.StatusNotFound)
		return
	}

	// Parse query parameters
	metricsParam := r.URL.Query().Get("metrics")
	if metricsParam == "" {
		// Default metrics depend on login method:
		// - Instagram Login: "reach,profile_views,follower_count"
		// - Facebook Login: "reach,follower_count,profile_views"
		// NOTE: "impressions" is NOT valid for account insights (only for media insights)
		if account.LoginMethod == "instagram_login" {
			// Instagram Login API - use valid account-level metrics
			// NOTE: "impressions" is NOT valid for account insights (only for media)
			metricsParam = "reach,profile_views,follower_count"
		} else {
			// Facebook Login API - use valid account-level metrics
			// NOTE: "impressions" is NOT valid for account insights (only for media)
			metricsParam = "reach,follower_count,profile_views"
		}
	}
	metrics := strings.Split(metricsParam, ",")
	for i := range metrics {
		metrics[i] = strings.TrimSpace(metrics[i])
	}

	// Filter out invalid metrics automatically
	// "impressions" is NOT valid for account insights (only for media insights)
	invalidAccountMetrics := map[string]bool{
		"impressions": true, // Only valid for media insights, not account insights
	}

	filteredMetrics := make([]string, 0, len(metrics))
	for _, m := range metrics {
		if m != "" && !invalidAccountMetrics[m] {
			filteredMetrics = append(filteredMetrics, m)
		} else if invalidAccountMetrics[m] {
			h.Logger.Warn("removed invalid metric from account insights request (impressions is only for media insights)",
				zap.String("metric", m),
				zap.String("login_method", account.LoginMethod))
		}
	}

	// If all metrics were filtered out, use defaults
	if len(filteredMetrics) == 0 {
		if account.LoginMethod == "instagram_login" {
			filteredMetrics = []string{"reach", "profile_views", "follower_count"}
		} else {
			filteredMetrics = []string{"reach", "follower_count", "profile_views"}
		}
		h.Logger.Info("all requested metrics were deprecated, using defaults",
			zap.Strings("default_metrics", filteredMetrics))
	}

	metrics = filteredMetrics

	period := r.URL.Query().Get("period")
	if period == "" {
		period = "day" // Default period
	}

	// Fetch insights
	ctx := r.Context()
	insights, err := h.InstagramInsights.GetAccountInsights(ctx, &account, metrics, period)
	if err != nil {
		h.Logger.Error("failed to fetch Instagram account insights", zap.Error(err))
		http.Error(w, fmt.Sprintf("failed to fetch insights: %v", err), http.StatusInternalServerError)
		return
	}

	// Optionally save to database
	saveParam := r.URL.Query().Get("save")
	if saveParam == "true" {
		igAccountID := account.ExternalID
		if igAccountID == "" {
			igAccountID = account.PageID
		}
		if err := h.InstagramInsights.SaveAccountInsights(ctx, claims.TenantID, igAccountID, insights); err != nil {
			h.Logger.Warn("failed to save account insights", zap.Error(err))
			// Continue - don't fail the request
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"insights": insights.Data,
		"account_id": func() string {
			if account.ExternalID != "" {
				return account.ExternalID
			}
			return account.PageID
		}(),
		"period": period,
	})
}

// getInstagramMediaInsights fetches media-level insights for a specific Instagram post
// Query params: metrics (comma-separated), period (day, week, days_28, lifetime)
func (h *Handler) getInstagramMediaInsights(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if h.InstagramInsights == nil {
		http.Error(w, "Instagram insights service not available", http.StatusServiceUnavailable)
		return
	}

	mediaID := chi.URLParam(r, "mediaId")
	if mediaID == "" {
		http.Error(w, "media ID required", http.StatusBadRequest)
		return
	}

	// Get Instagram account
	var account storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ? AND connected = ?", claims.TenantID, "instagram", true).
		First(&account).Error; err != nil {
		http.Error(w, "Instagram account not connected", http.StatusNotFound)
		return
	}

	// Parse query parameters
	metricsParam := r.URL.Query().Get("metrics")
	if metricsParam == "" {
		// Valid media-level metrics: likes, comments, shares, saves, profile_links_taps,
		// views, impressions, reach (for media), threads_likes, threads_replies, etc.
		// NOTE: "engagement" is NOT valid - use likes, comments, shares, saves instead
		metricsParam = "likes,comments,shares,saves" // Default media metrics
	}
	metrics := strings.Split(metricsParam, ",")
	for i := range metrics {
		metrics[i] = strings.TrimSpace(metrics[i])
	}

	period := r.URL.Query().Get("period")
	if period == "" {
		period = "lifetime" // Default period for media
	}

	// Fetch insights
	ctx := r.Context()
	insights, err := h.InstagramInsights.GetMediaInsights(ctx, &account, mediaID, metrics, period)
	if err != nil {
		h.Logger.Error("failed to fetch Instagram media insights", zap.Error(err))
		http.Error(w, fmt.Sprintf("failed to fetch insights: %v", err), http.StatusInternalServerError)
		return
	}

	// Optionally save to database
	saveParam := r.URL.Query().Get("save")
	if saveParam == "true" {
		if err := h.InstagramInsights.SaveMediaInsights(ctx, claims.TenantID, mediaID, insights); err != nil {
			h.Logger.Warn("failed to save media insights", zap.Error(err))
			// Continue - don't fail the request
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"insights": insights.Data,
		"media_id": mediaID,
		"period":   period,
	})
}
