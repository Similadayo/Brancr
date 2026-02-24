package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/quota"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

type TenantsListResponse struct {
	Total  int64           `json:"total"`
	Items  []TenantSummary `json:"items"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
}

// AdminTenantsListAPIHandler returns tenant metadata (no content).
func AdminTenantsListAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := ctx.DB.Model(&storage.Tenant{})

		if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
			q = q.Where("status = ?", status)
		}
		if step := strings.TrimSpace(r.URL.Query().Get("onboarding_step")); step != "" {
			q = q.Where("onboarding_step = ?", step)
		}
		if search := strings.TrimSpace(r.URL.Query().Get("q")); search != "" {
			like := "%" + search + "%"
			q = q.Where("name ILIKE ? OR email ILIKE ? OR CAST(id AS TEXT) ILIKE ?", like, like, like)
		}

		limit := 50
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v > 0 && v <= 200 {
				limit = v
			}
		}
		offset := 0
		if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
				offset = v
			}
		}

		var total int64
		if err := q.Count(&total).Error; err != nil {
			ctx.Logger.Error("failed to count tenants", zap.Error(err))
			http.Error(w, "failed to count tenants", http.StatusInternalServerError)
			return
		}

		var tenants []storage.Tenant
		if err := q.Order("updated_at desc").Limit(limit).Offset(offset).Find(&tenants).Error; err != nil {
			ctx.Logger.Error("failed to query tenants", zap.Error(err))
			http.Error(w, "failed to query tenants", http.StatusInternalServerError)
			return
		}

		ids := make([]uuid.UUID, 0, len(tenants))
		for _, t := range tenants {
			ids = append(ids, t.ID)
		}

		platformsByTenant := map[uuid.UUID]map[string]struct{}{}
		if len(ids) > 0 {
			var accounts []storage.SocialAccount
			if err := ctx.DB.Where("tenant_id IN ?", ids).Find(&accounts).Error; err == nil {
				for _, acct := range accounts {
					set := platformsByTenant[acct.TenantID]
					if set == nil {
						set = map[string]struct{}{}
						platformsByTenant[acct.TenantID] = set
					}
					set[strings.Title(acct.Platform)] = struct{}{}
				}
			}
		}

		type activityRow struct {
			TenantID uuid.UUID
			LastAt   time.Time
		}
		lastActivity := map[uuid.UUID]time.Time{}
		if len(ids) > 0 {
			var rows []activityRow
			_ = ctx.DB.Model(&storage.Interaction{}).
				Select("tenant_id, MAX(created_at) AS last_at").
				Where("tenant_id IN ?", ids).
				Group("tenant_id").
				Scan(&rows).Error
			for _, row := range rows {
				lastActivity[row.TenantID] = row.LastAt
			}
		}

		quotaSvc := quota.NewQuotaService(ctx.DB, ctx.Logger, nil)
		// Get pending escalations count per tenant
		pendingEscalationsMap := make(map[uuid.UUID]int64)
		if len(ids) > 0 {
			type escalationCount struct {
				TenantID uuid.UUID
				Count    int64
			}
			var counts []escalationCount
			ctx.DB.Model(&storage.Interaction{}).
				Select("tenant_id, COUNT(*) as count").
				Where("tenant_id IN ? AND response_type = ? AND response_status = ?", ids, "escalated", "pending").
				Group("tenant_id").
				Scan(&counts)
			for _, c := range counts {
				pendingEscalationsMap[c.TenantID] = c.Count
			}
		}

		// Build comprehensive tenant summaries
		items := make([]TenantSummary, 0, len(tenants))
		for _, tenant := range tenants {
			set := platformsByTenant[tenant.ID]
			platforms := make([]string, 0, len(set))
			for p := range set {
				platforms = append(platforms, p)
			}

			usagePercent := 0.0
			if quotaData, err := quotaSvc.GetQuotaUsage(r.Context(), tenant.ID); err == nil && quotaData != nil {
				usagePercent = maxFloat(
					percentOf(quotaData.AICallsUsed, quotaData.AICallsLimit),
					percentOf(quotaData.PostsUsed, quotaData.PostsLimit),
					percentOf(quotaData.StorageUsedMB, quotaData.StorageLimitMB),
					percentOf(quotaData.APICallsUsed, quotaData.APICallsLimit),
				)
			}

			lastText := ""
			if ts := lastActivity[tenant.ID]; !ts.IsZero() {
				lastText = ts.Format(time.RFC3339)
			}

			items = append(items, TenantSummary{
				ID:                 tenant.ID,
				Name:               tenant.Name,
				Email:              tenant.Email,
				PlanType:           tenant.PlanType,
				Status:             tenant.Status,
				OnboardingStep:     tenant.OnboardingStep,
				OnboardingComplete: tenant.OnboardingComplete,
				Platforms:          platforms,
				LastActivity:       lastText,
				UsagePercent:       round(usagePercent, 1),
				AIHumanMode:        tenant.AIHumanMode,
				KYBStatus:          tenant.KYBStatus,
				AdminTags:          tenant.AdminTags,
				PendingEscalations: pendingEscalationsMap[tenant.ID],
				CreatedAt:          tenant.CreatedAt,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TenantsListResponse{
			Total:  total,
			Items:  items,
			Limit:  limit,
			Offset: offset,
		})
	}
}

type quotaUpdateRequest struct {
	PlanType       string   `json:"plan_type"`
	AICallsLimit   *int     `json:"ai_calls_limit"`
	PostsLimit     *int     `json:"posts_limit"`
	StorageLimitMB *int     `json:"storage_limit_mb"`
	APICallsLimit  *int     `json:"api_calls_limit"`
	PlatformsLimit *int     `json:"platforms_limit"`
	CostLimit      *float64 `json:"cost_limit"` // Cost limit in USD (0 = unlimited)
}

type modeUpdateRequest struct {
	Mode string `json:"mode"` // ai or human
}

// AdminTenantQuotaAPIHandler updates tenant quota limits + plan type (BillingAdmin+).
func AdminTenantQuotaAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := parseTenantParam(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var payload quotaUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}

		limits := map[string]int{}
		if payload.AICallsLimit != nil {
			limits["ai_calls_limit"] = *payload.AICallsLimit
		}
		if payload.PostsLimit != nil {
			limits["posts_limit"] = *payload.PostsLimit
		}
		if payload.PlatformsLimit != nil {
			limits["platforms_limit"] = *payload.PlatformsLimit
		}
		if payload.StorageLimitMB != nil {
			limits["storage_limit_mb"] = *payload.StorageLimitMB
		}
		if payload.APICallsLimit != nil {
			limits["api_calls_limit"] = *payload.APICallsLimit
		}

		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.quota_change",
			TenantID:   &tenantID,
			Metadata: map[string]interface{}{
				"tenant_id": tenantID,
				"limits":    limits,
				"plan_type": payload.PlanType,
			},
		}
		if err := auditWriter(r.Context(), ctx, entry); err != nil {
			http.Error(w, "audit failed", http.StatusInternalServerError)
			return
		}

		// Parse cost limit (optional)
		costLimit := 0.0
		if payload.CostLimit != nil {
			costLimit = *payload.CostLimit
		}

		quotaService := quota.NewQuotaService(ctx.DB, ctx.Logger, nil)
		if err := quotaService.UpdateQuotaLimits(r.Context(), tenantID, limits, costLimit, payload.PlanType); err != nil {
			ctx.Logger.Error("failed to update quota", zap.Stringer("tenant_id", tenantID), zap.Error(err))
			http.Error(w, "failed to update quota", http.StatusInternalServerError)
			return
		}

		if strings.TrimSpace(payload.PlanType) != "" {
			_ = ctx.DB.Model(&storage.Tenant{}).Where("id = ?", tenantID).Update("plan_type", payload.PlanType).Error
		}

		respondJSON(w, actionResponse{Status: "ok"})
	}
}

type expiringTokenRow struct {
	TenantID   uuid.UUID `json:"tenant_id"`
	TenantName string    `json:"tenant_name"`
	Platform   string    `json:"platform"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// AdminTenantModeAPIHandler updates tenant AI/Human mode (Admin only)
func AdminTenantModeAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := parseTenantParam(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var payload modeUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}

		mode := strings.TrimSpace(payload.Mode)
		if mode != "ai" && mode != "human" {
			http.Error(w, "invalid mode", http.StatusBadRequest)
			return
		}

		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.ai_mode_change",
			TenantID:   &tenantID,
			Metadata: map[string]interface{}{
				"tenant_id": tenantID,
				"mode":      mode,
			},
		}
		if err := auditWriter(r.Context(), ctx, entry); err != nil {
			http.Error(w, "audit failed", http.StatusInternalServerError)
			return
		}

		if err := ctx.DB.Model(&storage.Tenant{}).Where("id = ?", tenantID).Update("ai_human_mode", mode).Error; err != nil {
			ctx.Logger.Error("failed to update tenant ai mode", zap.Stringer("tenant_id", tenantID), zap.Error(err))
			http.Error(w, "failed to update mode", http.StatusInternalServerError)
			return
		}

		respondJSON(w, actionResponse{Status: "ok"})
	}
}

// AdminExpiringTokensAPIHandler lists expiring social tokens (PlatformAdmin+).
func AdminExpiringTokensAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		threshold := time.Now().Add(72 * time.Hour)
		var accounts []storage.SocialAccount
		if err := ctx.DB.Where("expires_at IS NOT NULL AND expires_at < ?", threshold).
			Order("expires_at asc").
			Limit(200).
			Find(&accounts).Error; err != nil {
			ctx.Logger.Error("failed to fetch expiring tokens", zap.Error(err))
			http.Error(w, "failed to fetch expiring tokens", http.StatusInternalServerError)
			return
		}

		tenantIDs := make([]uuid.UUID, 0, len(accounts))
		seen := map[uuid.UUID]struct{}{}
		for _, a := range accounts {
			if _, ok := seen[a.TenantID]; !ok {
				seen[a.TenantID] = struct{}{}
				tenantIDs = append(tenantIDs, a.TenantID)
			}
		}

		nameMap := map[uuid.UUID]string{}
		if len(tenantIDs) > 0 {
			var tenants []storage.Tenant
			_ = ctx.DB.Select("id,name").Where("id IN ?", tenantIDs).Find(&tenants).Error
			for _, t := range tenants {
				nameMap[t.ID] = t.Name
			}
		}

		out := make([]expiringTokenRow, 0, len(accounts))
		for _, a := range accounts {
			if a.ExpiresAt == nil {
				continue
			}
			out = append(out, expiringTokenRow{
				TenantID:   a.TenantID,
				TenantName: nameMap[a.TenantID],
				Platform:   strings.Title(a.Platform),
				ExpiresAt:  *a.ExpiresAt,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}

// AdminTenantImpersonationStartAPIHandler is a placeholder for SPA wiring. The existing HTML handler is case-gated.
func AdminTenantImpersonationStartAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := parseTenantParam(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = tenantID
		http.Error(w, "impersonation is handled via /admin/support/impersonate (case-gated)", http.StatusNotImplemented)
	}
}
