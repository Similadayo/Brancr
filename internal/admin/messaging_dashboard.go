package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// MessagingActivityData represents Section 4: Messaging Activity (Core Value)
type MessagingActivityData struct {
	TotalConversations  int64           `json:"total_conversations"`
	MessagesByAI        int64           `json:"messages_by_ai"`
	MessagesEscalated   int64           `json:"messages_escalated"`
	MessagesPerPlatform []PlatformCount `json:"messages_per_platform"`
	PeakHours           []HourVolume    `json:"peak_hours"`
}

// PlatformCount represents message count per platform
type PlatformCount struct {
	Platform string `json:"platform"`
	Count    int64  `json:"count"`
}

// HourVolume represents message volume by hour of day
type HourVolume struct {
	Hour  int   `json:"hour"` // 0-23
	Count int64 `json:"count"`
}

// BuildMessagingActivityData builds messaging activity metrics for the dashboard
func BuildMessagingActivityData(ctx *AdminContext, period string, customStart, customEnd *time.Time, reqCtx context.Context) (*MessagingActivityData, error) {
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
		// Default 24h
		since = time.Now().Add(-24 * time.Hour)
	}

	data := &MessagingActivityData{
		MessagesPerPlatform: []PlatformCount{},
		PeakHours:           []HourVolume{},
	}

	// Total conversations (unique conversation IDs)
	var totalConversations int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.Interaction{}).
		Distinct("conversation_id").
		Where("created_at >= ? AND created_at <= ?", since, unti).
		Count(&totalConversations).Error; err != nil {
		return nil, fmt.Errorf("failed to count conversations: %w", err)
	}
	data.TotalConversations = totalConversations

	// Messages by AI
	var messagesByAI int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.Interaction{}).
		Where("created_at >= ? AND response_type = ?", since, "auto_reply").
		Count(&messagesByAI).Error; err != nil {
		return nil, fmt.Errorf("failed to count AI messages: %w", err)
	}
	data.MessagesByAI = messagesByAI

	// Messages escalated - count from interactions table (most accurate)
	var messagesEscalated int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.Interaction{}).
		Where("created_at >= ? AND response_type = ?", since, "escalated").
		Count(&messagesEscalated).Error; err != nil {
		ctx.Logger.Debug("failed to count escalated messages", zap.Error(err))
		// Don't set to 0 on error, keep previous value (0 if first call)
	}
	data.MessagesEscalated = messagesEscalated

	// Messages per platform
	var platformCounts []struct {
		Platform string
		Count    int64
	}
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.Interaction{}).
		Select("platform, COUNT(*) as count").
		Where("created_at >= ?", since).
		Group("platform").
		Scan(&platformCounts).Error; err != nil {
		return nil, fmt.Errorf("failed to get platform counts: %w", err)
	}

	for _, pc := range platformCounts {
		data.MessagesPerPlatform = append(data.MessagesPerPlatform, PlatformCount{
			Platform: pc.Platform,
			Count:    pc.Count,
		})
	}

	// Peak hours - group by hour of day
	var hourVolumes []struct {
		Hour  int
		Count int64
	}
	if err := ctx.DB.WithContext(reqCtx).
		Raw(`
			SELECT EXTRACT(HOUR FROM created_at)::int as hour, COUNT(*) as count
			FROM interactions
			WHERE created_at >= ?
			GROUP BY hour
			ORDER BY hour
		`, since).
		Scan(&hourVolumes).Error; err != nil {
		return nil, fmt.Errorf("failed to get peak hours: %w", err)
	}

	for _, hv := range hourVolumes {
		data.PeakHours = append(data.PeakHours, HourVolume{
			Hour:  hv.Hour,
			Count: hv.Count,
		})
	}

	return data, nil
}
