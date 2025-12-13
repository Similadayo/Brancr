package tenantapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"fmt"
	"time"

	"github.com/similadayo/brancr/internal/cache"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

type signupRequest struct {
	Name        string `json:"name" example:"John Doe"`
	Email       string `json:"email" example:"john@example.com"`
	Password    string `json:"password" example:"securePassword123"`
	CompanyName string `json:"company_name" example:"Acme Corp"`
	Phone       string `json:"phone" example:"+1234567890"`
}

// signup handles tenant registration
// @Summary      Register a new tenant
// @Description  Create a new tenant account with email, password, and company information
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        request  body      signupRequest  true  "Signup request"
// @Success      201     {object}  map[string]interface{}  "Tenant created successfully"
// @Failure      400     {object}  map[string]interface{}  "Invalid request"
// @Failure      409     {object}  map[string]interface{}  "Email already exists"
// @Failure      500     {object}  map[string]interface{}  "Internal server error"
// @Router       /auth/signup [post]
func (h *Handler) signup(w http.ResponseWriter, r *http.Request) {
	var req signupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.Logger.Warn("signup: failed to decode request", zap.Error(err))
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_payload",
			"message": "Invalid request format. Please check your input.",
		})
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.Name = strings.TrimSpace(req.Name)
	req.CompanyName = strings.TrimSpace(req.CompanyName)

	if req.Email == "" || req.Password == "" || req.CompanyName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "missing_fields",
			"message": "Email, password, and company name are required.",
		})
		return
	}

	var existing int64
	h.DB.Model(&storage.Tenant{}).Where("email = ?", req.Email).Count(&existing)
	if existing > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   "email_exists",
			"message": "An account with this email already exists. Please login instead.",
		})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		h.Logger.Error("bcrypt password hashing failed", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "signup_failed",
			"message": "Could not process password. Please try again.",
		})
		return
	}

	// Get default "General" industry ID (or use 0 if not found)
	var defaultIndustry storage.Industry
	defaultIndustryID := uint(0)
	if err := h.DB.Where("name = ?", "General").First(&defaultIndustry).Error; err == nil {
		defaultIndustryID = defaultIndustry.ID
	} else {
		// Fallback: try to get any industry
		if err := h.DB.Order("id ASC").First(&defaultIndustry).Error; err == nil {
			defaultIndustryID = defaultIndustry.ID
		}
		// If no industries exist, defaultIndustryID will be 0 (will be set during onboarding)
	}

	tenant := storage.Tenant{
		Name:               req.CompanyName,
		Email:              req.Email,
		OwnerName:          req.Name,
		OwnerPhone:         strings.TrimSpace(req.Phone),
		PlanType:           "trial",
		Status:             "pending_onboarding",
		PasswordHash:       string(hash),
		OnboardingStep:     "business_profile",
		OnboardingComplete: false,
		IndustryID:         defaultIndustryID, // Set to default industry or 0
		IndustrySettings:   "{}",
		Metadata:           "{}",
	}

	if err := h.DB.Create(&tenant).Error; err != nil {
		h.Logger.Error("create tenant failed", zap.Error(err), zap.String("email", req.Email))
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "signup_failed",
			"message": "Could not create account. Please try again.",
		})
		return
	}

	if err := storage.EnsureTenantOnboarding(r.Context(), h.DB, tenant.ID, req.CompanyName); err != nil {
		h.Logger.Error("signup: failed to initialize onboarding", zap.Error(err), zap.Uint("tenant_id", tenant.ID))
		h.DB.Delete(&tenant)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "signup_failed",
			"message": "Could not initialize onboarding. Please try again.",
		})
		return
	}

	// Set session cookie (non-critical - user can still login manually)
	if err := h.setSessionCookie(w, tenant.ID, tenant.Email); err != nil {
		h.Logger.Warn("failed to set session cookie during signup", zap.Error(err), zap.Uint("tenant_id", tenant.ID))
		// Don't fail signup if cookie fails - user can login manually
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"success":             true,
		"tenant_id":           tenant.ID,
		"name":                tenant.Name,
		"email":               tenant.Email,
		"onboarding_required": true,
		"redirect_to":         "/app/onboarding",
		"message":             "Account created successfully",
	})
}

type loginRequest struct {
	Email    string `json:"email" example:"john@example.com"`
	Password string `json:"password" example:"securePassword123"`
}

// login handles tenant authentication
// @Summary      Login tenant
// @Description  Authenticate tenant with email and password
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        request  body      loginRequest  true  "Login request"
// @Success      200     {object}  map[string]interface{}  "Login successful"
// @Failure      400     {object}  map[string]interface{}  "Invalid request"
// @Failure      401     {object}  map[string]interface{}  "Invalid credentials"
// @Failure      500     {object}  map[string]interface{}  "Internal server error"
// @Router       /auth/login [post]
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.Logger.Warn("login: failed to decode request body", zap.Error(err))
		h.writeValidationError(w, r, "invalid request payload", nil)
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	if req.Email == "" || req.Password == "" {
		h.writeValidationError(w, r, "email and password are required", nil)
		return
	}

	var tenant storage.Tenant
	if err := h.DB.Where("email = ?", req.Email).First(&tenant).Error; err != nil {
		h.Logger.Warn("login: tenant not found", zap.String("email", req.Email))
		h.writeUnauthorizedError(w, r, "invalid email or password")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(tenant.PasswordHash), []byte(req.Password)); err != nil {
		h.Logger.Warn("login: invalid password", zap.String("email", req.Email))
		h.writeUnauthorizedError(w, r, "invalid email or password")
		return
	}

	if err := h.setSessionCookie(w, tenant.ID, tenant.Email); err != nil {
		h.Logger.Error("failed to set session cookie", zap.Error(err))
		h.writeInternalError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":             true,
		"tenant_id":           tenant.ID,
		"name":                tenant.Name,
		"email":               tenant.Email,
		"onboarding_required": !tenant.OnboardingComplete,
		"redirect_to": func() string {
			if tenant.OnboardingComplete {
				return "/app"
			}
			return "/app/onboarding"
		}(),
		"message": "Login successful",
	})
}

// logout handles tenant logout
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	// Get tenant claims to invalidate session in cache
	claims, ok := middleware.TenantFromContext(r.Context())
	if ok && h.Cache != nil {
		ctx := r.Context()
		// Invalidate session in cache
		sessionKey := fmt.Sprintf("session:tenant:%d:%s", claims.TenantID, claims.Email)
		if err := h.Cache.Delete(ctx, sessionKey); err != nil {
			h.Logger.Warn("failed to invalidate session in cache", zap.Error(err), zap.Uint("tenant_id", claims.TenantID))
		}
		// Also invalidate tenant data cache
		tenantKey := cache.TenantKey(claims.TenantID)
		if err := h.Cache.Delete(ctx, tenantKey); err != nil {
			h.Logger.Warn("failed to invalidate tenant cache", zap.Error(err), zap.Uint("tenant_id", claims.TenantID))
		}
	}

	// Clear cookie with same settings as login (for consistency)
	cookie := &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,              // Delete immediately
		Expires:  time.Unix(0, 0), // Also set Expires for older browsers
		HttpOnly: true,
		Secure:   true,                  // Must match login cookie settings
		SameSite: http.SameSiteNoneMode, // Must match login cookie settings
		Domain:   h.cookieDomain(),
	}
	http.SetCookie(w, cookie)

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Logged out successfully",
	})
}

// me returns the current authenticated tenant's information
func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Try to get tenant data from cache first
	type cachedTenantData struct {
		Tenant          storage.Tenant
		BusinessProfile storage.BusinessProfile
		Persona         storage.Persona
	}
	var cachedData cachedTenantData
	cacheKey := cache.TenantKey(claims.TenantID)
	cacheHit := false

	if h.Cache != nil {
		if err := h.Cache.Get(r.Context(), cacheKey, &cachedData); err == nil {
			cacheHit = true
			h.Logger.Debug("tenant data cache hit", zap.Uint("tenant_id", claims.TenantID))
		}
	}

	var tenant storage.Tenant
	var profile storage.BusinessProfile
	var persona storage.Persona

	if cacheHit {
		tenant = cachedData.Tenant
		profile = cachedData.BusinessProfile
		persona = cachedData.Persona
	} else {
		// Cache miss - fetch from database
		if err := h.DB.First(&tenant, claims.TenantID).Error; err != nil {
			h.Logger.Error("failed to fetch tenant", zap.Error(err), zap.Uint("tenant_id", claims.TenantID))
			h.writeInternalError(w, r, err)
			return
		}

		h.DB.Where("tenant_id = ?", tenant.ID).First(&profile)
		h.DB.Where("tenant_id = ?", tenant.ID).First(&persona)

		// Cache the data for 5 minutes
		if h.Cache != nil {
			cachedData = cachedTenantData{
				Tenant:          tenant,
				BusinessProfile: profile,
				Persona:         persona,
			}
			if err := h.Cache.SetWithTTL(r.Context(), cacheKey, cachedData, 5*time.Minute); err != nil {
				h.Logger.Warn("failed to cache tenant data", zap.Error(err), zap.Uint("tenant_id", claims.TenantID))
			}
		}
	}

	// Auto-complete onboarding if tenant has sufficient data
	// This may modify tenant, so invalidate cache if it changes
	originalOnboardingComplete := tenant.OnboardingComplete
	h.checkAndAutoCompleteOnboarding(&tenant)
	if tenant.OnboardingComplete != originalOnboardingComplete {
		// Onboarding status changed - invalidate cache
		if h.Cache != nil {
			h.Cache.Delete(r.Context(), cacheKey)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"onboarding_required": !tenant.OnboardingComplete,
		"redirect_to": func() string {
			if tenant.OnboardingComplete {
				return ""
			}
			return "/app/onboarding"
		}(),
		"tenant": map[string]any{
			"id":                  tenant.ID,
			"name":                tenant.Name,
			"email":               tenant.Email,
			"owner_name":          tenant.OwnerName,
			"plan_type":           tenant.PlanType,
			"status":              tenant.Status,
			"onboarding_step":     tenant.OnboardingStep,
			"onboarding_complete": tenant.OnboardingComplete,
			"timezone":            tenant.Timezone,
			"industry_id":         tenant.IndustryID,
		},
		"business_profile": map[string]any{
			"name":     profile.Name,
			"niche":    profile.Niche,
			"tone":     profile.Tone,
			"industry": profile.Industry,
		},
		"persona": map[string]any{
			"bot_name":    persona.BotName,
			"tone":        persona.Tone,
			"language":    persona.Language,
			"humor":       persona.Humor,
			"style_notes": persona.StyleNotes,
		},
	})
}
