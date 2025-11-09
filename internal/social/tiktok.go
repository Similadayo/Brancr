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
	tikTokBaseAPI            = "https://open.tiktokapis.com/v2"
	tikTokAltContentBase     = "https://open-api.tiktok.com/v2"
	tikTokInitUploadPath     = "/post/publish/video/init/"
	tikTokFinalizeUploadPath = "/post/publish/video/publish/"
	tikTokStatusPath         = "/post/publish/status/fetch/"
	tikTokCreatorInfoPath    = "/post/publish/creator_info/query/"
	tikTokRefreshPath        = "/oauth/token/"
)

// TikTokConnector handles posting media to TikTok using the Content Posting API.
type TikTokConnector struct {
	DB           *gorm.DB
	Logger       *zap.Logger
	ClientKey    string
	ClientSecret string
	httpClient   *http.Client
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

	if err := t.logCreatorInfo(ctx, acc); err != nil {
		t.Logger.Warn("TikTok creator info query failed", zap.Error(err))
	}

	accessToken, err := t.ensureAccessToken(ctx, acc)
	if err != nil {
		return err
	}

	session, err := t.initUpload(ctx, accessToken, caption, videoSize)
	if err != nil {
		return fmt.Errorf("tiktok init upload failed: %w", err)
	}
	session.VideoSize = videoSize

	if err := t.uploadVideo(ctx, session, videoURL); err != nil {
		return fmt.Errorf("tiktok upload failed: %w", err)
	}

	if err := t.finalizeUpload(ctx, accessToken, session); err != nil {
		return fmt.Errorf("tiktok finalize upload failed: %w", err)
	}

	if err := t.pollPublishStatus(ctx, accessToken, session.PublishID); err != nil {
		return fmt.Errorf("tiktok publish status check failed: %w", err)
	}

	t.Logger.Info("✅ TikTok video published",
		zap.Uint("tenant_id", acc.TenantID),
		zap.String("video_id", session.VideoID))

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
	UploadID  string
	VideoID   string
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
	} else {
		t.Logger.Warn("HEAD request failed for video size", zap.Error(err))
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

func (t *TikTokConnector) initUpload(ctx context.Context, accessToken, caption string, videoSize int64) (*tiktokSession, error) {
	if videoSize <= 0 {
		return nil, errors.New("invalid video size for TikTok upload")
	}

	payload := map[string]interface{}{
		"source_info": map[string]interface{}{
			"source":            "FILE_UPLOAD",
			"video_size":        videoSize,
			"chunk_size":        videoSize,
			"total_chunk_count": 1,
		},
		"post_info": map[string]interface{}{
			"title":         truncateCaption(caption, 150),
			"privacy_level": "PUBLIC",
		},
	}

	body, _ := json.Marshal(payload)
	bases := []string{tikTokBaseAPI, tikTokAltContentBase}
	var lastErr error

	for _, base := range bases {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+tikTokInitUploadPath, bytes.NewReader(body))
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Content-Type", "application/json; charset=UTF-8")
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := t.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("tiktok init upload failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
			continue
		}

		var parsed struct {
			Data struct {
				UploadURL string `json:"upload_url"`
				UploadID  string `json:"upload_id"`
				VideoID   string `json:"video_id"`
				PublishID string `json:"publish_id"`
			} `json:"data"`
			Error      json.RawMessage `json:"error"`
			Message    string          `json:"message"`
			StatusCode int             `json:"status_code"`
		}
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			lastErr = err
			continue
		}
		apiErr := parseTikTokAPIError(parsed.Error, parsed.Message)
		if apiErr.Code != 0 {
			lastErr = fmt.Errorf("tiktok init upload error (%d): %s (raw=%s)", apiErr.Code, apiErr.Message, apiErr.Raw)
			continue
		}
		if parsed.Data.UploadURL == "" || parsed.Data.VideoID == "" || parsed.Data.PublishID == "" {
			lastErr = errors.New("tiktok init upload missing data")
			continue
		}

		return &tiktokSession{
			UploadURL: parsed.Data.UploadURL,
			UploadID:  parsed.Data.UploadID,
			VideoID:   parsed.Data.VideoID,
			PublishID: parsed.Data.PublishID,
		}, nil
	}

	return nil, lastErr
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
		uploadReq.Header.Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", length-1, length))
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

func (t *TikTokConnector) finalizeUpload(ctx context.Context, accessToken string, session *tiktokSession) error {
	payload := map[string]string{
		"publish_id": session.PublishID,
		"video_id":   session.VideoID,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tikTokBaseAPI+tikTokFinalizeUploadPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("tiktok finalize upload failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed struct {
		Error      json.RawMessage `json:"error"`
		Message    string          `json:"message"`
		StatusCode int             `json:"status_code"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return err
	}
	apiErr := parseTikTokAPIError(parsed.Error, parsed.Message)
	if apiErr.Code != 0 {
		return fmt.Errorf("tiktok finalize upload error (%d): %s (raw=%s)", apiErr.Code, apiErr.Message, apiErr.Raw)
	}
	return nil
}

func (t *TikTokConnector) pollPublishStatus(ctx context.Context, accessToken, publishID string) error {
	if publishID == "" {
		return errors.New("publish id missing for status check")
	}

	payload := map[string]string{"publish_id": publishID}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tikTokBaseAPI+tikTokStatusPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	for attempt := 0; attempt < 3; attempt++ {
		resp, err := t.httpClient.Do(req)
		if err != nil {
			return err
		}
		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}

		if resp.StatusCode >= 400 {
			return fmt.Errorf("tiktok publish status failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
		}

		var parsed struct {
			Data struct {
				Status string `json:"status"`
			} `json:"data"`
			Error      json.RawMessage `json:"error"`
			Message    string          `json:"message"`
			StatusCode int             `json:"status_code"`
		}
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			return err
		}
		apiErr := parseTikTokAPIError(parsed.Error, parsed.Message)
		if apiErr.Code != 0 {
			return fmt.Errorf("tiktok publish status error (%d): %s (raw=%s)", apiErr.Code, apiErr.Message, apiErr.Raw)
		}

		status := strings.ToUpper(parsed.Data.Status)
		if status == "" || status == "PROCESSING" {
			time.Sleep(2 * time.Second)
			continue
		}
		if status != "SUCCESS" {
			return fmt.Errorf("tiktok publish reported status %s", status)
		}
		return nil
	}

	t.Logger.Warn("TikTok publish status polling timed out", zap.String("publish_id", publishID))
	return nil
}

type tiktokTokenResponse struct {
	AccessToken      string
	RefreshToken     string
	ExpiresIn        int64
	RefreshExpiresIn int64
}

func (t *TikTokConnector) refreshAccessToken(ctx context.Context, refreshToken string) (*tiktokTokenResponse, error) {
	if t.ClientKey == "" || t.ClientSecret == "" {
		return nil, errors.New("tiktok client credentials missing")
	}

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

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		t.Logger.Info("TikTok creator info skipped", zap.Uint("tenant_id", acc.TenantID), zap.String("reason", msg))
		return nil
	}

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
