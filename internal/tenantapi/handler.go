package tenantapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"strconv"

	"github.com/go-chi/chi/v5"
	redis "github.com/redis/go-redis/v9"
	"github.com/similadayo/brancr/internal/config"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/social"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	SessionCookie = "brancr_tenant_session"
	sessionTTL    = 7 * 24 * time.Hour
)

type Handler struct {
	DB             *gorm.DB
	Logger         *zap.Logger
	Config         *config.Config
	Redis          *redis.Client
	GupshupPartner *social.GupshupPartnerClient

	signupCache sync.Map
	tokenCache  sync.Map
}

func NewHandler(db *gorm.DB, logger *zap.Logger, cfg *config.Config, redis *redis.Client, gupshupPartner *social.GupshupPartnerClient) *Handler {
	return &Handler{
		DB:             db,
		Logger:         logger,
		Config:         cfg,
		Redis:          redis,
		GupshupPartner: gupshupPartner,
	}
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	// Public OAuth (start/callback)
	r.Route("/oauth", func(r chi.Router) {
		r.Get("/{platform}/start", h.oauthStart)
		r.Get("/{platform}/callback", h.oauthCallback)
	})

	// Public Gupshup Partner callback (no auth required - Gupshup redirects here)
	r.Get("/tenant/whatsapp/callback", h.handleWhatsAppGupshupCallback)

	r.Route("/auth", func(r chi.Router) {
		r.Post("/signup", h.signup)
		r.Post("/login", h.login)
		r.Post("/logout", h.logout)
		r.Group(func(r chi.Router) {
			r.Use(middleware.TenantAuth([]byte(h.Config.TenantJWTSecret), SessionCookie))
			r.Get("/me", h.me)
		})
	})

	r.Group(func(r chi.Router) {
		r.Use(middleware.TenantAuth([]byte(h.Config.TenantJWTSecret), SessionCookie))

		// Onboarding endpoints
		r.Get("/tenant/onboarding/status", h.getOnboardingStatus)
		r.Post("/tenant/onboarding/business-profile", h.updateBusinessProfile)
		r.Post("/tenant/onboarding/persona", h.updatePersona)
		r.Post("/tenant/onboarding/business-details", h.updateBusinessDetails)
		r.Post("/tenant/onboarding/complete", h.completeOnboarding)
		// Post-onboarding settings
		r.Put("/tenant/settings/business-profile", h.updateBusinessProfileSettings)
		r.Put("/tenant/settings/persona", h.updatePersonaSettings)

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

		// Scheduled posts CRUD
		r.Get("/tenant/scheduled-posts", h.listScheduledPosts)
		r.Get("/tenant/scheduled-posts/{id}", h.getScheduledPost)
		r.Put("/tenant/scheduled-posts/{id}", h.updateScheduledPost)
		r.Delete("/tenant/scheduled-posts/{id}", h.deleteScheduledPost)
		// Calendar range view
		r.Get("/tenant/calendar", h.getCalendar)

		// Overview and stats
		r.Get("/tenant/overview", h.overview)

		// WhatsApp provider-owned model (new)
		r.Get("/tenant/whatsapp/numbers", h.listAvailablePhoneNumbers)
		r.Post("/tenant/whatsapp/numbers/{id}/assign", h.assignPhoneNumber)
		r.Post("/tenant/whatsapp/select-number", h.selectWhatsAppNumber)
		r.Get("/tenant/whatsapp/current", h.getAssignedPhoneNumber)
		r.Post("/tenant/whatsapp/disconnect", h.disconnectPhoneNumber)
		r.Get("/tenant/whatsapp/webhook-status", h.checkWebhookStatus)
		r.Post("/tenant/whatsapp/resubscribe", h.resubscribeWebhook)
		r.Get("/tenant/whatsapp/diagnostics", h.comprehensiveDiagnostics)

		// Tenant-provided phone numbers (for Gupshup/Meta direct)
		r.Post("/tenant/whatsapp/request-number", h.requestTenantPhoneNumber)
		r.Post("/tenant/whatsapp/verify-number", h.verifyTenantPhoneNumber)
		r.Post("/tenant/whatsapp/check-number", h.checkTenantPhoneNumber) // Check if number is ready to verify

	// New simplified WhatsApp connection (Respond.io/Gupshup Partner)
	r.Post("/tenant/whatsapp/connect", h.connectWhatsApp)
	r.Get("/tenant/whatsapp/connection-status", h.getWhatsAppConnectionStatus)

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

		// Analytics API
		r.Get("/tenant/analytics", h.getAnalytics)

		// Media Library
		r.Get("/tenant/media", h.listMedia)
		r.Get("/tenant/media/{id}", h.getMedia)
		r.Post("/tenant/media", h.createMedia)
		r.Put("/tenant/media/{id}", h.updateMedia)
		r.Delete("/tenant/media/{id}", h.deleteMedia)

		// Post creation
		r.Post("/tenant/posts", h.createPost)
		r.Post("/tenant/posts/generate-caption", h.generateCaption)
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
	})
}

// OAuth start: placeholder URL generation
func (h *Handler) oauthStart(w http.ResponseWriter, r *http.Request) {
	platform := chi.URLParam(r, "platform")
	tenantID := r.URL.Query().Get("tenant_id")
	success := r.URL.Query().Get("success_redirect")
	if platform == "" || tenantID == "" {
		http.Error(w, "platform and tenant_id required", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"platform":         platform,
		"auth_url":         fmt.Sprintf("https://auth.%s.brancr.com/start?tenant_id=%s", platform, url.QueryEscape(tenantID)),
		"success_redirect": success,
	})
}

// OAuth callback: stub
func (h *Handler) oauthCallback(w http.ResponseWriter, r *http.Request) {
	platform := chi.URLParam(r, "platform")
	code := r.URL.Query().Get("code")
	if platform == "" || code == "" {
		http.Error(w, "platform and code required", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"platform": platform,
		"message":  "OAuth callback received",
	})
}

type signupRequest struct {
	Name        string `json:"name"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	CompanyName string `json:"company_name"`
	Phone       string `json:"phone"`
}

func (h *Handler) signup(w http.ResponseWriter, r *http.Request) {
	var req signupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.Name = strings.TrimSpace(req.Name)
	req.CompanyName = strings.TrimSpace(req.CompanyName)

	if req.Email == "" || req.Password == "" || req.CompanyName == "" {
		http.Error(w, "missing required fields", http.StatusBadRequest)
		return
	}

	var existing int64
	h.DB.Model(&storage.Tenant{}).Where("email = ?", req.Email).Count(&existing)
	if existing > 0 {
		http.Error(w, "email already registered", http.StatusConflict)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		h.Logger.Error("bcrypt error", zap.Error(err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	tenant := storage.Tenant{
		Name:               req.CompanyName,
		Email:              req.Email,
		OwnerName:          req.Name,
		OwnerPhone:         strings.TrimSpace(req.Phone),
		PlanType:           "trial",
		Status:             "active",
		PasswordHash:       string(hash),
		OnboardingStep:     "business_profile",
		OnboardingComplete: false,
	}

	if err := h.DB.Create(&tenant).Error; err != nil {
		h.Logger.Error("create tenant", zap.Error(err))
		http.Error(w, "could not create tenant", http.StatusInternalServerError)
		return
	}

	// Create initial business profile
	profile := storage.BusinessProfile{
		TenantID: tenant.ID,
		Name:     req.CompanyName,
		Tone:     "friendly",
	}
	h.DB.Create(&profile)

	// Create initial persona
	persona := storage.Persona{
		TenantID:   tenant.ID,
		BotName:    "Assistant",
		Tone:       "friendly, helpful",
		Language:   "en",
		Humor:      true,
		StyleNotes: "",
	}
	h.DB.Create(&persona)

	if err := h.setSessionCookie(w, tenant.ID, tenant.Email); err != nil {
		h.Logger.Error("issue session cookie", zap.Error(err))
		http.Error(w, "login failed", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"tenant_id": tenant.ID,
		"name":      tenant.Name,
		"email":     tenant.Email,
	})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	var tenant storage.Tenant
	if err := h.DB.Where("LOWER(email) = ?", req.Email).First(&tenant).Error; err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(tenant.PasswordHash), []byte(req.Password)); err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	if err := h.setSessionCookie(w, tenant.ID, tenant.Email); err != nil {
		h.Logger.Error("issue session cookie", zap.Error(err))
		http.Error(w, "login failed", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": tenant.ID,
		"name":      tenant.Name,
		"email":     tenant.Email,
	})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	cookie := &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Domain:   h.cookieDomain(),
		HttpOnly: true,
		SameSite: http.SameSiteNoneMode,
		Secure:   true,
	}
	http.SetCookie(w, cookie)
	writeJSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var tenant storage.Tenant
	if err := h.DB.First(&tenant, claims.TenantID).Error; err != nil {
		http.Error(w, "tenant not found", http.StatusUnauthorized)
		return
	}

	// Get onboarding status
	var onboardingStatus struct {
		Step     string `json:"step"`
		Complete bool   `json:"complete"`
	}
	onboardingStatus.Step = tenant.OnboardingStep
	onboardingStatus.Complete = tenant.OnboardingComplete

	// Get social accounts summary
	var accounts []storage.SocialAccount
	h.DB.Where("tenant_id = ?", claims.TenantID).Find(&accounts)

	connectedCount := 0
	platforms := make(map[string]bool)
	for _, acc := range accounts {
		if acc.Connected {
			connectedCount++
		}
		platforms[acc.Platform] = true
	}

	// Get scheduled posts count
	var scheduledCount, postedCount int64
	h.DB.Model(&storage.ScheduledPost{}).
		Where("tenant_id = ?", claims.TenantID).
		Count(&scheduledCount)
	h.DB.Model(&storage.ScheduledPost{}).
		Where("tenant_id = ? AND status = ?", claims.TenantID, "posted").
		Count(&postedCount)

	platformList := make([]string, 0, len(platforms))
	for p := range platforms {
		platformList = append(platformList, p)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id":   tenant.ID,
		"name":        tenant.Name,
		"email":       tenant.Email,
		"plan":        tenant.PlanType,
		"status":      tenant.Status,
		"owner_name":  tenant.OwnerName,
		"owner_phone": tenant.OwnerPhone,
		"onboarding":  onboardingStatus,
		"integrations": map[string]any{
			"total":     len(accounts),
			"connected": connectedCount,
			"platforms": platformList,
		},
		"scheduled_posts": map[string]any{
			"total":  scheduledCount,
			"posted": postedCount,
		},
		"created_at": tenant.CreatedAt,
		"updated_at": tenant.UpdatedAt,
	})
}

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

	resp := make([]integration, 0, len(accounts))
	for _, acc := range accounts {
		resp = append(resp, integration{
			ID:         acc.ID,
			Platform:   acc.Platform,
			Connected:  acc.Connected,
			Username:   acc.Username,
			ExternalID: acc.ExternalID,
			PageID:     acc.PageID,
			Mode:       acc.Mode,
			ExpiresAt:  acc.ExpiresAt,
			CreatedAt:  acc.CreatedAt,
			UpdatedAt:  acc.UpdatedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"integrations": resp,
	})
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

func (h *Handler) listScheduledPosts(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var posts []storage.ScheduledPost
	if err := h.DB.Where("tenant_id = ?", claims.TenantID).
		Order("scheduled_at ASC, created_at ASC").
		Limit(100).
		Find(&posts).Error; err != nil {
		h.Logger.Error("fetch scheduled posts", zap.Error(err))
		http.Error(w, "could not fetch scheduled posts", http.StatusInternalServerError)
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

	writeJSON(w, http.StatusOK, map[string]any{
		"scheduled_posts": resp,
	})
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, tenantID uint, email string) error {
	token, err := middleware.CreateTenantToken([]byte(h.Config.TenantJWTSecret), tenantID, email, sessionTTL)
	if err != nil {
		return err
	}

	cookie := &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(sessionTTL),
		Domain:   h.cookieDomain(),
		HttpOnly: true,
		SameSite: http.SameSiteNoneMode,
		Secure:   true,
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
		Connected:  account.Connected,
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

	// Soft delete by setting Connected to false and clearing sensitive data
	account.Connected = false
	account.AccessToken = ""
	account.RefreshToken = ""
	account.ExternalID = ""
	account.PageID = ""

	if err := h.DB.Save(&account).Error; err != nil {
		h.Logger.Error("failed to disconnect integration", zap.Error(err), zap.String("platform", platform))
		http.Error(w, "failed to disconnect", http.StatusInternalServerError)
		return
	}

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
	if err := h.DB.Where("tenant_id = ? AND platform = ?", claims.TenantID, platform).First(&account).Error; err != nil {
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

	// Log verification attempt for debugging
	h.Logger.Info("verifying integration",
		zap.Uint("tenant_id", claims.TenantID),
		zap.String("platform", platform),
		zap.String("mode", account.Mode),
		zap.Bool("connected", account.Connected))

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
			hint = "Your integration is missing required information. Please reconnect your account via OAuth."
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

func (h *Handler) getScheduledPost(w http.ResponseWriter, r *http.Request) {
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

	var post storage.ScheduledPost
	if err := h.DB.Where("tenant_id = ? AND id = ?", claims.TenantID, postID).First(&post).Error; err != nil {
		http.Error(w, "scheduled post not found", http.StatusNotFound)
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

func (h *Handler) deleteScheduledPost(w http.ResponseWriter, r *http.Request) {
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

	var post storage.ScheduledPost
	if err := h.DB.Where("tenant_id = ? AND id = ?", claims.TenantID, postID).First(&post).Error; err != nil {
		http.Error(w, "scheduled post not found", http.StatusNotFound)
		return
	}

	// Only allow cancellation of posts that haven't been posted yet
	if post.Status == "posted" {
		http.Error(w, "cannot delete already posted content", http.StatusBadRequest)
		return
	}

	// Mark as cancelled instead of deleting (soft delete)
	post.Status = "cancelled"
	if err := h.DB.Save(&post).Error; err != nil {
		h.Logger.Error("failed to cancel scheduled post", zap.Error(err), zap.Uint("post_id", post.ID))
		http.Error(w, "failed to cancel post", http.StatusInternalServerError)
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

	// Step 1: Disconnect SocialAccount (for Respond.io/Gupshup connections)
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
			"success": true,
			"message": "WhatsApp disconnected successfully",
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
			"quality_rating":            metaStatus["quality_rating"],
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
		"status":  map[bool]string{true: "healthy", false: "unhealthy"}[len(errors) == 0],
		"errors":  errors,
		"warnings": warnings,
		"ready":   len(errors) == 0 && phoneSubscribed,
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
			"phone_number":     phoneNum.PhoneNumber,
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
		"success": true,
		"message": "Verification code received. Admin will verify it with Meta and complete the setup. You'll be notified once your number is ready.",
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

	// Get business profile and persona
	var profile storage.BusinessProfile
	h.DB.Where("tenant_id = ?", tenant.ID).First(&profile)

	var persona storage.Persona
	h.DB.Where("tenant_id = ?", tenant.ID).First(&persona)

	writeJSON(w, http.StatusOK, map[string]any{
		"step":             tenant.OnboardingStep,
		"complete":         tenant.OnboardingComplete,
		"business_profile": profile,
		"persona":          persona,
		"has_telegram_bot": tenant.TelegramBotToken != "",
	})
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
	if err := h.DB.Save(&post).Error; err != nil {
		h.Logger.Error("failed to update scheduled post", zap.Error(err))
		http.Error(w, "failed to update", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

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
	var posts []storage.ScheduledPost
	if err := h.DB.Where("tenant_id = ? AND scheduled_at >= ? AND scheduled_at <= ?", claims.TenantID, start, end).
		Order("scheduled_at ASC").Find(&posts).Error; err != nil {
		http.Error(w, "failed to fetch calendar", http.StatusInternalServerError)
		return
	}
	type calPost struct {
		ID          uint       `json:"id"`
		Name        string     `json:"name"`
		Status      string     `json:"status"`
		ScheduledAt *time.Time `json:"scheduled_at"`
		Platforms   []string   `json:"platforms"`
	}
	resp := make([]calPost, 0, len(posts))
	for _, p := range posts {
		var plats []string
		if strings.TrimSpace(p.Platforms) != "" {
			_ = json.Unmarshal([]byte(p.Platforms), &plats)
		}
		resp = append(resp, calPost{
			ID:          p.ID,
			Name:        p.Name,
			Status:      p.Status,
			ScheduledAt: p.ScheduledAt,
			Platforms:   plats,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": resp})
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
		http.Error(w, "failed to list media", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": assets})
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
		http.Error(w, "media not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, asset)
}

func (h *Handler) createMedia(w http.ResponseWriter, r *http.Request) {
	// Stub: in production, return signed URL for S3/GCS then accept finalize callback
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"message":  "Use pre-signed upload workflow",
		"tenantId": claims.TenantID,
	})
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
func (h *Handler) createPost(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		MediaIDs    []uint   `json:"media_ids"`
		Caption     string   `json:"caption"`
		Platforms   []string `json:"platforms"`
		ScheduledAt string   `json:"scheduled_at"` // RFC3339
		Name        string   `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.MediaIDs) == 0 {
		http.Error(w, "media_ids required", http.StatusBadRequest)
		return
	}
	var scheduledAt *time.Time
	if req.ScheduledAt != "" {
		if t, err := time.Parse(time.RFC3339, req.ScheduledAt); err == nil {
			scheduledAt = &t
		} else {
			http.Error(w, "invalid scheduled_at", http.StatusBadRequest)
			return
		}
	}
	mediaJSON, _ := json.Marshal(req.MediaIDs)
	platsJSON, _ := json.Marshal(req.Platforms)
	post := storage.ScheduledPost{
		TenantID:      claims.TenantID,
		Name:          req.Name,
		Caption:       req.Caption,
		MediaAssetIDs: string(mediaJSON),
		Platforms:     string(platsJSON),
		ScheduledAt:   scheduledAt,
		Status:        "scheduled",
	}
	if err := h.DB.Create(&post).Error; err != nil {
		http.Error(w, "failed to create post", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": post.ID})
}

func (h *Handler) generateCaption(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		Context string `json:"context"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Context) == "" {
		http.Error(w, "context required", http.StatusBadRequest)
		return
	}
	// Simple placeholder caption
	caption := fmt.Sprintf("✨ %s\n\n#brancr #socialmedia", req.Context)
	writeJSON(w, http.StatusOK, map[string]string{"caption": caption, "tenant_id": fmt.Sprintf("%d", claims.TenantID)})
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
	writeJSON(w, http.StatusNotImplemented, map[string]string{"message": "Bulk upload creation via dashboard not implemented yet"})
}

func (h *Handler) updateBulkUploadStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"message": "Pause/Resume not implemented yet"})
}

func (h *Handler) cancelBulkUpload(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"message": "Cancel not implemented yet"})
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

	// Get customer info and last message for each conversation
	type conversationResp struct {
		ID             uint      `json:"id"`
		CustomerID     uint      `json:"customer_id"`
		CustomerName   string    `json:"customer_name"`
		CustomerAvatar string    `json:"customer_avatar"`
		Platform       string    `json:"platform"`
		Status         string    `json:"status"`
		LastMessage    string    `json:"last_message"`
		LastMessageAt  time.Time `json:"last_message_at"`
		UnreadCount    int       `json:"unread_count"`
		CreatedAt      time.Time `json:"created_at"`
		UpdatedAt      time.Time `json:"updated_at"`
	}

	resp := make([]conversationResp, 0, len(conversations))
	for _, conv := range conversations {
		var customer storage.Customer
		h.DB.Where("id = ?", conv.CustomerID).First(&customer)

		// Get last interaction
		var lastInteraction storage.Interaction
		h.DB.Where("conversation_id = ?", conv.ID).
			Order("created_at DESC").
			First(&lastInteraction)

		// Count unread messages (incoming messages not responded to)
		var unreadCount int64
		h.DB.Model(&storage.Interaction{}).
			Where("conversation_id = ? AND direction = ? AND response_status != ?", conv.ID, "incoming", "sent").
			Count(&unreadCount)

		resp = append(resp, conversationResp{
			ID:             conv.ID,
			CustomerID:     conv.CustomerID,
			CustomerName:   customer.DisplayName,
			CustomerAvatar: customer.ProfilePicURL,
			Platform:       conv.Platform,
			Status:         conv.Status,
			LastMessage:    lastInteraction.Content,
			LastMessageAt:  conv.LastMessageAt,
			UnreadCount:    int(unreadCount),
			CreatedAt:      conv.CreatedAt,
			UpdatedAt:      conv.UpdatedAt,
		})
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

	// Get all interactions
	var interactions []storage.Interaction
	h.DB.Where("conversation_id = ?", conversation.ID).
		Order("created_at ASC").
		Find(&interactions)

	type interactionResp struct {
		ID             uint      `json:"id"`
		Direction      string    `json:"direction"`
		MessageType    string    `json:"message_type"`
		Content        string    `json:"content"`
		DetectedIntent string    `json:"detected_intent"`
		DetectedTone   string    `json:"detected_tone"`
		ResponseType   string    `json:"response_type"`
		ResponseStatus string    `json:"response_status"`
		SuggestedReply string    `json:"suggested_reply"`
		FinalReply     string    `json:"final_reply"`
		CreatedAt      time.Time `json:"created_at"`
	}

	messages := make([]interactionResp, 0, len(interactions))
	for _, i := range interactions {
		messages = append(messages, interactionResp{
			ID:             i.ID,
			Direction:      i.Direction,
			MessageType:    i.MessageType,
			Content:        i.Content,
			DetectedIntent: i.DetectedIntent,
			DetectedTone:   i.DetectedTone,
			ResponseType:   i.ResponseType,
			ResponseStatus: i.ResponseStatus,
			SuggestedReply: i.SuggestedReply,
			FinalReply:     i.FinalReply,
			CreatedAt:      i.CreatedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":              conversation.ID,
		"customer_id":     conversation.CustomerID,
		"customer_name":   customer.DisplayName,
		"customer_avatar": customer.ProfilePicURL,
		"platform":        conversation.Platform,
		"status":          conversation.Status,
		"messages":        messages,
		"created_at":      conversation.CreatedAt,
		"updated_at":      conversation.UpdatedAt,
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

	// Get social account for the platform
	var account storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ?", claims.TenantID, conversation.Platform).
		First(&account).Error; err != nil {
		http.Error(w, fmt.Sprintf("%s account not connected", conversation.Platform), http.StatusBadRequest)
		return
	}

	// Create message handler to send reply
	ctx := r.Context()
	// MessageHandler requires specific fields, so we'll call platform-specific functions directly
	err := h.sendPlatformReply(ctx, conversation.Platform, &account, &customer, req.Message)
	if err != nil {
		h.Logger.Error("failed to send reply", zap.Error(err))
		http.Error(w, fmt.Sprintf("failed to send reply: %v", err), http.StatusInternalServerError)
		return
	}

	// Create interaction record
	interaction := storage.Interaction{
		TenantID:       claims.TenantID,
		CustomerID:     conversation.CustomerID,
		ConversationID: conversation.ID,
		Platform:       conversation.Platform,
		Direction:      "outgoing",
		MessageType:    "text",
		Content:        req.Message,
		ResponseType:   "manual",
		ResponseStatus: "sent",
		FinalReply:     req.Message,
	}

	h.DB.Create(&interaction)

	// Update conversation last message time
	conversation.LastMessageAt = time.Now()
	h.DB.Save(&conversation)

	writeJSON(w, http.StatusOK, map[string]any{
		"success":        true,
		"message":        "Reply sent successfully",
		"interaction_id": interaction.ID,
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

	writeJSON(w, http.StatusOK, map[string]any{
		"summary": map[string]any{
			"scheduled_posts": scheduledCount,
			"posted":          postedCount,
			"conversations":   conversationsCount,
			"interactions":    interactionsCount,
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
		"platforms":             platformBreakdown,
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
		"message": "Team management coming soon",
	})
}

func (h *Handler) inviteTeamMember(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{
		"message": "Team invitations coming soon",
	})
}

func (h *Handler) getTeamMember(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{
		"message": "Team management coming soon",
	})
}

func (h *Handler) updateTeamMember(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{
		"message": "Team management coming soon",
	})
}

func (h *Handler) deleteTeamMember(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{
		"message": "Team management coming soon",
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

	apiURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages", igAccountID)
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

	jsonData, _ := json.Marshal(payload)
	apiURLWithToken := fmt.Sprintf("%s?access_token=%s", apiURL, url.QueryEscape(token))

	req, _ := http.NewRequestWithContext(ctx, "POST", apiURLWithToken, strings.NewReader(string(jsonData)))
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

// connectWhatsApp - New simplified WhatsApp connection endpoint for Respond.io/Gupshup
// This replaces the Meta embedded signup flow
func (h *Handler) connectWhatsApp(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		PhoneNumber string `json:"phone_number"` // e.g., "+2347039841392"
		Provider    string `json:"provider"`     // "respondio", "gupshup", or "auto" (default)
		ChannelID   string `json:"channel_id"`   // Optional: manual Respond.io channel ID override (e.g., "440617")
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Determine provider first (to check if phone number is required)
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider == "" || provider == "auto" {
		// Auto-detect: Prefer Gupshup Partner if configured, else Respond.io
		if h.Config.GupshupPartnerAPIKey != "" && h.Config.GupshupPartnerID != "" {
			provider = "gupshup_partner"
		} else if h.Config.GupshupSourceNumber != "" {
			provider = "gupshup"
		} else if h.Config.RespondIOAPIKey != "" {
			provider = "respondio"
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "no_provider_configured",
				"message": "Neither Gupshup Partner nor Respond.io is configured. Please contact support.",
			})
			return
		}
	}

	// Normalize phone number (optional for Gupshup Partner, required for Respond.io)
	phoneNumber := strings.TrimSpace(req.PhoneNumber)
	
	// For Respond.io, phone number is required
	if provider == "respondio" && phoneNumber == "" {
		http.Error(w, "phone_number is required for Respond.io provider", http.StatusBadRequest)
		return
	}
	
	// For Gupshup Partner, phone number is optional (will be selected in Gupshup UI)
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
				"external_id": existingAccount.ExternalID,
				"phone_number": existingAccount.PageID,
			},
		})
		return
	}

	// Check WhatsAppApp table (for Gupshup Partner)
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
	case "gupshup", "gupshup_partner":
		// Use Gupshup Partner onboarding link flow (multi-tenant)
		h.connectWhatsAppGupshupPartner(w, r, claims.TenantID, phoneNumber)

	case "respondio":
		// For Respond.io, we use the shared account
		h.connectWhatsAppRespondIO(w, r, claims.TenantID, phoneNumber, req.ChannelID)

	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_provider",
			"message": fmt.Sprintf("Invalid provider '%s'. Use 'gupshup_partner', 'respondio', or 'auto'.", provider),
		})
	}
}

// connectWhatsAppGupshupPartner - Connect WhatsApp via Gupshup Partner API (multi-tenant)
func (h *Handler) connectWhatsAppGupshupPartner(w http.ResponseWriter, r *http.Request, tenantID uint, phoneNumber string) {
	if h.Config.GupshupPartnerAPIKey == "" || h.Config.GupshupPartnerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "gupshup_partner_not_configured",
			"message": "Gupshup Partner API is not configured. Please contact support.",
		})
		return
	}

	// Generate onboarding redirect link (VERIFIED: only correct method)
	redirectURL := fmt.Sprintf("%s/api/tenant/whatsapp/callback?tenant_id=%d", h.Config.PublicURL, tenantID)
	onboardingLink := h.GupshupPartner.GenerateOnboardingLink(h.Config.GupshupPartnerID, redirectURL)

	// Store pending status (app doesn't exist yet - will be created after onboarding)
	h.DB.Create(&storage.WhatsAppApp{
		TenantID: tenantID,
		Status:   "pending_onboarding",
		Mode:     "gupshup_partner",
	})

	h.Logger.Info("✅ Generated Gupshup Partner onboarding link",
		zap.Uint("tenant_id", tenantID),
		zap.String("redirect_url", redirectURL))

	writeJSON(w, http.StatusOK, map[string]any{
		"onboarding_url": onboardingLink,
		"status":         "pending_onboarding",
		"message":        "Complete WhatsApp setup in Gupshup. Click the onboarding URL to proceed.",
	})
}

// connectWhatsAppRespondIO - Connect WhatsApp via Respond.io (simplified - uses shared account)
func (h *Handler) connectWhatsAppRespondIO(w http.ResponseWriter, r *http.Request, tenantID uint, phoneNumber string, manualChannelID string) {
	if h.Config.RespondIOAPIKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "respondio_not_configured",
			"message": "Respond.io is not configured. Please contact support or use Gupshup instead.",
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
			"error":   "no_whatsapp_channel",
			"message": "No WhatsApp channel found in Respond.io API response. The channel exists in dashboard (ID: 440617) but API returned 0 channels.",
			"total_channels": len(channels),
			"channel_types": channelTypes,
			"channel_details": channelDetails,
			"workaround": "Since channel ID 440617 exists in your dashboard, you can manually specify it by adding 'channel_id': '440617' to your request body.",
			"troubleshooting": []string{
				"1. The API returned 0 channels - this might be a permissions or endpoint issue",
				"2. Your channel (ID: 440617) exists in dashboard but isn't returned by the API",
				"3. As a workaround, add 'channel_id': '440617' to your POST request body",
				"4. Or verify API key has permission to list channels in Respond.io settings",
			},
			"example_request": map[string]any{
				"phone_number": phoneNumber,
				"provider": "respondio",
				"channel_id": "440617", // Manual override
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
				"type": "whatsapp",
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
		account.PageID = phoneNumber        // Store phone number for reference
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
			PageID:     phoneNumber,        // Store phone number for reference
			ExternalID: whatsappChannelID,  // Store Respond.io WhatsApp channel ID (CRITICAL!)
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
		"success":       true,
		"provider":      "respondio",
		"phone_number":  phoneNumber,
		"channel_id":    whatsappChannelID,
		"message":       fmt.Sprintf("✅ WhatsApp connected successfully via Respond.io! Your number %s is ready to receive messages.", phoneNumber),
		"account_id":    account.ID,
		"note":          "Respond.io uses a shared account. Messages will be sent from the configured Respond.io WhatsApp number to your customers.",
	})
}

// handleWhatsAppGupshupCallback - Handle Gupshup Partner redirect callback after onboarding
func (h *Handler) handleWhatsAppGupshupCallback(w http.ResponseWriter, r *http.Request) {
	// Extract tenant_id from query param (passed in redirect URL)
	tenantIDStr := r.URL.Query().Get("tenant_id")
	tenantID, err := strconv.ParseUint(tenantIDStr, 10, 32)
	if err != nil {
		errorURL := fmt.Sprintf("%s/integrations/whatsapp?error=invalid_tenant_id", h.Config.FrontendURL)
		http.Redirect(w, r, errorURL, http.StatusFound)
		return
	}

	// VERIFIED: Gupshup redirects with these params after onboarding
	appID := r.URL.Query().Get("appId")
	token := r.URL.Query().Get("token")
	wabaID := r.URL.Query().Get("wabaId")
	phoneNumber := r.URL.Query().Get("phoneNumber")
	status := r.URL.Query().Get("status") // "live" or "pending_verification"

	if appID == "" {
		h.Logger.Warn("Gupshup callback missing appId",
			zap.Uint64("tenant_id", tenantID),
			zap.String("query", r.URL.RawQuery))
		errorURL := fmt.Sprintf("%s/integrations/whatsapp?error=missing_app_id", h.Config.FrontendURL)
		http.Redirect(w, r, errorURL, http.StatusFound)
		return
	}

	// Encrypt token before storing
	encryptedToken, err := utils.EncryptToken(token)
	if err != nil {
		h.Logger.Error("Failed to encrypt Gupshup token", zap.Error(err))
		errorURL := fmt.Sprintf("%s/integrations/whatsapp?error=encryption_failed", h.Config.FrontendURL)
		http.Redirect(w, r, errorURL, http.StatusFound)
		return
	}

	// Update or create WhatsAppApp record
	var waApp storage.WhatsAppApp
	if err := h.DB.Where("tenant_id = ?", uint(tenantID)).First(&waApp).Error; err != nil {
		// Create new record
		waApp = storage.WhatsAppApp{
			TenantID:        uint(tenantID),
			GupshupAppID:    appID,
			GupshupAppToken: encryptedToken,
			WABAID:          wabaID,
			PhoneNumber:     phoneNumber,
			Status:          status,
			Mode:            "gupshup_partner",
		}
		if err := h.DB.Create(&waApp).Error; err != nil {
			h.Logger.Error("Failed to create WhatsAppApp", zap.Error(err))
			errorURL := fmt.Sprintf("%s/integrations/whatsapp?error=database_error", h.Config.FrontendURL)
			http.Redirect(w, r, errorURL, http.StatusFound)
			return
		}
	} else {
		// Update existing record
		waApp.GupshupAppID = appID
		waApp.GupshupAppToken = encryptedToken
		waApp.WABAID = wabaID
		waApp.PhoneNumber = phoneNumber
		waApp.Status = status
		if err := h.DB.Save(&waApp).Error; err != nil {
			h.Logger.Error("Failed to update WhatsAppApp", zap.Error(err))
			errorURL := fmt.Sprintf("%s/integrations/whatsapp?error=database_error", h.Config.FrontendURL)
			http.Redirect(w, r, errorURL, http.StatusFound)
			return
		}
	}

	// Configure webhook callback URL (VERIFIED endpoint)
	if h.GupshupPartner != nil {
		webhookURL := fmt.Sprintf("%s/webhooks/gupshup", h.Config.PublicURL)
		if err := h.GupshupPartner.SetCallback(r.Context(), appID, webhookURL); err != nil {
			h.Logger.Warn("Failed to set webhook callback", zap.Error(err), zap.String("app_id", appID))
			// Continue anyway - webhook can be set manually
		}
	}

	// Sync to SocialAccount for compatibility with existing code
	var socialAcc storage.SocialAccount
	h.DB.Where("tenant_id = ? AND platform = ?", uint(tenantID), "whatsapp").FirstOrCreate(&socialAcc, storage.SocialAccount{
		TenantID: uint(tenantID),
		Platform: "whatsapp",
	})
	socialAcc.ExternalID = appID       // Gupshup App ID (for webhook routing)
	socialAcc.PageID = phoneNumber     // Phone number
	socialAcc.Connected = (status == "live")
	socialAcc.Mode = "gupshup_partner"
	h.DB.Save(&socialAcc)

	h.Logger.Info("✅ Gupshup Partner app onboarding completed",
		zap.Uint64("tenant_id", tenantID),
		zap.String("app_id", appID),
		zap.String("phone_number", phoneNumber),
		zap.String("status", status))

	// Redirect to frontend success page
	successURL := fmt.Sprintf("%s/integrations/whatsapp?status=%s", h.Config.FrontendURL, status)
	http.Redirect(w, r, successURL, http.StatusFound)
}

// getWhatsAppConnectionStatus - Get current WhatsApp connection status
func (h *Handler) getWhatsAppConnectionStatus(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Check Gupshup Partner WhatsAppApp first
	var waApp storage.WhatsAppApp
	if err := h.DB.Where("tenant_id = ?", claims.TenantID).First(&waApp).Error; err == nil {
		status := map[string]any{
			"connected":    waApp.Status == "live",
			"provider":     waApp.Mode,
			"phone_number": waApp.PhoneNumber,
			"app_id":       waApp.GupshupAppID,
			"status":       waApp.Status,
		}
		if waApp.Status == "live" {
			status["message"] = "✅ WhatsApp connected via Gupshup Partner"
		} else {
			status["message"] = fmt.Sprintf("WhatsApp status: %s", waApp.Status)
		}
		writeJSON(w, http.StatusOK, status)
		return
	}

	// Fallback to SocialAccount (for Respond.io or legacy)
	var account storage.SocialAccount
	err := h.DB.Where("tenant_id = ? AND platform = ?", claims.TenantID, "whatsapp").First(&account).Error

	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"connected": false,
			"provider":  "none",
			"message":   "WhatsApp is not connected. Use POST /api/tenant/whatsapp/connect to connect.",
		})
		return
	}

	status := map[string]any{
		"connected":  account.Connected,
		"provider":   account.Mode,
		"account_id": account.ID,
	}

	if account.Connected {
		status["phone_number"] = account.PageID
		status["external_id"] = account.ExternalID
		status["message"] = fmt.Sprintf("✅ WhatsApp connected via %s", strings.ToUpper(account.Mode))
	} else {
		status["message"] = "WhatsApp account exists but is not connected"
	}

	writeJSON(w, http.StatusOK, status)
}
