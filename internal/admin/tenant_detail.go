package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/similadayo/brancr/internal/quota"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

func TenantDetailHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id64, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil || id64 == 0 {
			http.Error(w, "invalid tenant id", http.StatusBadRequest)
			return
		}
		tenantID := uint(id64)

		var tenant storage.Tenant
		if err := ctx.DB.First(&tenant, tenantID).Error; err != nil {
			http.Error(w, "tenant not found", http.StatusNotFound)
			return
		}

		quotaService := quota.NewQuotaService(ctx.DB, ctx.Logger, nil)
		quotaData, quotaErr := quotaService.GetQuotaUsage(r.Context(), tenantID)
		if quotaErr != nil {
			ctx.Logger.Warn("failed to load tenant quota", zap.Uint("tenant_id", tenantID), zap.Error(quotaErr))
		}

		var accounts []storage.SocialAccount
		if err := ctx.DB.Where("tenant_id = ?", tenantID).Order("platform ASC, created_at DESC").Find(&accounts).Error; err != nil {
			ctx.Logger.Warn("failed to load social accounts for tenant", zap.Uint("tenant_id", tenantID), zap.Error(err))
		}

		var scheduledPosts []storage.ScheduledPost
		if err := ctx.DB.Where("tenant_id = ?", tenantID).Order("scheduled_at DESC").Limit(25).Find(&scheduledPosts).Error; err != nil {
			ctx.Logger.Warn("failed to load scheduled posts for tenant", zap.Uint("tenant_id", tenantID), zap.Error(err))
		}

		var pendingEscalations int64
		_ = ctx.DB.Model(&storage.Interaction{}).
			Where("tenant_id = ? AND response_type = ? AND response_status = ?", tenantID, "escalated", "pending").
			Count(&pendingEscalations).Error

		data := map[string]any{
			"Page":               "tenants",
			"Tenant":             tenant,
			"Quota":              quotaData,
			"Accounts":           accounts,
			"ScheduledPosts":     scheduledPosts,
			"PendingEscalations": pendingEscalations,
		}

		renderTemplate(w, r, "tenant.html", data, ctx.Logger)
	}
}

func TenantUpdateStatusHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id64, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil || id64 == 0 {
			http.Error(w, "invalid tenant id", http.StatusBadRequest)
			return
		}
		tenantID := uint(id64)

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

		if err := ctx.DB.Model(&storage.Tenant{}).Where("id = ?", tenantID).Update("status", status).Error; err != nil {
			ctx.Logger.Error("failed to update tenant status", zap.Uint("tenant_id", tenantID), zap.Error(err))
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/admin/tenants/"+idStr, http.StatusSeeOther)
	}
}

func TenantUpdateQuotaHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id64, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil || id64 == 0 {
			http.Error(w, "invalid tenant id", http.StatusBadRequest)
			return
		}
		tenantID := uint(id64)

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
		parseIntField("storage_limit_mb")
		parseIntField("api_calls_limit")

		planType := strings.TrimSpace(r.FormValue("plan_type"))

		quotaService := quota.NewQuotaService(ctx.DB, ctx.Logger, nil)
		if err := quotaService.UpdateQuotaLimits(r.Context(), tenantID, limits, planType); err != nil {
			ctx.Logger.Error("failed to update quota limits", zap.Uint("tenant_id", tenantID), zap.Error(err))
			http.Error(w, "failed to update quota", http.StatusInternalServerError)
			return
		}

		if planType != "" {
			if err := ctx.DB.Model(&storage.Tenant{}).Where("id = ?", tenantID).Update("plan_type", planType).Error; err != nil {
				ctx.Logger.Warn("failed to update tenant plan_type", zap.Uint("tenant_id", tenantID), zap.Error(err))
			}
		}

		http.Redirect(w, r, "/admin/tenants/"+idStr, http.StatusSeeOther)
	}
}
