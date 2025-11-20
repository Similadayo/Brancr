package social

import (
	"context"
	"crypto/hmac"
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

// MetaOAuthHandler manages the OAuth flow for Meta platforms (Facebook, Instagram, WhatsApp).
type MetaOAuthHandler struct {
	DB              *gorm.DB
	Logger          *zap.Logger
	AppID           string
	AppSecret       string
	RedirectURI     string
	SuccessRedirect string
	HTTPClient      *http.Client
}

// NewMetaOAuthHandler returns a configured Meta OAuth handler or nil if configuration is incomplete.
func NewMetaOAuthHandler(db *gorm.DB, logger *zap.Logger, cfg *config.Config) (*MetaOAuthHandler, error) {
	if cfg.MetaAppID == "" || cfg.MetaAppSecret == "" || cfg.MetaRedirectURI == "" {
		logger.Info("Meta OAuth disabled — missing META_APP_ID, META_APP_SECRET, or META_REDIRECT_URI")
		return nil, nil
	}

	client := &http.Client{Timeout: 15 * time.Second}

	return &MetaOAuthHandler{
		DB:              db,
		Logger:          logger,
		AppID:           cfg.MetaAppID,
		AppSecret:       cfg.MetaAppSecret,
		RedirectURI:     cfg.MetaRedirectURI,
		SuccessRedirect: cfg.MetaSuccessURL,
		HTTPClient:      client,
	}, nil
}

type metaOAuthState struct {
	TenantID        uint     `json:"tenant_id"`
	Platforms       []string `json:"platforms"`
	PageID          string   `json:"page_id,omitempty"`
	InstagramID     string   `json:"instagram_id,omitempty"`
	BusinessID      string   `json:"business_id,omitempty"`
	PhoneNumberID   string   `json:"phone_number_id,omitempty"`
	SuccessRedirect string   `json:"success_redirect,omitempty"`
	Nonce           string   `json:"nonce"`
	IssuedAt        int64    `json:"iat"`
}

// StartOAuth initiates the Meta OAuth authorization flow.
func (h *MetaOAuthHandler) StartOAuth(w http.ResponseWriter, r *http.Request) {
	if h == nil {
		http.Error(w, "Meta OAuth disabled", http.StatusNotImplemented)
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

	platforms := h.parsePlatforms(query.Get("platforms"), query.Get("platform"))
	if len(platforms) == 0 {
		http.Error(w, "no valid platforms specified", http.StatusBadRequest)
		return
	}

	successRedirect := query.Get("success_redirect")
	if successRedirect == "" {
		successRedirect = h.SuccessRedirect
	}

	state := metaOAuthState{
		TenantID:        uint(tenantIDUint),
		Platforms:       platforms,
		PageID:          strings.TrimSpace(query.Get("page_id")),
		InstagramID:     strings.TrimSpace(query.Get("instagram_id")),
		BusinessID:      strings.TrimSpace(query.Get("business_id")),
		PhoneNumberID:   strings.TrimSpace(query.Get("phone_number_id")),
		SuccessRedirect: successRedirect,
		Nonce:           h.randomNonce(),
		IssuedAt:        time.Now().Unix(),
	}

	stateToken, err := h.encodeState(state)
	if err != nil {
		h.Logger.Error("failed to encode meta oauth state", zap.Error(err))
		http.Error(w, "failed to generate state", http.StatusInternalServerError)
		return
	}

	scopes := h.resolveScopes(platforms)
	if len(scopes) == 0 {
		http.Error(w, "no scopes available for requested platforms", http.StatusBadRequest)
		return
	}

	authURL := url.URL{
		Scheme: "https",
		Host:   "www.facebook.com",
		Path:   "/v18.0/dialog/oauth",
	}

	params := url.Values{}
	params.Set("client_id", h.AppID)
	params.Set("redirect_uri", h.RedirectURI)
	params.Set("response_type", "code")
	params.Set("scope", strings.Join(scopes, ","))
	params.Set("state", stateToken)
	params.Set("auth_type", "rerequest")

	if display := query.Get("display"); display != "" {
		params.Set("display", display)
	}

	authURL.RawQuery = params.Encode()
	h.Logger.Info("Redirecting to Meta OAuth", zap.Uint("tenant_id", state.TenantID), zap.Strings("platforms", platforms))

	http.Redirect(w, r, authURL.String(), http.StatusFound)
}

// Callback handles Meta OAuth redirect and persists connected accounts.
func (h *MetaOAuthHandler) Callback(w http.ResponseWriter, r *http.Request) {
	if h == nil {
		http.Error(w, "Meta OAuth disabled", http.StatusNotImplemented)
		return
	}

	ctx := r.Context()
	query := r.URL.Query()

	if errParam := query.Get("error"); errParam != "" {
		errDescription := query.Get("error_description")
		h.Logger.Warn("Meta OAuth denied", zap.String("error", errParam), zap.String("description", errDescription))
		successRedirect := ""
		if rawState := query.Get("state"); rawState != "" {
			if st, err := h.decodeState(rawState); err == nil {
				successRedirect = st.SuccessRedirect
			}
		}
		h.respondError(w, r, successRedirect, fmt.Sprintf("meta oauth error: %s", errParam))
		return
	}

	stateRaw := query.Get("state")
	if stateRaw == "" {
		h.Logger.Warn("Meta OAuth callback received without state parameter")
		// Redirect to success page with error status instead of returning error
		h.respondError(w, r, h.getDefaultSuccessURL(), "OAuth state parameter missing. This may happen if the OAuth flow was interrupted.")
		return
	}

	state, err := h.decodeState(stateRaw)
	if err != nil {
		h.Logger.Error("failed to decode meta oauth state", zap.Error(err))
		// Redirect to success page with error status instead of returning error
		h.respondError(w, r, h.getDefaultSuccessURL(), "Invalid OAuth state. Please try connecting again.")
		return
	}

	code := query.Get("code")
	if code == "" {
		h.Logger.Warn("Meta OAuth callback received without authorization code")
		// Redirect to success page with error status instead of returning error
		h.respondError(w, r, state.SuccessRedirect, "Authorization code missing. The OAuth flow may have been cancelled.")
		return
	}

	shortToken, err := h.exchangeCode(ctx, code)
	if err != nil {
		h.Logger.Error("failed to exchange code for token", zap.Error(err))
		h.respondError(w, r, state.SuccessRedirect, "failed to obtain access token")
		return
	}

	accessToken, expiresIn, err := h.exchangeLongLived(ctx, shortToken)
	if err != nil {
		h.Logger.Warn("failed to exchange for long-lived token, falling back to short-lived", zap.Error(err))
		accessToken = shortToken.AccessToken
		expiresIn = shortToken.ExpiresIn
	}

	results, err := h.processPlatforms(ctx, state, accessToken, expiresIn)
	if err != nil {
		h.Logger.Error("failed to process meta platforms", zap.Error(err))
		h.respondError(w, r, state.SuccessRedirect, err.Error())
		return
	}

	h.respondSuccess(w, r, state, results)
}

type metaTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

func (h *MetaOAuthHandler) exchangeCode(ctx context.Context, code string) (*metaTokenResponse, error) {
	params := url.Values{}
	params.Set("client_id", h.AppID)
	params.Set("redirect_uri", h.RedirectURI)
	params.Set("client_secret", h.AppSecret)
	params.Set("code", code)

	endpoint := url.URL{
		Scheme:   "https",
		Host:     "graph.facebook.com",
		Path:     "/v18.0/oauth/access_token",
		RawQuery: params.Encode(),
	}

	resp, err := h.HTTPClient.Do(newRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("meta token exchange error (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var token metaTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, err
	}

	if token.AccessToken == "" {
		return nil, errors.New("empty access token returned by Meta")
	}

	return &token, nil
}

func (h *MetaOAuthHandler) exchangeLongLived(ctx context.Context, short *metaTokenResponse) (string, int64, error) {
	params := url.Values{}
	params.Set("grant_type", "fb_exchange_token")
	params.Set("client_id", h.AppID)
	params.Set("client_secret", h.AppSecret)
	params.Set("fb_exchange_token", short.AccessToken)

	endpoint := url.URL{
		Scheme:   "https",
		Host:     "graph.facebook.com",
		Path:     "/v18.0/oauth/access_token",
		RawQuery: params.Encode(),
	}

	resp, err := h.HTTPClient.Do(newRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil))
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", 0, fmt.Errorf("meta long-lived exchange error (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var token metaTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return "", 0, err
	}

	if token.AccessToken == "" {
		return "", 0, errors.New("long-lived token missing access token")
	}

	expires := token.ExpiresIn
	if expires == 0 {
		expires = short.ExpiresIn
	}

	return token.AccessToken, expires, nil
}

type metaPlatformResult struct {
	Platform   string `json:"platform"`
	PageID     string `json:"page_id,omitempty"`
	ExternalID string `json:"external_id,omitempty"`
	Username   string `json:"username,omitempty"`
	Message    string `json:"message,omitempty"`
}

type metaOAuthResult struct {
	TenantID  uint                 `json:"tenant_id"`
	Connected []metaPlatformResult `json:"connected"`
	Warnings  []metaPlatformResult `json:"warnings,omitempty"`
}

func (h *MetaOAuthHandler) processPlatforms(ctx context.Context, state metaOAuthState, userAccessToken string, expiresIn int64) (*metaOAuthResult, error) {
	result := &metaOAuthResult{TenantID: state.TenantID}

	expiresAt := h.expiresAt(expiresIn)

	var pageData *metaPageSelection
	needPageData := false
	for _, platform := range state.Platforms {
		if platform == "facebook" || platform == "instagram" {
			needPageData = true
		}
	}

	if needPageData {
		selection, err := h.selectPage(ctx, userAccessToken, state)
		if err != nil {
			h.Logger.Warn("failed to resolve Facebook page", zap.Uint("tenant_id", state.TenantID), zap.Error(err))
			result.Warnings = append(result.Warnings, metaPlatformResult{
				Platform: "facebook",
				Message:  fmt.Sprintf("failed to resolve Facebook page: %v", err),
			})
		} else {
			pageData = selection
		}
	}

	for _, platform := range state.Platforms {
		switch platform {
		case "facebook":
			if pageData == nil || pageData.Page == nil {
				result.Warnings = append(result.Warnings, metaPlatformResult{Platform: "facebook", Message: "no Facebook page available"})
				continue
			}

			if err := h.saveFacebookAccount(ctx, state.TenantID, pageData.Page, expiresAt); err != nil {
				result.Warnings = append(result.Warnings, metaPlatformResult{Platform: "facebook", Message: err.Error()})
				continue
			}

			result.Connected = append(result.Connected, metaPlatformResult{
				Platform:   "facebook",
				PageID:     pageData.Page.ID,
				ExternalID: pageData.Page.ID,
				Username:   pageData.Page.Name,
				Message:    "Facebook Page connected",
			})

		case "instagram":
			if pageData == nil || pageData.Instagram == nil {
				result.Warnings = append(result.Warnings, metaPlatformResult{Platform: "instagram", Message: "no Instagram business account linked"})
				continue
			}

			if err := h.saveInstagramAccount(ctx, state.TenantID, pageData.Instagram, pageData.Page.ID, pageData.Page.AccessToken, expiresAt); err != nil {
				result.Warnings = append(result.Warnings, metaPlatformResult{Platform: "instagram", Message: err.Error()})
				continue
			}

			result.Connected = append(result.Connected, metaPlatformResult{
				Platform:   "instagram",
				PageID:     pageData.Page.ID, // Facebook Page ID (for subscription)
				ExternalID: pageData.Instagram.ID, // Instagram Business Account ID (for messaging)
				Username:   pageData.Instagram.Username,
				Message:    "Instagram Business Account connected",
			})

		case "whatsapp":
			selection, err := h.selectWhatsApp(ctx, userAccessToken, state)
			if err != nil {
				result.Warnings = append(result.Warnings, metaPlatformResult{Platform: "whatsapp", Message: err.Error()})
				continue
			}

			if err := h.saveWhatsAppAccount(ctx, state.TenantID, selection, userAccessToken, expiresAt); err != nil {
				result.Warnings = append(result.Warnings, metaPlatformResult{Platform: "whatsapp", Message: err.Error()})
				continue
			}

			result.Connected = append(result.Connected, metaPlatformResult{
				Platform:   "whatsapp",
				PageID:     selection.Phone.ID,
				ExternalID: selection.Phone.ID,
				Username:   selection.Phone.DisplayPhoneNumber,
				Message:    "WhatsApp Business Phone connected",
			})
		}
	}

	return result, nil
}

type metaPage struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	AccessToken string `json:"access_token"`
	Instagram   *struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"instagram_business_account"`
}

type metaPageSelection struct {
	Page      *metaPage
	Instagram *metaInstagramAccount
}

type metaInstagramAccount struct {
	ID       string
	Username string
	Token    string
}

func (h *MetaOAuthHandler) selectPage(ctx context.Context, accessToken string, state metaOAuthState) (*metaPageSelection, error) {
	pages, err := h.fetchPages(ctx, accessToken)
	if err != nil {
		return nil, err
	}

	if len(pages) == 0 {
		return nil, errors.New("no Facebook pages available for this user")
	}

	var selected *metaPage
	requestedPage := strings.TrimSpace(state.PageID)

	if requestedPage != "" {
		for idx := range pages {
			if pages[idx].ID == requestedPage {
				selected = &pages[idx]
				break
			}
		}
	}

	if selected == nil {
		selected = &pages[0]
	}

	selection := &metaPageSelection{Page: selected}
	if selected.AccessToken == "" {
		return nil, fmt.Errorf("page %s missing access token", selected.ID)
	}

	instaID := strings.TrimSpace(state.InstagramID)
	var igAccount *metaInstagramAccount

	if selected.Instagram != nil {
		igAccount = &metaInstagramAccount{
			ID:       selected.Instagram.ID,
			Username: selected.Instagram.Username,
			Token:    selected.AccessToken,
		}
	}

	if instaID != "" {
		// If requested Instagram account differs, search across pages
		if igAccount == nil || igAccount.ID != instaID {
			for idx := range pages {
				ig := pages[idx].Instagram
				if ig != nil && ig.ID == instaID {
					selection.Page = &pages[idx]
					igAccount = &metaInstagramAccount{
						ID:       ig.ID,
						Username: ig.Username,
						Token:    pages[idx].AccessToken,
					}
					break
				}
			}
		}
	}

	// Ensure Instagram account exists if requested platform includes Instagram
	selection.Instagram = igAccount

	return selection, nil
}

func (h *MetaOAuthHandler) fetchPages(ctx context.Context, accessToken string) ([]metaPage, error) {
	endpoint := "https://graph.facebook.com/v18.0/me/accounts"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Set("access_token", accessToken)
	q.Set("fields", "id,name,access_token,instagram_business_account{id,username}")
	req.URL.RawQuery = q.Encode()

	resp, err := h.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch pages (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Data []metaPage `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	return payload.Data, nil
}

type metaWhatsAppSelection struct {
	Business *metaBusiness
	WABA     *metaWABA
	Phone    *metaWhatsAppPhone
}

type metaBusiness struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type metaWABA struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type metaWhatsAppPhone struct {
	ID                 string `json:"id"`
	DisplayPhoneNumber string `json:"display_phone_number"`
	VerifiedName       string `json:"verified_name"`
}

func (h *MetaOAuthHandler) selectWhatsApp(ctx context.Context, accessToken string, state metaOAuthState) (*metaWhatsAppSelection, error) {
	business, err := h.fetchBusiness(ctx, accessToken, state.BusinessID)
	if err != nil {
		return nil, err
	}

	waba, err := h.fetchWhatsAppBusiness(ctx, accessToken, business.ID, state)
	if err != nil {
		return nil, err
	}

	phone, err := h.fetchWhatsAppPhone(ctx, accessToken, waba.ID, state.PhoneNumberID)
	if err != nil {
		return nil, err
	}

	return &metaWhatsAppSelection{Business: business, WABA: waba, Phone: phone}, nil
}

func (h *MetaOAuthHandler) fetchBusiness(ctx context.Context, accessToken, requestedID string) (*metaBusiness, error) {
	endpoint := "https://graph.facebook.com/v18.0/me/businesses"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Set("access_token", accessToken)
	q.Set("fields", "id,name")
	req.URL.RawQuery = q.Encode()

	resp, err := h.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch businesses (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Data []metaBusiness `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	if len(payload.Data) == 0 {
		return nil, errors.New("no Meta business accounts available")
	}

	if requestedID != "" {
		for idx := range payload.Data {
			if payload.Data[idx].ID == requestedID {
				return &payload.Data[idx], nil
			}
		}
	}

	return &payload.Data[0], nil
}

func (h *MetaOAuthHandler) fetchWhatsAppBusiness(ctx context.Context, accessToken, businessID string, state metaOAuthState) (*metaWABA, error) {
	endpoint := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/owned_whatsapp_business_accounts", businessID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Set("access_token", accessToken)
	q.Set("fields", "id,name")
	req.URL.RawQuery = q.Encode()

	resp, err := h.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch WhatsApp accounts (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Data []metaWABA `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	if len(payload.Data) == 0 {
		return nil, errors.New("no WhatsApp Business Accounts available")
	}

	if state.BusinessID != "" {
		for idx := range payload.Data {
			if payload.Data[idx].ID == state.BusinessID {
				return &payload.Data[idx], nil
			}
		}
	}

	if state.PhoneNumberID != "" {
		// Try to find phone number in each WABA to match requested phone number later
		for idx := range payload.Data {
			phone, err := h.fetchWhatsAppPhone(ctx, accessToken, payload.Data[idx].ID, state.PhoneNumberID)
			if err == nil && phone != nil && phone.ID == state.PhoneNumberID {
				return &payload.Data[idx], nil
			}
		}
	}

	return &payload.Data[0], nil
}

func (h *MetaOAuthHandler) fetchWhatsAppPhone(ctx context.Context, accessToken, wabaID, requestedPhone string) (*metaWhatsAppPhone, error) {
	endpoint := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/phone_numbers", wabaID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Set("access_token", accessToken)
	q.Set("fields", "id,display_phone_number,verified_name")
	req.URL.RawQuery = q.Encode()

	resp, err := h.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch WhatsApp phone numbers (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Data []metaWhatsAppPhone `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	if len(payload.Data) == 0 {
		return nil, errors.New("no WhatsApp phone numbers available")
	}

	if requestedPhone != "" {
		for idx := range payload.Data {
			if payload.Data[idx].ID == requestedPhone {
				return &payload.Data[idx], nil
			}
		}
	}

	return &payload.Data[0], nil
}

func (h *MetaOAuthHandler) saveFacebookAccount(ctx context.Context, tenantID uint, page *metaPage, expiresAt *time.Time) error {
	if page.AccessToken == "" {
		return errors.New("page access token missing")
	}

	encrypted, err := utils.EncryptToken(page.AccessToken)
	if err != nil {
		return fmt.Errorf("failed to encrypt page token: %w", err)
	}

	return h.upsertSocialAccount(ctx, tenantID, "facebook", func(acc *storage.SocialAccount) {
		acc.AccessToken = encrypted
		acc.RefreshToken = ""
		acc.PageID = page.ID
		acc.ExternalID = page.ID
		acc.Username = page.Name
		acc.Mode = "oauth2"
		acc.Connected = true
		acc.ExpiresAt = expiresAt
	})
}

func (h *MetaOAuthHandler) saveInstagramAccount(ctx context.Context, tenantID uint, ig *metaInstagramAccount, facebookPageID, pageToken string, expiresAt *time.Time) error {
	if ig == nil || ig.ID == "" {
		return errors.New("instagram account missing")
	}
	if pageToken == "" {
		return errors.New("page token missing for instagram")
	}
	if facebookPageID == "" {
		return errors.New("facebook page id missing for instagram")
	}

	encrypted, err := utils.EncryptToken(pageToken)
	if err != nil {
		return fmt.Errorf("failed to encrypt instagram token: %w", err)
	}

	return h.upsertSocialAccount(ctx, tenantID, "instagram", func(acc *storage.SocialAccount) {
		acc.AccessToken = encrypted
		acc.RefreshToken = ""
		acc.PageID = facebookPageID // Store Facebook Page ID (needed for webhook subscription)
		acc.ExternalID = ig.ID // Store Instagram Business Account ID (used for messaging API and webhook routing)
		acc.Username = ig.Username
		acc.Mode = "oauth2"
		acc.Connected = true
		acc.ExpiresAt = expiresAt
	})
}

func (h *MetaOAuthHandler) saveWhatsAppAccount(ctx context.Context, tenantID uint, selection *metaWhatsAppSelection, token string, expiresAt *time.Time) error {
	if selection == nil || selection.Phone == nil {
		return errors.New("whatsapp phone selection missing")
	}
	if token == "" {
		return errors.New("whatsapp token missing")
	}

	encrypted, err := utils.EncryptToken(token)
	if err != nil {
		return fmt.Errorf("failed to encrypt whatsapp token: %w", err)
	}

	return h.upsertSocialAccount(ctx, tenantID, "whatsapp", func(acc *storage.SocialAccount) {
		acc.AccessToken = encrypted
		acc.RefreshToken = ""
		acc.PageID = selection.Phone.ID
		acc.ExternalID = selection.Phone.ID
		acc.Username = selection.Phone.DisplayPhoneNumber
		acc.Mode = "oauth2"
		acc.Connected = true
		acc.ExpiresAt = expiresAt
	})
}

func (h *MetaOAuthHandler) upsertSocialAccount(ctx context.Context, tenantID uint, platform string, update func(*storage.SocialAccount)) error {
	var account storage.SocialAccount
	result := h.DB.WithContext(ctx).Where("tenant_id = ? AND platform = ?", tenantID, platform).First(&account)

	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		account = storage.SocialAccount{TenantID: tenantID, Platform: platform, CreatedAt: time.Now()}
	} else if result.Error != nil {
		return result.Error
	}

	update(&account)

	return h.DB.WithContext(ctx).Save(&account).Error
}

func (h *MetaOAuthHandler) resolveScopes(platforms []string) []string {
	scopeSet := map[string]struct{}{}

	for _, platform := range platforms {
		switch platform {
		case "facebook":
			h.addScopes(scopeSet,
				"pages_manage_posts",
				"pages_read_engagement",
				"pages_manage_metadata",
				"pages_show_list",
				"pages_messaging",
				"public_profile",
			)
		case "instagram":
			h.addScopes(scopeSet,
				"instagram_basic",
				"instagram_manage_messages",
				"instagram_manage_comments",
				"instagram_content_publish",
				"pages_show_list",
				"pages_messaging", // REQUIRED for Instagram messaging API (uses Page Access Token)
				"business_management",
			)
		case "whatsapp":
			h.addScopes(scopeSet,
				"whatsapp_business_messaging",
				"whatsapp_business_management",
				"business_management",
			)
		}
	}

	scopes := make([]string, 0, len(scopeSet))
	for scope := range scopeSet {
		scopes = append(scopes, scope)
	}

	return scopes
}

func (h *MetaOAuthHandler) addScopes(set map[string]struct{}, scopes ...string) {
	for _, scope := range scopes {
		if scope == "" {
			continue
		}
		set[scope] = struct{}{}
	}
}

func (h *MetaOAuthHandler) parsePlatforms(primary, fallback string) []string {
	var platforms []string
	input := primary
	if input == "" {
		input = fallback
	}

	if input == "" {
		return []string{"facebook", "instagram"}
	}

	seen := map[string]struct{}{}
	parts := strings.Split(input, ",")
	for _, part := range parts {
		p := strings.ToLower(strings.TrimSpace(part))
		if p == "" {
			continue
		}
		switch p {
		case "facebook", "instagram", "whatsapp":
			if _, ok := seen[p]; !ok {
				platforms = append(platforms, p)
				seen[p] = struct{}{}
			}
		}
	}

	return platforms
}

func (h *MetaOAuthHandler) encodeState(state metaOAuthState) (string, error) {
	payload, err := json.Marshal(state)
	if err != nil {
		return "", err
	}

	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(h.AppSecret))
	mac.Write([]byte(encoded))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return encoded + "." + signature, nil
}

func (h *MetaOAuthHandler) decodeState(token string) (metaOAuthState, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return metaOAuthState{}, errors.New("invalid state token format")
	}

	encoded := parts[0]
	receivedSig := parts[1]

	mac := hmac.New(sha256.New, []byte(h.AppSecret))
	mac.Write([]byte(encoded))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expectedSig), []byte(receivedSig)) {
		return metaOAuthState{}, errors.New("state signature mismatch")
	}

	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return metaOAuthState{}, err
	}

	var state metaOAuthState
	if err := json.Unmarshal(payload, &state); err != nil {
		return metaOAuthState{}, err
	}

	return state, nil
}

func (h *MetaOAuthHandler) randomNonce() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func (h *MetaOAuthHandler) expiresAt(expiresIn int64) *time.Time {
	if expiresIn <= 0 {
		return nil
	}
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	return &deadline
}

func (h *MetaOAuthHandler) respondError(w http.ResponseWriter, r *http.Request, redirect, message string) {
	trimmed := strings.TrimSpace(message)
	if len(trimmed) > 200 {
		trimmed = trimmed[:200]
	}

	errorURL := redirect
	if errorURL == "" {
		errorURL = h.getDefaultSuccessURL()
	}

	if uri, err := url.Parse(errorURL); err == nil {
			q := uri.Query()
			q.Set("status", "error")
			q.Set("reason", trimmed)
			uri.RawQuery = q.Encode()
			http.Redirect(w, r, uri.String(), http.StatusFound)
			return
	}

	// Fallback to error response if redirect URL is invalid
	http.Error(w, trimmed, http.StatusBadRequest)
}

func (h *MetaOAuthHandler) respondSuccess(w http.ResponseWriter, r *http.Request, state metaOAuthState, result *metaOAuthResult) {
	successURL := state.SuccessRedirect
	if successURL == "" {
		successURL = h.getDefaultSuccessURL()
	}

	if uri, err := url.Parse(successURL); err == nil {
			platforms := make([]string, len(result.Connected))
			for i, item := range result.Connected {
				platforms[i] = item.Platform
			}

			q := uri.Query()
			q.Set("status", "success")
			if len(platforms) > 0 {
				q.Set("platforms", strings.Join(platforms, ","))
			}
			if len(result.Warnings) > 0 {
				q.Set("warnings", strconv.Itoa(len(result.Warnings)))
			}
			uri.RawQuery = q.Encode()
			http.Redirect(w, r, uri.String(), http.StatusFound)
			return
	}

	// Fallback to JSON if redirect URL is invalid
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}

// getDefaultSuccessURL returns the default OAuth success page URL
func (h *MetaOAuthHandler) getDefaultSuccessURL() string {
	if h.SuccessRedirect != "" {
		return h.SuccessRedirect
	}
	// Default to OAuth success page endpoint
	return "/oauth/success"
}

func newRequestWithContext(ctx context.Context, method, endpoint string, body io.Reader) *http.Request {
	req, _ := http.NewRequestWithContext(ctx, method, endpoint, body)
	return req
}
