package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// SendAlertRequest represents a request to send an alert to tenants
type SendAlertRequest struct {
	TenantIDs   []uuid.UUID `json:"tenant_ids"` // Empty = all tenants (or filtered by plan_type/status)
	PlanType    string      `json:"plan_type"`  // Optional: filter by plan type (e.g., "trial", "pro", "enterprise")
	Status      string      `json:"status"`     // Optional: filter by status (e.g., "active", "suspended")
	Type        string      `json:"type"`       // system, billing, quota, security, maintenance, feature
	Severity    string      `json:"severity"`   // info, warning, critical
	Title       string      `json:"title"`
	Message     string      `json:"message"`
	ActionURL   string      `json:"action_url,omitempty"`
	ActionLabel string      `json:"action_label,omitempty"`
}

// SendAlertHandler sends alerts to tenants
func SendAlertHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		var req SendAlertRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Validate
		if req.Title == "" || req.Message == "" {
			http.Error(w, "title and message are required", http.StatusBadRequest)
			return
		}

		if ctx.AlertService == nil {
			http.Error(w, "alert service not available", http.StatusServiceUnavailable)
			return
		}

		validTypes := map[string]bool{
			"system": true, "billing": true, "quota": true,
			"security": true, "maintenance": true, "feature": true,
		}
		if !validTypes[req.Type] {
			req.Type = "system"
		}

		validSeverities := map[string]bool{"info": true, "warning": true, "critical": true}
		if !validSeverities[req.Severity] {
			req.Severity = "info"
		}

		// Get tenant IDs to send to
		var tenantIDs []uuid.UUID
		if len(req.TenantIDs) == 0 {
			// Send to tenants based on filters
			var tenants []storage.Tenant
			query := ctx.DB.WithContext(reqCtx)

			// Apply status filter (only if specified - no default to allow all tenants)
			if req.Status != "" {
				query = query.Where("status = ?", req.Status)
			}
			// Note: If no status specified, all tenants are selected

			// Apply plan type filter if specified
			if req.PlanType != "" {
				query = query.Where("plan_type = ?", req.PlanType)
			}

			query.Find(&tenants)
			for _, t := range tenants {
				tenantIDs = append(tenantIDs, t.ID)
			}
		} else {
			tenantIDs = req.TenantIDs
		}

		// Send alerts
		var sent int
		var failed int
		for _, tenantID := range tenantIDs {
			_, err := ctx.AlertService.SendAlert(
				reqCtx,
				tenantID,
				req.Type,
				req.Severity,
				req.Title,
				req.Message,
				req.ActionURL,
				req.ActionLabel,
			)
			if err != nil {
				ctx.Logger.Error("failed to send alert", zap.Error(err), zap.Stringer("tenant_id", tenantID))
				failed++
			} else {
				sent++
			}
		}

		// Audit log
		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "alert.send",
			Metadata: map[string]interface{}{
				"type":      req.Type,
				"title":     req.Title,
				"plan_type": req.PlanType,
				"status":    req.Status,
				"sent":      sent,
				"failed":    failed,
				"total":     len(tenantIDs),
			},
		}
		if err := auditWriter(reqCtx, ctx, entry); err != nil {
			ctx.Logger.Warn("failed to log audit entry", zap.Error(err))
		}

		respondJSON(w, map[string]interface{}{
			"status": "ok",
			"sent":   sent,
			"failed": failed,
		})
	}
}

// AlertResponse represents an alert in the API response
type AlertResponse struct {
	ID           uuid.UUID `json:"id"`
	TenantID     uuid.UUID `json:"tenant_id"`
	TenantName   string    `json:"tenant_name,omitempty"`
	Type         string    `json:"type"`
	Severity     string    `json:"severity"`
	Title        string    `json:"title"`
	Message      string    `json:"message"`
	ActionURL    string    `json:"action_url,omitempty"`
	ActionLabel  string    `json:"action_label,omitempty"`
	SentEmail    bool      `json:"sent_email"`
	SentTelegram bool      `json:"sent_telegram"`
	SentInApp    bool      `json:"sent_in_app"`
	CreatedAt    string    `json:"created_at"`
	ReadAt       *string   `json:"read_at,omitempty"`
}

// GetAlertsHandler returns alerts for a tenant
func GetAlertsHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		tenantIDStr := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
		var alerts []storage.TenantAlert

		query := ctx.DB.WithContext(reqCtx).Model(&storage.TenantAlert{}).Preload("Tenant")
		if tenantIDStr != "" {
			tenantID, _ := uuid.Parse(tenantIDStr)
			query = query.Where("tenant_id = ?", tenantID)
		}

		query.Order("created_at DESC").Limit(100).Find(&alerts)

		// Transform to response format
		responses := make([]AlertResponse, 0, len(alerts))
		for _, alert := range alerts {
			createdAtStr := ""
			if !alert.CreatedAt.IsZero() {
				createdAtStr = alert.CreatedAt.Format(time.RFC3339)
			}

			var readAtStr *string
			if alert.ReadAt != nil && !alert.ReadAt.IsZero() {
				formatted := alert.ReadAt.Format(time.RFC3339)
				readAtStr = &formatted
			}

			tenantName := ""
			if alert.Tenant.ID != uuid.Nil {
				tenantName = alert.Tenant.Name
			}

			responses = append(responses, AlertResponse{
				ID:           alert.ID,
				TenantID:     alert.TenantID,
				TenantName:   tenantName,
				Type:         alert.Type,
				Severity:     alert.Severity,
				Title:        alert.Title,
				Message:      alert.Message,
				ActionURL:    alert.ActionURL,
				ActionLabel:  alert.ActionLabel,
				SentEmail:    alert.SentEmail,
				SentTelegram: alert.SentTelegram,
				SentInApp:    alert.SentInApp,
				CreatedAt:    createdAtStr,
				ReadAt:       readAtStr,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(responses)
	}
}
