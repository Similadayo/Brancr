package social

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
)

type Scheduler struct {
	DB               *gorm.DB
	Logger           *zap.Logger
	CaptionOptimizer CaptionOptimizer
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
	now := time.Now()
	// Claim posts in a txn: find scheduled posts due now
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
		s.markFailed(p, "failed to load assets: "+err.Error())
		return
	}

	// Determine platforms
	var platforms []string
	_ = json.Unmarshal([]byte(p.Platforms), &platforms)
	if len(platforms) == 0 {
		platforms = []string{"instagram"}
	}

	// Build media URLs
	var urls []string
	mediaType := "image"
	for _, a := range assets {
		var arr []string
		_ = json.Unmarshal([]byte(a.URLs), &arr)
		urls = append(urls, arr...)
		if a.Type == "video" {
			mediaType = "video"
		}
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
				MediaType:       mediaType,
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

	for _, platform := range platforms {
		s.Logger.Info("📱 Posting to platform", 
			zap.String("platform", platform),
			zap.Int("media_count", len(urls)))

		// Log posting attempt
		s.logPostActivity(p.TenantID, p.ID, platform, "posting", originalCaption, optimizedCaption, "in_progress", "")

		if err := s.postToPlatform(ctx, p.TenantID, platform, urls, optimizedCaption); err != nil {
			s.Logger.Error("post failed", 
				zap.String("platform", platform), 
				zap.Error(err))
			lastError = err

			// Log failure
			s.logPostActivity(p.TenantID, p.ID, platform, "failed", originalCaption, optimizedCaption, "failed", err.Error())
		} else {
			successCount++
			s.Logger.Info("✅ Posted successfully", zap.String("platform", platform))

			// Log success
			s.logPostActivity(p.TenantID, p.ID, platform, "posted", originalCaption, optimizedCaption, "success", "")
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
		// Partial success
		errMsg := fmt.Sprintf("Posted to %d/%d platforms", successCount, len(platforms))
		if lastError != nil {
			errMsg += ": " + lastError.Error()
		}
		s.markFailed(p, errMsg)
	} else {
		// Complete failure
		if lastError != nil {
			s.markFailed(p, lastError.Error())
		} else {
			s.markFailed(p, "all platforms failed")
		}
	}
}

func (s *Scheduler) postToPlatform(ctx context.Context, tenantID uint, platform string, urls []string, caption string) error {
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

	// Get platform connector
	connector, exists := Connectors[strings.ToLower(platform)]
	if !exists {
		return fmt.Errorf("unsupported platform: %s", platform)
	}

	// Create temporary MediaAsset for posting
	urlsJSON, _ := json.Marshal(urls)
	asset := &storage.MediaAsset{
		TenantID: tenantID,
		Type:     "image", // simplified for now
		Caption:  caption,
		URLs:     string(urlsJSON),
	}

	// Post using platform connector
	if err := connector.PostMedia(ctx, &acc, asset, caption); err != nil {
		return fmt.Errorf("%s posting failed: %w", platform, err)
	}

	return nil
}

func (s *Scheduler) markFailed(p storage.ScheduledPost, reason string) {
	// Increment attempts and set status with exponential backoff
	attempts := p.Attempts + 1
	status := "failed"
	var nextSchedule *time.Time

	// Retry logic with exponential backoff
	if attempts < 3 {
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
