package social

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
)

type PostNotifier func(ctx context.Context, tenantID uint, platform string, success bool, message string)

type Scheduler struct {
	DB               *gorm.DB
	Logger           *zap.Logger
	CaptionOptimizer CaptionOptimizer
	PostNotifier     PostNotifier
	// Platform adapters will be injected here as needed
}

type CaptionOptimizer interface {
	OptimizeCaption(ctx context.Context, req OptimizeCaptionRequest) (*OptimizeCaptionResult, error)
}

type OptimizeCaptionRequest struct {
	OriginalCaption string
	Platform        string
	MediaType       string
	TenantID        uint
}

type OptimizeCaptionResult struct {
	OptimizedCaption string
	Hashtags         []string
	SuggestedTime    string
	Improvements     string
}

func NewScheduler(db *gorm.DB, logger *zap.Logger, optimizer CaptionOptimizer) *Scheduler {
	return &Scheduler{
		DB:               db,
		Logger:           logger,
		CaptionOptimizer: optimizer,
	}
}

// Run starts the scheduler loop; call as a goroutine from App.Run()
func (s *Scheduler) Run(ctx context.Context, pollInterval time.Duration, maxAttempts int) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.Logger.Info("scheduler stopped")
			return
		case <-ticker.C:
			s.processDuePosts(maxAttempts)
		}
	}
}

func (s *Scheduler) processDuePosts(maxAttempts int) {
	now := time.Now().UTC()
	// Claim posts in a txn: find scheduled posts due now
	// Note: scheduled_at is stored in UTC, but we compare with UTC now
	var posts []storage.ScheduledPost
	if err := s.DB.Where("status = ? AND scheduled_at <= ?", "scheduled", now).Limit(10).Find(&posts).Error; err != nil {
		s.Logger.Warn("scheduler: fetch due posts failed", zap.Error(err))
		return
	}

	for _, p := range posts {
		// Try to claim by updating status to "posting" if still scheduled (optimistic)
		tx := s.DB.Begin()
		if err := tx.Model(&storage.ScheduledPost{}).Where("id = ? AND status = ?", p.ID, "scheduled").Update("status", "posting").Error; err != nil {
			tx.Rollback()
			continue
		}
		tx.Commit()

		go s.executePost(p) // fire-and-forget per post
	}
}

func (s *Scheduler) executePost(p storage.ScheduledPost) {
	ctx := context.Background()

	s.Logger.Info("📤 Starting post execution",
		zap.Uint("post_id", p.ID),
		zap.Uint("tenant_id", p.TenantID),
		zap.String("name", p.Name))

	// Load assets
	var ids []int
	_ = json.Unmarshal([]byte(p.MediaAssetIDs), &ids)
	var assets []storage.MediaAsset
	if err := s.DB.Where("id IN ?", ids).Find(&assets).Error; err != nil {
		s.Logger.Error("scheduler: failed to load assets", zap.Error(err))
		s.markFailed(p, "failed to load assets: "+err.Error(), false, false)
		return
	}

	// Determine platforms
	var platforms []string
	_ = json.Unmarshal([]byte(p.Platforms), &platforms)
	if len(platforms) == 0 {
		platforms = []string{"instagram"}
	}

	// Build media URLs and detect types
	var urls []string
	mediaContext := "image"
	primaryType := ""
	hasExplicitReel := false

	for _, a := range assets {
		var arr []string
		_ = json.Unmarshal([]byte(a.URLs), &arr)
		urls = append(urls, arr...)

		itemType := inferMediaType(a.Type, arr)
		if primaryType == "" {
			primaryType = itemType
		}
		if itemType == "reel" {
			hasExplicitReel = true
		}
	}

	if len(urls) > 1 {
		mediaContext = "carousel"
	} else if primaryType == "video" || primaryType == "reel" {
		mediaContext = "video"
	}

	if mediaContext == "image" {
		switch primaryType {
		case "video", "reel":
			mediaContext = "video"
		case "carousel":
			mediaContext = "carousel"
		default:
			mediaContext = "image"
		}
	}

	assetType := primaryType
	if assetType == "" {
		assetType = mediaContext
	}
	if len(urls) > 1 && assetType != "carousel" && assetType != "reel" {
		assetType = "carousel"
	}

	mediaTypeForAI := mediaContext
	if hasExplicitReel {
		mediaTypeForAI = "reel"
	} else if primaryType != "" {
		mediaTypeForAI = primaryType
	}
	if mediaTypeForAI == "" {
		mediaTypeForAI = inferMediaType(assetType, urls)
	}
	if mediaTypeForAI == "" {
		mediaTypeForAI = "image"
	}

	// AI Caption Optimization with logging
	originalCaption := p.Caption
	optimizedCaption := originalCaption
	var improvements string

	if s.CaptionOptimizer != nil {
		for _, platform := range platforms {
			s.Logger.Info("🤖 Optimizing caption with AI",
				zap.String("platform", platform),
				zap.Uint("tenant_id", p.TenantID))

			// Log optimization start
			s.logPostActivity(p.TenantID, p.ID, platform, "optimizing", originalCaption, "", "in_progress", "")

			req := OptimizeCaptionRequest{
				OriginalCaption: originalCaption,
				Platform:        platform,
				MediaType:       mediaTypeForAI,
				TenantID:        p.TenantID,
			}

			result, err := s.CaptionOptimizer.OptimizeCaption(ctx, req)
			if err == nil && result != nil {
				optimizedCaption = result.OptimizedCaption
				improvements = result.Improvements
				s.Logger.Info("✨ Caption optimized",
					zap.String("original_length", fmt.Sprintf("%d chars", len(originalCaption))),
					zap.String("optimized_length", fmt.Sprintf("%d chars", len(optimizedCaption))),
					zap.Strings("hashtags", result.Hashtags))

				// Log optimization success
				hashtagsJSON, _ := json.Marshal(result.Hashtags)
				metadata := fmt.Sprintf(`{"hashtags":%s,"suggested_time":"%s"}`, hashtagsJSON, result.SuggestedTime)
				s.logPostActivityWithMeta(p.TenantID, p.ID, platform, "optimized", originalCaption, optimizedCaption, "success", improvements, metadata, "")
			}
			break // Use same optimized caption for all platforms
		}
	}

	// Post to each platform
	successCount := 0
	var lastError error
	succeeded := make([]string, 0, len(platforms))
	failed := make([]string, 0, len(platforms))

	for _, platform := range platforms {
		s.Logger.Info("📱 Posting to platform",
			zap.String("platform", platform),
			zap.Int("media_count", len(urls)))

		// Log posting attempt
		s.logPostActivity(p.TenantID, p.ID, platform, "posting", originalCaption, optimizedCaption, "in_progress", "")

		if err := s.postToPlatform(ctx, p.TenantID, platform, urls, optimizedCaption, assetType); err != nil {
			s.Logger.Error("post failed",
				zap.String("platform", platform),
				zap.Error(err))
			lastError = err
			failed = append(failed, platform)

			// Log failure
			s.logPostActivity(p.TenantID, p.ID, platform, "failed", originalCaption, optimizedCaption, "failed", err.Error())

			postLabel := strings.TrimSpace(p.Name)
			if postLabel == "" {
				postLabel = "Scheduled post"
			} else {
				postLabel = fmt.Sprintf("'%s'", postLabel)
			}
			failMsg := fmt.Sprintf("%s failed on %s: %s", postLabel, platform, err.Error())
			s.notifyPostResult(ctx, p.TenantID, platform, false, failMsg)
		} else {
			successCount++
			succeeded = append(succeeded, platform)
			s.Logger.Info("✅ Posted successfully", zap.String("platform", platform))

			// Log success
			s.logPostActivity(p.TenantID, p.ID, platform, "posted", originalCaption, optimizedCaption, "success", "")

			postLabel := strings.TrimSpace(p.Name)
			if postLabel == "" {
				postLabel = "Scheduled post"
			} else {
				postLabel = fmt.Sprintf("'%s'", postLabel)
			}
			successMsg := fmt.Sprintf("%s went live on %s.", postLabel, platform)
			s.notifyPostResult(ctx, p.TenantID, platform, true, successMsg)
		}
	}

	// Mark status based on results
	now := time.Now()
	if successCount == len(platforms) {
		// All successful
		if err := s.DB.Model(&storage.ScheduledPost{}).Where("id = ?", p.ID).
			Updates(map[string]interface{}{
				"status":     "posted",
				"posted_at":  &now,
				"last_error": "",
			}).Error; err != nil {
			s.Logger.Error("failed to mark posted", zap.Error(err))
		}
		s.Logger.Info("🎉 Post completed successfully",
			zap.Uint("post_id", p.ID),
			zap.Int("platforms", successCount))
	} else if successCount > 0 {
		// Partial success – do not retry
		msgParts := []string{}
		if len(succeeded) > 0 {
			msgParts = append(msgParts, fmt.Sprintf("succeeded: %s", strings.Join(succeeded, ", ")))
		}
		if len(failed) > 0 {
			msgParts = append(msgParts, fmt.Sprintf("failed: %s", strings.Join(failed, ", ")))
		}
		if lastError != nil {
			msgParts = append(msgParts, lastError.Error())
		}
		reason := fmt.Sprintf("Posted to %d/%d platforms (%s)", successCount, len(platforms), strings.Join(msgParts, " | "))
		s.markFailed(p, reason, false, true)
	} else {
		// Complete failure
		if lastError != nil {
			s.markFailed(p, lastError.Error(), true, false)
		} else {
			s.markFailed(p, "all platforms failed", true, false)
		}
	}
}

func (s *Scheduler) notifyPostResult(ctx context.Context, tenantID uint, platform string, success bool, message string) {
	if s.PostNotifier == nil {
		return
	}
	s.PostNotifier(ctx, tenantID, platform, success, message)
}

func (s *Scheduler) postToPlatform(ctx context.Context, tenantID uint, platform string, urls []string, caption string, assetType string) error {
	// Load tenant's social account for this platform
	var acc storage.SocialAccount
	if err := s.DB.Where("tenant_id = ? AND platform = ? AND connected = ?", tenantID, platform, true).
		First(&acc).Error; err != nil {
		return fmt.Errorf("no connected %s account: %w", platform, err)
	}

	// Decrypt access token if it's encrypted
	if acc.AccessToken != "" {
		decrypted, err := utils.DecryptToken(acc.AccessToken)
		if err == nil {
			// Only use decrypted if decryption succeeds (token might be plain text in some cases)
			acc.AccessToken = decrypted
		}
		// If decryption fails, assume token is already in plain text and use as-is
	}
	if acc.RefreshToken != "" {
		if decrypted, err := utils.DecryptToken(acc.RefreshToken); err == nil {
			acc.RefreshToken = decrypted
		}
	}

	platformKey := strings.ToLower(platform)

	if platformKey == "instagram" && assetType != "video" && assetType != "reel" {
		sanitized, err := normalizeInstagramAspectRatio(ctx, urls, s.Logger)
		if err != nil {
			return fmt.Errorf("instagram media validation failed: %w", err)
		}
		urls = sanitized
	}

	// Get platform connector
	connector, exists := Connectors[platformKey]
	if !exists {
		return fmt.Errorf("unsupported platform: %s", platform)
	}

	// Create temporary MediaAsset for posting
	urlsJSON, _ := json.Marshal(urls)
	if assetType == "" {
		assetType = inferMediaType("", urls)
	}
	asset := &storage.MediaAsset{
		TenantID: tenantID,
		Type:     assetType,
		Caption:  caption,
		URLs:     string(urlsJSON),
	}

	// Post using platform connector
	if err := connector.PostMedia(ctx, &acc, asset, caption); err != nil {
		return fmt.Errorf("%s posting failed: %w", platform, err)
	}

	return nil
}

func (s *Scheduler) markFailed(p storage.ScheduledPost, reason string, allowRetry bool, partial bool) {
	// Increment attempts and set status with exponential backoff
	attempts := p.Attempts + 1
	status := "failed"
	var nextSchedule *time.Time

	// Retry logic with exponential backoff
	if partial {
		status = "partial_failed"
	}

	if allowRetry && attempts < 3 {
		status = "scheduled"
		// Retry after 5min, 15min, then fail
		backoffMinutes := []int{5, 15, 60}
		if attempts-1 < len(backoffMinutes) {
			retry := time.Now().Add(time.Duration(backoffMinutes[attempts-1]) * time.Minute)
			nextSchedule = &retry
			s.Logger.Info("⏱️ Scheduling retry",
				zap.Uint("post_id", p.ID),
				zap.Int("attempt", attempts),
				zap.Time("retry_at", retry))
		}
	}

	updates := map[string]interface{}{
		"attempts":   attempts,
		"status":     status,
		"last_error": reason,
	}
	if nextSchedule != nil {
		updates["scheduled_at"] = nextSchedule
	}

	s.DB.Model(&storage.ScheduledPost{}).Where("id = ?", p.ID).Updates(updates)

	s.Logger.Warn("❌ Post attempt failed",
		zap.Uint("post_id", p.ID),
		zap.Int("attempt", attempts),
		zap.String("reason", reason))

	s.notifyTenantFailure(p, status, reason)
}

// logPostActivity creates a detailed log entry for post activity
func (s *Scheduler) logPostActivity(tenantID, postID uint, platform, action, originalCaption, optimizedCaption, status, errorMsg string) {
	s.logPostActivityWithMeta(tenantID, postID, platform, action, originalCaption, optimizedCaption, status, "", "{}", errorMsg)
}

// logPostActivityWithMeta creates a detailed log with metadata
func (s *Scheduler) logPostActivityWithMeta(tenantID, postID uint, platform, action, originalCaption, optimizedCaption, status, improvements, metadata, errorMsg string) {
	// Ensure metadata is valid JSON (empty string becomes empty JSON object)
	if metadata == "" {
		metadata = "{}"
	}

	log := storage.PostLog{
		TenantID:         tenantID,
		ScheduledPostID:  postID,
		Platform:         platform,
		Action:           action,
		OriginalCaption:  originalCaption,
		OptimizedCaption: optimizedCaption,
		Status:           status,
		ErrorMessage:     errorMsg,
		Improvements:     improvements,
		Metadata:         metadata,
		CreatedAt:        time.Now(),
	}

	if err := s.DB.Create(&log).Error; err != nil {
		s.Logger.Error("failed to log post activity", zap.Error(err))
	}
}

func (s *Scheduler) notifyTenantFailure(p storage.ScheduledPost, status, reason string) {
	entry := storage.EventLog{
		TenantID:   p.TenantID,
		EventType:  "posting",
		SenderName: "scheduler",
		Message:    fmt.Sprintf("Post '%s' status: %s. %s", p.Name, status, reason),
	}
	if err := s.DB.Create(&entry).Error; err != nil {
		s.Logger.Warn("failed to record posting failure event", zap.Error(err))
	}
}

const (
	instagramMinAspectRatio = 0.8
	instagramMaxAspectRatio = 1.91
)

func normalizeInstagramAspectRatio(ctx context.Context, urls []string, logger *zap.Logger) ([]string, error) {
	if len(urls) == 0 {
		return nil, fmt.Errorf("no media provided")
	}

	client := &http.Client{Timeout: 15 * time.Second}
	result := make([]string, len(urls))

	for idx, mediaURL := range urls {
		result[idx] = mediaURL

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, mediaURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build request for %s: %w", mediaURL, err)
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to download media %s: %w", mediaURL, err)
		}

		contentType := resp.Header.Get("Content-Type")
		if contentType != "" && !strings.HasPrefix(strings.ToLower(contentType), "image/") {
			resp.Body.Close()
			// Skip non-image media (video/reels)
			continue
		}

		cfg, _, err := image.DecodeConfig(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("unable to inspect image %s: %w", mediaURL, err)
		}

		if cfg.Height == 0 {
			return nil, fmt.Errorf("image %s has invalid dimensions", mediaURL)
		}

		ratio := float64(cfg.Width) / float64(cfg.Height)
		if ratio >= instagramMinAspectRatio && ratio <= instagramMaxAspectRatio {
			continue
		}

		transformation := "c_fill,g_auto,ar_4:5"
		targetRatio := instagramMinAspectRatio
		if ratio > instagramMaxAspectRatio {
			transformation = "c_fill,g_auto,ar_1.91:1"
			targetRatio = instagramMaxAspectRatio
		}

		transformedURL, err := applyCloudinaryTransformation(mediaURL, transformation)
		if err != nil {
			return nil, fmt.Errorf("image %s has aspect ratio %.2f. Instagram requires between %.2f and %.2f (automatic resize failed: %w)", mediaURL, ratio, instagramMinAspectRatio, instagramMaxAspectRatio, err)
		}

		if logger != nil {
			logger.Info("Adjusted Instagram carousel image",
				zap.String("original_url", mediaURL),
				zap.String("transformed_url", transformedURL),
				zap.Float64("original_ratio", ratio),
				zap.Float64("target_ratio", targetRatio))
		}

		result[idx] = transformedURL
	}

	return result, nil
}

func applyCloudinaryTransformation(mediaURL, transformation string) (string, error) {
	parsed, err := url.Parse(mediaURL)
	if err != nil {
		return "", fmt.Errorf("invalid media URL: %w", err)
	}

	if !strings.Contains(parsed.Host, "cloudinary.com") {
		return "", fmt.Errorf("media host %s is not Cloudinary", parsed.Host)
	}

	const uploadSegment = "/upload/"
	idx := strings.Index(parsed.Path, uploadSegment)
	if idx == -1 {
		return "", fmt.Errorf("media URL does not contain an upload segment")
	}

	prefix := parsed.Path[:idx+len(uploadSegment)]
	suffix := parsed.Path[idx+len(uploadSegment):]
	if suffix == "" {
		return "", fmt.Errorf("cloudinary path suffix missing")
	}

	segments := strings.Split(suffix, "/")
	if len(segments) == 0 {
		return "", fmt.Errorf("cloudinary path malformed")
	}

	// Strip existing transformations until we reach the version segment (starts with 'v').
	for len(segments) > 0 && !strings.HasPrefix(segments[0], "v") {
		if segments[0] == transformation {
			return mediaURL, nil
		}
		segments = segments[1:]
	}

	if len(segments) == 0 {
		return "", fmt.Errorf("cloudinary path missing version segment")
	}

	newSegments := append([]string{transformation}, segments...)
	parsed.Path = prefix + strings.Join(newSegments, "/")

	return parsed.String(), nil
}
