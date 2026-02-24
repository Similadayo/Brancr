package social

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/config"
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
	WABAID         string `json:"waba_id"`
	PhoneNumberID  string `json:"phone_number_id"`
	PhoneNumber    string `json:"phone_number"`
	BusinessName   string `json:"business_name"`
	TenantID       string `json:"tenant_id"`
	ClientState    string `json:"client_state"`
	State          string `json:"state"`
	AccessToken    string `json:"access_token"`
	PermanentToken string `json:"permanent_token"`
	Token          string `json:"token"`
}

func (h *EmbeddedSignupHandler) Verify(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("hub.mode")
	token := r.URL.Query().Get("hub.verify_token")
	challenge := r.URL.Query().Get("hub.challenge")

	expected := strings.TrimSpace(os.Getenv("META_WEBHOOK_VERIFY_TOKEN"))
	if expected == "" {
		expected = strings.TrimSpace(os.Getenv("WEBHOOK_VERIFY_TOKEN"))
	}

	if expected == "" {
		http.Error(w, "server misconfiguration", http.StatusInternalServerError)
		return
	}

	if h.Logger != nil {
		h.Logger.Info("WhatsApp embedded signup verification request",
			zap.String("mode", mode),
			zap.String("verify_token", token),
			zap.String("challenge", challenge),
			zap.String("expected_token", expected))
	}

	if mode != "subscribe" {
		if h.Logger != nil {
			h.Logger.Warn("WhatsApp embedded signup verification rejected: invalid mode",
				zap.String("received_mode", mode))
		}
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if token == "" {
		if h.Logger != nil {
			h.Logger.Warn("WhatsApp embedded signup verification rejected: token missing")
		}
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if token != expected {
		if h.Logger != nil {
			h.Logger.Warn("WhatsApp embedded signup verification rejected: token mismatch",
				zap.String("received", token),
				zap.Int("received_len", len(token)),
				zap.Int("expected_len", len(expected)))
		}
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if challenge == "" {
		if h.Logger != nil {
			h.Logger.Warn("WhatsApp embedded signup verification rejected: challenge missing")
		}
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	_, _ = fmt.Fprint(w, challenge)
}

func (h *EmbeddedSignupHandler) Callback(w http.ResponseWriter, r *http.Request) {
	if h.Logger != nil {
		metaVerifyToken := strings.TrimSpace(os.Getenv("META_WEBHOOK_VERIFY_TOKEN"))
		legacyVerifyToken := strings.TrimSpace(os.Getenv("WEBHOOK_VERIFY_TOKEN"))

		h.Logger.Info("WhatsApp embedded signup callback hit",
			zap.String("method", r.Method),
			zap.String("content_type", r.Header.Get("Content-Type")),
			zap.String("user_agent", r.Header.Get("User-Agent")),
			zap.String("remote_addr", r.RemoteAddr),
			zap.String("mode", r.URL.Query().Get("hub.mode")),
			zap.String("verify_token", r.URL.Query().Get("hub.verify_token")),
			zap.String("challenge", r.URL.Query().Get("hub.challenge")),
			zap.String("expected_meta_token", metaVerifyToken),
			zap.String("expected_legacy_token", legacyVerifyToken))
	}

	var rawBody []byte
	// Preserve request body for later inspection/logging
	if r.Body != nil {
		rawBody, _ = io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewBuffer(rawBody))
	}

	if err := r.ParseForm(); err != nil {
		h.Logger.Error("embedded signup: failed to parse form", zap.Error(err))
		http.Error(w, "invalid form payload", http.StatusBadRequest)
		return
	}

	if len(r.PostForm) > 0 {
		h.Logger.Debug("embedded signup form payload", zap.Any("form", r.PostForm))
	}

	signedRequest := r.PostFormValue("signed_request")
	if signedRequest == "" {
		if h.Logger != nil {
			preview := rawBody
			if len(preview) > 300 {
				preview = preview[:300]
			}
			h.Logger.Warn("embedded signup: signed_request missing",
				zap.Int("post_form_len", len(r.PostForm)),
				zap.Int("raw_body_len", len(rawBody)),
				zap.ByteString("raw_body_preview", preview))
		}

		legacy := EmbeddedSignupCallbackRequest{
			WABAID:        r.PostFormValue("waba_id"),
			PhoneNumberID: r.PostFormValue("phone_number_id"),
			PhoneNumber:   r.PostFormValue("phone_number"),
			BusinessName:  r.PostFormValue("business_name"),
			TenantID:      r.PostFormValue("tenant_id"),
			AccessToken:   r.PostFormValue("access_token"),
		}

		if legacy.WABAID == "" && legacy.PhoneNumberID == "" && legacy.AccessToken == "" && len(rawBody) > 0 {
			// Attempt to decode JSON payload (legacy testing flow)
			if err := json.Unmarshal(rawBody, &legacy); err != nil {
				h.Logger.Error("embedded signup: missing signed_request and fallback decode failed",
					zap.ByteString("body", rawBody),
					zap.Error(err))
				http.Error(w, "missing signed_request", http.StatusBadRequest)
				return
			}
		}

		if len(rawBody) > 0 {
			if err := json.Unmarshal(rawBody, &legacy); err != nil {
				h.Logger.Error("embedded signup: missing signed_request and fallback decode failed",
					zap.ByteString("body", rawBody),
					zap.Error(err))
			}
		}

		if legacy.AccessToken == "" {
			switch {
			case legacy.Token != "":
				legacy.AccessToken = legacy.Token
			case legacy.PermanentToken != "":
				legacy.AccessToken = legacy.PermanentToken
			}
		}

		if legacy.TenantID == "" {
			if tid := extractTenantID(legacy.ClientState); tid != "" {
				legacy.TenantID = tid
			} else if tid := extractTenantID(legacy.State); tid != "" {
				legacy.TenantID = tid
			}
		}

		if legacy.AccessToken == "" || legacy.WABAID == "" || legacy.PhoneNumberID == "" || strings.TrimSpace(legacy.TenantID) == "" {
			h.Logger.Error("embedded signup: legacy payload missing core identifiers",
				zap.Any("legacy_payload", legacy))
			http.Error(w, "missing signed_request", http.StatusBadRequest)
			return
		}

		h.handleLegacyCallback(w, legacy)
		return
	}

	if h.Config.MetaAppSecret == "" {
		h.Logger.Error("embedded signup: META_APP_SECRET not configured; cannot verify signed_request")
		http.Error(w, "server misconfiguration", http.StatusInternalServerError)
		return
	}

	payloadBytes, err := decodeSignedRequest(signedRequest, h.Config.MetaAppSecret)
	if err != nil {
		h.Logger.Error("invalid signed_request", zap.Error(err))
		http.Error(w, "invalid signed_request", http.StatusBadRequest)
		return
	}

	if h.Logger != nil {
		preview := payloadBytes
		if len(preview) > 500 {
			preview = preview[:500]
		}
		h.Logger.Info("WhatsApp embedded signup payload decoded",
			zap.Int("payload_len", len(payloadBytes)),
			zap.ByteString("payload_preview", preview))
	}

	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		h.Logger.Error("failed to parse embedded signup payload", zap.Error(err))
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	clientState := getString(payload, "client_state")
	if clientState == "" {
		clientState = getString(payload, "state")
	}
	tenantID := parseTenantState(clientState)
	if tenantID == uuid.Nil {
		tenantID, _ = uuid.Parse(r.PostFormValue("tenant_id"))
	}

	phoneID := getString(payload, "phone_number_id")
	phoneNumber := getString(payload, "phone_number")
	wabaID := getString(payload, "wa_business_id")
	if wabaID == "" {
		wabaID = getString(payload, "waba_info", "waba_id")
	}
	if wabaID == "" {
		wabaID = getString(payload, "waba_id")
	}

	token := getString(payload, "token")
	if token == "" {
		token = getString(payload, "waba_info", "token")
	}
	if token == "" {
		token = getString(payload, "waba_info", "permanent_token")
	}
	if token == "" {
		token = getString(payload, "permanent_token")
	}

	businessID := getString(payload, "business_id")

	status := strings.ToUpper(getString(payload, "phone_number_status"))
	if status == "" {
		status = strings.ToUpper(getString(payload, "waba_info", "onboarding_status"))
	}

	if tenantID == uuid.Nil || phoneID == "" || wabaID == "" {
		h.Logger.Error("embedded signup payload missing identifiers",
			zap.Stringer("tenant_id", tenantID),
			zap.String("phone_id", phoneID),
			zap.String("waba_id", wabaID),
			zap.Any("payload", payload))
		http.Error(w, "missing identifiers in payload", http.StatusBadRequest)
		return
	}

	if token == "" {
		h.Logger.Warn("embedded signup payload missing token", zap.String("phone_number_id", phoneID))
	}

	if err := SaveWhatsAppAccount(h.DB, h.Logger, h.Config, tenantID, businessID, wabaID, phoneID, phoneNumber, token, status); err != nil {
		h.Logger.Error("failed to save whatsapp account", zap.Error(err))
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	h.Logger.Info("✅ WhatsApp embedded signup stored",
		zap.Stringer("tenant_id", tenantID),
		zap.String("phone_number_id", phoneID),
		zap.String("status", status))

	w.WriteHeader(http.StatusOK)
}

func (h *EmbeddedSignupHandler) handleLegacyCallback(w http.ResponseWriter, payload EmbeddedSignupCallbackRequest) {
	if payload.AccessToken == "" {
		if payload.Token != "" {
			payload.AccessToken = payload.Token
		} else if payload.PermanentToken != "" {
			payload.AccessToken = payload.PermanentToken
		}
	}

	if payload.TenantID == "" {
		if tid := extractTenantID(payload.ClientState); tid != "" {
			payload.TenantID = tid
		} else if tid := extractTenantID(payload.State); tid != "" {
			payload.TenantID = tid
		}
	}

	if strings.TrimSpace(payload.TenantID) == "" || payload.WABAID == "" || payload.PhoneNumberID == "" || payload.AccessToken == "" {
		h.Logger.Error("legacy embedded signup payload incomplete", zap.Any("payload", payload))
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}

	if err := SaveWhatsAppAccount(
		h.DB,
		h.Logger,
		h.Config,
		parseTenantID(payload.TenantID),
		"", // businessID not available in legacy callback
		payload.WABAID,
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

func parseTenantID(raw string) uuid.UUID {
	if raw == "" {
		return uuid.Nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil
	}
	return id
}

func parseTenantState(state string) uuid.UUID {
	if state == "" {
		return uuid.Nil
	}
	if !strings.HasPrefix(state, "tenant:") {
		return uuid.Nil
	}
	parts := strings.Split(state, ":")
	if len(parts) < 2 {
		return uuid.Nil
	}
	return parseTenantID(parts[1])
}

func extractTenantID(state string) string {
	if state == "" {
		return ""
	}
	if strings.HasPrefix(state, "tenant:") {
		parts := strings.Split(state, ":")
		if len(parts) >= 2 {
			return parts[1]
		}
	}
	parts := strings.Split(state, ":")
	for _, part := range parts {
		if _, err := strconv.Atoi(part); err == nil {
			return part
		}
	}
	return ""
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
