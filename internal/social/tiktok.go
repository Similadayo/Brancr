package social

import (
	"bytes"
	"context"
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

const (
	tikTokBaseAPI         = "https://open.tiktokapis.com"
	tikTokInitUploadPath  = "/v2/post/publish/video/init/"
	tikTokStatusPath      = "/v2/post/publish/status/fetch/"
	tikTokCreatorInfoPath = "/v2/post/publish/creator_info/query/"
	tikTokRefreshPath     = "/v2/oauth/token/"
)

// TikTokConnector handles posting media to TikTok using the Content Posting API.
type TikTokConnector struct {
	DB           *gorm.DB
	Logger       *zap.Logger
	ClientKey    string
	ClientSecret string
	PrivacyLevel string
	httpClient   *http.Client
}

var allowedTikTokPrivacyLevels = map[string]struct{}{
	"PUBLIC_TO_EVERYONE":    {},
	"MUTUAL_FOLLOW_FRIENDS": {},
	"SELF_ONLY":             {},
}

func normalizeTikTokPrivacyLevel(logger *zap.Logger, raw string) string {
	level := strings.ToUpper(strings.TrimSpace(raw))
	if level == "" {
		level = "SELF_ONLY"
	}
	if _, ok := allowedTikTokPrivacyLevels[level]; !ok {
		if logger != nil {
			logger.Warn("Unsupported TikTok privacy level provided; defaulting to SELF_ONLY", zap.String("requested_privacy", raw))
		}
		return "SELF_ONLY"
	}
	return level
}

func normalizePrivacyForPayload(level string) string {
	level = strings.ToUpper(strings.TrimSpace(level))
	if _, ok := allowedTikTokPrivacyLevels[level]; ok {
		return level
	}
	return "SELF_ONLY"
}

// NewTikTokConnector wires dependencies for the TikTok connector.
func NewTikTokConnector(db *gorm.DB, logger *zap.Logger, cfg *config.Config) SocialConnector {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &TikTokConnector{
		DB:           db,
		Logger:       logger,
		ClientKey:    cfg.TikTokClientKey,
		ClientSecret: cfg.TikTokClientSecret,
		PrivacyLevel: normalizeTikTokPrivacyLevel(logger, cfg.TikTokDefaultPrivacy),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (t *TikTokConnector) Name() string { return "tiktok" }

func (t *TikTokConnector) PostMedia(ctx context.Context, acc *storage.SocialAccount, asset *storage.MediaAsset, caption string) error {
	if acc == nil || acc.AccessToken == "" {
		return errors.New("tiktok access token missing")
	}
	if asset == nil {
		return errors.New("tiktok asset payload missing")
	}

	videoURL, err := firstVideoURL(asset)
	if err != nil {
		return err
	}

	videoSize, err := t.getRemoteVideoSize(ctx, videoURL)
	if err != nil {
		return err
	}

	t.Logger.Info("TikTok video size detected",
		zap.Uint("tenant_id", acc.TenantID),
		zap.String("video_url", videoURL),
		zap.Int64("video_size", videoSize))

	if err := t.logCreatorInfo(ctx, acc); err != nil {
		t.Logger.Warn("TikTok creator info query failed", zap.Error(err))
	}

	accessToken, err := t.ensureAccessToken(ctx, acc)
	if err != nil {
		return err
	}

	session, err := t.initUpload(ctx, accessToken, caption, videoSize, t.PrivacyLevel)
	if err != nil {
		if apiErr := (*TikTokAPIError)(nil); errors.As(err, &apiErr) {
			if apiErr.Code == "unaudited_client_can_only_post_to_private_accounts" && !strings.EqualFold(t.PrivacyLevel, "SELF_ONLY") {
				t.Logger.Info("TikTok retrying with SELF_ONLY privacy due to unaudited client restriction",
					zap.Uint("tenant_id", acc.TenantID),
					zap.String("current_privacy", t.PrivacyLevel))
				session, err = t.initUpload(ctx, accessToken, caption, videoSize, "SELF_ONLY")
			}
		}
		if err != nil {
			return fmt.Errorf("tiktok init upload failed: %w", err)
		}
	}

	if err := t.uploadVideo(ctx, session, videoURL); err != nil {
		return fmt.Errorf("tiktok upload failed: %w", err)
	}

	t.Logger.Info("✅ TikTok video published",
		zap.Uint("tenant_id", acc.TenantID),
		zap.String("publish_id", session.PublishID))

	return nil
}

func (t *TikTokConnector) HealthCheck(ctx context.Context, acc *storage.SocialAccount) error {
	if acc == nil || acc.AccessToken == "" {
		return errors.New("access token missing")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, tikTokBaseAPI+"/user/info/?fields=open_id", nil)
	req.Header.Set("Authorization", "Bearer "+acc.AccessToken)
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("tiktok health check failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// ensureAccessToken refreshes the TikTok access token if it is close to expiry.
func (t *TikTokConnector) ensureAccessToken(ctx context.Context, acc *storage.SocialAccount) (string, error) {
	if acc.ExpiresAt == nil || time.Until(*acc.ExpiresAt) > 5*time.Minute {
		return acc.AccessToken, nil
	}

	if acc.RefreshToken == "" {
		return "", errors.New("tiktok access token expired and refresh token missing")
	}

	t.Logger.Info("🔄 Refreshing TikTok access token",
		zap.Uint("tenant_id", acc.TenantID),
		zap.Uint("account_id", acc.ID))

	newTokens, err := t.refreshAccessToken(ctx, acc.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("tiktok token refresh failed: %w", err)
	}

	encryptedAccess, err := utils.EncryptToken(newTokens.AccessToken)
	if err != nil {
		return "", fmt.Errorf("encrypt access token: %w", err)
	}

	encryptedRefresh := ""
	if newTokens.RefreshToken != "" {
		if encryptedRefresh, err = utils.EncryptToken(newTokens.RefreshToken); err != nil {
			t.Logger.Warn("failed to encrypt refreshed TikTok refresh token", zap.Error(err))
		}
	}

	expiry := time.Now().Add(time.Duration(newTokens.ExpiresIn) * time.Second)
	updates := map[string]interface{}{
		"access_token": encryptedAccess,
		"expires_at":   &expiry,
	}
	if encryptedRefresh != "" {
		updates["refresh_token"] = encryptedRefresh
	}

	if t.DB != nil {
		if err := t.DB.WithContext(ctx).
			Model(&storage.SocialAccount{}).
			Where("id = ?", acc.ID).
			Updates(updates).Error; err != nil {
			t.Logger.Warn("failed to persist refreshed TikTok token", zap.Error(err))
		}
	}

	acc.AccessToken = newTokens.AccessToken
	if newTokens.RefreshToken != "" {
		acc.RefreshToken = newTokens.RefreshToken
	}
	acc.ExpiresAt = &expiry

	return acc.AccessToken, nil
}

type tiktokSession struct {
	UploadURL string
	PublishID string
	VideoSize int64
}

func (t *TikTokConnector) getRemoteVideoSize(ctx context.Context, videoURL string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, videoURL, nil)
	if err != nil {
		return 0, err
	}

	resp, err := t.httpClient.Do(req)
	if err == nil {
		resp.Body.Close()
		if resp.ContentLength > 0 {
			return resp.ContentLength, nil
		}
	}

	rangeReq, err := http.NewRequestWithContext(ctx, http.MethodGet, videoURL, nil)
	if err != nil {
		return 0, err
	}
	rangeReq.Header.Set("Range", "bytes=0-0")
	rangeResp, err := t.httpClient.Do(rangeReq)
	if err != nil {
		return 0, err
	}
	defer rangeResp.Body.Close()

	if cr := rangeResp.Header.Get("Content-Range"); cr != "" {
		if parts := strings.Split(cr, "/"); len(parts) == 2 {
			if size, parseErr := strconv.ParseInt(parts[1], 10, 64); parseErr == nil {
				return size, nil
			}
		}
	}

	if rangeResp.ContentLength > 0 {
		return rangeResp.ContentLength, nil
	}

	return 0, errors.New("unable to determine video size for TikTok upload")
}

func (t *TikTokConnector) initUpload(ctx context.Context, accessToken, caption string, videoSize int64, privacyLevel string) (*tiktokSession, error) {
	if videoSize <= 0 {
		return nil, errors.New("invalid video size for TikTok upload")
	}

	level := normalizePrivacyForPayload(privacyLevel)

	payload := map[string]interface{}{
		"post_info": map[string]interface{}{
			"title":           defaultTikTokTitle(caption),
			"privacy_level":   level,
			"disable_duet":    false,
			"disable_stitch":  false,
			"disable_comment": false,
		},
		"source_info": map[string]interface{}{
			"source":            "FILE_UPLOAD",
			"video_size":        videoSize,
			"chunk_size":        videoSize,
			"total_chunk_count": 1,
		},
	}

	body, _ := json.Marshal(payload)
	t.Logger.Info("TikTok init payload", zap.String("json", string(body)))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tikTokBaseAPI+tikTokInitUploadPath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		errResp := parseTikTokError(resp.StatusCode, respBody)
		t.Logger.Warn("TikTok init upload response", zap.Int("status", resp.StatusCode), zap.String("body", errResp.Body))
		return nil, errResp
	}

	var parsed struct {
		Data struct {
			UploadURL string `json:"upload_url"`
			PublishID string `json:"publish_id"`
		} `json:"data"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			LogID   string `json:"log_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, err
	}

	if parsed.Error.Code != "" && parsed.Error.Code != "ok" {
		apiErr := &TikTokAPIError{
			Status:  resp.StatusCode,
			Code:    parsed.Error.Code,
			Message: parsed.Error.Message,
			LogID:   parsed.Error.LogID,
		}
		t.Logger.Warn("TikTok init logical error", zap.String("code", apiErr.Code), zap.String("message", apiErr.Message), zap.String("log_id", apiErr.LogID))
		return nil, apiErr
	}

	if parsed.Data.UploadURL == "" || parsed.Data.PublishID == "" {
		return nil, errors.New("tiktok init upload missing data")
	}

	return &tiktokSession{
		UploadURL: parsed.Data.UploadURL,
		PublishID: parsed.Data.PublishID,
		VideoSize: videoSize,
	}, nil
}

func (t *TikTokConnector) initUploadWithURL(ctx context.Context, accessToken, caption, videoURL, privacyLevel string) (*tiktokSession, error) {
	if videoURL == "" {
		return nil, errors.New("video URL is required for TikTok upload")
	}

	level := normalizePrivacyForPayload(privacyLevel)

	payload := map[string]interface{}{
		"post_info": map[string]interface{}{
			"title":           defaultTikTokTitle(caption),
			"privacy_level":   level,
			"disable_duet":    false,
			"disable_stitch":  false,
			"disable_comment": false,
		},
		"source_info": map[string]interface{}{
			"source":    "PULL_FROM_URL",
			"video_url": videoURL,
		},
	}

	body, _ := json.Marshal(payload)
	t.Logger.Debug("TikTok init payload (PULL_FROM_URL)", zap.String("json", string(body)))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tikTokBaseAPI+tikTokInitUploadPath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, parseTikTokError(resp.StatusCode, respBody)
	}

	var parsed struct {
		Data struct {
			PublishID string `json:"publish_id"`
		} `json:"data"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			LogID   string `json:"log_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, err
	}

	if parsed.Error.Code != "" && parsed.Error.Code != "ok" {
		return nil, &TikTokAPIError{
			Status:  resp.StatusCode,
			Code:    parsed.Error.Code,
			Message: parsed.Error.Message,
			LogID:   parsed.Error.LogID,
		}
	}

	if parsed.Data.PublishID == "" {
		return nil, errors.New("tiktok init upload missing publish_id")
	}

	return &tiktokSession{
		PublishID: parsed.Data.PublishID,
	}, nil
}

func (t *TikTokConnector) uploadVideo(ctx context.Context, session *tiktokSession, videoURL string) error {
	if session == nil || session.UploadURL == "" {
		return errors.New("upload URL missing")
	}
	if videoURL == "" {
		return errors.New("video URL missing")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, videoURL, nil)
	if err != nil {
		return err
	}
	downloadResp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download video failed: %w", err)
	}
	defer downloadResp.Body.Close()

	if downloadResp.StatusCode >= 400 {
		body, _ := io.ReadAll(downloadResp.Body)
		return fmt.Errorf("download video failed (%d): %s", downloadResp.StatusCode, strings.TrimSpace(string(body)))
	}

	uploadReq, err := http.NewRequestWithContext(ctx, http.MethodPut, session.UploadURL, downloadResp.Body)
	if err != nil {
		return err
	}
	contentType := downloadResp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	uploadReq.Header.Set("Content-Type", contentType)

	length := session.VideoSize
	if length <= 0 && downloadResp.ContentLength > 0 {
		length = downloadResp.ContentLength
	}
	if length > 0 {
		uploadReq.ContentLength = length
		uploadReq.Header.Set("Content-Length", strconv.FormatInt(length, 10))
	}

	uploadResp, err := t.httpClient.Do(uploadReq)
	if err != nil {
		return fmt.Errorf("upload video failed: %w", err)
	}
	defer uploadResp.Body.Close()
	if uploadResp.StatusCode >= 400 {
		body, _ := io.ReadAll(uploadResp.Body)
		return fmt.Errorf("upload video failed (%d): %s", uploadResp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

type tiktokTokenResponse struct {
	AccessToken      string
	RefreshToken     string
	ExpiresIn        int64
	RefreshExpiresIn int64
}

func (t *TikTokConnector) refreshAccessToken(ctx context.Context, refreshToken string) (*tiktokTokenResponse, error) {
	form := url.Values{}
	form.Set("client_key", t.ClientKey)
	form.Set("client_secret", t.ClientSecret)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tikTokBaseAPI+tikTokRefreshPath, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("tiktok refresh failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed struct {
		Data struct {
			AccessToken      string `json:"access_token"`
			RefreshToken     string `json:"refresh_token"`
			ExpiresIn        int64  `json:"expires_in"`
			RefreshExpiresIn int64  `json:"refresh_expires_in"`
		} `json:"data"`
		Error      json.RawMessage `json:"error"`
		Message    string          `json:"message"`
		StatusCode int             `json:"status_code"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, err
	}
	apiErr := parseTikTokAPIError(parsed.Error, parsed.Message)
	if apiErr.Code != 0 {
		return nil, fmt.Errorf("tiktok refresh error (%d): %s (raw=%s)", apiErr.Code, apiErr.Message, apiErr.Raw)
	}
	if parsed.Data.AccessToken == "" {
		return nil, errors.New("tiktok refresh returned empty access token")
	}

	return &tiktokTokenResponse{
		AccessToken:      parsed.Data.AccessToken,
		RefreshToken:     parsed.Data.RefreshToken,
		ExpiresIn:        parsed.Data.ExpiresIn,
		RefreshExpiresIn: parsed.Data.RefreshExpiresIn,
	}, nil
}

func (t *TikTokConnector) logCreatorInfo(ctx context.Context, acc *storage.SocialAccount) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tikTokBaseAPI+tikTokCreatorInfoPath, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("Authorization", "Bearer "+acc.AccessToken)

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("creator info query failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return err
	}
	t.Logger.Info("TikTok creator info", zap.Uint("tenant_id", acc.TenantID), zap.Any("info", payload["data"]))
	return nil
}

func truncateCaption(input string, limit int) string {
	input = strings.TrimSpace(input)
	if limit <= 0 || len([]rune(input)) <= limit {
		return input
	}
	runes := []rune(input)
	return string(runes[:limit])
}

func defaultTikTokTitle(caption string) string {
	title := truncateCaption(caption, 150)
	if strings.TrimSpace(title) != "" {
		return title
	}
	return "Posted via Brancr"
}

func firstVideoURL(asset *storage.MediaAsset) (string, error) {
	var urls []string
	if err := json.Unmarshal([]byte(asset.URLs), &urls); err != nil {
		return "", fmt.Errorf("invalid media URLs: %w", err)
	}
	if len(urls) == 0 {
		return "", errors.New("no media URLs provided for TikTok")
	}
	if !strings.HasSuffix(strings.ToLower(urls[0]), ".mp4") &&
		!strings.Contains(strings.ToLower(urls[0]), "video") {
		return "", errors.New("tiktok requires a video asset (mp4 recommended)")
	}
	return urls[0], nil
}

type TikTokAPIError struct {
	Status  int
	Code    string
	Message string
	LogID   string
	Body    string
}

func (e *TikTokAPIError) Error() string {
	if e == nil {
		return ""
	}
	code := e.Code
	if code == "" {
		code = strconv.Itoa(e.Status)
	}
	if e.LogID != "" {
		return fmt.Sprintf("tiktok API error (%s): %s (log_id: %s)", code, e.Message, e.LogID)
	}
	return fmt.Sprintf("tiktok API error (%s): %s", code, e.Message)
}

func parseTikTokError(status int, body []byte) *TikTokAPIError {
	errBody := strings.TrimSpace(string(body))
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			LogID   string `json:"log_id"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &payload)
	return &TikTokAPIError{
		Status:  status,
		Code:    payload.Error.Code,
		Message: payload.Error.Message,
		LogID:   payload.Error.LogID,
		Body:    errBody,
	}
}
