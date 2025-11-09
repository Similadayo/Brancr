package social

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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

type EmbeddedSignupHandler struct {
	DB     *gorm.DB
	Logger *zap.Logger
	Config *config.Config
}

type EmbeddedSignupInitResponse struct {
	SignupURL string `json:"signup_url"`
}

func NewEmbeddedSignupHandler(db *gorm.DB, logger *zap.Logger, cfg *config.Config) *EmbeddedSignupHandler {
	if cfg == nil {
		return nil
	}
	return &EmbeddedSignupHandler{DB: db, Logger: logger, Config: cfg}
}

func (h *EmbeddedSignupHandler) Start(w http.ResponseWriter, r *http.Request) {
	if h.Config.MetaAppID == "" || h.Config.MetaEmbeddedConfigID == "" {
		h.Logger.Warn("embedded signup unavailable: missing app_id or config_id")
		http.Error(w, "Meta embedded signup not configured", http.StatusNotImplemented)
		return
	}

	q := r.URL.Query()
	tenantID := q.Get("tenant_id")
	if tenantID == "" {
		http.Error(w, "tenant_id required", http.StatusBadRequest)
		return
	}

	extras := h.Config.MetaEmbeddedExtras
	if strings.TrimSpace(extras) == "" {
		extras = `{"sessionInfoVersion":"3","version":"v3"}`
	}

	signupURL := fmt.Sprintf(
		"https://business.facebook.com/messaging/whatsapp/onboard/?app_id=%s&config_id=%s&extras=%s",
		url.QueryEscape(h.Config.MetaAppID),
		url.QueryEscape(h.Config.MetaEmbeddedConfigID),
		url.QueryEscape(extras),
	)

	state := fmt.Sprintf("tenant:%s:%d", tenantID, time.Now().Unix())
	signupURL = signupURL + "&state=" + url.QueryEscape(state)

	http.Redirect(w, r, signupURL, http.StatusFound)
}

type EmbeddedSignupCallbackRequest struct {
	WABAID        string `json:"waba_id"`
	PhoneNumberID string `json:"phone_number_id"`
	PhoneNumber   string `json:"phone_number"`
	BusinessName  string `json:"business_name"`
	TenantID      string `json:"tenant_id"`
	AccessToken   string `json:"access_token"`
}

func (h *EmbeddedSignupHandler) Callback(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form payload", http.StatusBadRequest)
		return
	}

	signedRequest := r.PostFormValue("signed_request")
	if signedRequest == "" {
		// Fallback to the legacy JSON payload for easier testing
		var legacy EmbeddedSignupCallbackRequest
		if err := json.NewDecoder(r.Body).Decode(&legacy); err != nil {
			http.Error(w, "missing signed_request", http.StatusBadRequest)
			return
		}
		h.handleLegacyCallback(w, legacy)
		return
	}

	payloadBytes, err := decodeSignedRequest(signedRequest, h.Config.MetaAppSecret)
	if err != nil {
		h.Logger.Error("invalid signed_request", zap.Error(err))
		http.Error(w, "invalid signed_request", http.StatusBadRequest)
		return
	}

	h.Logger.Info("WhatsApp embedded signup payload", zap.ByteString("payload", payloadBytes))

	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		h.Logger.Error("failed to parse embedded signup payload", zap.Error(err))
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	clientState := getString(payload, "client_state")
	tenantID := parseTenantState(clientState)
	if tenantID == 0 {
		tenantID = parseTenantID(r.PostFormValue("tenant_id"))
	}

	phoneID := getString(payload, "phone_number_id")
	phoneNumber := getString(payload, "phone_number")
	wabaID := getString(payload, "wa_business_id")
	if wabaID == "" {
		wabaID = getString(payload, "waba_info", "waba_id")
	}

	token := getString(payload, "token")
	if token == "" {
		token = getString(payload, "waba_info", "token")
	}
	if token == "" {
		token = getString(payload, "waba_info", "permanent_token")
	}

	status := strings.ToUpper(getString(payload, "phone_number_status"))
	if status == "" {
		status = strings.ToUpper(getString(payload, "waba_info", "onboarding_status"))
	}

	if tenantID == 0 || phoneID == "" || wabaID == "" {
		http.Error(w, "missing identifiers in payload", http.StatusBadRequest)
		return
	}

	if token == "" {
		h.Logger.Warn("embedded signup payload missing token", zap.String("phone_number_id", phoneID))
	}

	if err := h.upsertWhatsAppAccount(tenantID, phoneID, phoneNumber, token, status); err != nil {
		h.Logger.Error("failed to save whatsapp account", zap.Error(err))
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	h.Logger.Info("✅ WhatsApp embedded signup stored",
		zap.Uint("tenant_id", tenantID),
		zap.String("phone_number_id", phoneID),
		zap.String("status", status))

	w.WriteHeader(http.StatusOK)
}

func (h *EmbeddedSignupHandler) handleLegacyCallback(w http.ResponseWriter, payload EmbeddedSignupCallbackRequest) {
	if strings.TrimSpace(payload.TenantID) == "" || payload.WABAID == "" || payload.PhoneNumberID == "" || payload.AccessToken == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}

	if err := h.upsertWhatsAppAccount(
		parseTenantID(payload.TenantID),
		payload.PhoneNumberID,
		payload.PhoneNumber,
		payload.AccessToken,
		"APPROVED",
	); err != nil {
		h.Logger.Error("failed to save whatsapp account", zap.Error(err))
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func encryptIfPossible(token string) (string, error) {
	if token == "" {
		return "", nil
	}
	enc, err := utils.EncryptToken(token)
	if err != nil {
		return token, nil
	}
	return enc, nil
}

func parseTenantID(raw string) uint {
	if raw == "" {
		return 0
	}
	val, _ := strconv.ParseUint(raw, 10, 64)
	return uint(val)
}

func parseTenantState(state string) uint {
	if state == "" {
		return 0
	}
	if !strings.HasPrefix(state, "tenant:") {
		return 0
	}
	parts := strings.Split(state, ":")
	if len(parts) < 2 {
		return 0
	}
	return parseTenantID(parts[1])
}

func decodeSignedRequest(sr, secret string) ([]byte, error) {
	parts := strings.SplitN(sr, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid signed_request format")
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid signature encoding: %w", err)
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid payload encoding: %w", err)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[1]))
	expected := mac.Sum(nil)

	if !hmac.Equal(signature, expected) {
		return nil, fmt.Errorf("signature mismatch")
	}

	return payload, nil
}

func getString(data map[string]any, path ...string) string {
	current := any(data)
	for _, p := range path {
		switch node := current.(type) {
		case map[string]any:
			current = node[p]
		default:
			return ""
		}
	}

	switch v := current.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

func (h *EmbeddedSignupHandler) upsertWhatsAppAccount(tenantID uint, phoneID, phoneNumber, token, status string) error {
	var account storage.SocialAccount
	err := h.DB.Where("tenant_id = ? AND platform = ?", tenantID, "whatsapp").First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		account = storage.SocialAccount{
			TenantID: tenantID,
			Platform: "whatsapp",
		}
	} else if err != nil {
		return err
	}

	encrypted, err := encryptIfPossible(token)
	if err != nil {
		return err
	}

	if encrypted != "" {
		account.AccessToken = encrypted
	}

	account.PageID = phoneID
	account.ExternalID = phoneID
	account.Username = phoneNumber
	account.Mode = "oauth2"
	account.Connected = strings.ToUpper(status) == "APPROVED" || status == ""

	return h.DB.Save(&account).Error
}
