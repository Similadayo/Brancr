package social

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/config"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// TikTokOAuthHandler manages OAuth for TikTok Business accounts.
type TikTokOAuthHandler struct {
	DB              *gorm.DB
	Logger          *zap.Logger
	ClientKey       string
	ClientSecret    string
	RedirectURI     string
	Scopes          []string
	SuccessRedirect string
	HTTPClient      *http.Client
}

// NewTikTokOAuthHandler returns a configured handler or nil if configuration is incomplete.
func NewTikTokOAuthHandler(db *gorm.DB, logger *zap.Logger, cfg *config.Config) *TikTokOAuthHandler {
	if cfg.TikTokClientKey == "" || cfg.TikTokClientSecret == "" || cfg.TikTokRedirectURI == "" {
		logger.Info("TikTok OAuth disabled — missing TIKTOK_CLIENT_KEY, TIKTOK_CLIENT_SECRET, or TIKTOK_REDIRECT_URI")
		return nil
	}

	scopeParts := strings.Split(cfg.TikTokScopes, ",")
	scopes := make([]string, 0, len(scopeParts))
	scopeSet := make(map[string]struct{})
	for _, raw := range scopeParts {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			key := strings.ToLower(trimmed)
			if _, exists := scopeSet[key]; !exists {
				scopes = append(scopes, trimmed)
				scopeSet[key] = struct{}{}
			}
		}
	}
	if len(scopes) == 0 {
		// Default to only the scopes that are typically available in TikTok apps
		// These match what's commonly configured: user.info.basic and video.upload
		scopes = []string{"user.info.basic", "video.upload"}
		scopeSet = map[string]struct{}{
			"user.info.basic": {},
			"video.upload":    {},
		}
	}

	// Ensure essential scopes are always present for basic functionality.
	// Only include scopes that are commonly available and don't require special approval.
	essentialScopes := []string{"user.info.basic", "video.upload"}
	for _, s := range essentialScopes {
		key := strings.ToLower(s)
		if _, exists := scopeSet[key]; !exists {
			scopes = append(scopes, s)
			scopeSet[key] = struct{}{}
		}
	}

	successRedirect := cfg.MetaSuccessURL
	if successRedirect == "" {
		successRedirect = cfg.PublicURL
	}

	return &TikTokOAuthHandler{
		DB:              db,
		Logger:          logger,
		ClientKey:       cfg.TikTokClientKey,
		ClientSecret:    cfg.TikTokClientSecret,
		RedirectURI:     cfg.TikTokRedirectURI,
		Scopes:          scopes,
		SuccessRedirect: successRedirect,
		HTTPClient:      &http.Client{Timeout: 20 * time.Second},
	}
}

type tiktokState struct {
	TenantID        uint   `json:"tenant_id"`
	SuccessRedirect string `json:"success_redirect,omitempty"`
	CodeVerifier    string `json:"code_verifier"`
	Nonce           string `json:"nonce"`
	IssuedAt        int64  `json:"iat"`
}

// StartOAuth initiates the TikTok OAuth flow.
func (h *TikTokOAuthHandler) StartOAuth(w http.ResponseWriter, r *http.Request) {
	if h == nil {
		http.Error(w, "TikTok OAuth disabled", http.StatusNotImplemented)
		return
	}

	query := r.URL.Query()
	tenantIDStr := strings.TrimSpace(query.Get("tenant_id"))
	if tenantIDStr == "" {
		http.Error(w, "tenant_id is required", http.StatusBadRequest)
		return
	}

	tenantID, err := parseUint(tenantIDStr)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}

	successRedirect := query.Get("success_redirect")
	if successRedirect == "" {
		successRedirect = h.SuccessRedirect
	}

	codeVerifier := generateRandomString(64)
	state := tiktokState{
		TenantID:        tenantID,
		SuccessRedirect: successRedirect,
		CodeVerifier:    codeVerifier,
		Nonce:           generateRandomString(24),
		IssuedAt:        time.Now().Unix(),
	}

	stateToken, err := encodeTikTokState(state)
	if err != nil {
		h.Logger.Error("failed to encode tiktok state", zap.Error(err))
		http.Error(w, "failed to generate state", http.StatusInternalServerError)
		return
	}

	codeChallenge := pkceChallenge(codeVerifier)
	authURL := url.URL{
		Scheme: "https",
		Host:   "www.tiktok.com",
		Path:   "/v2/auth/authorize/",
	}

	params := url.Values{}
	params.Set("client_key", h.ClientKey)
	// TikTok OAuth requires space-separated scopes, not comma-separated
	params.Set("scope", strings.Join(h.Scopes, " "))
	params.Set("redirect_uri", h.RedirectURI)
	params.Set("response_type", "code")
	params.Set("state", stateToken)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	
	// Log scopes for debugging
	h.Logger.Info("TikTok OAuth scopes",
		zap.String("scopes", strings.Join(h.Scopes, " ")),
		zap.Strings("scope_list", h.Scopes))

	authURL.RawQuery = params.Encode()

	h.Logger.Info("Redirecting to TikTok OAuth", zap.Uint("tenant_id", tenantID))
	http.Redirect(w, r, authURL.String(), http.StatusFound)
}

// Callback handles the TikTok OAuth redirect and persists the connected account.
func (h *TikTokOAuthHandler) Callback(w http.ResponseWriter, r *http.Request) {
	if h == nil {
		http.Error(w, "TikTok OAuth disabled", http.StatusNotImplemented)
		return
	}

	query := r.URL.Query()
	if errParam := query.Get("error"); errParam != "" {
		description := query.Get("error_description")
		h.Logger.Warn("TikTok OAuth denied", zap.String("error", errParam), zap.String("description", description))
		h.respond(w, r, "", "error", fmt.Sprintf("tiktok oauth error: %s", errParam))
		return
	}

	stateRaw := query.Get("state")
	if stateRaw == "" {
		http.Error(w, "state missing", http.StatusBadRequest)
		return
	}

	state, err := decodeTikTokState(stateRaw)
	if err != nil {
		h.Logger.Error("invalid TikTok state", zap.Error(err))
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}

	successRedirect := state.SuccessRedirect
	code := query.Get("code")
	if code == "" {
		http.Error(w, "authorization code missing", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	tokenData, err := h.exchangeToken(ctx, code, state.CodeVerifier)
	if err != nil {
		h.Logger.Error("tiktok token exchange failed", zap.Error(err))
		h.respond(w, r, successRedirect, "error", "failed to exchange TikTok code")
		return
	}

	profile, err := h.fetchUserProfile(ctx, tokenData.AccessToken)
	if err != nil {
		h.Logger.Warn("failed to fetch TikTok profile", zap.Error(err))
	}

	if err := h.saveTikTokAccount(ctx, state.TenantID, tokenData, profile); err != nil {
		h.Logger.Error("failed to persist TikTok account", zap.Error(err))
		h.respond(w, r, successRedirect, "error", "failed to save TikTok account")
		return
	}

	h.respond(w, r, successRedirect, "success", "TikTok account connected")
}

type tiktokTokenData struct {
	AccessToken      string
	RefreshToken     string
	ExpiresIn        int64
	RefreshExpiresIn int64
	OpenID           string
	Scope            string
	RawResponse      json.RawMessage
}

func (h *TikTokOAuthHandler) exchangeToken(ctx context.Context, code, verifier string) (*tiktokTokenData, error) {
	form := url.Values{}
	form.Set("client_key", h.ClientKey)
	form.Set("client_secret", h.ClientSecret)
	form.Set("code", code)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", h.RedirectURI)
	form.Set("code_verifier", verifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://open.tiktokapis.com/v2/oauth/token/", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := h.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("tiktok token exchange failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed struct {
		Data struct {
			AccessToken      string `json:"access_token"`
			RefreshToken     string `json:"refresh_token"`
			ExpiresIn        int64  `json:"expires_in"`
			RefreshExpiresIn int64  `json:"refresh_expires_in"`
			OpenID           string `json:"open_id"`
			Scope            string `json:"scope"`
		} `json:"data"`
		Error      json.RawMessage `json:"error"`
		Message    string          `json:"message"`
		StatusCode int             `json:"status_code"`
	}

	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	apiErr := parseTikTokAPIError(parsed.Error, parsed.Message)
	if apiErr.Code != 0 {
		return nil, fmt.Errorf("tiktok token exchange error (%d): %s (raw=%s)", apiErr.Code, apiErr.Message, apiErr.Raw)
	}

	if parsed.Data.AccessToken == "" {
		var direct struct {
			AccessToken      string `json:"access_token"`
			RefreshToken     string `json:"refresh_token"`
			ExpiresIn        int64  `json:"expires_in"`
			RefreshExpiresIn int64  `json:"refresh_expires_in"`
			OpenID           string `json:"open_id"`
			Scope            string `json:"scope"`
		}
		if err := json.Unmarshal(respBody, &direct); err == nil && direct.AccessToken != "" {
			parsed.Data.AccessToken = direct.AccessToken
			parsed.Data.RefreshToken = direct.RefreshToken
			parsed.Data.ExpiresIn = direct.ExpiresIn
			parsed.Data.RefreshExpiresIn = direct.RefreshExpiresIn
			parsed.Data.OpenID = direct.OpenID
			parsed.Data.Scope = direct.Scope
		}
	}

	if parsed.Data.AccessToken == "" {
		if h.Logger != nil {
			h.Logger.Warn("TikTok token response missing access token",
				zap.String("raw_response", strings.TrimSpace(string(respBody))))
		}
		return nil, errors.New("tiktok access token missing in response")
	}

	return &tiktokTokenData{
		AccessToken:      parsed.Data.AccessToken,
		RefreshToken:     parsed.Data.RefreshToken,
		ExpiresIn:        parsed.Data.ExpiresIn,
		RefreshExpiresIn: parsed.Data.RefreshExpiresIn,
		OpenID:           parsed.Data.OpenID,
		Scope:            parsed.Data.Scope,
		RawResponse:      json.RawMessage(respBody),
	}, nil
}

type tiktokUserProfile struct {
	OpenID      string
	DisplayName string
	Username    string
	PictureURL  string
}

func (h *TikTokOAuthHandler) fetchUserProfile(ctx context.Context, accessToken string) (*tiktokUserProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://open.tiktokapis.com/v2/user/info/?fields=display_name,username,profile_image_url,open_id", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := h.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("tiktok user info failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed struct {
		Data struct {
			User struct {
				OpenID      string `json:"open_id"`
				DisplayName string `json:"display_name"`
				Username    string `json:"username"`
				PictureURL  string `json:"profile_image_url"`
			} `json:"user"`
		} `json:"data"`
		Error      json.RawMessage `json:"error"`
		Message    string          `json:"message"`
		StatusCode int             `json:"status_code"`
	}

	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}

	apiErr := parseTikTokAPIError(parsed.Error, parsed.Message)
	if apiErr.Code != 0 {
		return nil, fmt.Errorf("tiktok user info error (%d): %s (raw=%s)", apiErr.Code, apiErr.Message, apiErr.Raw)
	}

	profile := parsed.Data.User
	if profile.OpenID == "" && profile.Username == "" && profile.DisplayName == "" {
		return nil, errors.New("tiktok user info missing identifiers")
	}

	return &tiktokUserProfile{
		OpenID:      profile.OpenID,
		DisplayName: profile.DisplayName,
		Username:    profile.Username,
		PictureURL:  profile.PictureURL,
	}, nil
}

func (h *TikTokOAuthHandler) saveTikTokAccount(ctx context.Context, tenantID uint, tokens *tiktokTokenData, profile *tiktokUserProfile) error {
	if tokens == nil {
		return errors.New("token response missing")
	}

	var (
		encryptedAccess  string
		encryptedRefresh string
		err              error
	)

	if tokens.AccessToken != "" {
		encryptedAccess, err = utils.EncryptToken(tokens.AccessToken)
		if err != nil {
			return fmt.Errorf("encrypt access token: %w", err)
		}
	}

	if tokens.RefreshToken != "" {
		encryptedRefresh, err = utils.EncryptToken(tokens.RefreshToken)
		if err != nil {
			h.Logger.Warn("failed to encrypt TikTok refresh token", zap.Error(err))
		}
	}

	var expiresAt *time.Time
	if tokens.ExpiresIn > 0 {
		exp := time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second)
		expiresAt = &exp
	}

	username := ""
	externalID := tokens.OpenID
	if profile != nil {
		if profile.DisplayName != "" {
			username = profile.DisplayName
		} else if profile.Username != "" {
			username = profile.Username
		}
		if profile.OpenID != "" {
			externalID = profile.OpenID
		}
	}

	return h.upsertTikTokAccount(ctx, tenantID, func(acc *storage.SocialAccount) {
		if encryptedAccess != "" {
			acc.AccessToken = encryptedAccess
		}
		if encryptedRefresh != "" {
			acc.RefreshToken = encryptedRefresh
		}
		acc.Platform = "tiktok"
		acc.Mode = "oauth2"
		acc.Connected = true
		acc.ExternalID = externalID
		acc.PageID = externalID
		if username != "" {
			acc.Username = username
		}
		acc.ExpiresAt = expiresAt
	})
}

func (h *TikTokOAuthHandler) upsertTikTokAccount(ctx context.Context, tenantID uint, update func(*storage.SocialAccount)) error {
	if h.DB == nil {
		return errors.New("database connection missing")
	}

	var account storage.SocialAccount
	result := h.DB.WithContext(ctx).Where("tenant_id = ? AND platform = ?", tenantID, "tiktok").First(&account)

	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		account = storage.SocialAccount{
			TenantID:  tenantID,
			Platform:  "tiktok",
			Mode:      "oauth2",
			Connected: true,
		}
	} else if result.Error != nil {
		return result.Error
	}

	update(&account)

	return h.DB.WithContext(ctx).Save(&account).Error
}

func (h *TikTokOAuthHandler) respond(w http.ResponseWriter, r *http.Request, redirect, status, message string) {
	payload := map[string]string{
		"status":   status,
		"platform": "tiktok",
		"message":  message,
	}

	if redirect != "" {
		target, err := url.Parse(redirect)
		if err == nil {
			q := target.Query()
			for k, v := range payload {
				q.Set(k, v)
			}
			target.RawQuery = q.Encode()
			http.Redirect(w, r, target.String(), http.StatusFound)
			return
		}
		h.Logger.Warn("invalid TikTok success redirect", zap.String("redirect", redirect))
	}

	w.Header().Set("Content-Type", "application/json")
	if status == "success" {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusBadRequest)
	}
	_ = json.NewEncoder(w).Encode(payload)
}

// Helpers --------------------------------------------------------------------

func parseUint(input string) (uint, error) {
	val, err := strconv.ParseUint(input, 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(val), nil
}

func encodeTikTokState(state tiktokState) (string, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeTikTokState(encoded string) (tiktokState, error) {
	var state tiktokState
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	return state, nil
}

func generateRandomString(length int) string {
	if length <= 0 {
		length = 32
	}
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		fallback := fmt.Sprintf("%d", time.Now().UnixNano())
		encoded := base64.RawURLEncoding.EncodeToString([]byte(fallback))
		if len(encoded) >= length {
			return encoded[:length]
		}
		return encoded
	}
	return base64.RawURLEncoding.EncodeToString(buf)[:length]
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
