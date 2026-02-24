package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// PostingStatusData represents Section 5: Posting Status (Secondary)
type PostingStatusData struct {
	PostsScheduled int64           `json:"posts_scheduled"`
	PostsPublished int64           `json:"posts_published"`
	FailedPosts    int64           `json:"failed_posts"`
	FailureReasons []FailureReason `json:"failure_reasons"`
}

// FailureReason represents a reason for post failures
type FailureReason struct {
	Reason string `json:"reason"`
	Count  int64  `json:"count"`
}

// BuildPostingStatusData builds posting status metrics for the dashboard
func BuildPostingStatusData(ctx *AdminContext, period string, customStart, customEnd *time.Time, reqCtx context.Context) (*PostingStatusData, error) {
	var since time.Time
	var unti time.Time = time.Now()

	if period == "custom" && customStart != nil && customEnd != nil {
		since = *customStart
		unti = *customEnd
		if since.After(unti) {
			since, unti = unti, since
		}
	} else if period == "7d" {
		since = time.Now().Add(-7 * 24 * time.Hour)
	} else if period == "30d" {
		since = time.Now().Add(-30 * 24 * time.Hour)
	} else {
		since = time.Now().Add(-24 * time.Hour)
	}

	data := &PostingStatusData{
		FailureReasons: []FailureReason{},
	}

	// Posts scheduled (Total posts in the period)
	var scheduledCount int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.ScheduledPost{}).
		Where("created_at >= ? AND created_at <= ?", since, unti).
		Count(&scheduledCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count scheduled posts: %w", err)
	}
	data.PostsScheduled = scheduledCount

	// Posts published (status = 'published' or 'posted')
	var publishedCount int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.ScheduledPost{}).
		Where("created_at >= ? AND created_at <= ? AND status IN ?", since, unti, []string{"published", "posted"}).
		Count(&publishedCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count published posts: %w", err)
	}
	data.PostsPublished = publishedCount

	// Failed posts (status = 'failed' or 'error')
	var failedCount int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.ScheduledPost{}).
		Where("created_at >= ? AND created_at <= ? AND status IN ?", since, unti, []string{"failed", "error"}).
		Count(&failedCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count failed posts: %w", err)
	}
	data.FailedPosts = failedCount

	// Failure reasons - check post_logs or error messages
	// For now, we'll check if ScheduledPost has an error_message field or use post_logs
	var failureReasons []struct {
		Reason string
		Count  int64
	}

	// Try to get failure reasons from post_logs if available
	if err := ctx.DB.WithContext(reqCtx).
		Table("post_logs").
		Select("error_message as reason, COUNT(*) as count").
		Where("created_at >= ? AND status = ? AND error_message IS NOT NULL", since, "failed").
		Group("error_message").
		Scan(&failureReasons).Error; err != nil {
		ctx.Logger.Debug("failed to get failure reasons from post_logs", zap.Error(err))
		// Fallback: use generic reason if specific reasons not available
		if data.FailedPosts > 0 {
			data.FailureReasons = []FailureReason{
				{Reason: "Unknown error", Count: data.FailedPosts},
			}
		}
	} else {
		for _, fr := range failureReasons {
			if fr.Reason == "" {
				fr.Reason = "Unknown error"
			}
			data.FailureReasons = append(data.FailureReasons, FailureReason{
				Reason: fr.Reason,
				Count:  fr.Count,
			})
		}
	}

	return data, nil
}
