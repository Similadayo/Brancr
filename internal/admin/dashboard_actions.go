package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// PauseTenantRequest represents a request to pause/unpause a tenant
type PauseTenantRequest struct {
	Paused bool `json:"paused"`
}

// PauseTenantHandler pauses or unpauses a tenant (Founder only)
// POST /admin/api/tenants/{id}/pause
func PauseTenantHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check permission: only Founder can pause tenants
		role := AdminRoleFromRequest(r)
		if role != RoleSuperAdmin {
			http.Error(w, "forbidden: only founder can pause tenants", http.StatusForbidden)
			return
		}

		tenantID, err := parseTenantParam(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var req PauseTenantRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Update tenant status
		status := "active"
		if req.Paused {
			status = "suspended"
		}

		if err := ctx.DB.WithContext(r.Context()).
			Model(&storage.Tenant{}).
			Where("id = ?", tenantID).
			Update("status", status).Error; err != nil {
			ctx.Logger.Error("failed to pause/unpause tenant", zap.Stringer("tenant_id", tenantID), zap.Error(err))
			http.Error(w, "failed to update tenant status", http.StatusInternalServerError)
			return
		}

		// Log audit event
		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  role,
			Action:     "tenant.pause",
			TenantID:   &tenantID,
			Metadata: map[string]interface{}{
				"tenant_id": tenantID,
				"paused":    req.Paused,
				"status":    status,
			},
		}
		if err := auditWriter(r.Context(), ctx, entry); err != nil {
			ctx.Logger.Warn("failed to log audit entry", zap.Error(err))
		}

		respondJSON(w, map[string]interface{}{
			"status": "ok",
			"paused": req.Paused,
		})
	}
}

// CreateInsightRequest represents a request to create a qualitative insight
type CreateInsightRequest struct {
	InsightType    string     `json:"insight_type"` // 'complaint', 'aha_moment', 'objection', 'risk_flag', 'onboarding_note'
	TenantID       *uuid.UUID `json:"tenant_id,omitempty"`
	ConversationID *uuid.UUID `json:"conversation_id,omitempty"`
	Content        string     `json:"content"`
	Tags           []string   `json:"tags,omitempty"`
}

// CreateInsightHandler creates a new qualitative insight
// POST /admin/api/insights
func CreateInsightHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check permission
		role := AdminRoleFromRequest(r)
		if !hasPermission(role, "insights.write") {
			http.Error(w, "forbidden: insufficient permissions", http.StatusForbidden)
			return
		}

		var req CreateInsightRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Validate insight type
		validTypes := map[string]bool{
			"complaint":       true,
			"aha_moment":      true,
			"objection":       true,
			"risk_flag":       true,
			"onboarding_note": true,
		}
		if !validTypes[req.InsightType] {
			http.Error(w, "invalid insight_type", http.StatusBadRequest)
			return
		}

		insight := &QualitativeInsight{
			InsightType:    req.InsightType,
			TenantID:       req.TenantID,
			ConversationID: req.ConversationID,
			Content:        req.Content,
			AdminEmail:     AdminEmailFromRequest(r),
			AdminRole:      role,
			Tags:           req.Tags,
		}

		insightsService := NewInsightsService(ctx.DB, ctx.Logger)
		if err := insightsService.CreateInsight(r.Context(), insight); err != nil {
			ctx.Logger.Error("failed to create insight", zap.Error(err))
			http.Error(w, "failed to create insight", http.StatusInternalServerError)
			return
		}

		// Log audit event
		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  role,
			Action:     "insight.create",
			TenantID:   req.TenantID,
			Metadata: map[string]interface{}{
				"insight_id": insight.ID,
				"type":       req.InsightType,
			},
		}
		if err := auditWriter(r.Context(), ctx, entry); err != nil {
			ctx.Logger.Warn("failed to log audit entry", zap.Error(err))
		}

		respondJSON(w, insight)
	}
}

// FlagBlockerRequest represents a request to flag a blocker
type FlagBlockerRequest struct {
	Description string     `json:"description"`
	Priority    string     `json:"priority"` // "low", "medium", "high", "critical"
	TenantID    *uuid.UUID `json:"tenant_id,omitempty"`
	Section     string     `json:"section,omitempty"` // Which dashboard section this blocker affects
}

// FlagBlockerHandler flags a blocker (PM only)
// POST /admin/api/blockers/flag
func FlagBlockerHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check permission: blocker.flag (PlatformAdmin only)
		role := AdminRoleFromRequest(r)
		if !hasPermission(role, "blocker.flag") {
			http.Error(w, "forbidden: only PM can flag blockers", http.StatusForbidden)
			return
		}

		var req FlagBlockerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Store blocker as a qualitative insight with type "risk_flag"
		insight := &QualitativeInsight{
			InsightType: "risk_flag",
			TenantID:    req.TenantID,
			Content:     fmt.Sprintf("[BLOCKER] Priority: %s | Section: %s | %s", req.Priority, req.Section, req.Description),
			AdminEmail:  AdminEmailFromRequest(r),
			AdminRole:   role,
			Tags:        []string{"blocker", req.Priority},
		}

		insightsService := NewInsightsService(ctx.DB, ctx.Logger)
		if err := insightsService.CreateInsight(r.Context(), insight); err != nil {
			ctx.Logger.Error("failed to flag blocker", zap.Error(err))
			http.Error(w, "failed to flag blocker", http.StatusInternalServerError)
			return
		}

		// Log audit event
		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  role,
			Action:     "blocker.flag",
			TenantID:   req.TenantID,
			Metadata: map[string]interface{}{
				"insight_id": insight.ID,
				"priority":   req.Priority,
				"section":    req.Section,
			},
		}
		if err := auditWriter(r.Context(), ctx, entry); err != nil {
			ctx.Logger.Warn("failed to log audit entry", zap.Error(err))
		}

		respondJSON(w, map[string]interface{}{
			"status":     "ok",
			"insight_id": insight.ID,
		})
	}
}

// CapTenantUsageRequest represents a request to cap tenant usage
type CapTenantUsageRequest struct {
	CostLimit    *float64 `json:"cost_limit,omitempty"` // USD
	AICallsLimit *int     `json:"ai_calls_limit,omitempty"`
}

// CapTenantUsageHandler caps tenant usage (Founder only)
// POST /admin/api/tenants/{id}/cap-usage
func CapTenantUsageHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check permission: only Founder can cap usage
		role := AdminRoleFromRequest(r)
		if role != RoleSuperAdmin {
			http.Error(w, "forbidden: only founder can cap tenant usage", http.StatusForbidden)
			return
		}

		tenantID, err := parseTenantParam(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var req CapTenantUsageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Update tenant quota
		updates := map[string]interface{}{}
		if req.CostLimit != nil {
			updates["cost_limit"] = *req.CostLimit
		}
		if req.AICallsLimit != nil {
			updates["ai_calls_limit"] = *req.AICallsLimit
		}

		if len(updates) == 0 {
			http.Error(w, "no limits provided", http.StatusBadRequest)
			return
		}

		if err := ctx.DB.WithContext(r.Context()).
			Model(&storage.TenantQuota{}).
			Where("tenant_id = ?", tenantID).
			Updates(updates).Error; err != nil {
			ctx.Logger.Error("failed to cap tenant usage", zap.Stringer("tenant_id", tenantID), zap.Error(err))
			http.Error(w, "failed to update tenant quota", http.StatusInternalServerError)
			return
		}

		// Log audit event
		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  role,
			Action:     "tenant.cap_usage",
			TenantID:   &tenantID,
			Metadata:   updates,
		}
		if err := auditWriter(r.Context(), ctx, entry); err != nil {
			ctx.Logger.Warn("failed to log audit entry", zap.Error(err))
		}

		respondJSON(w, map[string]interface{}{
			"status": "ok",
			"limits": updates,
		})
	}
}

// Helper function to check permissions
func hasPermission(role, permission string) bool {
	// SuperAdmin has all permissions
	if role == RoleSuperAdmin {
		return true
	}

	permissions, exists := RolePermissions[role]
	if !exists {
		return false
	}

	// Check for wildcard permission
	if permissions["*"] {
		return true
	}

	return permissions[permission]
}

// --- Moved from dashboard_api.go ---

type actionResponse struct {
	Status string `json:"status"`
}

type statusRequest struct {
	Status string `json:"status"`
}

type tokenRefreshRequest struct {
	Force bool `json:"force"`
}

func AdminTenantStatusAPIHandler(ctx *AdminContext) http.HandlerFunc {
	allowed := map[string]struct{}{"active": {}, "suspended": {}, "pending": {}}
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := parseTenantParam(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var payload statusRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		status := strings.ToLower(payload.Status)
		if _, ok := allowed[status]; !ok {
			http.Error(w, "invalid status", http.StatusBadRequest)
			return
		}

		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.status_change",
			TenantID:   &tenantID,
			Metadata: map[string]interface{}{
				"tenant_id": tenantID,
				"status":    status,
			},
		}
		if err := auditWriter(r.Context(), ctx, entry); err != nil {
			http.Error(w, "audit failed", http.StatusInternalServerError)
			return
		}

		if err := ctx.DB.Model(&storage.Tenant{}).Where("id = ?", tenantID).Update("status", status).Error; err != nil {
			ctx.Logger.Warn("failed to update tenant status", zap.Stringer("tenant_id", tenantID), zap.Error(err))
			http.Error(w, "failed to update status", http.StatusInternalServerError)
			return
		}

		respondJSON(w, actionResponse{Status: "ok"})
	}
}

func AdminTenantOnboardingResetHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := parseTenantParam(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var tenant storage.Tenant
		if err := ctx.DB.Select("onboarding_step").First(&tenant, tenantID).Error; err != nil {
			http.Error(w, "tenant not found", http.StatusNotFound)
			return
		}

		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.onboarding_reset",
			TenantID:   &tenantID,
			Metadata: map[string]interface{}{
				"tenant_id":     tenantID,
				"previous_step": tenant.OnboardingStep,
			},
		}
		if err := auditWriter(r.Context(), ctx, entry); err != nil {
			http.Error(w, "audit failed", http.StatusInternalServerError)
			return
		}

		if err := ctx.DB.Model(&storage.Tenant{}).
			Where("id = ?", tenantID).
			Updates(map[string]interface{}{
				"onboarding_step":     "business_profile",
				"onboarding_complete": false,
			}).Error; err != nil {
			ctx.Logger.Warn("failed to reset onboarding", zap.Stringer("tenant_id", tenantID), zap.Error(err))
			http.Error(w, "failed to reset onboarding", http.StatusInternalServerError)
			return
		}

		respondJSON(w, actionResponse{Status: "ok"})
	}
}

func AdminTenantTokenRefreshHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := parseTenantParam(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var payload tokenRefreshRequest
		_ = json.NewDecoder(r.Body).Decode(&payload) // payload optional

		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.tokens_refresh",
			TenantID:   &tenantID,
			Metadata: map[string]interface{}{
				"tenant_id": tenantID,
				"force":     payload.Force,
			},
		}
		if err := auditWriter(r.Context(), ctx, entry); err != nil {
			http.Error(w, "audit failed", http.StatusInternalServerError)
			return
		}

		newExpiry := time.Now().Add(72 * time.Hour)
		if err := ctx.DB.Model(&storage.SocialAccount{}).
			Where("tenant_id = ?", tenantID).
			Update("expires_at", newExpiry).Error; err != nil {
			ctx.Logger.Warn("failed to refresh tokens", zap.Stringer("tenant_id", tenantID), zap.Error(err))
			http.Error(w, "failed to refresh tokens", http.StatusInternalServerError)
			return
		}

		respondJSON(w, actionResponse{Status: "ok"})
	}
}

func parseTenantParam(r *http.Request) (uuid.UUID, error) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("invalid tenant id")
	}
	return id, nil
}
