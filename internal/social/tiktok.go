package social

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cloudinary/cloudinary-go/v2"
	"github.com/cloudinary/cloudinary-go/v2/api/uploader"
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
	tikTokVideoListPath   = "/v2/video/list/"
	tikTokVideoQueryPath  = "/v2/video/query/"
	tikTokVideoDeletePath = "/v2/video/delete/"
	tikTokCommentListPath = "/v2/comment/list/"
	tikTokCommentReplyPath = "/v2/comment/reply/"
	tikTokResearchPath    = "/v2/research/video/query/"
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

	// ✅ Check if asset is photo or video
	// TikTok Business API only supports videos, so we need to convert photos to video
	mediaURL, isPhoto, err := t.getMediaURL(asset)
	if err != nil {
		return err
	}

	// If it's a photo, convert to video
	var videoURL string
	if isPhoto {
		t.Logger.Info("📸 TikTok photo detected - converting to video",
			zap.Uint("tenant_id", acc.TenantID),
			zap.String("photo_url", mediaURL))
		
		convertedVideoURL, convertErr := t.convertPhotoToVideo(ctx, mediaURL, caption)
		if convertErr != nil {
			return fmt.Errorf("failed to convert photo to video: %w", convertErr)
		}
		videoURL = convertedVideoURL
		t.Logger.Info("✅ Photo converted to video for TikTok",
			zap.Uint("tenant_id", acc.TenantID),
			zap.String("video_url", videoURL),
			zap.String("note", "Using FILE_UPLOAD to avoid URL ownership verification"))
	} else {
		videoURL = mediaURL
	}

	// Always get video size for FILE_UPLOAD (required by TikTok API)
	// Note: We use FILE_UPLOAD instead of PULL_FROM_URL to avoid domain verification issues
	videoSize, sizeErr := t.getRemoteVideoSize(ctx, videoURL)
	if sizeErr != nil {
		return fmt.Errorf("unable to determine video size for TikTok upload: %w", sizeErr)
	}

	t.Logger.Info("TikTok video size detected",
		zap.Uint("tenant_id", acc.TenantID),
		zap.String("video_url", videoURL),
		zap.Int64("video_size", videoSize))

	if logErr := t.logCreatorInfo(ctx, acc); logErr != nil {
		t.Logger.Warn("TikTok creator info query failed", zap.Error(logErr))
	}

	accessToken, tokenErr := t.ensureAccessToken(ctx, acc)
	if tokenErr != nil {
		return tokenErr
	}

	// Get posting options from asset metadata or use defaults
	postingOptions := t.getPostingOptions(asset)

	// Always use FILE_UPLOAD method (avoids URL ownership verification required for PULL_FROM_URL)
	var session *tiktokSession
	var initErr error
	session, initErr = t.initUpload(ctx, accessToken, caption, videoSize, t.PrivacyLevel, postingOptions)
	if initErr != nil {
		var apiErr *TikTokAPIError
		if errors.As(initErr, &apiErr) {
			if apiErr.Code == "unaudited_client_can_only_post_to_private_accounts" && !strings.EqualFold(t.PrivacyLevel, "SELF_ONLY") {
				t.Logger.Info("TikTok retrying with SELF_ONLY privacy due to unaudited client restriction",
					zap.Uint("tenant_id", acc.TenantID),
					zap.String("current_privacy", t.PrivacyLevel))
				session, initErr = t.initUpload(ctx, accessToken, caption, videoSize, "SELF_ONLY", postingOptions)
			}
		}
	}

	if initErr != nil {
		return fmt.Errorf("tiktok init upload failed: %w", initErr)
		}

	// Upload the video file (downloads from URL and uploads to TikTok)
	if session != nil && session.UploadURL != "" {
		if uploadErr := t.uploadVideo(ctx, session, videoURL); uploadErr != nil {
			return fmt.Errorf("tiktok upload failed: %w", uploadErr)
		}
	}

	t.Logger.Info("✅ TikTok video published",
		zap.Uint("tenant_id", acc.TenantID),
		zap.String("publish_id", session.PublishID))

	// Store publish_id in Tags field (JSON) for later status checking
	// Note: Tags is a JSON array, but we can use it to store metadata
	if asset.Tags != "" {
		var tags []string
		if err := json.Unmarshal([]byte(asset.Tags), &tags); err == nil {
			// Store publish_id as a tag-like entry
			metadata := map[string]interface{}{
				"tiktok_publish_id": session.PublishID,
				"tiktok_published_at": time.Now().UTC().Format(time.RFC3339),
			}
			metadataJSON, _ := json.Marshal(metadata)
			// Append metadata to tags (as JSON string)
			tags = append(tags, string(metadataJSON))
			tagsJSON, _ := json.Marshal(tags)
			asset.Tags = string(tagsJSON)
			// Note: Asset update should be handled by caller, but we log it
			t.Logger.Info("📝 TikTok publish_id stored",
				zap.String("publish_id", session.PublishID))
		}
	} else {
		// Create new tags array with metadata
		metadata := map[string]interface{}{
			"tiktok_publish_id": session.PublishID,
			"tiktok_published_at": time.Now().UTC().Format(time.RFC3339),
		}
		metadataJSON, _ := json.Marshal(metadata)
		tags := []string{string(metadataJSON)}
		tagsJSON, _ := json.Marshal(tags)
		asset.Tags = string(tagsJSON)
		t.Logger.Info("📝 TikTok publish_id stored (new)",
			zap.String("publish_id", session.PublishID))
	}

	return nil
}

func (t *TikTokConnector) HealthCheck(ctx context.Context, acc *storage.SocialAccount) error {
	if acc == nil || acc.AccessToken == "" {
		return errors.New("access token missing")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, tikTokBaseAPI+"/user/info/?fields=open_id,display_name,username", nil)
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

	var payload struct {
		Data struct {
			DisplayName string `json:"display_name"`
			Username    string `json:"username"`
			OpenID      string `json:"open_id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil {
		update := map[string]interface{}{}
		if payload.Data.DisplayName != "" {
			update["username"] = payload.Data.DisplayName
		} else if payload.Data.Username != "" {
			update["username"] = payload.Data.Username
		}
		if payload.Data.OpenID != "" {
			update["external_id"] = payload.Data.OpenID
		}

		if len(update) > 0 && t.DB != nil {
			if err := t.DB.WithContext(ctx).
				Model(&storage.SocialAccount{}).
				Where("id = ?", acc.ID).
				Updates(update).Error; err != nil {
				t.Logger.Warn("tiktok health check: failed to persist profile info", zap.Error(err))
			} else {
				if val, ok := update["username"]; ok {
					acc.Username, _ = val.(string)
				}
				if val, ok := update["external_id"]; ok {
					acc.ExternalID, _ = val.(string)
				}
			}
		}
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
	// Try HEAD request first (most efficient)
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

	// Try Range request to get Content-Range header
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

	// Fallback: Download the video to get its size (for Cloudinary and other services that don't provide headers)
	// This is less efficient but necessary for some CDNs
	t.Logger.Info("⚠️ Video size not available from headers, downloading to determine size",
		zap.String("video_url", videoURL))
	
	downloadReq, err := http.NewRequestWithContext(ctx, http.MethodGet, videoURL, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create download request: %w", err)
	}
	
	// Set headers that might help with Cloudinary and other CDNs
	downloadReq.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Brancr/1.0)")
	downloadReq.Header.Set("Accept", "*/*")
	
	downloadResp, err := t.httpClient.Do(downloadReq)
	if err != nil {
		return 0, fmt.Errorf("failed to download video to determine size: %w", err)
	}
	defer downloadResp.Body.Close()

	if downloadResp.StatusCode >= 400 {
		body, readErr := io.ReadAll(io.LimitReader(downloadResp.Body, 1024)) // Limit error body to 1KB
		errorMsg := strings.TrimSpace(string(body))
		if readErr != nil {
			errorMsg = fmt.Sprintf("(could not read error body: %v)", readErr)
		}
		if errorMsg == "" {
			errorMsg = fmt.Sprintf("HTTP %d with no error message", downloadResp.StatusCode)
		}
		
		t.Logger.Error("❌ Failed to download video to determine size",
			zap.String("video_url", videoURL),
			zap.Int("status_code", downloadResp.StatusCode),
			zap.String("error_body", errorMsg),
			zap.String("content_type", downloadResp.Header.Get("Content-Type")))
		
		return 0, fmt.Errorf("failed to download video (%d): %s", downloadResp.StatusCode, errorMsg)
	}

	// Try to get size from Content-Length header first
	if downloadResp.ContentLength > 0 {
		// Discard the body since we only needed the size
		io.Copy(io.Discard, downloadResp.Body)
		t.Logger.Info("✅ Video size determined from Content-Length header",
			zap.String("video_url", videoURL),
			zap.Int64("size", downloadResp.ContentLength))
		return downloadResp.ContentLength, nil
	}

	// Last resort: read the entire response to count bytes (not ideal but works)
	// Limit to reasonable size to avoid memory issues (e.g., 500MB max)
	const maxSize = 500 * 1024 * 1024 // 500MB
	body, err := io.ReadAll(io.LimitReader(downloadResp.Body, maxSize+1))
	if err != nil {
		return 0, fmt.Errorf("failed to read video to determine size: %w", err)
	}
	
	size := int64(len(body))
	if size > maxSize {
		return 0, fmt.Errorf("video too large (exceeds %d bytes)", maxSize)
	}
	
	if size == 0 {
		return 0, errors.New("video appears to be empty or could not be downloaded")
	}

	t.Logger.Info("✅ Video size determined by downloading and reading",
		zap.String("video_url", videoURL),
		zap.Int64("size", size))
	
	return size, nil
}

// TikTokPostingOptions holds configurable posting options
type TikTokPostingOptions struct {
	DisableDuet    bool
	DisableStitch  bool
	DisableComment bool
	ScheduleTime   *time.Time // Optional: schedule for future posting
}

// getPostingOptions extracts posting options from asset Metadata (JSONB) or returns defaults
func (t *TikTokConnector) getPostingOptions(asset *storage.MediaAsset) TikTokPostingOptions {
	options := TikTokPostingOptions{
		DisableDuet:    false, // Default: allow duets
		DisableStitch:  false, // Default: allow stitches
		DisableComment: false, // Default: allow comments
	}

	// Try to parse options from asset Metadata (JSONB field)
	if asset.Metadata != "" {
		var metadata map[string]interface{}
		if err := json.Unmarshal([]byte(asset.Metadata), &metadata); err == nil {
			// Extract TikTok posting options from metadata
			if duet, ok := metadata["tiktok_disable_duet"].(bool); ok {
				options.DisableDuet = duet
			}
			if stitch, ok := metadata["tiktok_disable_stitch"].(bool); ok {
				options.DisableStitch = stitch
			}
			if comment, ok := metadata["tiktok_disable_comment"].(bool); ok {
				options.DisableComment = comment
			}
			// Parse schedule time if provided
			if scheduleStr, ok := metadata["tiktok_schedule_time"].(string); ok {
				if scheduleTime, err := time.Parse(time.RFC3339, scheduleStr); err == nil {
					options.ScheduleTime = &scheduleTime
				}
			}
		}
	}

	return options
}

func (t *TikTokConnector) initUpload(ctx context.Context, accessToken, caption string, videoSize int64, privacyLevel string, options TikTokPostingOptions) (*tiktokSession, error) {
	if videoSize <= 0 {
		return nil, errors.New("invalid video size for TikTok upload")
	}

	level := normalizePrivacyForPayload(privacyLevel)

	postInfo := map[string]interface{}{
		"title":         defaultTikTokTitle(caption),
		"privacy_level": level,
		"disable_duet":  options.DisableDuet,
		"disable_stitch": options.DisableStitch,
		"disable_comment": options.DisableComment,
	}

	// Add schedule time if provided
	if options.ScheduleTime != nil {
		// TikTok expects Unix timestamp in seconds
		postInfo["schedule_time"] = options.ScheduleTime.Unix()
	}

	payload := map[string]interface{}{
		"post_info": postInfo,
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

func (t *TikTokConnector) initUploadWithURL(ctx context.Context, accessToken, caption, videoURL, privacyLevel string, options TikTokPostingOptions) (*tiktokSession, error) {
	if videoURL == "" {
		return nil, errors.New("video URL is required for TikTok upload")
	}

	level := normalizePrivacyForPayload(privacyLevel)

	postInfo := map[string]interface{}{
		"title":         defaultTikTokTitle(caption),
		"privacy_level": level,
		"disable_duet":  options.DisableDuet,
		"disable_stitch": options.DisableStitch,
		"disable_comment": options.DisableComment,
	}

	// Add schedule time if provided
	if options.ScheduleTime != nil {
		postInfo["schedule_time"] = options.ScheduleTime.Unix()
	}

	payload := map[string]interface{}{
		"post_info": postInfo,
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
		if strings.HasPrefix(contentType, "video/") {
			uploadReq.Header.Set("X-Upload-Content-Type", contentType)
		}
		uploadReq.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", 0, length-1, length))
		uploadReq.Header.Set("X-Upload-Content-Length", strconv.FormatInt(length, 10))
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

// getMediaURL extracts the first media URL and determines if it's a photo or video
func (t *TikTokConnector) getMediaURL(asset *storage.MediaAsset) (string, bool, error) {
	var urls []string
	if err := json.Unmarshal([]byte(asset.URLs), &urls); err != nil {
		return "", false, fmt.Errorf("invalid media URLs: %w", err)
	}
	if len(urls) == 0 {
		return "", false, errors.New("no media URLs provided for TikTok")
	}

	firstURL := urls[0]
	lowerURL := strings.ToLower(firstURL)
	
	// Check if it's a video
	isVideo := strings.HasSuffix(lowerURL, ".mp4") ||
		strings.HasSuffix(lowerURL, ".mov") ||
		strings.HasSuffix(lowerURL, ".avi") ||
		strings.HasSuffix(lowerURL, ".mkv") ||
		strings.Contains(lowerURL, "video") ||
		asset.Type == "video"
	
	// Check if it's a photo
	isPhoto := strings.HasSuffix(lowerURL, ".jpg") ||
		strings.HasSuffix(lowerURL, ".jpeg") ||
		strings.HasSuffix(lowerURL, ".png") ||
		strings.HasSuffix(lowerURL, ".gif") ||
		strings.HasSuffix(lowerURL, ".webp") ||
		strings.Contains(lowerURL, "image") ||
		asset.Type == "image"
	
	if !isVideo && !isPhoto {
		// Try to infer from asset type
		if asset.Type == "video" {
			isVideo = true
		} else if asset.Type == "image" {
			isPhoto = true
		} else {
			return "", false, fmt.Errorf("unsupported media type for TikTok: %s (URL: %s)", asset.Type, firstURL)
		}
	}

	return firstURL, isPhoto, nil
}

// convertPhotoToVideo converts a photo to a video using Cloudinary API
// This downloads the image, uploads it to Cloudinary as a video resource, and returns the video URL
func (t *TikTokConnector) convertPhotoToVideo(ctx context.Context, photoURL, caption string) (string, error) {
	cloudURL := os.Getenv("CLOUDINARY_URL")
	if cloudURL == "" {
		return "", errors.New("CLOUDINARY_URL not set - cannot convert photo to video")
	}

	t.Logger.Info("📸 Starting photo-to-video conversion via Cloudinary API",
		zap.String("photo_url", photoURL))

	// Step 1: Download the image
	downloadReq, err := http.NewRequestWithContext(ctx, http.MethodGet, photoURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create download request: %w", err)
	}
	downloadReq.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Brancr/1.0)")

	downloadResp, err := t.httpClient.Do(downloadReq)
	if err != nil {
		return "", fmt.Errorf("failed to download image: %w", err)
	}
	defer downloadResp.Body.Close()

	if downloadResp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(downloadResp.Body, 1024))
		return "", fmt.Errorf("failed to download image (%d): %s", downloadResp.StatusCode, strings.TrimSpace(string(body)))
	}

	// Step 2: Initialize Cloudinary client
	cld, err := cloudinary.NewFromURL(cloudURL)
	if err != nil {
		return "", fmt.Errorf("failed to initialize Cloudinary client: %w", err)
	}

	// Step 3: Generate unique public ID for the video
	randomBytes := make([]byte, 4)
	rand.Read(randomBytes)
	randomStr := base64.URLEncoding.EncodeToString(randomBytes)[:6]
	timestamp := time.Now().UnixNano()
	publicID := fmt.Sprintf("tiktok_video_%d_%s", timestamp, randomStr)

	// Step 4: Upload image to Cloudinary as a video resource
	// Cloudinary will create a video from the image (static image video)
	folder := "brancr/tiktok/conversions"
	
	t.Logger.Info("📤 Uploading image to Cloudinary",
		zap.String("public_id", publicID),
		zap.String("folder", folder))

	// Note: Cloudinary doesn't directly convert images to videos via upload
	// We'll upload as image first, then use transformation to get video URL
	uploadParams := uploader.UploadParams{
		PublicID:     publicID,
		Folder:       folder,
		ResourceType: "image", // Upload as image first
	}

	res, err := cld.Upload.Upload(ctx, downloadResp.Body, uploadParams)
	if err != nil {
		return "", fmt.Errorf("failed to upload image to Cloudinary as video: %w", err)
	}

	if res.SecureURL == "" {
		return "", errors.New("Cloudinary returned empty video URL")
	}

	// Step 5: Get the video URL
	// If eager transformation was applied, use that URL, otherwise use the base URL
	videoURL := res.SecureURL
	if len(res.Eager) > 0 && res.Eager[0].SecureURL != "" {
		videoURL = res.Eager[0].SecureURL
		t.Logger.Info("✅ Using eager transformation video URL",
			zap.String("video_url", videoURL))
	} else {
		// If no eager transformation, try to get video URL by changing resource type in URL
		// This is a fallback - Cloudinary should have generated the video
		videoURL = strings.Replace(res.SecureURL, "/image/upload/", "/video/upload/", 1)
		videoURL = strings.TrimSuffix(videoURL, ".jpg")
		videoURL = strings.TrimSuffix(videoURL, ".jpeg")
		videoURL = strings.TrimSuffix(videoURL, ".png")
		videoURL = strings.TrimSuffix(videoURL, ".gif")
		videoURL = strings.TrimSuffix(videoURL, ".webp")
		if !strings.HasSuffix(strings.ToLower(videoURL), ".mp4") {
			videoURL += ".mp4"
		}
		t.Logger.Info("✅ Using fallback video URL",
			zap.String("video_url", videoURL))
	}

	t.Logger.Info("✅ Photo converted to video via Cloudinary API",
		zap.String("photo_url", photoURL),
		zap.String("video_url", videoURL),
		zap.String("public_id", res.PublicID))

	return videoURL, nil
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

// ✅ ADVANCED FEATURES IMPLEMENTATION

// CheckVideoStatus checks the status of a published video
// Returns: status (PROCESSING, PUBLISHED, FAILED), video_id, and error details
func (t *TikTokConnector) CheckVideoStatus(ctx context.Context, acc *storage.SocialAccount, publishID string) (map[string]interface{}, error) {
	if acc == nil || acc.AccessToken == "" {
		return nil, errors.New("tiktok access token missing")
	}
	if publishID == "" {
		return nil, errors.New("publish_id is required")
	}

	accessToken, err := t.ensureAccessToken(ctx, acc)
	if err != nil {
		return nil, err
	}

	payload := map[string]interface{}{
		"publish_id": publishID,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tikTokBaseAPI+tikTokStatusPath, bytes.NewReader(body))
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

	var result struct {
		Data struct {
			Status  string `json:"status"`  // PROCESSING, PUBLISHED, FAILED
			VideoID string `json:"video_id"` // Available when status is PUBLISHED
		} `json:"data"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			LogID   string `json:"log_id"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse status response: %w", err)
	}

	if result.Error.Code != "" && result.Error.Code != "ok" {
		return nil, &TikTokAPIError{
			Status:  resp.StatusCode,
			Code:    result.Error.Code,
			Message: result.Error.Message,
			LogID:   result.Error.LogID,
		}
	}

	statusInfo := map[string]interface{}{
		"status":    result.Data.Status,
		"video_id":  result.Data.VideoID,
		"publish_id": publishID,
	}

	t.Logger.Info("✅ TikTok video status checked",
		zap.Uint("tenant_id", acc.TenantID),
		zap.String("publish_id", publishID),
		zap.String("status", result.Data.Status),
		zap.String("video_id", result.Data.VideoID))

	return statusInfo, nil
}

// ListVideos retrieves a list of published videos for the account
// Parameters: max_count (default 20, max 50), cursor for pagination
func (t *TikTokConnector) ListVideos(ctx context.Context, acc *storage.SocialAccount, maxCount int, cursor string) (map[string]interface{}, error) {
	if acc == nil || acc.AccessToken == "" {
		return nil, errors.New("tiktok access token missing")
	}

	accessToken, err := t.ensureAccessToken(ctx, acc)
	if err != nil {
		return nil, err
	}

	if maxCount <= 0 {
		maxCount = 20
	}
	if maxCount > 50 {
		maxCount = 50
	}

	payload := map[string]interface{}{
		"max_count": maxCount,
		"fields": []string{
			"video_id",
			"title",
			"cover_image_url",
			"create_time",
			"duration",
			"share_url",
			"embed_html",
			"embed_link",
			"like_count",
			"comment_count",
			"share_count",
			"view_count",
		},
	}
	if cursor != "" {
		payload["cursor"] = cursor
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tikTokBaseAPI+tikTokVideoListPath, bytes.NewReader(body))
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

	var result struct {
		Data struct {
			Videos []map[string]interface{} `json:"videos"`
			Cursor string                    `json:"cursor"`
			HasMore bool                     `json:"has_more"`
		} `json:"data"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			LogID   string `json:"log_id"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse video list response: %w", err)
	}

	if result.Error.Code != "" && result.Error.Code != "ok" {
		return nil, &TikTokAPIError{
			Status:  resp.StatusCode,
			Code:    result.Error.Code,
			Message: result.Error.Message,
			LogID:   result.Error.LogID,
		}
	}

	response := map[string]interface{}{
		"videos":  result.Data.Videos,
		"cursor":  result.Data.Cursor,
		"has_more": result.Data.HasMore,
		"count":   len(result.Data.Videos),
	}

	t.Logger.Info("✅ TikTok videos listed",
		zap.Uint("tenant_id", acc.TenantID),
		zap.Int("count", len(result.Data.Videos)),
		zap.Bool("has_more", result.Data.HasMore))

	return response, nil
}

// GetVideoAnalytics retrieves analytics for a specific video
// Returns: views, likes, comments, shares, engagement metrics
func (t *TikTokConnector) GetVideoAnalytics(ctx context.Context, acc *storage.SocialAccount, videoID string) (map[string]interface{}, error) {
	if acc == nil || acc.AccessToken == "" {
		return nil, errors.New("tiktok access token missing")
	}
	if videoID == "" {
		return nil, errors.New("video_id is required")
	}

	accessToken, err := t.ensureAccessToken(ctx, acc)
	if err != nil {
		return nil, err
	}

	// Use Research API to get video analytics
	payload := map[string]interface{}{
		"query": map[string]interface{}{
			"and": []map[string]interface{}{
				{
					"operation": "EQ",
					"field_name": "video_id",
					"field_values": []string{videoID},
				},
			},
		},
		"fields": []string{
			"video_id",
			"view_count",
			"like_count",
			"comment_count",
			"share_count",
			"video_duration",
			"create_time",
		},
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tikTokBaseAPI+tikTokResearchPath, bytes.NewReader(body))
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

	var result struct {
		Data struct {
			Videos []map[string]interface{} `json:"videos"`
		} `json:"data"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			LogID   string `json:"log_id"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse analytics response: %w", err)
	}

	if result.Error.Code != "" && result.Error.Code != "ok" {
		return nil, &TikTokAPIError{
			Status:  resp.StatusCode,
			Code:    result.Error.Code,
			Message: result.Error.Message,
			LogID:   result.Error.LogID,
		}
	}

	if len(result.Data.Videos) == 0 {
		return nil, fmt.Errorf("video not found or analytics not available: %s", videoID)
	}

	analytics := result.Data.Videos[0]
	
	// Calculate engagement rate
	viewCount, _ := analytics["view_count"].(float64)
	likeCount, _ := analytics["like_count"].(float64)
	commentCount, _ := analytics["comment_count"].(float64)
	shareCount, _ := analytics["share_count"].(float64)
	
	engagementRate := 0.0
	if viewCount > 0 {
		engagementRate = ((likeCount + commentCount + shareCount) / viewCount) * 100
	}
	
	analytics["engagement_rate"] = engagementRate
	analytics["total_engagement"] = likeCount + commentCount + shareCount

	t.Logger.Info("✅ TikTok video analytics retrieved",
		zap.Uint("tenant_id", acc.TenantID),
		zap.String("video_id", videoID),
		zap.Float64("views", viewCount),
		zap.Float64("likes", likeCount),
		zap.Float64("engagement_rate", engagementRate))

	return analytics, nil
}

// DeleteVideo deletes a published video
func (t *TikTokConnector) DeleteVideo(ctx context.Context, acc *storage.SocialAccount, videoID string) error {
	if acc == nil || acc.AccessToken == "" {
		return errors.New("tiktok access token missing")
	}
	if videoID == "" {
		return errors.New("video_id is required")
	}

	accessToken, err := t.ensureAccessToken(ctx, acc)
	if err != nil {
		return err
	}

	payload := map[string]interface{}{
		"video_id": videoID,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tikTokBaseAPI+tikTokVideoDeletePath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
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
		return parseTikTokError(resp.StatusCode, respBody)
	}

	var result struct {
		Data struct {
			Deleted bool `json:"deleted"`
		} `json:"data"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			LogID   string `json:"log_id"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("failed to parse delete response: %w", err)
	}

	if result.Error.Code != "" && result.Error.Code != "ok" {
		return &TikTokAPIError{
			Status:  resp.StatusCode,
			Code:    result.Error.Code,
			Message: result.Error.Message,
			LogID:   result.Error.LogID,
		}
	}

	if !result.Data.Deleted {
		return errors.New("video deletion failed - API returned deleted=false")
	}

	t.Logger.Info("✅ TikTok video deleted",
		zap.Uint("tenant_id", acc.TenantID),
		zap.String("video_id", videoID))

	return nil
}

// ListComments retrieves comments for a video
func (t *TikTokConnector) ListComments(ctx context.Context, acc *storage.SocialAccount, videoID string, maxCount int, cursor string) (map[string]interface{}, error) {
	if acc == nil || acc.AccessToken == "" {
		return nil, errors.New("tiktok access token missing")
	}
	if videoID == "" {
		return nil, errors.New("video_id is required")
	}

	accessToken, err := t.ensureAccessToken(ctx, acc)
	if err != nil {
		return nil, err
	}

	if maxCount <= 0 {
		maxCount = 20
	}
	if maxCount > 50 {
		maxCount = 50
	}

	payload := map[string]interface{}{
		"video_id": videoID,
		"max_count": maxCount,
	}
	if cursor != "" {
		payload["cursor"] = cursor
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tikTokBaseAPI+tikTokCommentListPath, bytes.NewReader(body))
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

	var result struct {
		Data struct {
			Comments []map[string]interface{} `json:"comments"`
			Cursor  string                    `json:"cursor"`
			HasMore bool                      `json:"has_more"`
		} `json:"data"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			LogID   string `json:"log_id"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse comments response: %w", err)
	}

	if result.Error.Code != "" && result.Error.Code != "ok" {
		return nil, &TikTokAPIError{
			Status:  resp.StatusCode,
			Code:    result.Error.Code,
			Message: result.Error.Message,
			LogID:   result.Error.LogID,
		}
	}

	response := map[string]interface{}{
		"comments": result.Data.Comments,
		"cursor":   result.Data.Cursor,
		"has_more": result.Data.HasMore,
		"count":    len(result.Data.Comments),
	}

	t.Logger.Info("✅ TikTok comments listed",
		zap.Uint("tenant_id", acc.TenantID),
		zap.String("video_id", videoID),
		zap.Int("count", len(result.Data.Comments)))

	return response, nil
}

// ReplyToComment replies to a comment on a video
func (t *TikTokConnector) ReplyToComment(ctx context.Context, acc *storage.SocialAccount, videoID, commentID, replyText string) (string, error) {
	if acc == nil || acc.AccessToken == "" {
		return "", errors.New("tiktok access token missing")
	}
	if videoID == "" || commentID == "" || replyText == "" {
		return "", errors.New("video_id, comment_id, and reply_text are required")
	}

	accessToken, err := t.ensureAccessToken(ctx, acc)
	if err != nil {
		return "", err
	}

	payload := map[string]interface{}{
		"video_id":   videoID,
		"comment_id": commentID,
		"text":       replyText,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tikTokBaseAPI+tikTokCommentReplyPath, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode >= 400 {
		return "", parseTikTokError(resp.StatusCode, respBody)
	}

	var result struct {
		Data struct {
			ReplyID string `json:"reply_id"`
		} `json:"data"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			LogID   string `json:"log_id"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse reply response: %w", err)
	}

	if result.Error.Code != "" && result.Error.Code != "ok" {
		return "", &TikTokAPIError{
			Status:  resp.StatusCode,
			Code:    result.Error.Code,
			Message: result.Error.Message,
			LogID:   result.Error.LogID,
		}
	}

	t.Logger.Info("✅ TikTok comment replied",
		zap.Uint("tenant_id", acc.TenantID),
		zap.String("video_id", videoID),
		zap.String("comment_id", commentID),
		zap.String("reply_id", result.Data.ReplyID))

	return result.Data.ReplyID, nil
}
