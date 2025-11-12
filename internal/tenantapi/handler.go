package tenantapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/similadayo/brancr/internal/config"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
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
}

func NewHandler(db *gorm.DB, logger *zap.Logger, cfg *config.Config) *Handler {
	return &Handler{
		DB:     db,
		Logger: logger,
		Config: cfg,
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
		r.Get("/tenant/social-accounts", h.listIntegrations) // backward-compatible
		r.Get("/tenant/integrations", h.listIntegrations)
		r.Get("/tenant/scheduled-posts", h.listScheduledPosts)
		r.Get("/tenant/overview", h.overview)
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
		Name:         req.CompanyName,
		Email:        req.Email,
		OwnerName:    req.Name,
		OwnerPhone:   strings.TrimSpace(req.Phone),
		PlanType:     "trial",
		Status:       "active",
		PasswordHash: string(hash),
	}

	if err := h.DB.Create(&tenant).Error; err != nil {
		h.Logger.Error("create tenant", zap.Error(err))
		http.Error(w, "could not create tenant", http.StatusInternalServerError)
		return
	}

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

	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": tenant.ID,
		"name":      tenant.Name,
		"email":     tenant.Email,
		"plan":      tenant.PlanType,
		"status":    tenant.Status,
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
		Platform   string    `json:"platform"`
		Connected  bool      `json:"connected"`
		Username   string    `json:"username"`
		ExternalID string    `json:"external_id"`
		PageID     string    `json:"page_id"`
		Mode       string    `json:"mode"`
		UpdatedAt  time.Time `json:"updated_at"`
	}

	resp := make([]integration, 0, len(accounts))
	for _, acc := range accounts {
		resp = append(resp, integration{
			Platform:   acc.Platform,
			Connected:  acc.Connected,
			Username:   acc.Username,
			ExternalID: acc.ExternalID,
			PageID:     acc.PageID,
			Mode:       acc.Mode,
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

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
