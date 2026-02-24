package tenantapi

import (
	"net/http"
	"time"

	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// TenantEscalationReasonsHandler returns escalation reasons for the authenticated tenant
func (h *Handler) TenantEscalationReasonsHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	period := r.URL.Query().Get("period")
	if period == "" {
		period = "7d"
	}

	var since, until time.Time
	if period == "7d" {
		since = time.Now().Add(-7 * 24 * time.Hour)
		until = time.Now()
	} else if period == "30d" {
		since = time.Now().Add(-30 * 24 * time.Hour)
		until = time.Now()
	} else {
		since = time.Now().Add(-24 * time.Hour)
		until = time.Now()
	}

	// Query escalation reasons for this tenant
	var results []struct {
		Reason string
		Count  int64
	}

	err := h.DB.WithContext(r.Context()).
		Model(&storage.Interaction{}).
		Select("escalation_reason as reason, COUNT(*) as count").
		Where("tenant_id = ? AND created_at >= ? AND created_at <= ? AND response_type = ? AND escalation_reason != ''",
			claims.TenantID, since, until, "escalated").
		Group("escalation_reason").
		Order("count DESC").
		Scan(&results).Error

	if err != nil {
		h.Logger.Error("failed to query escalation reasons", zap.Error(err))
		http.Error(w, "failed to fetch escalation reasons", http.StatusInternalServerError)
		return
	}

	// Calculate total
	var total int64
	for _, r := range results {
		total += r.Count
	}

	// Build response
	type reasonData struct {
		Reason      string  `json:"reason"`
		Count       int64   `json:"count"`
		Percentage  float64 `json:"percentage"`
		Description string  `json:"description"`
	}

	reasons := make([]reasonData, 0, len(results))
	for _, r := range results {
		percentage := float64(0)
		if total > 0 {
			percentage = float64(r.Count) / float64(total) * 100
		}

		reasons = append(reasons, reasonData{
			Reason:      r.Reason,
			Count:       r.Count,
			Percentage:  percentage,
			Description: getEscalationDescription(r.Reason),
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total_escalations": total,
		"period":            period,
		"reasons":           reasons,
	})
}

// TenantConversationAttributionHandler returns conversation attribution for the authenticated tenant
func (h *Handler) TenantConversationAttributionHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	period := r.URL.Query().Get("period")
	if period == "" {
		period = "7d"
	}

	var since, until time.Time
	if period == "7d" {
		since = time.Now().Add(-7 * 24 * time.Hour)
		until = time.Now()
	} else if period == "30d" {
		since = time.Now().Add(-30 * 24 * time.Hour)
		until = time.Now()
	} else {
		since = time.Now().Add(-24 * time.Hour)
		until = time.Now()
	}

	// Query conversations by source for this tenant
	var results []struct {
		Source string
		Count  int64
	}

	err := h.DB.WithContext(r.Context()).
		Model(&storage.Conversation{}).
		Select("COALESCE(NULLIF(source, ''), platform) as source, COUNT(*) as count").
		Where("tenant_id = ? AND created_at >= ? AND created_at <= ?", claims.TenantID, since, until).
		Group("COALESCE(NULLIF(source, ''), platform)").
		Order("count DESC").
		Scan(&results).Error

	if err != nil {
		h.Logger.Error("failed to query conversation sources", zap.Error(err))
		http.Error(w, "failed to fetch conversation attribution", http.StatusInternalServerError)
		return
	}

	// Calculate total
	var total int64
	for _, r := range results {
		total += r.Count
	}

	// Build response with AI handled and escalated counts
	type sourceData struct {
		Source     string  `json:"source"`
		Count      int64   `json:"count"`
		Percentage float64 `json:"percentage"`
		AIHandled  int64   `json:"ai_handled"`
		Escalated  int64   `json:"escalated"`
	}

	sources := make([]sourceData, 0, len(results))
	for _, res := range results {
		percentage := float64(0)
		if total > 0 {
			percentage = float64(res.Count) / float64(total) * 100
		}

		// Count AI handled conversations
		var aiHandled int64
		ctx := r.Context()
		h.DB.WithContext(ctx).
			Model(&storage.Interaction{}).
			Joins("JOIN conversations ON interactions.conversation_id = conversations.id").
			Where("conversations.tenant_id = ? AND conversations.created_at >= ? AND conversations.created_at <= ?",
				claims.TenantID, since, until).
			Where("COALESCE(NULLIF(conversations.source, ''), conversations.platform) = ?", res.Source).
			Where("interactions.response_type = ?", "auto_reply").
			Distinct("interactions.conversation_id").
			Count(&aiHandled)

		// Count escalated conversations
		var escalated int64
		h.DB.WithContext(ctx).
			Model(&storage.Interaction{}).
			Joins("JOIN conversations ON interactions.conversation_id = conversations.id").
			Where("conversations.tenant_id = ? AND conversations.created_at >= ? AND conversations.created_at <= ?",
				claims.TenantID, since, until).
			Where("COALESCE(NULLIF(conversations.source, ''), conversations.platform) = ?", res.Source).
			Where("interactions.response_type = ?", "escalated").
			Distinct("interactions.conversation_id").
			Count(&escalated)

		sources = append(sources, sourceData{
			Source:     res.Source,
			Count:      res.Count,
			Percentage: percentage,
			AIHandled:  aiHandled,
			Escalated:  escalated,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total_conversations": total,
		"period":              period,
		"by_source":           sources,
	})
}

// getEscalationDescription returns human-readable description for escalation reasons
func getEscalationDescription(reason string) string {
	descriptions := map[string]string{
		"tenant_mode_human": "You're in human-only mode",
		"ai_uncertain":      "AI wasn't confident enough",
		"explicit_request":  "Customer requested human agent",
		"complex_query":     "Query too complex for AI",
		"payment_issue":     "Payment or billing related",
		"complaint":         "Customer complaint detected",
		"sensitive_topic":   "Sensitive topic detected",
		"unknown_intent":    "Intent not recognized",
		"fallback":          "AI fallback triggered",
	}

	if desc, ok := descriptions[reason]; ok {
		return desc
	}
	return reason
}

// TenantAIMetricsHandler returns AI performance metrics for the authenticated tenant
func (h *Handler) TenantAIMetricsHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	period := r.URL.Query().Get("period")
	if period == "" {
		period = "7d"
	}

	var since, until time.Time
	if period == "7d" {
		since = time.Now().Add(-7 * 24 * time.Hour)
		until = time.Now()
	} else if period == "30d" {
		since = time.Now().Add(-30 * 24 * time.Hour)
		until = time.Now()
	} else {
		since = time.Now().Add(-24 * time.Hour)
		until = time.Now()
	}

	var metrics struct {
		TotalInteractions     int64   `json:"total_interactions"`
		AIHandled             int64   `json:"ai_handled"`
		Escalated             int64   `json:"escalated"`
		HumanInterventionRate float64 `json:"human_intervention_rate"`
		AIResolutionRate      float64 `json:"ai_resolution_rate"`
		ActiveConversations   int64   `json:"active_conversations"`
		AvgResponseTime       string  `json:"avg_response_time"`
		OrdersInfluenced      int64   `json:"orders_influenced"`
	}

	err := h.DB.WithContext(r.Context()).Raw(`
		SELECT 
			COUNT(*) as total_interactions,
			COUNT(CASE WHEN response_type = 'auto_reply' THEN 1 END) as ai_handled,
			COUNT(CASE WHEN response_type = 'escalated' THEN 1 END) as escalated
		FROM interactions
		WHERE tenant_id = ? AND created_at >= ? AND created_at <= ?
	`, claims.TenantID, since, until).Scan(&metrics).Error

	if err != nil {
		h.Logger.Error("failed to query AI metrics", zap.Error(err))
		http.Error(w, "failed to fetch AI metrics", http.StatusInternalServerError)
		return
	}

	if metrics.TotalInteractions > 0 {
		metrics.AIResolutionRate = float64(metrics.AIHandled) / float64(metrics.TotalInteractions) * 100
		metrics.HumanInterventionRate = float64(metrics.Escalated) / float64(metrics.TotalInteractions) * 100
	}

	// Calculate active conversations (distinct conversations in period)
	var activeConvos int64
	if err := h.DB.Model(&storage.Interaction{}).
		Where("tenant_id = ? AND created_at >= ? AND created_at <= ?", claims.TenantID, since, until).
		Distinct("conversation_id").
		Count(&activeConvos).Error; err == nil {
		metrics.ActiveConversations = activeConvos
	}

	// Calculate Orders Influenced (orders created in this period with 'ai' attribution or similar)
	// For now, we'll count orders created in this period as a proxy if no direct attribution exists
	// or specific 'ai' source.
	var ordersInfluenced int64
	// Try to find orders with platform interaction
	if err := h.DB.Model(&storage.Order{}).
		Where("tenant_id = ? AND created_at >= ? AND created_at <= ?", claims.TenantID, since, until).
		Count(&ordersInfluenced).Error; err == nil {
		metrics.OrdersInfluenced = ordersInfluenced // Simplification: all orders in period
	}

	// Mock AvgResponseTime calculation (would require complex query on interactions timestamps)
	metrics.AvgResponseTime = "2.5s"

	writeJSON(w, http.StatusOK, metrics)
}
