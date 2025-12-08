package social

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
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

// InstagramLoginOAuthHandler manages the OAuth flow for Instagram Login (Business Login for Instagram)
// This is separate from Facebook Login and doesn't require a Facebook Page
type InstagramLoginOAuthHandler struct {
	DB              *gorm.DB
	Logger          *zap.Logger
	AppID           string // Instagram App ID (separate from Meta App ID)
	AppSecret       string // Instagram App Secret (separate from Meta App Secret)
	RedirectURI     string
	SuccessRedirect string
	HTTPClient      *http.Client
}

// NewInstagramLoginOAuthHandler returns a configured Instagram Login OAuth handler or nil if configuration is incomplete
func NewInstagramLoginOAuthHandler(db *gorm.DB, logger *zap.Logger, cfg *config.Config) (*InstagramLoginOAuthHandler, error) {
	if cfg.InstagramAppID == "" || cfg.InstagramAppSecret == "" || cfg.InstagramRedirectURI == "" {
		logger.Info("Instagram Login OAuth disabled — missing INSTAGRAM_APP_ID, INSTAGRAM_APP_SECRET, or INSTAGRAM_REDIRECT_URI")
		return nil, nil
	}

	client := &http.Client{Timeout: 15 * time.Second}

	return &InstagramLoginOAuthHandler{
		DB:              db,
		Logger:          logger,
		AppID:           cfg.InstagramAppID,
		AppSecret:       cfg.InstagramAppSecret,
		RedirectURI:     cfg.InstagramRedirectURI,
		SuccessRedirect: cfg.MetaSuccessURL, // Use same success redirect as Meta OAuth
		HTTPClient:      client,
	}, nil
}

type instagramLoginOAuthState struct {
	TenantID        uint     `json:"tenant_id"`
	SuccessRedirect string   `json:"success_redirect,omitempty"`
	Nonce           string   `json:"nonce"`
	IssuedAt        int64    `json:"iat"`
}

// StartOAuth initiates the Instagram Login OAuth authorization flow
// According to: https://developers.facebook.com/docs/instagram-platform/instagram-api-with-instagram-login/business-login
func (h *InstagramLoginOAuthHandler) StartOAuth(w http.ResponseWriter, r *http.Request) {
	if h == nil {
		http.Error(w, "Instagram Login OAuth disabled", http.StatusNotImplemented)
		return
	}

	query := r.URL.Query()
	tenantIDStr := query.Get("tenant_id")
	if tenantIDStr == "" {
		http.Error(w, "tenant_id is required", http.StatusBadRequest)
		return
	}

	tenantIDUint, err := strconv.ParseUint(tenantIDStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}

	successRedirect := query.Get("success_redirect")
	if successRedirect == "" {
		successRedirect = h.SuccessRedirect
	}

	state := instagramLoginOAuthState{
		TenantID:        uint(tenantIDUint),
		SuccessRedirect: successRedirect,
		Nonce:           h.randomNonce(),
		IssuedAt:        time.Now().Unix(),
	}

	stateToken, err := h.encodeState(state)
	if err != nil {
		h.Logger.Error("failed to encode instagram login oauth state", zap.Error(err))
		http.Error(w, "failed to generate state", http.StatusInternalServerError)
		return
	}

	// Store state in cookie as backup
	stateCookie := &http.Cookie{
		Name:     "instagram_oauth_state_backup",
		Value:    stateToken,
		Path:     "/",
		MaxAge:   600, // 10 minutes
		HttpOnly: true,
		Secure:   strings.HasPrefix(h.RedirectURI, "https://"),
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, stateCookie)

	// Build authorization URL
	// Endpoint: https://www.instagram.com/oauth/authorize
	authURL := url.URL{
		Scheme: "https",
		Host:   "www.instagram.com",
		Path:   "/oauth/authorize",
	}

	params := url.Values{}
	params.Set("client_id", h.AppID)
	params.Set("redirect_uri", h.RedirectURI)
	params.Set("response_type", "code")
	
	// New scope values for Instagram Login (as per documentation)
	// https://developers.facebook.com/docs/instagram-platform/instagram-api-with-instagram-login
	// These replace the deprecated business_* scopes (deprecated Jan 27, 2025)
	scopes := []string{
		"instagram_business_basic",              // Required - replaces business_basic
		"instagram_business_content_publish",    // For publishing - replaces business_content_publish
		"instagram_business_manage_messages",   // For messaging - replaces business_manage_messages
		"instagram_business_manage_comments",   // For comment moderation - replaces business_manage_comments
		"instagram_business_manage_insights",   // For insights/analytics - REQUIRED for insights API
	}
	params.Set("scope", strings.Join(scopes, ","))
	params.Set("state", stateToken)

	authURL.RawQuery = params.Encode()

	h.Logger.Info("Starting Instagram Login OAuth flow",
		zap.Uint("tenant_id", uint(tenantIDUint)),
		zap.String("redirect_uri", h.RedirectURI))

	http.Redirect(w, r, authURL.String(), http.StatusFound)
}

// HandleCallback handles the OAuth callback from Instagram Login
func (h *InstagramLoginOAuthHandler) HandleCallback(w http.ResponseWriter, r *http.Request) {
	if h == nil {
		http.Error(w, "Instagram Login OAuth disabled", http.StatusNotImplemented)
		return
	}

	query := r.URL.Query()
	
	// Check for errors
	if errorParam := query.Get("error"); errorParam != "" {
		errorReason := query.Get("error_reason")
		errorDesc := query.Get("error_description")
		h.Logger.Warn("Instagram Login OAuth error",
			zap.String("error", errorParam),
			zap.String("error_reason", errorReason),
			zap.String("error_description", errorDesc))
		
		// Redirect to frontend with error
		redirectURL := fmt.Sprintf("%s?error=%s&error_description=%s", h.SuccessRedirect, errorParam, url.QueryEscape(errorDesc))
		http.Redirect(w, r, redirectURL, http.StatusFound)
		return
	}

	// Get authorization code
	code := query.Get("code")
	if code == "" {
		http.Error(w, "authorization code not provided", http.StatusBadRequest)
		return
	}

	// Get and verify state
	stateToken := query.Get("state")
	if stateToken == "" {
		// Try to get from cookie
		cookie, err := r.Cookie("instagram_oauth_state_backup")
		if err != nil {
			http.Error(w, "state parameter missing", http.StatusBadRequest)
			return
		}
		stateToken = cookie.Value
	}

	state, err := h.decodeState(stateToken)
	if err != nil {
		h.Logger.Error("failed to decode instagram login oauth state", zap.Error(err))
		http.Error(w, "invalid state parameter", http.StatusBadRequest)
		return
	}

	// Verify state is not too old (10 minutes max)
	if time.Now().Unix()-state.IssuedAt > 600 {
		http.Error(w, "state expired", http.StatusBadRequest)
		return
	}

	// Step 1: Exchange authorization code for short-lived access token
	// Endpoint: https://api.instagram.com/oauth/access_token
	shortLivedToken, userID, permissions, err := h.exchangeCodeForToken(code)
	if err != nil {
		h.Logger.Error("failed to exchange code for token",
			zap.Error(err),
			zap.String("code_preview", func() string {
				if len(code) > 20 {
					return code[:20] + "..."
				}
				return code
			}()))
		http.Error(w, fmt.Sprintf("failed to exchange authorization code: %v", err), http.StatusInternalServerError)
		return
	}

	// Step 2: Exchange short-lived token for long-lived token (60 days)
	// Endpoint: https://graph.instagram.com/access_token
	longLivedToken, expiresIn, err := h.exchangeForLongLivedToken(shortLivedToken)
	if err != nil {
		h.Logger.Error("failed to exchange for long-lived token", zap.Error(err))
		// Continue with short-lived token - user can refresh later
		longLivedToken = shortLivedToken
		expiresIn = 3600 // 1 hour
	}

	// Calculate expiration time
	expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)

	// Step 3: Get Instagram account information
	// Use /me endpoint with Instagram User access token
	igAccount, err := h.getInstagramAccount(r.Context(), longLivedToken)
	if err != nil {
		h.Logger.Error("failed to get instagram account info", zap.Error(err))
		http.Error(w, fmt.Sprintf("failed to get account info: %v", err), http.StatusInternalServerError)
		return
	}

	// Step 4: Save Instagram account to database
	ctx := r.Context()
	if err := h.saveInstagramAccount(ctx, state.TenantID, igAccount, longLivedToken, permissions, expiresAt); err != nil {
		h.Logger.Error("failed to save instagram account", zap.Error(err))
		http.Error(w, fmt.Sprintf("failed to save account: %v", err), http.StatusInternalServerError)
		return
	}

	// Step 5: Subscribe to webhooks for Instagram Login account
	// This enables receiving messages, comments, and other webhook events
	client := &http.Client{Timeout: 15 * time.Second}
	webhookFields := []string{"messages", "comments"} // Subscribe to messages and comments
	if subscribeErr := SubscribeInstagramLoginAccount(ctx, client, igAccount.ID, longLivedToken, webhookFields); subscribeErr != nil {
		// Log warning but don't fail the OAuth flow - subscription can be done later
		h.Logger.Warn("failed to subscribe Instagram Login account to webhooks",
			zap.Error(subscribeErr),
			zap.String("instagram_account_id", igAccount.ID),
			zap.Uint("tenant_id", state.TenantID))
	} else {
		h.Logger.Info("✅ Instagram Login account subscribed to webhooks",
			zap.String("instagram_account_id", igAccount.ID),
			zap.Uint("tenant_id", state.TenantID),
			zap.Strings("webhook_fields", webhookFields))
	}

	h.Logger.Info("✅ Instagram Login OAuth completed successfully",
		zap.Uint("tenant_id", state.TenantID),
		zap.String("instagram_user_id", userID),
		zap.String("username", igAccount.Username))

	// Redirect to success page
	redirectURL := state.SuccessRedirect
	if redirectURL == "" {
		redirectURL = h.SuccessRedirect
	}
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// exchangeCodeForToken exchanges authorization code for short-lived access token
// Endpoint: https://api.instagram.com/oauth/access_token
func (h *InstagramLoginOAuthHandler) exchangeCodeForToken(code string) (accessToken, userID string, permissions []string, err error) {
	endpoint := "https://api.instagram.com/oauth/access_token"
	
	formData := url.Values{}
	formData.Set("client_id", h.AppID)
	formData.Set("client_secret", h.AppSecret)
	formData.Set("grant_type", "authorization_code")
	formData.Set("redirect_uri", h.RedirectURI)
	formData.Set("code", code)

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(formData.Encode()))
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := h.HTTPClient.Do(req)
	if err != nil {
		return "", "", nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var apiError struct {
			ErrorType    string `json:"error_type"`
			Code         int    `json:"code"`
			ErrorMessage string `json:"error_message"`
		}
		if err := json.Unmarshal(body, &apiError); err == nil {
			return "", "", nil, fmt.Errorf("Instagram API error %d: %s", apiError.Code, apiError.ErrorMessage)
		}
		return "", "", nil, fmt.Errorf("Instagram API error %d: %s", resp.StatusCode, string(body))
	}

	// Response format: {"data": [{"access_token": "...", "user_id": ..., "permissions": [...]}]}
	// OR direct: {"access_token": "...", "user_id": ..., "permissions": [...]}
	// Note: user_id can be a number or string, permissions can be array or comma-separated string
	var tokenResponse struct {
		Data []struct {
			AccessToken interface{} `json:"access_token"` // Can be string
			UserID      interface{} `json:"user_id"`      // Can be number or string
			Permissions interface{} `json:"permissions"`  // Can be array or string
		} `json:"data"`
		// Also try direct fields (in case response is not wrapped in data array)
		AccessToken interface{} `json:"access_token"`
		UserID      interface{} `json:"user_id"`
		Permissions interface{} `json:"permissions"`
	}

	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		// Log the actual response for debugging
		responsePreview := string(body)
		if len(responsePreview) > 500 {
			responsePreview = responsePreview[:500] + "..."
		}
		return "", "", nil, fmt.Errorf("failed to parse token response: %w (response: %s)", err, responsePreview)
	}

	// Helper function to extract string from interface{} (handles both string and number)
	extractString := func(v interface{}) string {
		if v == nil {
			return ""
		}
		switch val := v.(type) {
		case string:
			return val
		case float64:
			// JSON numbers are unmarshaled as float64
			return fmt.Sprintf("%.0f", val)
		case int64:
			return fmt.Sprintf("%d", val)
		case int:
			return fmt.Sprintf("%d", val)
		default:
			return fmt.Sprintf("%v", val)
		}
	}

	// Helper function to extract permissions array from interface{}
	extractPermissions := func(v interface{}) []string {
		if v == nil {
			return []string{}
		}
		// Try array first
		if arr, ok := v.([]interface{}); ok {
			perms := make([]string, 0, len(arr))
			for _, p := range arr {
				if str, ok := p.(string); ok {
					perms = append(perms, strings.TrimSpace(str))
				}
			}
			return perms
		}
		// Try string (comma-separated)
		if str, ok := v.(string); ok {
			parts := strings.Split(str, ",")
			for i := range parts {
				parts[i] = strings.TrimSpace(parts[i])
			}
			return parts
		}
		return []string{}
	}

	var token, uid string
	var permissionList []string

	// Check if we have data in the array format
	if len(tokenResponse.Data) > 0 {
		data := tokenResponse.Data[0]
		token = extractString(data.AccessToken)
		uid = extractString(data.UserID)
		permissionList = extractPermissions(data.Permissions)
	} else {
		// Check if we have direct fields (alternative response format)
		token = extractString(tokenResponse.AccessToken)
		uid = extractString(tokenResponse.UserID)
		permissionList = extractPermissions(tokenResponse.Permissions)
	}

	// Validate we have the required data
	if token == "" {
		return "", "", nil, fmt.Errorf("access_token missing in response")
	}
	if uid == "" {
		return "", "", nil, fmt.Errorf("user_id missing in response")
	}

	return token, uid, permissionList, nil

	// No token data found - log the actual response for debugging
	responsePreview := string(body)
	if len(responsePreview) > 500 {
		responsePreview = responsePreview[:500] + "..."
	}
	return "", "", nil, fmt.Errorf("no token data in response (response body: %s)", responsePreview)
}

// exchangeForLongLivedToken exchanges short-lived token for long-lived token (60 days)
// Endpoint: https://graph.instagram.com/access_token
func (h *InstagramLoginOAuthHandler) exchangeForLongLivedToken(shortLivedToken string) (accessToken string, expiresIn int, err error) {
	endpoint := "https://graph.instagram.com/access_token"
	
	params := url.Values{}
	params.Set("grant_type", "ig_exchange_token")
	params.Set("client_secret", h.AppSecret)
	params.Set("access_token", shortLivedToken)

	req, err := http.NewRequest("GET", endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := h.HTTPClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("Instagram API error %d: %s", resp.StatusCode, string(body))
	}

	var tokenResponse struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"` // Seconds until expiration
	}

	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return "", 0, fmt.Errorf("failed to parse token response: %w", err)
	}

	return tokenResponse.AccessToken, tokenResponse.ExpiresIn, nil
}

// getInstagramAccount gets Instagram account information using /me endpoint
// Endpoint: https://graph.instagram.com/v24.0/me
// According to: https://developers.facebook.com/docs/instagram-platform/instagram-api-with-instagram-login/get-started
func (h *InstagramLoginOAuthHandler) getInstagramAccount(ctx context.Context, accessToken string) (*instagramAccountInfo, error) {
	endpoint := "https://graph.instagram.com/v24.0/me"
	params := url.Values{}
	params.Set("fields", "user_id,username") // Use user_id field as per documentation
	params.Set("access_token", accessToken)

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := h.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Instagram API error %d: %s", resp.StatusCode, string(body))
	}

	// Handle response format - Instagram API may return direct object or wrapped in data array
	// Try direct object first (most common)
	var account struct {
		UserID   string `json:"user_id"`   // Instagram professional account ID
		ID       string `json:"id"`        // App-scoped ID (fallback)
		Username string `json:"username"`
	}

	if err := json.Unmarshal(body, &account); err != nil {
		// Try data array format
		var dataWrapper struct {
			Data []struct {
				UserID   string `json:"user_id"`
				ID       string `json:"id"`
				Username string `json:"username"`
			} `json:"data"`
		}
		if err2 := json.Unmarshal(body, &dataWrapper); err2 != nil {
			return nil, fmt.Errorf("failed to parse account response: %w (also tried data wrapper: %v)", err, err2)
		}
		if len(dataWrapper.Data) == 0 {
			return nil, fmt.Errorf("no account data in response")
		}
		account = dataWrapper.Data[0]
	}

	// Use user_id if available, otherwise fall back to id (app-scoped ID)
	igID := account.UserID
	if igID == "" {
		igID = account.ID
	}
	if igID == "" {
		return nil, fmt.Errorf("no user ID found in response")
	}

	return &instagramAccountInfo{
		ID:       igID, // This is the Instagram professional account ID (IG_ID)
		Username: account.Username,
	}, nil
}

type instagramAccountInfo struct {
	ID       string
	Username string
}

// saveInstagramAccount saves the Instagram account to the database
func (h *InstagramLoginOAuthHandler) saveInstagramAccount(ctx context.Context, tenantID uint, account *instagramAccountInfo, accessToken string, permissions []string, expiresAt time.Time) error {
	// Check if account already exists - first by external_id, then by tenant_id + platform
	// This handles the case where a tenant disconnects and reconnects (external_id might be empty or different)
	var existing storage.SocialAccount
	err := h.DB.WithContext(ctx).
		Where("tenant_id = ? AND platform = ? AND external_id = ?", tenantID, "instagram", account.ID).
		First(&existing).Error
	
	// If not found by external_id, try finding by tenant_id + platform (for reconnection scenarios)
	if err != nil {
		err = h.DB.WithContext(ctx).
			Where("tenant_id = ? AND platform = ?", tenantID, "instagram").
			First(&existing).Error
	}

	metadata := map[string]interface{}{
		"login_method":    "instagram_login",
		"permissions":     permissions,
		"token_type":      "instagram_user",
		"authorized_at":   time.Now().Format(time.RFC3339),
	}
	metadataJSON, _ := json.Marshal(metadata)

	if err == nil {
		// Found existing account (either by external_id or tenant_id + platform)
		// This handles reconnection scenarios where the account was disconnected
		// Encrypt token before updating
		encryptedToken, encErr := utils.EncryptToken(accessToken)
		if encErr != nil {
			h.Logger.Error("failed to encrypt Instagram access token", zap.Error(encErr))
			encryptedToken = accessToken // Fallback to plain token if encryption fails
		}

		// Update existing account with new connection details
		existing.ExpiresAt = &expiresAt
		existing.Username = account.Username
		existing.ExternalID = account.ID
		existing.PageID = account.ID // For Instagram Login, PageID is the Instagram User ID
		existing.Connected = true
		existing.Mode = "oauth2"
		existing.LoginMethod = "instagram_login"
		existing.Metadata = string(metadataJSON)
		existing.AccessToken = encryptedToken // Save encrypted token directly
		
		// Save the updated account
		if saveErr := h.DB.WithContext(ctx).Save(&existing).Error; saveErr != nil {
			return fmt.Errorf("failed to update instagram account: %w", saveErr)
		}
		
		// Verify the save was successful by reloading
		var verifyAccount storage.SocialAccount
		if verifyErr := h.DB.WithContext(ctx).First(&verifyAccount, existing.ID).Error; verifyErr == nil {
			h.Logger.Info("✅ Updated existing Instagram account (reconnection)",
				zap.Uint("tenant_id", tenantID),
				zap.Uint("account_id", existing.ID),
				zap.String("instagram_id", account.ID),
				zap.String("username", account.Username),
				zap.Bool("was_connected", existing.Connected),
				zap.Bool("now_connected", verifyAccount.Connected),
				zap.Bool("has_access_token", verifyAccount.AccessToken != ""),
				zap.Int("access_token_length", len(encryptedToken)))
		} else {
			h.Logger.Warn("⚠️ Account updated but verification reload failed",
				zap.Uint("tenant_id", tenantID),
				zap.Uint("account_id", existing.ID),
				zap.Error(verifyErr))
		}
		return nil
	}

	// Encrypt token BEFORE creating account to ensure it's saved properly
	encryptedToken, err := utils.EncryptToken(accessToken)
	if err != nil {
		h.Logger.Error("failed to encrypt Instagram access token", zap.Error(err))
		// Continue with plain token if encryption fails (shouldn't happen in production)
		encryptedToken = accessToken
	}

	// Create new account with encrypted token
	newAccount := storage.SocialAccount{
		TenantID:    tenantID,
		Platform:    "instagram",
		ExpiresAt:   &expiresAt,
		Username:    account.Username,
		ExternalID:  account.ID,
		PageID:      account.ID, // For Instagram Login, PageID is the Instagram User ID
		Connected:   true,
		Mode:        "oauth2",
		LoginMethod: "instagram_login",
		Metadata:    string(metadataJSON),
		AccessToken: encryptedToken, // Save encrypted token directly
	}

	if err := h.DB.WithContext(ctx).Create(&newAccount).Error; err != nil {
		return fmt.Errorf("failed to create instagram account: %w", err)
	}

	// Verify the save was successful by reloading
	var verifyAccount storage.SocialAccount
	if verifyErr := h.DB.WithContext(ctx).First(&verifyAccount, newAccount.ID).Error; verifyErr == nil {
		h.Logger.Info("✅ Created new Instagram account",
			zap.Uint("tenant_id", tenantID),
			zap.Uint("account_id", newAccount.ID),
			zap.String("instagram_id", account.ID),
			zap.String("username", account.Username),
			zap.Bool("connected", verifyAccount.Connected),
			zap.Bool("has_access_token", verifyAccount.AccessToken != ""),
			zap.Int("access_token_length", len(encryptedToken)))
	} else {
		h.Logger.Warn("⚠️ Account created but verification reload failed",
			zap.Uint("tenant_id", tenantID),
			zap.String("instagram_id", account.ID),
			zap.Error(verifyErr))
	}

	return nil
}

// RefreshAccessToken refreshes a long-lived Instagram User access token
// Endpoint: https://graph.instagram.com/refresh_access_token
func (h *InstagramLoginOAuthHandler) RefreshAccessToken(ctx context.Context, accessToken string) (newToken string, expiresIn int, err error) {
	endpoint := "https://graph.instagram.com/refresh_access_token"
	
	params := url.Values{}
	params.Set("grant_type", "ig_refresh_token")
	params.Set("client_secret", h.AppSecret) // Required for token refresh
	params.Set("access_token", accessToken)

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := h.HTTPClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("Instagram API error %d: %s", resp.StatusCode, string(body))
	}

	var tokenResponse struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return "", 0, fmt.Errorf("failed to parse token response: %w", err)
	}

	return tokenResponse.AccessToken, tokenResponse.ExpiresIn, nil
}

// encodeState encodes OAuth state into a secure token
func (h *InstagramLoginOAuthHandler) encodeState(state instagramLoginOAuthState) (string, error) {
	jsonData, err := json.Marshal(state)
	if err != nil {
		return "", err
	}

	// Use a simple base64 encoding for now (can be enhanced with encryption)
	return base64.URLEncoding.EncodeToString(jsonData), nil
}

// decodeState decodes OAuth state from token
func (h *InstagramLoginOAuthHandler) decodeState(token string) (*instagramLoginOAuthState, error) {
	jsonData, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return nil, err
	}

	var state instagramLoginOAuthState
	if err := json.Unmarshal(jsonData, &state); err != nil {
		return nil, err
	}

	return &state, nil
}

// randomNonce generates a random nonce for state
func (h *InstagramLoginOAuthHandler) randomNonce() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

