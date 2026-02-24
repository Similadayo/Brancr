package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// EscalationsResponse represents escalations list response
type EscalationsResponse struct {
	Total int64              `json:"total"`
	Items []EscalationDetail `json:"items"`
}

// EscalationDetail represents detailed escalation information
type EscalationDetail struct {
	InteractionID  uuid.UUID `json:"interaction_id"`
	TenantID       uuid.UUID `json:"tenant_id"`
	TenantName     string    `json:"tenant_name"`
	CustomerID     uuid.UUID `json:"customer_id"`
	Platform       string    `json:"platform"`
	ContentPreview string    `json:"content_preview"` // Masked PII
	Intent         string    `json:"intent,omitempty"`
	Tone           string    `json:"tone,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	Status         string    `json:"status"`
}

// EscalationsAPIHandler returns all pending escalations
func EscalationsAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		// Get filters
		status := strings.TrimSpace(r.URL.Query().Get("status"))
		if status == "" {
			status = "pending" // Default to pending
		}
		tenantIDStr := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
		limitStr := strings.TrimSpace(r.URL.Query().Get("limit"))
		limit := 200
		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 500 {
				limit = l
			}
		}

		data, err := BuildEscalationsData(ctx, status, tenantIDStr, limit, reqCtx)
		if err != nil {
			ctx.Logger.Error("failed to build escalations data", zap.Error(err))
			http.Error(w, "failed to build escalations data", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(data); err != nil {
			ctx.Logger.Error("failed to encode escalations data", zap.Error(err))
			http.Error(w, "encoding error", http.StatusInternalServerError)
			return
		}
	}
}

// BuildEscalationsData builds escalations list with tenant information
func BuildEscalationsData(ctx *AdminContext, status, tenantIDStr string, limit int, reqCtx context.Context) (*EscalationsResponse, error) {
	query := ctx.DB.WithContext(reqCtx).
		Model(&storage.Interaction{}).
		Where("response_type = ?", "escalated")

	// Apply filters
	if status != "" {
		query = query.Where("response_status = ?", status)
	}
	if tenantIDStr != "" {
		tenantID, err := uuid.Parse(tenantIDStr)
		if err == nil {
			query = query.Where("tenant_id = ?", tenantID)
		}
	}

	// Count total
	var total int64
	query.Count(&total)

	// Get interactions
	var interactions []storage.Interaction
	if err := query.Order("created_at DESC").
		Limit(limit).
		Find(&interactions).Error; err != nil {
		return nil, fmt.Errorf("failed to query escalations: %w", err)
	}

	// Get tenant names
	tenantMap := make(map[uuid.UUID]string)
	var tenantIDs []uuid.UUID
	for _, it := range interactions {
		if _, ok := tenantMap[it.TenantID]; !ok {
			tenantIDs = append(tenantIDs, it.TenantID)
			tenantMap[it.TenantID] = "" // Placeholder to avoid duplicates in tenantIDs
		}
	}
	if len(tenantIDs) > 0 {
		var tenants []storage.Tenant
		ctx.DB.WithContext(reqCtx).Where("id IN ?", tenantIDs).Find(&tenants)
		for _, t := range tenants {
			tenantMap[t.ID] = t.Name
		}
	}

	items := make([]EscalationDetail, 0, len(interactions))
	for _, it := range interactions {
		// Extract intent and tone from metadata
		intent := ""
		tone := ""
		if it.Metadata != "" {
			var meta map[string]interface{}
			if json.Unmarshal([]byte(it.Metadata), &meta) == nil {
				if i, ok := meta["intent"].(string); ok {
					intent = i
				}
				if t, ok := meta["tone"].(string); ok {
					tone = t
				}
			}
		}

		// Create masked preview
		preview := maskPII(previewText(it.Content, 40))

		items = append(items, EscalationDetail{
			InteractionID:  it.ID,
			TenantID:       it.TenantID,
			TenantName:     tenantMap[it.TenantID],
			CustomerID:     it.CustomerID,
			Platform:       it.Platform,
			ContentPreview: preview,
			Intent:         intent,
			Tone:           tone,
			CreatedAt:      it.CreatedAt,
			Status:         it.ResponseStatus,
		})
	}

	return &EscalationsResponse{
		Total: total,
		Items: items,
	}, nil
}

// ResolveEscalationRequest represents a request to resolve an escalation
type ResolveEscalationRequest struct {
	Action string `json:"action"` // "resolve" or "ignore"
}

// ResolveEscalationHandler resolves or ignores an escalation
func ResolveEscalationHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		// Check permission: only PlatformAdmin and SuperAdmin can resolve escalations
		role := AdminRoleFromRequest(r)
		if role != RoleSuperAdmin && role != RolePlatformAdmin {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		idStr := chi.URLParam(r, "id")
		id, err := uuid.Parse(idStr)
		if err != nil || id == uuid.Nil {
			http.Error(w, "invalid interaction id", http.StatusBadRequest)
			return
		}
		interactionID := id

		var req ResolveEscalationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// Try query parameter as fallback
			req.Action = r.URL.Query().Get("action")
		}

		status := "resolved"
		if req.Action == "ignore" {
			status = "ignored"
		}

		// Audit log
		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  role,
			Action:     "escalation.resolve",
			Metadata: map[string]interface{}{
				"interaction_id": interactionID,
				"status":         status,
				"resource":       "interaction",
				"resource_id":    idStr,
			},
		}
		if err := auditWriter(reqCtx, ctx, entry); err != nil {
			ctx.Logger.Warn("failed to log audit entry", zap.Error(err))
		}

		// Update escalation status
		if err := ctx.DB.WithContext(reqCtx).
			Model(&storage.Interaction{}).
			Where("id = ?", interactionID).
			Update("response_status", status).Error; err != nil {
			ctx.Logger.Error("failed to update escalation status", zap.Stringer("interaction_id", interactionID), zap.Error(err))
			http.Error(w, "failed to update escalation status", http.StatusInternalServerError)
			return
		}

		// Send SSE notification
		if ctx.Events != nil {
			msg := fmt.Sprintf("Escalation %s %s by %s", interactionID.String(), status, AdminEmailFromRequest(r))
			ctx.Events.Publish(msg)
		}

		respondJSON(w, actionResponse{Status: "ok"})
	}
}
