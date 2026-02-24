package admin

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// ConversationSource represents attribution data for a single source
type ConversationSource struct {
	Source     string  `json:"source"`
	Count      int64   `json:"count"`
	Percentage float64 `json:"percentage"`
	AIHandled  int64   `json:"ai_handled"`
	Escalated  int64   `json:"escalated"`
}

// ConversationAttributionData represents the response for conversation attribution endpoint
type ConversationAttributionData struct {
	TotalConversations int64                `json:"total_conversations"`
	Period             string               `json:"period"`
	BySources          []ConversationSource `json:"by_source"`
	TopSource          string               `json:"top_source"`
}

// ConversationAttributionHandler returns conversation attribution breakdown
func ConversationAttributionHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		period := r.URL.Query().Get("period")
		if period == "" {
			period = "7d"
		}

		customStart, customEnd := parseCustomDates(r)
		data, err := BuildConversationAttributionData(ctx, period, customStart, customEnd, r.Context())
		if err != nil {
			ctx.Logger.Error("failed to build conversation attribution data", zap.Error(err))
			http.Error(w, "failed to fetch conversation attribution", http.StatusInternalServerError)
			return
		}

		respondJSON(w, data)
	}
}

// BuildConversationAttributionData builds conversation attribution breakdown
func BuildConversationAttributionData(ctx *AdminContext, period string, customStart, customEnd *time.Time, reqCtx context.Context) (*ConversationAttributionData, error) {
	var since, until time.Time

	if period == "custom" && customStart != nil && customEnd != nil {
		since = *customStart
		until = *customEnd
		if since.After(until) {
			since, until = until, since
		}
	} else if period == "7d" {
		since = time.Now().Add(-7 * 24 * time.Hour)
		until = time.Now()
	} else if period == "30d" {
		since = time.Now().Add(-30 * 24 * time.Hour)
		until = time.Now()
	} else {
		since = time.Now().Add(-24 * time.Hour)
		until = time.Now()
	}

	// Query conversations grouped by source
	var results []struct {
		Source string
		Count  int64
	}

	// Use COALESCE to handle NULL sources and use platform as fallback
	err := ctx.DB.WithContext(reqCtx).
		Model(&storage.Conversation{}).
		Select("COALESCE(NULLIF(source, ''), platform) as source, COUNT(*) as count").
		Where("created_at >= ? AND created_at <= ?", since, until).
		Group("COALESCE(NULLIF(source, ''), platform)").
		Order("count DESC").
		Scan(&results).Error

	if err != nil {
		return nil, fmt.Errorf("failed to query conversation sources: %w", err)
	}

	// Calculate total conversations
	var totalConversations int64
	for _, r := range results {
		totalConversations += r.Count
	}

	// Build response with AI handled and escalated counts
	sources := make([]ConversationSource, 0, len(results))
	var topSource string

	for i, r := range results {
		percentage := float64(0)
		if totalConversations > 0 {
			percentage = float64(r.Count) / float64(totalConversations) * 100
		}

		if i == 0 {
			topSource = r.Source
		}

		// Count AI handled conversations (interactions with auto_reply)
		var aiHandled int64
		ctx.DB.WithContext(reqCtx).
			Model(&storage.Interaction{}).
			Joins("JOIN conversations ON interactions.conversation_id = conversations.id").
			Where("conversations.created_at >= ? AND conversations.created_at <= ?", since, until).
			Where("COALESCE(NULLIF(conversations.source, ''), conversations.platform) = ?", r.Source).
			Where("interactions.response_type = ?", "auto_reply").
			Distinct("interactions.conversation_id").
			Count(&aiHandled)

		// Count escalated conversations
		var escalated int64
		ctx.DB.WithContext(reqCtx).
			Model(&storage.Interaction{}).
			Joins("JOIN conversations ON interactions.conversation_id = conversations.id").
			Where("conversations.created_at >= ? AND conversations.created_at <= ?", since, until).
			Where("COALESCE(NULLIF(conversations.source, ''), conversations.platform) = ?", r.Source).
			Where("interactions.response_type = ?", "escalated").
			Distinct("interactions.conversation_id").
			Count(&escalated)

		sources = append(sources, ConversationSource{
			Source:     r.Source,
			Count:      r.Count,
			Percentage: percentage,
			AIHandled:  aiHandled,
			Escalated:  escalated,
		})
	}

	return &ConversationAttributionData{
		TotalConversations: totalConversations,
		Period:             period,
		BySources:          sources,
		TopSource:          topSource,
	}, nil
}
