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
	DB     *gorm.DB
	Logger *zap.Logger
	Config *config.Config
	Redis  *redis.Client

	signupCache sync.Map
	tokenCache  sync.Map
}

func NewHandler(db *gorm.DB, logger *zap.Logger, cfg *config.Config, redis *redis.Client) *Handler {
	return &Handler{
		DB:     db,
		Logger: logger,
		Config: cfg,
		Redis:  redis,
	}
}

func (h *Handler) RegisterRoutes(r chi.Router) {
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
		r.Delete("/tenant/scheduled-posts/{id}", h.deleteScheduledPost)

		// Overview and stats
		r.Get("/tenant/overview", h.overview)

		// WhatsApp provider-owned model (new)
		r.Get("/tenant/whatsapp/numbers", h.listAvailablePhoneNumbers)
		r.Post("/tenant/whatsapp/numbers/{id}/assign", h.assignPhoneNumber)
		r.Get("/tenant/whatsapp/current", h.getAssignedPhoneNumber)
		r.Post("/tenant/whatsapp/disconnect", h.disconnectPhoneNumber)

		// Tenant-provided phone numbers
		r.Post("/tenant/whatsapp/request-number", h.requestTenantPhoneNumber)
		r.Post("/tenant/whatsapp/verify-number", h.verifyTenantPhoneNumber)
		r.Post("/tenant/whatsapp/check-number", h.checkTenantPhoneNumber) // Check if number is ready to verify

		// Legacy embedded signup (keep for migration)
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

		// Analytics API
		r.Get("/tenant/analytics", h.getAnalytics)

		// Team Management API (coming soon - placeholder)
		r.Get("/tenant/team/members", h.listTeamMembers)
		r.Post("/tenant/team/invite", h.inviteTeamMember)
		r.Get("/tenant/team/members/{id}", h.getTeamMember)
		r.Put("/tenant/team/members/{id}", h.updateTeamMember)
		r.Delete("/tenant/team/members/{id}", h.deleteTeamMember)
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

	// Find connected Facebook Page
	pageURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s?fields=connected_page&access_token=%s", igAccountID, token)
	pageResp, err := client.Get(pageURL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "failed_to_fetch_page",
			"message": fmt.Sprintf("Failed to fetch connected Facebook Page: %v", err),
		})
		return
	}
	defer pageResp.Body.Close()

	pageBody, _ := io.ReadAll(pageResp.Body)
	if pageResp.StatusCode >= 400 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "no_connected_page",
			"message": fmt.Sprintf("No connected Facebook Page found. Response: %s", string(pageBody)),
		})
		return
	}

	var pageData struct {
		ConnectedPage struct {
			ID string `json:"id"`
		} `json:"connected_page"`
	}

	if err := json.Unmarshal(pageBody, &pageData); err != nil || pageData.ConnectedPage.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "page_parse_error",
			"message": "Could not parse connected Facebook Page ID",
		})
		return
	}

	pageID := pageData.ConnectedPage.ID

	// Subscribe Page to receive Instagram message webhooks
	// Import the function from social package
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

	// Get tenant's assigned number
	var phoneNum storage.WhatsAppPhoneNumber
	if err := h.DB.Where("tenant_id = ? AND status = ?", claims.TenantID, "assigned").First(&phoneNum).Error; err != nil {
		http.Error(w, "no WhatsApp number assigned", http.StatusNotFound)
		return
	}

	// Unassign from tenant
	phoneNum.TenantID = nil
	phoneNum.Status = "available"
	phoneNum.AssignedAt = nil

	if err := h.DB.Save(&phoneNum).Error; err != nil {
		h.Logger.Error("failed to disconnect phone number", zap.Error(err))
		http.Error(w, "failed to disconnect", http.StatusInternalServerError)
		return
	}

	// Update social account
	var account storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ?", claims.TenantID, "whatsapp").First(&account).Error; err == nil {
		account.Connected = false
		account.PageID = ""
		account.ExternalID = ""
		account.Username = ""
		h.DB.Save(&account)
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "WhatsApp number disconnected successfully",
	})
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

	// New flow: Tenant requests number → Admin sees request → Admin adds in Meta BM → Meta sends code to tenant → Tenant provides code to admin → Admin verifies

	// Format phone number (ensure + prefix)
	phoneNumber := strings.TrimSpace(req.PhoneNumber)
	if !strings.HasPrefix(phoneNumber, "+") {
		phoneNumber = "+" + phoneNumber
	}

	// Check if tenant already has a pending request for this number
	var existingRequest storage.WhatsAppPhoneNumberRequest
	if err := h.DB.Where("tenant_id = ? AND phone_number = ? AND status IN (?)", claims.TenantID, phoneNumber, []string{"pending", "added_to_meta", "verification_code_sent"}).First(&existingRequest).Error; err == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":      "request_already_exists",
			"message":    fmt.Sprintf("You already have a pending request for %s. Status: %s. Please wait for admin to process it.", phoneNumber, existingRequest.Status),
			"request_id": existingRequest.ID,
			"status":     existingRequest.Status,
		})
		return
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

	// Create a new phone number request
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

	h.Logger.Info("tenant created phone number request",
		zap.Uint("tenant_id", claims.TenantID),
		zap.String("phone_number", phoneNumber),
		zap.Uint("request_id", request.ID))

	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"request_id":   request.ID,
		"message":      "Phone number request submitted. Admin will add your number to Meta Business Manager. Meta will send a verification code to your phone - please provide that code to admin once you receive it.",
		"phone_number": phoneNumber,
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

	// Update request with verification code
	phoneRequest.VerificationCode = req.VerificationCode
	phoneRequest.Status = "verification_code_sent"

	if err := h.DB.Save(&phoneRequest).Error; err != nil {
		h.Logger.Error("failed to save verification code", zap.Error(err), zap.Uint("tenant_id", claims.TenantID))
		http.Error(w, "failed to save verification code", http.StatusInternalServerError)
		return
	}

	h.Logger.Info("tenant provided verification code",
		zap.Uint("tenant_id", claims.TenantID),
		zap.Uint("request_id", req.RequestID),
		zap.String("phone_number", phoneRequest.PhoneNumber))

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Verification code received. Admin will verify it with Meta and complete the setup. You'll be notified once your number is ready.",
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
		ID             uint       `json:"id"`
		Name           string     `json:"name"`
		Frequency      string     `json:"frequency"`
		Schedule       string     `json:"schedule"`
		MediaTemplate  string     `json:"media_template"`
		CaptionTemplate string    `json:"caption_template"`
		Platforms      []string   `json:"platforms"`
		Active         bool       `json:"active"`
		NextRun        *time.Time `json:"next_run"`
		LastRun        *time.Time `json:"last_run"`
		CreatedAt      time.Time  `json:"created_at"`
		UpdatedAt      time.Time  `json:"updated_at"`
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
		ID              uint       `json:"id"`
		CustomerID      uint       `json:"customer_id"`
		CustomerName    string     `json:"customer_name"`
		CustomerAvatar  string     `json:"customer_avatar"`
		Platform        string     `json:"platform"`
		Status          string     `json:"status"`
		LastMessage     string     `json:"last_message"`
		LastMessageAt   time.Time  `json:"last_message_at"`
		UnreadCount     int        `json:"unread_count"`
		CreatedAt       time.Time  `json:"created_at"`
		UpdatedAt       time.Time  `json:"updated_at"`
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
		ID              uint      `json:"id"`
		Direction       string    `json:"direction"`
		MessageType     string    `json:"message_type"`
		Content         string    `json:"content"`
		DetectedIntent  string    `json:"detected_intent"`
		DetectedTone    string    `json:"detected_tone"`
		ResponseType    string    `json:"response_type"`
		ResponseStatus  string    `json:"response_status"`
		SuggestedReply  string    `json:"suggested_reply"`
		FinalReply      string    `json:"final_reply"`
		CreatedAt       time.Time `json:"created_at"`
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
		"id":             conversation.ID,
		"customer_id":    conversation.CustomerID,
		"customer_name":  customer.DisplayName,
		"customer_avatar": customer.ProfilePicURL,
		"platform":       conversation.Platform,
		"status":         conversation.Status,
		"messages":       messages,
		"created_at":     conversation.CreatedAt,
		"updated_at":     conversation.UpdatedAt,
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
		"success": true,
		"message": "Reply sent successfully",
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
	h.DB.Model(&storage.Interaction{}).Where("tenant_id = ? AND created_at >= ? AND created_at <= ?", claims.TenantID, start, end).Count(&interactionsCount)

	// Get platform breakdown
	type platformStats struct {
		Platform string `json:"platform"`
		Posts    int64  `json:"posts"`
		Conversations int64 `json:"conversations"`
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

		platformBreakdown = append(platformBreakdown, platformStats{
			Platform:       p,
			Posts:          posts,
			Conversations:  convos,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"scheduled_posts":  scheduledCount,
		"posted":           postedCount,
		"conversations":    conversationsCount,
		"interactions":     interactionsCount,
		"platforms":        platformBreakdown,
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
		"text": map[string]string{"body": message},
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
