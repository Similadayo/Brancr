package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// SystemHealthData represents Section 1: System & Platform Health
type SystemHealthData struct {
	Platforms    []PlatformHealth `json:"platforms"`
	System       SystemMetrics    `json:"system"`
	LastIncident *time.Time       `json:"last_incident,omitempty"`
}

// PlatformHealth represents health metrics for a platform
type PlatformHealth struct {
	Name             string `json:"name"`
	MessagesReceived int64  `json:"messages_received"`
	MessagesSent     int64  `json:"messages_sent"`
	FailedSends      int64  `json:"failed_sends"`
	RetryCount       int64  `json:"retry_count"`
	APIErrors        int64  `json:"api_errors"`
	Status           string `json:"status"` // "green", "amber", "red"
}

// SystemMetrics represents system-level metrics
type SystemMetrics struct {
	BackgroundJobFailures int64      `json:"background_job_failures"`
	QueueBacklogSize      int64      `json:"queue_backlog_size"`
	LastIncidentTimestamp *time.Time `json:"last_incident_timestamp,omitempty"`
	ServiceUptime         string     `json:"service_uptime"` // "99.9%" or similar
}

// BuildSystemHealthData builds system health metrics for the dashboard
func BuildSystemHealthData(ctx *AdminContext, period string, customStart, customEnd *time.Time, reqCtx context.Context) (*SystemHealthData, error) {
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

	data := &SystemHealthData{
		Platforms: []PlatformHealth{},
		System:    SystemMetrics{},
	}

	// Build platform health metrics
	platforms := []string{"instagram", "facebook", "whatsapp", "tiktok"}
	for _, platform := range platforms {
		health, err := buildPlatformHealth(ctx, platform, since, unti, reqCtx)
		if err != nil {
			ctx.Logger.Warn("failed to build platform health", zap.String("platform", platform), zap.Error(err))
			continue
		}
		data.Platforms = append(data.Platforms, *health)
	}

	// Build system metrics
	systemMetrics, err := buildSystemMetrics(ctx, since, reqCtx)
	if err != nil {
		ctx.Logger.Warn("failed to build system metrics", zap.Error(err))
	} else {
		data.System = *systemMetrics
	}

	return data, nil
}

// buildPlatformHealth builds health metrics for a specific platform
func buildPlatformHealth(ctx *AdminContext, platform string, since, unti time.Time, reqCtx context.Context) (*PlatformHealth, error) {
	health := &PlatformHealth{
		Name:   platform,
		Status: "green",
	}

	// Messages received (incoming) - customer messages to tenants
	var receivedCount int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.Interaction{}).
		Where("platform = ? AND created_at >= ? AND created_at <= ? AND direction = ?", platform, since, unti, "incoming").
		Count(&receivedCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count messages received: %w", err)
	}
	health.MessagesReceived = receivedCount

	// Messages sent (outgoing) - tenant/AI replies to customers
	var sentCount int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.Interaction{}).
		Where("platform = ? AND created_at >= ? AND created_at <= ? AND direction = ?", platform, since, unti, "outgoing").
		Count(&sentCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count messages sent: %w", err)
	}
	health.MessagesSent = sentCount

	// Failed sends - check interactions with error status or failed response_status
	var failedCount int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.Interaction{}).
		Where("platform = ? AND created_at >= ? AND created_at <= ? AND direction = ? AND response_status IN ?",
			platform, since, unti, "outgoing", []string{"failed", "error"}).
		Count(&failedCount).Error; err != nil {
		ctx.Logger.Debug("failed to count failed sends from interactions", zap.String("platform", platform), zap.Error(err))
		// Fallback: check log entries for errors
		if err := ctx.DB.WithContext(reqCtx).
			Model(&storage.LogEntry{}).
			Where("source ILIKE ? AND level = ? AND created_at >= ? AND created_at <= ? AND message ILIKE ?",
				fmt.Sprintf("%%%s%%", platform), "ERROR", since, unti, "%failed%").
			Count(&failedCount).Error; err != nil {
			ctx.Logger.Debug("failed to count failed sends from logs", zap.String("platform", platform), zap.Error(err))
		}
	}
	health.FailedSends = failedCount

	// Retry count - count from interactions metadata or webhook retry logs
	var retryCount int64
	// Check if metadata contains retry information
	if err := ctx.DB.WithContext(reqCtx).
		Raw(`
			SELECT COUNT(*) as count
			FROM interactions
			WHERE platform = ? AND created_at >= ? AND created_at <= ? AND direction = 'outgoing'
			  AND (metadata::text LIKE '%retry%' OR metadata::text LIKE '%retries%')
		`, platform, since, unti).
		Scan(&retryCount).Error; err != nil {
		ctx.Logger.Debug("failed to count retries", zap.String("platform", platform), zap.Error(err))
	}
	health.RetryCount = retryCount

	// API errors - count from log entries
	var apiErrorCount int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.LogEntry{}).
		Where("source ILIKE ? AND level = ? AND created_at >= ? AND created_at <= ? AND message ILIKE ?",
			fmt.Sprintf("%%%s%%", platform), "ERROR", since, unti, "%api%").
		Count(&apiErrorCount).Error; err != nil {
		ctx.Logger.Debug("failed to count API errors", zap.String("platform", platform), zap.Error(err))
	}
	health.APIErrors = apiErrorCount

	// Determine status
	if health.FailedSends > 10 || health.APIErrors > 5 {
		health.Status = "red"
	} else if health.FailedSends > 5 || health.APIErrors > 2 {
		health.Status = "amber"
	}

	return health, nil
}

// buildSystemMetrics builds system-level metrics
func buildSystemMetrics(ctx *AdminContext, since time.Time, reqCtx context.Context) (*SystemMetrics, error) {
	metrics := &SystemMetrics{}

	// Background job failures - count from log entries
	var failures int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.LogEntry{}).
		Where("source IN ? AND level = ? AND created_at >= ?",
			[]string{"scheduler", "worker", "background"}, "ERROR", since).
		Count(&failures).Error; err != nil {
		ctx.Logger.Debug("failed to count background job failures", zap.Error(err))
	}
	metrics.BackgroundJobFailures = failures

	// Queue backlog size - count pending interactions or scheduled tasks
	var queueBacklog int64
	// Count pending outgoing interactions that haven't been sent
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.Interaction{}).
		Where("direction = ? AND response_status = ? AND created_at >= ?",
			"outbound", "pending", time.Now().Add(-24*time.Hour)).
		Count(&queueBacklog).Error; err != nil {
		ctx.Logger.Debug("failed to count queue backlog", zap.Error(err))
		// Fallback: count scheduled posts pending
		if err := ctx.DB.WithContext(reqCtx).
			Model(&storage.ScheduledPost{}).
			Where("status = ? AND scheduled_at >= ?", "pending", time.Now()).
			Count(&queueBacklog).Error; err != nil {
			ctx.Logger.Debug("failed to count scheduled posts backlog", zap.Error(err))
		}
	}
	metrics.QueueBacklogSize = queueBacklog

	// Last incident - find most recent ERROR log entry
	var lastError storage.LogEntry
	if err := ctx.DB.WithContext(reqCtx).
		Where("level = ?", "ERROR").
		Order("created_at DESC").
		First(&lastError).Error; err == nil {
		metrics.LastIncidentTimestamp = &lastError.CreatedAt
	}

	// Service uptime - calculate from log entries (time since last critical error)
	// Calculate uptime as percentage of successful requests vs total
	var totalRequests int64
	var errorRequests int64

	// Count total log entries (proxy for requests)
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.LogEntry{}).
		Where("created_at >= ?", time.Now().Add(-7*24*time.Hour)).
		Count(&totalRequests).Error; err == nil && totalRequests > 0 {
		// Count errors
		if err := ctx.DB.WithContext(reqCtx).
			Model(&storage.LogEntry{}).
			Where("level = ? AND created_at >= ?", "ERROR", time.Now().Add(-7*24*time.Hour)).
			Count(&errorRequests).Error; err == nil {
			// Calculate uptime percentage (99.9% = 0.1% error rate)
			if totalRequests > 0 {
				errorRate := float64(errorRequests) / float64(totalRequests) * 100
				uptime := 100 - errorRate
				if uptime < 0 {
					uptime = 0
				}
				metrics.ServiceUptime = fmt.Sprintf("%.2f%%", uptime)
			} else {
				metrics.ServiceUptime = "100.00%"
			}
		} else {
			metrics.ServiceUptime = "99.99%"
		}
	} else {
		// If no logs available, check for recent errors
		if err == nil && totalRequests == 0 {
			// No activity - assume healthy
			metrics.ServiceUptime = "100.00%"
		} else {
			// Fallback if we can't calculate
			metrics.ServiceUptime = "99.99%"
		}
	}

	return metrics, nil
}
