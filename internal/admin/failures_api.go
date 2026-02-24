package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// FailuresData represents all tracked failures in the system
type FailuresData struct {
	Summary              FailuresSummary       `json:"summary"`
	FailedMessages       []FailedMessage       `json:"failed_messages"`
	FailedPosts          []FailedPost          `json:"failed_posts"`
	FailedPayments       []FailedPayment       `json:"failed_payments"`
	FailedEscalations    []FailedEscalation    `json:"failed_escalations"`
	FailedIntegrations   []FailedIntegration   `json:"failed_integrations"`
	FailedScheduledPosts []FailedScheduledPost `json:"failed_scheduled_posts"`
	FailedWebhooks       []FailedWebhook       `json:"failed_webhooks"`
	FailedTokenRefreshes []FailedTokenRefresh  `json:"failed_token_refreshes"`
	FailedAPIRequests    []FailedAPIRequest    `json:"failed_api_requests"`
}

// FailuresSummary provides aggregated failure statistics
type FailuresSummary struct {
	TotalFailures      int64   `json:"total_failures"`
	FailedMessages     int64   `json:"failed_messages"`
	FailedPosts        int64   `json:"failed_posts"`
	FailedPayments     int64   `json:"failed_payments"`
	FailedEscalations  int64   `json:"failed_escalations"`
	FailedIntegrations int64   `json:"failed_integrations"`
	FailedScheduled    int64   `json:"failed_scheduled"`
	FailedWebhooks     int64   `json:"failed_webhooks"`
	FailedTokens       int64   `json:"failed_tokens"`
	FailedAPIRequests  int64   `json:"failed_api_requests"`
	ErrorRate          float64 `json:"error_rate"` // Percentage
}

// FailedMessage represents a failed message send
type FailedMessage struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	TenantName  string     `json:"tenant_name"`
	Platform    string     `json:"platform"`
	Direction   string     `json:"direction"` // "inbound" or "outbound"
	CustomerID  uuid.UUID  `json:"customer_id"`
	Error       string     `json:"error"`
	FailedAt    time.Time  `json:"failed_at"`
	RetryCount  int        `json:"retry_count"`
	LastRetryAt *time.Time `json:"last_retry_at,omitempty"`
}

// FailedPost represents a failed post
type FailedPost struct {
	ID         uuid.UUID `json:"id"`
	TenantID   uuid.UUID `json:"tenant_id"`
	TenantName string    `json:"tenant_name"`
	Platform   string    `json:"platform"`
	Status     string    `json:"status"`
	Error      string    `json:"error"`
	FailedAt   time.Time `json:"failed_at"`
}

// FailedPayment represents a failed payment
type FailedPayment struct {
	ID         uuid.UUID `json:"id"`
	TenantID   uuid.UUID `json:"tenant_id"`
	TenantName string    `json:"tenant_name"`
	OrderID    uuid.UUID `json:"order_id"`
	Amount     float64   `json:"amount"`
	Currency   string    `json:"currency"`
	Error      string    `json:"error"`
	FailedAt   time.Time `json:"failed_at"`
}

// FailedEscalation represents a failed escalation
type FailedEscalation struct {
	ID            uuid.UUID `json:"id"`
	TenantID      uuid.UUID `json:"tenant_id"`
	TenantName    string    `json:"tenant_name"`
	InteractionID uuid.UUID `json:"interaction_id"`
	Error         string    `json:"error"`
	FailedAt      time.Time `json:"failed_at"`
}

// FailedIntegration represents a failed integration/API call
type FailedIntegration struct {
	ID         uuid.UUID  `json:"id"`
	TenantID   *uuid.UUID `json:"tenant_id,omitempty"`
	TenantName string     `json:"tenant_name,omitempty"`
	Platform   string     `json:"platform"`
	Service    string     `json:"service"` // "meta", "whatsapp", "instagram", etc.
	Error      string     `json:"error"`
	FailedAt   time.Time  `json:"failed_at"`
	RetryCount int        `json:"retry_count"`
}

// FailedScheduledPost represents a failed scheduled post
type FailedScheduledPost struct {
	ID          uuid.UUID `json:"id"`
	TenantID    uuid.UUID `json:"tenant_id"`
	TenantName  string    `json:"tenant_name"`
	Platform    string    `json:"platform"`
	ScheduledAt time.Time `json:"scheduled_at"`
	Error       string    `json:"error"`
	FailedAt    time.Time `json:"failed_at"`
}

// FailedWebhook represents a failed webhook delivery
type FailedWebhook struct {
	ID         uuid.UUID  `json:"id"`
	TenantID   *uuid.UUID `json:"tenant_id,omitempty"`
	TenantName string     `json:"tenant_name,omitempty"`
	Platform   string     `json:"platform"`
	EventType  string     `json:"event_type"`
	Error      string     `json:"error"`
	FailedAt   time.Time  `json:"failed_at"`
	RetryCount int        `json:"retry_count"`
}

// FailedTokenRefresh represents a failed token refresh
type FailedTokenRefresh struct {
	ID         uuid.UUID  `json:"id"`
	TenantID   uuid.UUID  `json:"tenant_id"`
	TenantName string     `json:"tenant_name"`
	Platform   string     `json:"platform"`
	Error      string     `json:"error"`
	FailedAt   time.Time  `json:"failed_at"`
	NextRetry  *time.Time `json:"next_retry,omitempty"`
}

// FailedAPIRequest represents a failed API request
type FailedAPIRequest struct {
	ID         uuid.UUID  `json:"id"`
	TenantID   *uuid.UUID `json:"tenant_id,omitempty"`
	TenantName string     `json:"tenant_name,omitempty"`
	Service    string     `json:"service"`
	Endpoint   string     `json:"endpoint"`
	Method     string     `json:"method"`
	StatusCode int        `json:"status_code"`
	Error      string     `json:"error"`
	FailedAt   time.Time  `json:"failed_at"`
}

// FailuresAPIHandler returns all failures in the system as JSON
func FailuresAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		// Get period filter (default: 24h)
		period := r.URL.Query().Get("period")
		if period == "" {
			period = "24h"
		}
		var since time.Time
		if period == "7d" {
			since = time.Now().Add(-7 * 24 * time.Hour)
		} else {
			since = time.Now().Add(-24 * time.Hour)
		}

		// Get failure type filter (optional)
		failureType := r.URL.Query().Get("type") // messages, posts, payments, etc.

		data, err := BuildFailuresData(ctx, since, failureType, reqCtx)
		if err != nil {
			ctx.Logger.Error("failed to build failures data", zap.Error(err))
			http.Error(w, "failed to build failures data", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(data); err != nil {
			ctx.Logger.Error("failed to encode failures data", zap.Error(err))
			http.Error(w, "encoding error", http.StatusInternalServerError)
			return
		}
	}
}

// BuildFailuresData builds comprehensive failure data
func BuildFailuresData(ctx *AdminContext, since time.Time, failureType string, reqCtx context.Context) (*FailuresData, error) {
	data := &FailuresData{
		FailedMessages:       []FailedMessage{},
		FailedPosts:          []FailedPost{},
		FailedPayments:       []FailedPayment{},
		FailedEscalations:    []FailedEscalation{},
		FailedIntegrations:   []FailedIntegration{},
		FailedScheduledPosts: []FailedScheduledPost{},
		FailedWebhooks:       []FailedWebhook{},
		FailedTokenRefreshes: []FailedTokenRefresh{},
		FailedAPIRequests:    []FailedAPIRequest{},
	}

	// Build summary and populate all failure types (or just the requested type)
	summary, err := buildFailuresSummary(ctx, since, reqCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to build failures summary: %w", err)
	}
	data.Summary = summary

	// If specific type requested, only fetch that type
	if failureType != "" {
		switch failureType {
		case "messages":
			data.FailedMessages, _ = buildFailedMessages(ctx, since, reqCtx)
		case "posts":
			data.FailedPosts, _ = buildFailedPosts(ctx, since, reqCtx)
		case "payments":
			data.FailedPayments, _ = buildFailedPayments(ctx, since, reqCtx)
		case "escalations":
			data.FailedEscalations, _ = buildFailedEscalations(ctx, since, reqCtx)
		case "integrations":
			data.FailedIntegrations, _ = buildFailedIntegrations(ctx, since, reqCtx)
		case "scheduled":
			data.FailedScheduledPosts, _ = buildFailedScheduledPosts(ctx, since, reqCtx)
		case "webhooks":
			data.FailedWebhooks, _ = buildFailedWebhooks(ctx, since, reqCtx)
		case "tokens":
			data.FailedTokenRefreshes, _ = buildFailedTokenRefreshes(ctx, since, reqCtx)
		case "api":
			data.FailedAPIRequests, _ = buildFailedAPIRequests(ctx, since, reqCtx)
		}
	} else {
		// Fetch all failure types
		data.FailedMessages, _ = buildFailedMessages(ctx, since, reqCtx)
		data.FailedPosts, _ = buildFailedPosts(ctx, since, reqCtx)
		data.FailedPayments, _ = buildFailedPayments(ctx, since, reqCtx)
		data.FailedEscalations, _ = buildFailedEscalations(ctx, since, reqCtx)
		data.FailedIntegrations, _ = buildFailedIntegrations(ctx, since, reqCtx)
		data.FailedScheduledPosts, _ = buildFailedScheduledPosts(ctx, since, reqCtx)
		data.FailedWebhooks, _ = buildFailedWebhooks(ctx, since, reqCtx)
		data.FailedTokenRefreshes, _ = buildFailedTokenRefreshes(ctx, since, reqCtx)
		data.FailedAPIRequests, _ = buildFailedAPIRequests(ctx, since, reqCtx)
	}

	return data, nil
}

// buildFailuresSummary builds the summary statistics
func buildFailuresSummary(ctx *AdminContext, since time.Time, reqCtx context.Context) (FailuresSummary, error) {
	summary := FailuresSummary{}

	// Failed messages (interactions with failed status)
	var failedMessagesCount int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.Interaction{}).
		Where("created_at >= ? AND direction = ? AND response_status IN ?",
			since, "outbound", []string{"failed", "error"}).
		Count(&failedMessagesCount).Error; err == nil {
		summary.FailedMessages = failedMessagesCount
	}

	// Failed posts (scheduled posts with failed status)
	var failedPostsCount int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.ScheduledPost{}).
		Where("created_at >= ? AND status IN ?", since, []string{"failed", "error"}).
		Count(&failedPostsCount).Error; err == nil {
		summary.FailedPosts = failedPostsCount
	}

	// Failed payments - only tenant-to-Brancr payments (subscriptions), NOT customer payments
	// TODO: Track tenant subscription payment failures separately when subscription system is implemented
	// For now, check admin audit logs for payment-related failures
	var failedPaymentsCount int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.AdminAuditLog{}).
		Where("created_at >= ? AND action LIKE ? AND (status_code >= ? OR success = ?)",
			since, "%payment%", 400, false).
		Count(&failedPaymentsCount).Error; err == nil {
		summary.FailedPayments = failedPaymentsCount
	}

	// Failed escalations (interactions that failed to escalate)
	var failedEscalationsCount int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.LogEntry{}).
		Where("created_at >= ? AND level = ? AND message ILIKE ?",
			since, "ERROR", "%failed to escalate%").
		Count(&failedEscalationsCount).Error; err == nil {
		summary.FailedEscalations = failedEscalationsCount
	}

	// Failed integrations (from log entries)
	var failedIntegrationsCount int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.LogEntry{}).
		Where("created_at >= ? AND level = ? AND (source ILIKE ? OR message ILIKE ?)",
			since, "ERROR", "%api%", "%integration%").
		Count(&failedIntegrationsCount).Error; err == nil {
		summary.FailedIntegrations = failedIntegrationsCount
	}

	// Failed scheduled posts (already counted above, but also check post_logs)
	var failedScheduledCount int64
	if err := ctx.DB.WithContext(reqCtx).
		Table("post_logs").
		Where("created_at >= ? AND status = ?", since, "failed").
		Count(&failedScheduledCount).Error; err == nil {
		summary.FailedScheduled = failedScheduledCount + failedPostsCount // Combine both sources
	}

	// Failed webhooks (from log entries)
	var failedWebhooksCount int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.LogEntry{}).
		Where("created_at >= ? AND level = ? AND (source ILIKE ? OR message ILIKE ?)",
			since, "ERROR", "%webhook%", "%webhook%").
		Count(&failedWebhooksCount).Error; err == nil {
		summary.FailedWebhooks = failedWebhooksCount
	}

	// Failed token refreshes (from log entries)
	var failedTokensCount int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.LogEntry{}).
		Where("created_at >= ? AND level = ? AND message ILIKE ?",
			since, "ERROR", "%token refresh%").
		Count(&failedTokensCount).Error; err == nil {
		summary.FailedTokens = failedTokensCount
	}

	// Failed API requests (from admin audit logs)
	var failedAPIRequestsCount int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.AdminAuditLog{}).
		Where("created_at >= ? AND status_code >= ? AND status_code < ?",
			since, 400, 600).
		Count(&failedAPIRequestsCount).Error; err == nil {
		summary.FailedAPIRequests = failedAPIRequestsCount
	}

	// Calculate total
	summary.TotalFailures = failedMessagesCount + failedPostsCount + failedPaymentsCount +
		failedEscalationsCount + failedIntegrationsCount + failedScheduledCount +
		failedWebhooksCount + failedTokensCount + failedAPIRequestsCount

	// Calculate error rate (total requests vs failures)
	var totalRequests int64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.Interaction{}).
		Where("created_at >= ?", since).
		Count(&totalRequests).Error; err == nil && totalRequests > 0 {
		summary.ErrorRate = float64(summary.TotalFailures) / float64(totalRequests) * 100
	}

	return summary, nil
}

// buildFailedMessages builds list of failed messages
func buildFailedMessages(ctx *AdminContext, since time.Time, reqCtx context.Context) ([]FailedMessage, error) {
	var interactions []storage.Interaction
	if err := ctx.DB.WithContext(reqCtx).
		Where("created_at >= ? AND direction = ? AND response_status IN ?",
			since, "outbound", []string{"failed", "error"}).
		Order("created_at DESC").
		Limit(100).
		Find(&interactions).Error; err != nil {
		return nil, err
	}

	// Get tenant names
	tenantMap := make(map[uuid.UUID]string)
	var tenantIDs []uuid.UUID
	for _, i := range interactions {
		if _, ok := tenantMap[i.TenantID]; !ok {
			tenantIDs = append(tenantIDs, i.TenantID)
			tenantMap[i.TenantID] = "" // Placeholder to avoid duplicates
		}
	}
	if len(tenantIDs) > 0 {
		var tenants []storage.Tenant
		ctx.DB.Where("id IN ?", tenantIDs).Find(&tenants)
		for _, t := range tenants {
			tenantMap[t.ID] = t.Name
		}
	}

	failed := make([]FailedMessage, 0, len(interactions))
	for _, i := range interactions {
		// Extract detailed error information from metadata
		errorMsg := "Unknown error"
		retryCount := 0
		var lastRetryAt *time.Time

		if i.Metadata != "" {
			var meta map[string]interface{}
			if json.Unmarshal([]byte(i.Metadata), &meta) == nil {
				// Extract error message
				if e, ok := meta["error"].(string); ok {
					errorMsg = e
				} else if e, ok := meta["last_error"].(string); ok {
					errorMsg = e
				} else if e, ok := meta["failure_reason"].(string); ok {
					errorMsg = e
				}

				// Extract retry information
				if rc, ok := meta["retry_count"].(float64); ok {
					retryCount = int(rc)
				}
				if lra, ok := meta["last_retry_at"].(string); ok {
					if parsed, err := time.Parse(time.RFC3339, lra); err == nil {
						lastRetryAt = &parsed
					}
				}

				// Add context about where failure originated
				if source, ok := meta["source"].(string); ok && source != "" {
					errorMsg = fmt.Sprintf("[%s] %s", source, errorMsg)
				}
			}
		}

		// Add response_status to error if available
		if i.ResponseStatus != "" && i.ResponseStatus != "failed" && i.ResponseStatus != "error" {
			errorMsg = fmt.Sprintf("%s (Status: %s)", errorMsg, i.ResponseStatus)
		}

		failed = append(failed, FailedMessage{
			ID:          i.ID,
			TenantID:    i.TenantID,
			TenantName:  tenantMap[i.TenantID],
			Platform:    i.Platform,
			Direction:   i.Direction,
			CustomerID:  i.CustomerID,
			Error:       errorMsg,
			FailedAt:    i.CreatedAt,
			RetryCount:  retryCount,
			LastRetryAt: lastRetryAt,
		})
	}

	return failed, nil
}

// buildFailedPosts builds list of failed posts
func buildFailedPosts(ctx *AdminContext, since time.Time, reqCtx context.Context) ([]FailedPost, error) {
	var posts []storage.ScheduledPost
	if err := ctx.DB.WithContext(reqCtx).
		Where("created_at >= ? AND status IN ?", since, []string{"failed", "error"}).
		Order("created_at DESC").
		Limit(100).
		Find(&posts).Error; err != nil {
		return nil, err
	}

	// Get tenant names
	tenantMap := make(map[uuid.UUID]string)
	var tenantIDs []uuid.UUID
	for _, p := range posts {
		if _, ok := tenantMap[p.TenantID]; !ok {
			tenantIDs = append(tenantIDs, p.TenantID)
			tenantMap[p.TenantID] = ""
		}
	}
	if len(tenantIDs) > 0 {
		var tenants []storage.Tenant
		ctx.DB.Where("id IN ?", tenantIDs).Find(&tenants)
		for _, t := range tenants {
			tenantMap[t.ID] = t.Name
		}
	}

	failed := make([]FailedPost, 0, len(posts))
	for _, p := range posts {
		errorMsg := "Unknown error"
		if p.LastError != "" {
			errorMsg = p.LastError
		} else if p.Status != "" {
			// Use status as context
			errorMsg = fmt.Sprintf("Post failed with status: %s", p.Status)
		}

		// Extract platform from Platforms field (JSON array stored as text)
		platform := "unknown"
		if p.Platforms != "" {
			// Platforms is stored as JSON array string, e.g., ["instagram","facebook"]
			var platforms []string
			if json.Unmarshal([]byte(p.Platforms), &platforms) == nil && len(platforms) > 0 {
				platform = platforms[0] // Use first platform
				if len(platforms) > 1 {
					platform += fmt.Sprintf(" (+%d more)", len(platforms)-1)
				}
			}
		}

		// Add context about scheduled time vs failure time
		if p.ScheduledAt != nil && !p.ScheduledAt.IsZero() && p.CreatedAt.After(*p.ScheduledAt) {
			delay := p.CreatedAt.Sub(*p.ScheduledAt)
			errorMsg = fmt.Sprintf("%s (Failed %v after scheduled time)", errorMsg, delay.Round(time.Minute))
		}

		failed = append(failed, FailedPost{
			ID:         p.ID,
			TenantID:   p.TenantID,
			TenantName: tenantMap[p.TenantID],
			Platform:   platform,
			Status:     p.Status,
			Error:      errorMsg,
			FailedAt:   p.CreatedAt,
		})
	}

	return failed, nil
}

// buildFailedPayments builds list of failed tenant-to-Brancr payments (subscriptions)
// NOTE: This is for tenant subscription payments, NOT customer payments
func buildFailedPayments(ctx *AdminContext, since time.Time, reqCtx context.Context) ([]FailedPayment, error) {
	// TODO: When tenant subscription/payment tracking is implemented, query that here
	// For now, query from admin audit logs for payment-related failures
	var auditLogs []storage.AdminAuditLog
	if err := ctx.DB.WithContext(reqCtx).
		Where("created_at >= ? AND action LIKE ? AND (status_code >= ? OR success = ?)",
			since, "%payment%", 400, false).
		Order("created_at DESC").
		Limit(100).
		Find(&auditLogs).Error; err != nil {
		return nil, err
	}

	// Get tenant names
	tenantMap := make(map[uuid.UUID]string)
	var tenantIDs []uuid.UUID
	for _, log := range auditLogs {
		if log.TenantID != nil {
			if _, ok := tenantMap[*log.TenantID]; !ok {
				tenantIDs = append(tenantIDs, *log.TenantID)
				tenantMap[*log.TenantID] = ""
			}
		}
	}
	if len(tenantIDs) > 0 {
		var tenants []storage.Tenant
		ctx.DB.Where("id IN ?", tenantIDs).Find(&tenants)
		for _, t := range tenants {
			tenantMap[t.ID] = t.Name
		}
	}

	failed := make([]FailedPayment, 0, len(auditLogs))
	for _, log := range auditLogs {
		if log.TenantID == nil {
			continue // Skip non-tenant payment failures
		}

		errorMsg := fmt.Sprintf("Payment failed (HTTP %d)", log.StatusCode)
		if log.Metadata != "" {
			var meta map[string]interface{}
			if json.Unmarshal([]byte(log.Metadata), &meta) == nil {
				if e, ok := meta["error"].(string); ok {
					errorMsg = e
				}
			}
		}

		failed = append(failed, FailedPayment{
			ID:         log.ID,
			TenantID:   *log.TenantID,
			TenantName: tenantMap[*log.TenantID],
			OrderID:    uuid.Nil, // Not applicable for tenant subscriptions
			Amount:     0,        // Would need to extract from metadata
			Currency:   "USD",
			Error:      errorMsg,
			FailedAt:   log.CreatedAt,
		})
	}

	return failed, nil
}

// buildFailedEscalations builds list of failed escalations
func buildFailedEscalations(ctx *AdminContext, since time.Time, reqCtx context.Context) ([]FailedEscalation, error) {
	var logs []storage.LogEntry
	if err := ctx.DB.WithContext(reqCtx).
		Where("created_at >= ? AND level = ? AND message ILIKE ?",
			since, "ERROR", "%failed to escalate%").
		Order("created_at DESC").
		Limit(100).
		Find(&logs).Error; err != nil {
		return nil, err
	}

	// Build tenant map from log entries - LogEntry uses Meta field (not Metadata)
	tenantMap := make(map[uuid.UUID]string)
	var tenantIDs []uuid.UUID

	for _, log := range logs {
		// Try to extract tenant ID from Meta field (JSON-encoded details)
		if log.Meta != "" {
			var meta map[string]interface{}
			if json.Unmarshal([]byte(log.Meta), &meta) == nil {
				if tidStr, ok := meta["tenant_id"].(string); ok {
					if tID, err := uuid.Parse(tidStr); err == nil && tID != uuid.Nil {
						if _, exists := tenantMap[tID]; !exists {
							tenantIDs = append(tenantIDs, tID)
							tenantMap[tID] = "" // Will be filled below
						}
					}
				}
			}
		}
	}

	if len(tenantIDs) > 0 {
		var tenants []storage.Tenant
		ctx.DB.WithContext(reqCtx).Where("id IN ?", tenantIDs).Find(&tenants)
		for _, t := range tenants {
			tenantMap[t.ID] = t.Name
		}
	}

	failed := make([]FailedEscalation, 0, len(logs))
	for _, log := range logs {
		// Try to extract interaction_id and tenant_id from Meta field
		interactionID := uuid.Nil
		tenantID := uuid.Nil

		if log.Meta != "" {
			var meta map[string]interface{}
			if json.Unmarshal([]byte(log.Meta), &meta) == nil {
				if iidStr, ok := meta["interaction_id"].(string); ok {
					interactionID, _ = uuid.Parse(iidStr)
				}
				if tidStr, ok := meta["tenant_id"].(string); ok {
					tenantID, _ = uuid.Parse(tidStr)
				}
			}
		}

		// Extract more context from error message
		errorMsg := log.Message
		if log.Source != "" {
			errorMsg = fmt.Sprintf("[Source: %s] %s", log.Source, errorMsg)
		}

		failed = append(failed, FailedEscalation{
			ID:            log.ID,
			TenantID:      tenantID,
			TenantName:    tenantMap[tenantID],
			InteractionID: interactionID,
			Error:         errorMsg,
			FailedAt:      log.CreatedAt,
		})
	}

	return failed, nil
}

// buildFailedIntegrations, buildFailedScheduledPosts, buildFailedWebhooks, etc.
// These would follow similar patterns - querying relevant tables and log entries

func buildFailedIntegrations(ctx *AdminContext, since time.Time, reqCtx context.Context) ([]FailedIntegration, error) {
	// Implementation similar to above
	return []FailedIntegration{}, nil
}

func buildFailedScheduledPosts(ctx *AdminContext, since time.Time, reqCtx context.Context) ([]FailedScheduledPost, error) {
	// Implementation similar to above - can reuse buildFailedPosts logic
	return []FailedScheduledPost{}, nil
}

func buildFailedWebhooks(ctx *AdminContext, since time.Time, reqCtx context.Context) ([]FailedWebhook, error) {
	// Implementation similar to above
	return []FailedWebhook{}, nil
}

func buildFailedTokenRefreshes(ctx *AdminContext, since time.Time, reqCtx context.Context) ([]FailedTokenRefresh, error) {
	// Implementation similar to above
	return []FailedTokenRefresh{}, nil
}

func buildFailedAPIRequests(ctx *AdminContext, since time.Time, reqCtx context.Context) ([]FailedAPIRequest, error) {
	var auditLogs []storage.AdminAuditLog
	if err := ctx.DB.WithContext(reqCtx).
		Where("created_at >= ? AND status_code >= ? AND status_code < ?",
			since, 400, 600).
		Order("created_at DESC").
		Limit(100).
		Find(&auditLogs).Error; err != nil {
		return nil, err
	}

	failed := make([]FailedAPIRequest, 0, len(auditLogs))
	for _, log := range auditLogs {
		failed = append(failed, FailedAPIRequest{
			ID:         log.ID,
			TenantID:   log.TenantID,
			Service:    "admin",
			Endpoint:   log.Path,
			Method:     log.Method,
			StatusCode: log.StatusCode,
			Error:      fmt.Sprintf("HTTP %d", log.StatusCode),
			FailedAt:   log.CreatedAt,
		})
	}

	return failed, nil
}
