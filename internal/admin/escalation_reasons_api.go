package admin

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// EscalationReason represents a breakdown of escalation reasons
type EscalationReason struct {
	Reason      string  `json:"reason"`
	Count       int64   `json:"count"`
	Percentage  float64 `json:"percentage"`
	Description string  `json:"description"`
}

// EscalationReasonsData represents the response for escalation reasons endpoint
type EscalationReasonsData struct {
	TotalEscalations int64              `json:"total_escalations"`
	Period           string             `json:"period"`
	Reasons          []EscalationReason `json:"reasons"`
	TopReason        string             `json:"top_reason"`
}

// EscalationReasonsHandler returns escalation reasons breakdown
func EscalationReasonsHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		period := r.URL.Query().Get("period")
		if period == "" {
			period = "7d"
		}

		customStart, customEnd := parseCustomDates(r)
		data, err := BuildEscalationReasonsData(ctx, period, customStart, customEnd, r.Context())
		if err != nil {
			ctx.Logger.Error("failed to build escalation reasons data", zap.Error(err))
			http.Error(w, "failed to fetch escalation reasons", http.StatusInternalServerError)
			return
		}

		respondJSON(w, data)
	}
}

// BuildEscalationReasonsData builds escalation reasons breakdown
func BuildEscalationReasonsData(ctx *AdminContext, period string, customStart, customEnd *time.Time, reqCtx context.Context) (*EscalationReasonsData, error) {
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

	// Query escalation reasons from interactions table
	var results []struct {
		Reason string
		Count  int64
	}

	err := ctx.DB.WithContext(reqCtx).
		Model(&storage.Interaction{}).
		Select("escalation_reason as reason, COUNT(*) as count").
		Where("created_at >= ? AND created_at <= ? AND response_type = ? AND escalation_reason != ''", since, until, "escalated").
		Group("escalation_reason").
		Order("count DESC").
		Scan(&results).Error

	if err != nil {
		return nil, fmt.Errorf("failed to query escalation reasons: %w", err)
	}

	// Calculate total escalations
	var totalEscalations int64
	for _, r := range results {
		totalEscalations += r.Count
	}

	// Build response
	reasons := make([]EscalationReason, 0, len(results))
	var topReason string

	for i, r := range results {
		percentage := float64(0)
		if totalEscalations > 0 {
			percentage = float64(r.Count) / float64(totalEscalations) * 100
		}

		if i == 0 {
			topReason = r.Reason
		}

		reasons = append(reasons, EscalationReason{
			Reason:      r.Reason,
			Count:       r.Count,
			Percentage:  percentage,
			Description: getEscalationReasonDescription(r.Reason),
		})
	}

	return &EscalationReasonsData{
		TotalEscalations: totalEscalations,
		Period:           period,
		Reasons:          reasons,
		TopReason:        topReason,
	}, nil
}

// getEscalationReasonDescription returns a human-readable description for escalation reasons
func getEscalationReasonDescription(reason string) string {
	descriptions := map[string]string{
		"tenant_mode_human":   "Tenant is in human-only mode",
		"ai_uncertain":        "AI confidence below threshold",
		"explicit_request":    "Customer requested human agent",
		"complex_query":       "Query too complex for AI",
		"payment_issue":       "Payment or billing related",
		"complaint":           "Customer complaint detected",
		"sensitive_topic":     "Sensitive topic detected",
		"unknown_intent":      "Intent not recognized",
		"fallback":            "AI fallback triggered",
		"manual_escalation":   "Manually escalated by admin",
		"policy_violation":    "Potential policy violation",
		"high_value_customer": "High-value customer interaction",
	}

	if desc, ok := descriptions[reason]; ok {
		return desc
	}
	return reason // Return the reason itself if no description found
}

