package tenantapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/social"
	"go.uber.org/zap"
)

const (
	whatsAppSessionTTL = 15 * time.Minute

	sessionKeyPrefix = "whatsapp:signup:session:"
	tokenKeyPrefix   = "whatsapp:signup:token:"
)

var errWhatsAppSignupNotFound = errors.New("whatsapp signup payload not found")

type whatsAppCodeRequest struct {
	Code        string `json:"code"`
	RedirectURI string `json:"redirect_uri"`
}

type whatsAppSessionPayload struct {
	Type  string          `json:"type"`
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

type whatsAppSessionData struct {
	Event           string    `json:"event"`
	WABAID          string    `json:"waba_id"`
	PhoneNumberID   string    `json:"phone_number_id"`
	BusinessID      string    `json:"business_id"`
	CatalogIDs      []string  `json:"catalog_ids"`
	PageIDs         []string  `json:"page_ids"`
	DatasetIDs      []string  `json:"dataset_ids"`
	CapturedAt      time.Time `json:"captured_at"`
	Raw             json.RawMessage
	DisplayPhone    string `json:"display_phone_number"`
	VerifiedName    string `json:"verified_name"`
	SessionID       string `json:"session_id,omitempty"`
	ConfigurationID string `json:"config_id,omitempty"`
	CallbackURI     string `json:"callback_uri,omitempty"`
	FallbackURI     string `json:"fallback_redirect_uri,omitempty"`
}

type whatsAppTokenData struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresIn   int64     `json:"expires_in"`
	ReceivedAt  time.Time `json:"received_at"`
}

type cachedEntry struct {
	Payload    []byte
	CapturedAt time.Time
}

type metaTokenResponse struct {
	AccessToken string        `json:"access_token"`
	TokenType   string        `json:"token_type"`
	ExpiresIn   int64         `json:"expires_in"`
	Error       *metaAPIError `json:"error"`
}

type metaAPIError struct {
	Message      string `json:"message"`
	Type         string `json:"type"`
	Code         int    `json:"code"`
	ErrorSubcode int    `json:"error_subcode"`
}

type phoneNumberListing struct {
	Data []struct {
		ID                  string `json:"id"`
		DisplayPhoneNumber  string `json:"display_phone_number"`
		VerifiedName        string `json:"verified_name"`
		Status              string `json:"status"`
		QualityRating       string `json:"quality_rating"`
		PendingDeletion     bool   `json:"pending_deletion"`
		NewCertificate      bool   `json:"is_new_certificate"`
		NewCertificateState string `json:"new_certificate_status"`
	} `json:"data"`
	Error *metaAPIError `json:"error"`
}

type singlePhoneNumber struct {
	ID                 string        `json:"id"`
	DisplayPhoneNumber string        `json:"display_phone_number"`
	VerifiedName       string        `json:"verified_name"`
	Error              *metaAPIError `json:"error"`
}

func (h *Handler) handleWhatsAppSession(w http.ResponseWriter, r *http.Request) {
	var payload whatsAppSessionPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	if payload.Type != "WA_EMBEDDED_SIGNUP" {
		http.Error(w, "unsupported payload type", http.StatusBadRequest)
		return
	}

	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var sessionBody struct {
		WABAID        string   `json:"waba_id"`
		PhoneNumberID string   `json:"phone_number_id"`
		BusinessID    string   `json:"business_id"`
		CatalogIDs    []string `json:"catalog_ids"`
		PageIDs       []string `json:"page_ids"`
		DatasetIDs    []string `json:"dataset_ids"`
		SessionID     string   `json:"session_id"`
		ConfigID      string   `json:"config_id"`
		CallbackURI   string   `json:"callback_uri"`
		RedirectURI   string   `json:"redirect_uri"`
		FallbackURI   string   `json:"fallback_redirect_uri"`
	}

	if len(payload.Data) > 0 {
		if err := json.Unmarshal(payload.Data, &sessionBody); err != nil {
			h.Logger.Warn("failed to decode WA session data", zap.Error(err))
		}
	}

	if sessionBody.WABAID == "" {
		http.Error(w, "missing waba_id in session payload", http.StatusBadRequest)
		return
	}

	if h.Logger != nil {
		h.Logger.Info("whatsapp signup session raw data",
			zap.Stringer("tenant_id", claims.TenantID),
			zap.String("data", string(payload.Data)))
	}

	session := whatsAppSessionData{
		Event:           payload.Event,
		WABAID:          sessionBody.WABAID,
		PhoneNumberID:   sessionBody.PhoneNumberID,
		BusinessID:      sessionBody.BusinessID,
		CatalogIDs:      sessionBody.CatalogIDs,
		PageIDs:         sessionBody.PageIDs,
		DatasetIDs:      sessionBody.DatasetIDs,
		CapturedAt:      time.Now(),
		Raw:             payload.Data,
		SessionID:       sessionBody.SessionID,
		ConfigurationID: sessionBody.ConfigID,
		CallbackURI:     firstNonEmpty(sessionBody.CallbackURI, sessionBody.RedirectURI),
		FallbackURI:     sessionBody.FallbackURI,
	}

	if session.CallbackURI == "" || session.FallbackURI == "" {
		var generic map[string]interface{}
		if err := json.Unmarshal(payload.Data, &generic); err == nil {
			if session.CallbackURI == "" {
				session.CallbackURI = pickString(generic, "callback_uri", "redirect_uri", "callback_url")
			}
			if session.FallbackURI == "" {
				session.FallbackURI = pickString(generic, "fallback_redirect_uri", "fallback_redirect_url", "fallback_uri")
			}
		}
	}

	if err := h.storeWhatsAppSession(r.Context(), claims.TenantID, session); err != nil {
		h.Logger.Error("failed to store whatsapp session payload", zap.Error(err), zap.Stringer("tenant_id", claims.TenantID))
		http.Error(w, "failed to persist session payload", http.StatusInternalServerError)
		return
	}

	if h.Logger != nil {
		h.Logger.Info("received WhatsApp embedded signup session payload",
			zap.Stringer("tenant_id", claims.TenantID),
			zap.String("event", payload.Event))
	}

	if tokenData, err := h.getWhatsAppToken(r.Context(), claims.TenantID); err == nil {
		if err := h.finalizeWhatsAppSignup(r.Context(), claims.TenantID, session, tokenData); err != nil {
			h.Logger.Error("failed to finalize WhatsApp signup (session path)", zap.Error(err), zap.Stringer("tenant_id", claims.TenantID))
			http.Error(w, "failed to finalize signup", http.StatusBadGateway)
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleWhatsAppExchangeCode(w http.ResponseWriter, r *http.Request) {
	var req whatsAppCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		http.Error(w, "code required", http.StatusBadRequest)
		return
	}

	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if h.Logger != nil {
		prefix := req.Code
		if len(prefix) > 6 {
			prefix = prefix[:6]
		}
		h.Logger.Info("received WhatsApp exchange code",
			zap.Stringer("tenant_id", claims.TenantID),
			zap.String("code_prefix", prefix))
	}

	sessionData, sessionErr := h.getWhatsAppSession(r.Context(), claims.TenantID)
	if sessionErr != nil && !errors.Is(sessionErr, errWhatsAppSignupNotFound) {
		h.Logger.Warn("failed to load whatsapp session while exchanging code", zap.Error(sessionErr), zap.Stringer("tenant_id", claims.TenantID))
	}
	hasSession := sessionErr == nil

	if hasSession && h.Logger != nil {
		h.Logger.Info("whatsapp session redirect hints",
			zap.Stringer("tenant_id", claims.TenantID),
			zap.String("callback_uri", sessionData.CallbackURI),
			zap.String("fallback_uri", sessionData.FallbackURI))
	}

	redirectURI := normalizeBrancrRedirect(strings.TrimSpace(req.RedirectURI))
	if redirectURI == "" {
		redirectURI = normalizeBrancrRedirect(firstNonEmpty(
			sessionData.CallbackURI,
			sessionData.FallbackURI,
			h.Config.MetaSuccessURL,
			h.Config.PublicURL,
			h.Config.MetaRedirectURI,
			"https://www.brancr.com/",
		))
	}

	if shouldOmitRedirect(redirectURI) {
		h.Logger.Info("omitting redirect uri for meta exchange (embedded signup detected)",
			zap.Stringer("tenant_id", claims.TenantID),
			zap.String("original_redirect_uri", redirectURI))
		redirectURI = ""
	}

	h.Logger.Info("processing whatsapp code exchange", zap.Stringer("tenant_id", claims.TenantID), zap.String("redirect_uri", redirectURI))

	tokenResp, err := h.exchangeWhatsAppCode(r.Context(), req.Code, redirectURI)
	if err != nil {
		h.Logger.Error("failed to exchange whatsapp code", zap.Error(err), zap.Stringer("tenant_id", claims.TenantID))
		http.Error(w, "failed to exchange code", http.StatusBadGateway)
		return
	}

	tokenData := whatsAppTokenData{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
		ExpiresIn:   tokenResp.ExpiresIn,
		ReceivedAt:  time.Now(),
	}

	if err := h.storeWhatsAppToken(r.Context(), claims.TenantID, tokenData); err != nil {
		h.Logger.Error("failed to store whatsapp token", zap.Error(err), zap.Stringer("tenant_id", claims.TenantID))
		http.Error(w, "failed to persist token", http.StatusInternalServerError)
		return
	}

	if hasSession {
		if err := h.finalizeWhatsAppSignup(r.Context(), claims.TenantID, sessionData, tokenData); err != nil {
			h.Logger.Error("failed to finalize WhatsApp signup (token path)", zap.Error(err), zap.Stringer("tenant_id", claims.TenantID))
			http.Error(w, "failed to finalize signup", http.StatusBadGateway)
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) finalizeWhatsAppSignup(ctx context.Context, tenantID uuid.UUID, session whatsAppSessionData, token whatsAppTokenData) error {
	phoneID := session.PhoneNumberID
	displayNumber := session.DisplayPhone
	verifiedName := session.VerifiedName

	if phoneID == "" || displayNumber == "" {
		info, err := h.lookupPhoneNumber(ctx, session.WABAID, phoneID, token.AccessToken)
		if err != nil {
			return err
		}
		phoneID = info.ID
		displayNumber = info.DisplayPhoneNumber
		verifiedName = info.VerifiedName
	}

	if phoneID == "" {
		return errors.New("unable to determine phone_number_id from Meta response")
	}

	if err := social.SaveWhatsAppAccount(h.DB, h.Logger, h.Config, tenantID, session.BusinessID, session.WABAID, phoneID, firstNonEmpty(displayNumber, verifiedName), token.AccessToken, "APPROVED"); err != nil {
		return err
	}

	_ = h.deleteWhatsAppSession(ctx, tenantID)
	_ = h.deleteWhatsAppToken(ctx, tenantID)

	if h.Logger != nil {
		h.Logger.Info("finalized WhatsApp embedded signup",
			zap.Stringer("tenant_id", tenantID),
			zap.String("waba_id", session.WABAID),
			zap.String("phone_number_id", phoneID))
	}

	return nil
}

func (h *Handler) exchangeWhatsAppCode(ctx context.Context, code, redirectURI string) (*metaTokenResponse, error) {
	if h.Config.MetaAppID == "" || h.Config.MetaAppSecret == "" {
		return nil, errors.New("meta app credentials not configured")
	}

	form := url.Values{}
	form.Set("client_id", h.Config.MetaAppID)
	form.Set("client_secret", h.Config.MetaAppSecret)
	if strings.TrimSpace(redirectURI) != "" {
		form.Set("redirect_uri", redirectURI)
	}
	form.Set("code", code)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://graph.facebook.com/v18.0/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var token metaTokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 || token.AccessToken == "" {
		if token.Error != nil {
			return nil, fmt.Errorf("meta error (%d/%d): %s", token.Error.Code, token.Error.ErrorSubcode, token.Error.Message)
		}
		return nil, fmt.Errorf("meta returned status %d", resp.StatusCode)
	}

	return &token, nil
}

func (h *Handler) lookupPhoneNumber(ctx context.Context, wabaID, preferID, accessToken string) (*singlePhoneNumber, error) {
	if accessToken == "" {
		return nil, errors.New("access token empty")
	}

	if preferID != "" {
		info, err := h.fetchSinglePhoneNumber(ctx, preferID, accessToken)
		if err == nil && info != nil && info.ID != "" {
			return info, nil
		}
	}

	list, err := h.listPhoneNumbers(ctx, wabaID, accessToken)
	if err != nil {
		return nil, err
	}
	for _, entry := range list.Data {
		if preferID == "" || entry.ID == preferID {
			return &singlePhoneNumber{
				ID:                 entry.ID,
				DisplayPhoneNumber: entry.DisplayPhoneNumber,
				VerifiedName:       entry.VerifiedName,
			}, nil
		}
	}

	if preferID != "" {
		return nil, fmt.Errorf("phone number id %s not returned by Meta", preferID)
	}

	return nil, errors.New("no phone numbers returned by Meta")
}

func (h *Handler) fetchSinglePhoneNumber(ctx context.Context, phoneID, accessToken string) (*singlePhoneNumber, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("https://graph.facebook.com/v18.0/%s", phoneID), nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Set("fields", "id,display_phone_number,verified_name")
	q.Set("access_token", accessToken)
	req.URL.RawQuery = q.Encode()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var info singlePhoneNumber
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		if info.Error != nil {
			return nil, fmt.Errorf("meta error (%d/%d): %s", info.Error.Code, info.Error.ErrorSubcode, info.Error.Message)
		}
		return nil, fmt.Errorf("meta returned status %d while fetching phone id %s", resp.StatusCode, phoneID)
	}

	return &info, nil
}

func (h *Handler) listPhoneNumbers(ctx context.Context, wabaID, accessToken string) (*phoneNumberListing, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("https://graph.facebook.com/v18.0/%s/phone_numbers", wabaID), nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Set("fields", "id,display_phone_number,verified_name,status")
	q.Set("access_token", accessToken)
	req.URL.RawQuery = q.Encode()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var listing phoneNumberListing
	if err := json.Unmarshal(body, &listing); err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		if listing.Error != nil {
			return nil, fmt.Errorf("meta error (%d/%d): %s", listing.Error.Code, listing.Error.ErrorSubcode, listing.Error.Message)
		}
		return nil, fmt.Errorf("meta returned status %d while listing numbers", resp.StatusCode)
	}

	return &listing, nil
}

func (h *Handler) storeWhatsAppSession(ctx context.Context, tenantID uuid.UUID, data whatsAppSessionData) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	key := sessionKey(tenantID)

	if h.Redis != nil {
		return h.Redis.Set(ctx, key, payload, whatsAppSessionTTL).Err()
	}

	h.signupCache.Store(key, cachedEntry{Payload: payload, CapturedAt: time.Now()})
	return nil
}

func (h *Handler) storeWhatsAppToken(ctx context.Context, tenantID uuid.UUID, data whatsAppTokenData) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	key := tokenKey(tenantID)

	if h.Redis != nil {
		return h.Redis.Set(ctx, key, payload, whatsAppSessionTTL).Err()
	}

	h.tokenCache.Store(key, cachedEntry{Payload: payload, CapturedAt: time.Now()})
	return nil
}

func (h *Handler) getWhatsAppSession(ctx context.Context, tenantID uuid.UUID) (whatsAppSessionData, error) {
	key := sessionKey(tenantID)

	if h.Redis != nil {
		payload, err := h.Redis.Get(ctx, key).Bytes()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				return whatsAppSessionData{}, errWhatsAppSignupNotFound
			}
			return whatsAppSessionData{}, err
		}
		return decodeSessionPayload(payload)
	}

	if value, ok := h.signupCache.Load(key); ok {
		entry := value.(cachedEntry)
		if time.Since(entry.CapturedAt) > whatsAppSessionTTL {
			h.signupCache.Delete(key)
			return whatsAppSessionData{}, errWhatsAppSignupNotFound
		}
		return decodeSessionPayload(entry.Payload)
	}

	return whatsAppSessionData{}, errWhatsAppSignupNotFound
}

func (h *Handler) getWhatsAppToken(ctx context.Context, tenantID uuid.UUID) (whatsAppTokenData, error) {
	key := tokenKey(tenantID)

	if h.Redis != nil {
		payload, err := h.Redis.Get(ctx, key).Bytes()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				return whatsAppTokenData{}, errWhatsAppSignupNotFound
			}
			return whatsAppTokenData{}, err
		}
		return decodeTokenPayload(payload)
	}

	if value, ok := h.tokenCache.Load(key); ok {
		entry := value.(cachedEntry)
		if time.Since(entry.CapturedAt) > whatsAppSessionTTL {
			h.tokenCache.Delete(key)
			return whatsAppTokenData{}, errWhatsAppSignupNotFound
		}
		return decodeTokenPayload(entry.Payload)
	}

	return whatsAppTokenData{}, errWhatsAppSignupNotFound
}

func (h *Handler) deleteWhatsAppSession(ctx context.Context, tenantID uuid.UUID) error {
	key := sessionKey(tenantID)
	if h.Redis != nil {
		if err := h.Redis.Del(ctx, key).Err(); err != nil && !errors.Is(err, redis.Nil) {
			return err
		}
	} else {
		h.signupCache.Delete(key)
	}
	return nil
}

func (h *Handler) deleteWhatsAppToken(ctx context.Context, tenantID uuid.UUID) error {
	key := tokenKey(tenantID)
	if h.Redis != nil {
		if err := h.Redis.Del(ctx, key).Err(); err != nil && !errors.Is(err, redis.Nil) {
			return err
		}
	} else {
		h.tokenCache.Delete(key)
	}
	return nil
}

func decodeSessionPayload(payload []byte) (whatsAppSessionData, error) {
	var session whatsAppSessionData
	if err := json.Unmarshal(payload, &session); err != nil {
		return whatsAppSessionData{}, err
	}
	return session, nil
}

func decodeTokenPayload(payload []byte) (whatsAppTokenData, error) {
	var token whatsAppTokenData
	if err := json.Unmarshal(payload, &token); err != nil {
		return whatsAppTokenData{}, err
	}
	return token, nil
}

func pickString(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if val, ok := m[key]; ok {
			if str, ok := val.(string); ok && strings.TrimSpace(str) != "" {
				return str
			}
		}
	}
	return ""
}

func sessionKey(tenantID uuid.UUID) string {
	return fmt.Sprintf("%s%s", sessionKeyPrefix, tenantID)
}

func tokenKey(tenantID uuid.UUID) string {
	return fmt.Sprintf("%s%s", tokenKeyPrefix, tenantID)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func normalizeBrancrRedirect(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return value
	}

	host := strings.ToLower(parsed.Host)
	if strings.HasSuffix(host, "brancr.com") {
		if parsed.Path == "" || parsed.Path == "/" {
			parsed.Path = "/"
		} else {
			parsed.Path = "/"
		}
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.String()
	}
	return value
}

func shouldOmitRedirect(redirectURI string) bool {
	if strings.TrimSpace(redirectURI) == "" {
		return false
	}
	host := hostFromURL(redirectURI)
	if host == "" {
		return false
	}
	host = strings.ToLower(host)
	return strings.HasSuffix(host, "brancr.com")
}

func hostFromURL(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Host
}
