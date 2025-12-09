package tenantapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/similadayo/brancr/internal/config"
	"github.com/similadayo/brancr/internal/storage"
	"golang.org/x/crypto/bcrypt"
)

const (
	googleStateCookie = "tenant_google_oauth_state"
	googleScopeEmail  = "https://www.googleapis.com/auth/userinfo.email"
	googleScopeProfile = "https://www.googleapis.com/auth/userinfo.profile"
)

type googleUserInfo struct {
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email"`
	Picture       string `json:"picture"`
	Name          string `json:"name"`
	GivenName     string `json:"given_name"`
	FamilyName    string `json:"family_name"`
}

// GoogleOAuthHandler manages Google OAuth for tenant authentication
type GoogleOAuthHandler struct {
	DB       *gorm.DB
	Logger   *zap.Logger
	Config   *config.Config
	OAuthCfg *oauth2.Config
}

// NewGoogleOAuthHandler creates a new Google OAuth handler for tenants
func NewGoogleOAuthHandler(db *gorm.DB, logger *zap.Logger, cfg *config.Config) *GoogleOAuthHandler {
	if cfg.GoogleOAuthClientID == "" || cfg.GoogleOAuthClientSecret == "" {
		logger.Info("Google OAuth disabled — missing GOOGLE_OAUTH_CLIENT_ID or GOOGLE_OAUTH_CLIENT_SECRET")
		return nil
	}

	redirectURI := cfg.GoogleOAuthRedirectURI
	if redirectURI == "" && cfg.PublicURL != "" {
		redirectURI = strings.TrimRight(cfg.PublicURL, "/") + "/api/auth/google/callback"
	}

	oauthCfg := &oauth2.Config{
		ClientID:     cfg.GoogleOAuthClientID,
		ClientSecret: cfg.GoogleOAuthClientSecret,
		RedirectURL:  redirectURI,
		Scopes:       []string{googleScopeEmail, googleScopeProfile},
		Endpoint:     google.Endpoint,
	}

	return &GoogleOAuthHandler{
		DB:       db,
		Logger:   logger,
		Config:   cfg,
		OAuthCfg: oauthCfg,
	}
}

func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// StartOAuth initiates the Google OAuth flow for tenant login/signup
func (h *GoogleOAuthHandler) StartOAuth(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.OAuthCfg == nil {
		http.Error(w, "Google OAuth not configured", http.StatusServiceUnavailable)
		return
	}

	// Get redirect URL after OAuth (optional)
	successRedirect := r.URL.Query().Get("redirect")
	if successRedirect == "" {
		successRedirect = h.Config.FrontendURL
		if successRedirect == "" {
			successRedirect = "https://brancr.com/app" // Default to app dashboard
		}
	}
	
	// Ensure redirect URL is absolute (not relative)
	// If it's a relative path, prepend the frontend URL
	if !strings.HasPrefix(successRedirect, "http://") && !strings.HasPrefix(successRedirect, "https://") {
		// It's a relative path, make it absolute
		// Only use FrontendURL as base if it's actually absolute (starts with http:// or https://)
		// Otherwise, fallback to hardcoded default to avoid path duplication
		baseURL := h.Config.FrontendURL
		if baseURL == "" || (!strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://")) {
			baseURL = "https://brancr.com" // Default fallback (absolute URL)
		}
		// Remove trailing slash from base URL
		baseURL = strings.TrimRight(baseURL, "/")
		// Ensure redirect path starts with /
		if !strings.HasPrefix(successRedirect, "/") {
			successRedirect = "/" + successRedirect
		}
		successRedirect = baseURL + successRedirect
	}

	state, err := randomState()
	if err != nil {
		h.Logger.Error("failed to generate OAuth state", zap.Error(err))
		http.Error(w, "could not start login", http.StatusInternalServerError)
		return
	}

	// Store state in cookie with redirect URL
	stateData := map[string]string{
		"state":           state,
		"success_redirect": successRedirect,
	}
	stateJSON, _ := json.Marshal(stateData)
	stateValue := base64.URLEncoding.EncodeToString(stateJSON)

	http.SetCookie(w, &http.Cookie{
		Name:     googleStateCookie,
		Value:    stateValue,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   300, // 5 minutes
	})

	url := h.OAuthCfg.AuthCodeURL(state, oauth2.AccessTypeOffline)
	http.Redirect(w, r, url, http.StatusFound)
}

// Callback handles the Google OAuth callback and creates/logs in the tenant
func (h *GoogleOAuthHandler) Callback(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.OAuthCfg == nil {
		http.Error(w, "Google OAuth not configured", http.StatusServiceUnavailable)
		return
	}

	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if state == "" || code == "" {
		http.Error(w, "invalid OAuth response", http.StatusBadRequest)
		return
	}

	// Verify state from cookie
	stateCookie, err := r.Cookie(googleStateCookie)
	if err != nil || stateCookie.Value == "" {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}

	// Decode state data
	stateJSON, err := base64.URLEncoding.DecodeString(stateCookie.Value)
	if err != nil {
		http.Error(w, "invalid state cookie", http.StatusBadRequest)
		return
	}

	var stateData map[string]string
	if err := json.Unmarshal(stateJSON, &stateData); err != nil {
		http.Error(w, "invalid state cookie format", http.StatusBadRequest)
		return
	}

	if stateData["state"] != state {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}

	successRedirect := stateData["success_redirect"]
	
	// If no redirect was stored, use FrontendURL and append /app if needed
	// Only use FrontendURL if it's absolute (starts with http:// or https://)
	// Otherwise, fallback to hardcoded default to avoid path duplication
	if successRedirect == "" {
		baseURL := h.Config.FrontendURL
		if baseURL == "" || (!strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://")) {
			baseURL = "https://brancr.com" // Default fallback (absolute URL)
		}
		baseURL = strings.TrimRight(baseURL, "/")
		// Append /app if not already present
		if !strings.HasSuffix(baseURL, "/app") && !strings.Contains(baseURL, "/app/") {
			successRedirect = baseURL + "/app"
		} else {
			successRedirect = baseURL
		}
	}
	
	// Ensure redirect URL is absolute (not relative)
	// If it's a relative path, prepend the frontend URL
	if !strings.HasPrefix(successRedirect, "http://") && !strings.HasPrefix(successRedirect, "https://") {
		// It's a relative path, make it absolute
		// Only use FrontendURL as base if it's actually absolute (starts with http:// or https://)
		// Otherwise, fallback to hardcoded default to avoid path duplication
		baseURL := h.Config.FrontendURL
		if baseURL == "" || (!strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://")) {
			baseURL = "https://brancr.com" // Default fallback (absolute URL)
		}
		baseURL = strings.TrimRight(baseURL, "/")
		// Ensure redirect path starts with /
		if !strings.HasPrefix(successRedirect, "/") {
			successRedirect = "/" + successRedirect
		}
		successRedirect = baseURL + successRedirect
	}

	// Clear state cookie
	http.SetCookie(w, &http.Cookie{
		Name:     googleStateCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})

	// Exchange code for token
	token, err := h.OAuthCfg.Exchange(context.Background(), code)
	if err != nil {
		h.Logger.Error("token exchange failed", zap.Error(err))
		http.Error(w, "token exchange failed", http.StatusUnauthorized)
		return
	}

	// Get user info from Google
	client := h.OAuthCfg.Client(context.Background(), token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo?alt=json")
	if err != nil || resp.StatusCode != http.StatusOK {
		h.Logger.Error("failed to fetch user profile", zap.Error(err))
		http.Error(w, "failed to fetch profile", http.StatusUnauthorized)
		return
	}
	defer resp.Body.Close()

	var userInfo googleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		h.Logger.Error("failed to parse user profile", zap.Error(err))
		http.Error(w, "failed to parse profile", http.StatusUnauthorized)
		return
	}

	if !userInfo.VerifiedEmail {
		http.Error(w, "email not verified by Google", http.StatusUnauthorized)
		return
	}

	email := strings.ToLower(strings.TrimSpace(userInfo.Email))
	if email == "" {
		http.Error(w, "invalid email from Google", http.StatusUnauthorized)
		return
	}

	// Find or create tenant
	var tenant storage.Tenant
	var isNewTenant bool
	err = h.DB.Where("email = ?", email).First(&tenant).Error

	if err == gorm.ErrRecordNotFound {
		isNewTenant = true
		// Create new tenant
		companyName := userInfo.Name
		if companyName == "" {
			companyName = email
		}

		// Generate a random password (user won't need it since they use Google OAuth)
		randomPassword := base64.URLEncoding.EncodeToString(make([]byte, 32))
		hash, err := bcrypt.GenerateFromPassword([]byte(randomPassword), bcrypt.DefaultCost)
		if err != nil {
			h.Logger.Error("failed to hash password for new tenant", zap.Error(err))
			http.Error(w, "failed to create account", http.StatusInternalServerError)
			return
		}

		// Get default industry
		var defaultIndustry storage.Industry
		defaultIndustryID := uint(0)
		if err := h.DB.Where("name = ?", "General").First(&defaultIndustry).Error; err == nil {
			defaultIndustryID = defaultIndustry.ID
		} else if err := h.DB.Order("id ASC").First(&defaultIndustry).Error; err == nil {
			defaultIndustryID = defaultIndustry.ID
		}

		tenant = storage.Tenant{
			Name:               companyName,
			Email:              email,
			OwnerName:          userInfo.Name,
			PasswordHash:       string(hash),
			PlanType:           "trial",
			Status:             "active",
			OnboardingStep:     "business_profile",
			OnboardingComplete: false,
			IndustryID:         defaultIndustryID,
			IndustrySettings:   "{}",
		}

		if err := h.DB.Create(&tenant).Error; err != nil {
			h.Logger.Error("failed to create tenant from Google OAuth", zap.Error(err), zap.String("email", email))
			http.Error(w, "failed to create account", http.StatusInternalServerError)
			return
		}

		// Create business profile
		profile := storage.BusinessProfile{
			TenantID: tenant.ID,
			Name:     companyName,
			Tone:     "friendly",
		}
		if err := h.DB.Create(&profile).Error; err != nil {
			h.Logger.Error("failed to create business profile", zap.Error(err), zap.Uint("tenant_id", tenant.ID))
		}

		// Create persona
		persona := storage.Persona{
			TenantID:   tenant.ID,
			BotName:    "Assistant",
			Tone:       "friendly, helpful",
			Language:   "en",
			Humor:      true,
			StyleNotes: "",
		}
		if err := h.DB.Create(&persona).Error; err != nil {
			h.Logger.Error("failed to create persona", zap.Error(err), zap.Uint("tenant_id", tenant.ID))
		}

		h.Logger.Info("created new tenant from Google OAuth", zap.Uint("tenant_id", tenant.ID), zap.String("email", email))
	} else if err != nil {
		h.Logger.Error("failed to query tenant", zap.Error(err), zap.String("email", email))
		http.Error(w, "authentication failed", http.StatusInternalServerError)
		return
	}

	// Set session cookie
	handler := &Handler{
		DB:     h.DB,
		Logger: h.Logger,
		Config: h.Config,
	}
	if err := handler.setSessionCookie(w, tenant.ID, tenant.Email); err != nil {
		h.Logger.Error("failed to set session cookie", zap.Error(err))
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}

	// Redirect new users to onboarding, existing users to their intended destination
	var finalRedirect string
	if isNewTenant {
		// New users should go to onboarding, not dashboard
		// Determine base URL for onboarding
		baseURL := h.Config.FrontendURL
		if baseURL == "" || (!strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://")) {
			baseURL = "https://brancr.com" // Default fallback
		}
		baseURL = strings.TrimRight(baseURL, "/")
		finalRedirect = baseURL + "/app/onboarding"
		h.Logger.Info("redirecting new tenant to onboarding",
			zap.Uint("tenant_id", tenant.ID),
			zap.String("email", email),
			zap.String("redirect_url", finalRedirect))
	} else {
		// Existing users go to their intended destination (or dashboard if not specified)
		finalRedirect = successRedirect
	}

	// Redirect to frontend
	http.Redirect(w, r, finalRedirect, http.StatusSeeOther)
}

