package admin

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/quota"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

func TenantDetailHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		tenantID, err := uuid.Parse(idStr)
		if err != nil || tenantID == uuid.Nil {
			http.Error(w, "invalid tenant id", http.StatusBadRequest)
			return
		}

		var tenant storage.Tenant
		if err := ctx.DB.First(&tenant, tenantID).Error; err != nil {
			http.Error(w, "tenant not found", http.StatusNotFound)
			return
		}

		quotaService := quota.NewQuotaService(ctx.DB, ctx.Logger, nil)
		quotaData, quotaErr := quotaService.GetQuotaUsage(r.Context(), tenantID)
		if quotaErr != nil {
			ctx.Logger.Warn("failed to load tenant quota", zap.Stringer("tenant_id", tenantID), zap.Error(quotaErr))
		}

		var accounts []storage.SocialAccount
		if err := ctx.DB.Where("tenant_id = ?", tenantID).Order("platform ASC, created_at DESC").Find(&accounts).Error; err != nil {
			ctx.Logger.Warn("failed to load social accounts for tenant", zap.Stringer("tenant_id", tenantID), zap.Error(err))
		}

		// Count connected platforms
		var connectedPlatforms int64
		_ = ctx.DB.Model(&storage.SocialAccount{}).Where("tenant_id = ? AND connected = ?", tenantID, true).Count(&connectedPlatforms).Error

		var scheduledPosts []storage.ScheduledPost
		if err := ctx.DB.Where("tenant_id = ?", tenantID).Order("scheduled_at DESC").Limit(25).Find(&scheduledPosts).Error; err != nil {
			ctx.Logger.Warn("failed to load scheduled posts for tenant", zap.Stringer("tenant_id", tenantID), zap.Error(err))
		}

		var pendingEscalations int64
		_ = ctx.DB.Model(&storage.Interaction{}).
			Where("tenant_id = ? AND response_type = ? AND response_status = ?", tenantID, "escalated", "pending").
			Count(&pendingEscalations).Error

		// Compute usage percentages for display
		aiPercent := 0.0
		postsPercent := 0.0
		storagePercent := 0.0
		apiPercent := 0.0
		platformsLimit := 0
		if quotaData != nil {
			if quotaData.AICallsLimit > 0 {
				aiPercent = float64(quotaData.AICallsUsed) / float64(quotaData.AICallsLimit) * 100
			}
			if quotaData.PostsLimit > 0 {
				postsPercent = float64(quotaData.PostsUsed) / float64(quotaData.PostsLimit) * 100
			}
			if quotaData.StorageLimitMB > 0 {
				storagePercent = float64(quotaData.StorageUsedMB) / float64(quotaData.StorageLimitMB) * 100
			}
			if quotaData.APICallsLimit > 0 {
				apiPercent = float64(quotaData.APICallsUsed) / float64(quotaData.APICallsLimit) * 100
			}
			platformsLimit = quotaData.PlatformsLimit
		}

		// Warning messages (e.g., when admin reduced platforms_limit below current connected count)
		warningKey := strings.TrimSpace(r.URL.Query().Get("warning"))
		var warningMessage string
		switch warningKey {
		case "platforms_limit_less_than_connected":
			warningMessage = fmt.Sprintf("Warning: there are currently %d connected platforms — reducing platforms limit below that number may block operations.", connectedPlatforms)
		default:
			warningMessage = ""
		}

		data := map[string]any{
			"Page":                "tenants",
			"Tenant":              tenant,
			"Quota":               quotaData,
			"Accounts":            accounts,
			"ScheduledPosts":      scheduledPosts,
			"PendingEscalations":  pendingEscalations,
			"PlatformsCount":      connectedPlatforms,
			"AIUsagePercent":      aiPercent,
			"PostsUsagePercent":   postsPercent,
			"StorageUsagePercent": storagePercent,
			"APIUsagePercent":     apiPercent,
			"PlatformsLimit":      platformsLimit,
			"WarningMessage":      warningMessage,
		}

		renderTemplate(w, r, "tenant.html", data, ctx.Logger)
	}
}

func TenantUpdateStatusHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		tenantID, err := uuid.Parse(idStr)
		if err != nil || tenantID == uuid.Nil {
			http.Error(w, "invalid tenant id", http.StatusBadRequest)
			return
		}

		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		status := strings.TrimSpace(r.FormValue("status"))
		switch status {
		case "active", "suspended", "pending":
			// ok
		default:
			http.Error(w, "invalid status", http.StatusBadRequest)
			return
		}

		// Audit before mutation (fail-closed)
		ae := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.status_change",
			TenantID:   &tenantID,
			Metadata: map[string]interface{}{
				"tenant_id": tenantID,
				"status":    status,
			},
		}
		if err := auditWriter(r.Context(), ctx, ae); err != nil {
			http.Error(w, "audit log failed", http.StatusInternalServerError)
			return
		}
		if err := ctx.DB.Model(&storage.Tenant{}).Where("id = ?", tenantID).Update("status", status).Error; err != nil {
			ctx.Logger.Error("failed to update tenant status", zap.Stringer("tenant_id", tenantID), zap.Error(err))
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/admin/tenants/"+idStr, http.StatusSeeOther)
	}
}

func TenantUpdateAIModeHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		tenantID, err := uuid.Parse(idStr)
		if err != nil || tenantID == uuid.Nil {
			http.Error(w, "invalid tenant id", http.StatusBadRequest)
			return
		}

		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		mode := strings.TrimSpace(r.FormValue("ai_human_mode"))
		if mode != "ai" && mode != "human" {
			http.Error(w, "invalid ai_human_mode", http.StatusBadRequest)
			return
		}

		// Audit before mutation
		ae := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.ai_mode_change",
			TenantID:   &tenantID,
			Metadata: map[string]interface{}{
				"tenant_id":     tenantID,
				"ai_human_mode": mode,
			},
		}
		if err := auditWriter(r.Context(), ctx, ae); err != nil {
			http.Error(w, "audit log failed", http.StatusInternalServerError)
			return
		}

		if err := ctx.DB.Model(&storage.Tenant{}).Where("id = ?", tenantID).Update("ai_human_mode", mode).Error; err != nil {
			ctx.Logger.Error("failed to update tenant ai_human_mode", zap.Stringer("tenant_id", tenantID), zap.Error(err))
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/admin/tenants/"+idStr, http.StatusSeeOther)
	}
}

func TenantUpdateQuotaHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		tenantID, err := uuid.Parse(idStr)
		if err != nil || tenantID == uuid.Nil {
			http.Error(w, "invalid tenant id", http.StatusBadRequest)
			return
		}

		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		limits := map[string]int{}
		parseIntField := func(formKey string) {
			val := strings.TrimSpace(r.FormValue(formKey))
			if val == "" {
				return
			}
			parsed, err := strconv.Atoi(val)
			if err != nil {
				return
			}
			limits[formKey] = parsed
		}

		parseIntField("ai_calls_limit")
		parseIntField("posts_limit")
		parseIntField("platforms_limit")
		parseIntField("storage_limit_mb")
		parseIntField("api_calls_limit")

		planType := strings.TrimSpace(r.FormValue("plan_type"))

		// Audit before mutation (fail-closed)
		ae := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.quota_change",
			TenantID:   &tenantID,
			Metadata: map[string]interface{}{
				"limits":   limits,
				"planType": planType,
			},
		}
		if err := auditWriter(r.Context(), ctx, ae); err != nil {
			http.Error(w, "audit log failed", http.StatusInternalServerError)
			return
		}
		// If platforms_limit is being set, check current connected platforms and warn if lowering below current count
		var connected int64
		_ = ctx.DB.Model(&storage.SocialAccount{}).Where("tenant_id = ? AND connected = ?", tenantID, true).Count(&connected).Error
		warningKey := ""
		if val, ok := limits["platforms_limit"]; ok {
			if val < int(connected) {
				warningKey = "platforms_limit_less_than_connected"
			}
		}

		// Parse cost limit from form (optional)
		costLimit := 0.0
		if costLimitStr := r.FormValue("cost_limit"); costLimitStr != "" {
			if parsed, err := strconv.ParseFloat(costLimitStr, 64); err == nil {
				costLimit = parsed
			}
		}

		quotaService := quota.NewQuotaService(ctx.DB, ctx.Logger, nil)
		if err := quotaService.UpdateQuotaLimits(r.Context(), tenantID, limits, costLimit, planType); err != nil {
			ctx.Logger.Error("failed to update quota limits", zap.Stringer("tenant_id", tenantID), zap.Error(err))
			http.Error(w, "failed to update quota", http.StatusInternalServerError)
			return
		}

		if planType != "" {
			if err := ctx.DB.Model(&storage.Tenant{}).Where("id = ?", tenantID).Update("plan_type", planType).Error; err != nil {
				ctx.Logger.Warn("failed to update tenant plan_type", zap.Stringer("tenant_id", tenantID), zap.Error(err))
			}
		}

		redirectURL := "/admin/tenants/" + idStr
		if warningKey != "" {
			redirectURL = redirectURL + "?warning=" + warningKey
		}

		http.Redirect(w, r, redirectURL, http.StatusSeeOther)
	}
}
