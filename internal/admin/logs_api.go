package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// LogsResponse represents logs list response
type LogsResponse struct {
	Total int64            `json:"total"`
	Items []LogEntryDetail `json:"items"`
}

// LogEntryDetail represents a log entry
type LogEntryDetail struct {
	ID        uuid.UUID  `json:"id"`
	Level     string     `json:"level"` // INFO, WARN, ERROR
	Message   string     `json:"message"`
	Source    string     `json:"source,omitempty"`
	TenantID  *uuid.UUID `json:"tenant_id,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// LogsAPIHandler returns system logs
func LogsAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		// Get filters
		level := strings.TrimSpace(r.URL.Query().Get("level"))
		tenantIDStr := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
		limitStr := strings.TrimSpace(r.URL.Query().Get("limit"))
		limit := 200
		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
				limit = l
			}
		}

		// Date filtering
		var since, until *time.Time
		startDateStr := r.URL.Query().Get("start_date")
		endDateStr := r.URL.Query().Get("end_date")

		if startDateStr != "" && endDateStr != "" {
			s, err1 := time.Parse("2006-01-02", startDateStr)
			e, err2 := time.Parse("2006-01-02", endDateStr)
			if err1 == nil && err2 == nil {
				since = &s
				e = e.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
				until = &e
			}
		}

		data, err := BuildLogsData(ctx, level, tenantIDStr, limit, since, until, reqCtx)
		if err != nil {
			ctx.Logger.Error("failed to build logs data", zap.Error(err))
			http.Error(w, "failed to build logs data", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(data); err != nil {
			ctx.Logger.Error("failed to encode logs data", zap.Error(err))
			http.Error(w, "encoding error", http.StatusInternalServerError)
			return
		}
	}
}

// BuildLogsData builds logs list
func BuildLogsData(ctx *AdminContext, level, tenantIDStr string, limit int, since, until *time.Time, reqCtx context.Context) (*LogsResponse, error) {
	query := ctx.DB.WithContext(reqCtx).Model(&storage.LogEntry{})

	// Apply filters
	if level != "" {
		query = query.Where("level = ?", strings.ToUpper(level))
	}
	if tenantIDStr != "" {
		tenantID, err := uuid.Parse(tenantIDStr)
		if err == nil {
			query = query.Where("tenant_id = ?", tenantID)
		}
	}
	if since != nil && until != nil {
		query = query.Where("created_at >= ? AND created_at <= ?", *since, *until)
	}

	// Count total
	var total int64
	query.Count(&total)

	// Get logs
	var logs []storage.LogEntry
	if err := query.Order("created_at DESC").
		Limit(limit).
		Find(&logs).Error; err != nil {
		return nil, fmt.Errorf("failed to query logs: %w", err)
	}

	items := make([]LogEntryDetail, 0, len(logs))
	for _, log := range logs {
		items = append(items, LogEntryDetail{
			ID:        log.ID,
			Level:     log.Level,
			Message:   log.Message,
			Source:    log.Source,
			TenantID:  log.TenantID,
			CreatedAt: log.CreatedAt,
		})
	}

	return &LogsResponse{
		Total: total,
		Items: items,
	}, nil
}

// HealthAPIHandler returns system health status (uses existing HealthJSONHandler logic)
func HealthAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return HealthJSONHandler(ctx)
}
