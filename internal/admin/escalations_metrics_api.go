package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// EscalationMetricsResponse represents escalation metrics per tenant
type EscalationMetricsResponse struct {
	TotalTenants int64                     `json:"total_tenants"`
	Metrics      []TenantEscalationMetrics `json:"metrics"`
}

// TenantEscalationMetrics represents escalation metrics for a single tenant
type TenantEscalationMetrics struct {
	TenantID         uuid.UUID  `json:"tenant_id"`
	TenantName       string     `json:"tenant_name"`
	TotalEscalations int64      `json:"total_escalations"`
	PendingCount     int64      `json:"pending_count"`
	ResolvedCount    int64      `json:"resolved_count"`
	FailedCount      int64      `json:"failed_count"`
	SuccessRate      float64    `json:"success_rate"` // Percentage of resolved vs total
	LastEscalationAt *time.Time `json:"last_escalation_at,omitempty"`
}

// EscalationMetricsAPIHandler returns escalation metrics per tenant
func EscalationMetricsAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		period := strings.TrimSpace(r.URL.Query().Get("period"))
		if period == "" {
			period = "30d"
		}

		var since, until time.Time
		now := time.Now()
		until = now // Default until is now

		switch period {
		case "24h":
			since = now.Add(-24 * time.Hour)
		case "7d":
			since = now.Add(-7 * 24 * time.Hour)
		case "30d":
			since = now.Add(-30 * 24 * time.Hour)
		case "90d":
			since = now.Add(-90 * 24 * time.Hour)
		case "custom":
			startDateStr := r.URL.Query().Get("start_date")
			endDateStr := r.URL.Query().Get("end_date")
			if startDateStr != "" && endDateStr != "" {
				s, err1 := time.Parse("2006-01-02", startDateStr)
				e, err2 := time.Parse("2006-01-02", endDateStr)
				if err1 == nil && err2 == nil {
					since = s
					// Set end time to end of day
					until = e.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
				} else {
					// Fallback if parsing fails
					since = now.Add(-30 * 24 * time.Hour)
				}
			} else {
				// Fallback if dates missing
				since = now.Add(-30 * 24 * time.Hour)
			}
		default:
			since = now.Add(-30 * 24 * time.Hour)
		}

		metrics, err := BuildEscalationMetrics(ctx, since, until, reqCtx)
		if err != nil {
			ctx.Logger.Error("failed to build escalation metrics", zap.Error(err))
			http.Error(w, "failed to build escalation metrics", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(metrics); err != nil {
			ctx.Logger.Error("failed to encode escalation metrics", zap.Error(err))
			http.Error(w, "encoding error", http.StatusInternalServerError)
			return
		}
	}
}

// BuildEscalationMetrics builds escalation metrics grouped by tenant
func BuildEscalationMetrics(ctx *AdminContext, since, until time.Time, reqCtx context.Context) (*EscalationMetricsResponse, error) {
	// Get all tenants that have escalations in this period
	var tenantEscalationCounts []struct {
		TenantID uuid.UUID
		Count    int64
	}
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.Interaction{}).
		Select("tenant_id, COUNT(*) as count").
		Where("(response_type = ? OR response_status = ?) AND created_at >= ? AND created_at <= ?", "escalated", "resolved", since, until).
		Group("tenant_id").
		Scan(&tenantEscalationCounts).Error; err != nil {
		return nil, fmt.Errorf("failed to query tenant escalations: %w", err)
	}

	tenantIDs := make([]uuid.UUID, 0, len(tenantEscalationCounts))
	for _, tec := range tenantEscalationCounts {
		tenantIDs = append(tenantIDs, tec.TenantID)
	}

	// Get tenant names
	tenantMap := make(map[uuid.UUID]string)
	if len(tenantIDs) > 0 {
		var tenants []storage.Tenant
		ctx.DB.WithContext(reqCtx).Where("id IN ?", tenantIDs).Find(&tenants)
		for _, t := range tenants {
			tenantMap[t.ID] = t.Name
		}
	}

	// Build metrics for each tenant
	metrics := make([]TenantEscalationMetrics, 0, len(tenantIDs))
	for _, tenantID := range tenantIDs {
		// Total escalations (active escalations OR resolved ones)
		var totalEscalations int64
		ctx.DB.WithContext(reqCtx).
			Model(&storage.Interaction{}).
			Where("tenant_id = ? AND (response_type = ? OR response_status = ?) AND created_at >= ? AND created_at <= ?",
				tenantID, "escalated", "resolved", since, until).
			Count(&totalEscalations)

		// Pending escalations
		var pendingCount int64
		ctx.DB.WithContext(reqCtx).
			Model(&storage.Interaction{}).
			Where("tenant_id = ? AND response_type = ? AND response_status = ? AND created_at >= ? AND created_at <= ?",
				tenantID, "escalated", "pending", since, until).
			Count(&pendingCount)

		// Resolved escalations (identified by status 'resolved', regardless of type 'manual')
		var resolvedCount int64
		ctx.DB.WithContext(reqCtx).
			Model(&storage.Interaction{}).
			Where("tenant_id = ? AND response_status = ? AND created_at >= ? AND created_at <= ?",
				tenantID, "resolved", since, until).
			Count(&resolvedCount)

		// Failed escalations (if status = 'failed' or 'error')
		var failedCount int64
		ctx.DB.WithContext(reqCtx).
			Model(&storage.Interaction{}).
			Where("tenant_id = ? AND response_type = ? AND response_status IN ? AND created_at >= ? AND created_at <= ?",
				tenantID, "escalated", []string{"failed", "error"}, since, until).
			Count(&failedCount)

		// Calculate success rate
		successRate := 0.0
		if totalEscalations > 0 {
			successRate = float64(resolvedCount) / float64(totalEscalations) * 100
		}

		// Get last escalation time
		var lastEscalation storage.Interaction
		var lastEscalationAt *time.Time
		if err := ctx.DB.WithContext(reqCtx).
			Where("tenant_id = ? AND response_type = ? AND created_at >= ? AND created_at <= ?", tenantID, "escalated", since, until).
			Order("created_at DESC").
			First(&lastEscalation).Error; err == nil {
			lastEscalationAt = &lastEscalation.CreatedAt
		}

		metrics = append(metrics, TenantEscalationMetrics{
			TenantID:         tenantID,
			TenantName:       tenantMap[tenantID],
			TotalEscalations: totalEscalations,
			PendingCount:     pendingCount,
			ResolvedCount:    resolvedCount,
			FailedCount:      failedCount,
			SuccessRate:      successRate,
			LastEscalationAt: lastEscalationAt,
		})
	}

	return &EscalationMetricsResponse{
		TotalTenants: int64(len(metrics)),
		Metrics:      metrics,
	}, nil
}
