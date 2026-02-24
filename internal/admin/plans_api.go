package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/billing"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// PlanTemplateResponse represents a plan template in API responses
type PlanTemplateResponse struct {
	ID                          uuid.UUID `json:"id"`
	Name                        string    `json:"name"`
	DefaultAIReplyQuota         int       `json:"default_ai_reply_quota"`
	DefaultWhatsAppMessageQuota int       `json:"default_whatsapp_message_quota"`
	DefaultPostQuota            int       `json:"default_post_quota"`
	DefaultStorageLimitMB       int       `json:"default_storage_limit_mb"`
	DefaultAPICallsLimit        int       `json:"default_api_calls_limit"`
	DefaultPlatformsLimit       int       `json:"default_platforms_limit"`
	AINegotiationEnabled        bool      `json:"ai_negotiation_enabled"`
	EscalationBehavior          string    `json:"escalation_behavior"`
	OveragesAllowed             bool      `json:"overages_allowed"`
	DefaultMonthlyPrice         float64   `json:"default_monthly_price"`
	Status                      string    `json:"status"`
	Description                 string    `json:"description"`
	CreatedAt                   string    `json:"created_at"`
	UpdatedAt                   string    `json:"updated_at"`
}

// ListPlanTemplatesHandler returns all plan templates
func ListPlanTemplatesHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		includeDeprecated := r.URL.Query().Get("include_deprecated") == "true"

		billingService := billing.NewBillingService(ctx.DB, ctx.Logger)
		templates, err := billingService.ListPlanTemplates(reqCtx, includeDeprecated)
		if err != nil {
			ctx.Logger.Error("failed to list plan templates", zap.Error(err))
			http.Error(w, "failed to list plan templates", http.StatusInternalServerError)
			return
		}

		response := make([]PlanTemplateResponse, len(templates))
		for i, t := range templates {
			response[i] = PlanTemplateResponse{
				ID:                          t.ID,
				Name:                        t.Name,
				DefaultAIReplyQuota:         t.DefaultAIReplyQuota,
				DefaultWhatsAppMessageQuota: t.DefaultWhatsAppMessageQuota,
				DefaultPostQuota:            t.DefaultPostQuota,
				DefaultStorageLimitMB:       t.DefaultStorageLimitMB,
				DefaultAPICallsLimit:        t.DefaultAPICallsLimit,
				DefaultPlatformsLimit:       t.DefaultPlatformsLimit,
				AINegotiationEnabled:        t.AINegotiationEnabled,
				EscalationBehavior:          t.EscalationBehavior,
				OveragesAllowed:             t.OveragesAllowed,
				DefaultMonthlyPrice:         t.DefaultMonthlyPrice,
				Status:                      t.Status,
				Description:                 t.Description,
				CreatedAt:                   t.CreatedAt.Format(time.RFC3339),
				UpdatedAt:                   t.UpdatedAt.Format(time.RFC3339),
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// CreatePlanTemplateHandler creates a new plan template
func CreatePlanTemplateHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		var req struct {
			Name                        string  `json:"name"`
			DefaultAIReplyQuota         int     `json:"default_ai_reply_quota"`
			DefaultWhatsAppMessageQuota int     `json:"default_whatsapp_message_quota"`
			DefaultPostQuota            int     `json:"default_post_quota"`
			DefaultStorageLimitMB       int     `json:"default_storage_limit_mb"`
			DefaultAPICallsLimit        int     `json:"default_api_calls_limit"`
			DefaultPlatformsLimit       int     `json:"default_platforms_limit"`
			AINegotiationEnabled        bool    `json:"ai_negotiation_enabled"`
			EscalationBehavior          string  `json:"escalation_behavior"`
			OveragesAllowed             bool    `json:"overages_allowed"`
			DefaultMonthlyPrice         float64 `json:"default_monthly_price"`
			Description                 string  `json:"description"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if req.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}

		// Validate escalation behavior
		validEscalationBehaviors := map[string]bool{
			"always_on": true, "configurable": true, "advanced": true,
		}
		if !validEscalationBehaviors[req.EscalationBehavior] {
			req.EscalationBehavior = "always_on"
		}

		template := &storage.PlanTemplate{
			Name:                        req.Name,
			DefaultAIReplyQuota:         req.DefaultAIReplyQuota,
			DefaultWhatsAppMessageQuota: req.DefaultWhatsAppMessageQuota,
			DefaultPostQuota:            req.DefaultPostQuota,
			DefaultStorageLimitMB:       req.DefaultStorageLimitMB,
			DefaultAPICallsLimit:        req.DefaultAPICallsLimit,
			DefaultPlatformsLimit:       req.DefaultPlatformsLimit,
			AINegotiationEnabled:        req.AINegotiationEnabled,
			EscalationBehavior:          req.EscalationBehavior,
			OveragesAllowed:             req.OveragesAllowed,
			DefaultMonthlyPrice:         req.DefaultMonthlyPrice,
			Status:                      "active",
			Description:                 req.Description,
		}

		billingService := billing.NewBillingService(ctx.DB, ctx.Logger)
		created, err := billingService.CreatePlanTemplate(reqCtx, template)
		if err != nil {
			ctx.Logger.Error("failed to create plan template", zap.Error(err))
			http.Error(w, "failed to create plan template", http.StatusInternalServerError)
			return
		}

		// Audit log
		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "plan_template.create",
			Metadata: map[string]interface{}{
				"template_name": created.Name,
				"template_id":   created.ID,
			},
		}
		if err := auditWriter(reqCtx, ctx, entry); err != nil {
			ctx.Logger.Warn("failed to log audit entry", zap.Error(err))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(created)
	}
}

// UpdatePlanTemplateHandler updates a plan template
func UpdatePlanTemplateHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		idStr := chi.URLParam(r, "id")
		id, err := uuid.Parse(idStr)
		if err != nil {
			http.Error(w, "invalid plan template id", http.StatusBadRequest)
			return
		}

		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Remove fields that shouldn't be updated
		delete(req, "id")
		delete(req, "name") // Name cannot be changed
		delete(req, "created_at")
		delete(req, "updated_at")

		billingService := billing.NewBillingService(ctx.DB, ctx.Logger)
		if err := billingService.UpdatePlanTemplate(reqCtx, id, req); err != nil {
			if err == billing.ErrPlanTemplateNotFound {
				http.Error(w, "plan template not found", http.StatusNotFound)
				return
			}
			ctx.Logger.Error("failed to update plan template", zap.Error(err))
			http.Error(w, "failed to update plan template", http.StatusInternalServerError)
			return
		}

		// Audit log
		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "plan_template.update",
			Metadata: map[string]interface{}{
				"template_id": id,
				"updates":     req,
			},
		}
		if err := auditWriter(reqCtx, ctx, entry); err != nil {
			ctx.Logger.Warn("failed to log audit entry", zap.Error(err))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
	}
}

// DeprecatePlanTemplateHandler deprecates a plan template
func DeprecatePlanTemplateHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		idStr := chi.URLParam(r, "id")
		id, err := uuid.Parse(idStr)
		if err != nil {
			http.Error(w, "invalid plan template id", http.StatusBadRequest)
			return
		}

		billingService := billing.NewBillingService(ctx.DB, ctx.Logger)
		if err := billingService.DeprecatePlanTemplate(reqCtx, id); err != nil {
			if err == billing.ErrPlanTemplateNotFound {
				http.Error(w, "plan template not found", http.StatusNotFound)
				return
			}
			ctx.Logger.Error("failed to deprecate plan template", zap.Error(err))
			http.Error(w, "failed to deprecate plan template", http.StatusInternalServerError)
			return
		}

		// Audit log
		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "plan_template.deprecate",
			Metadata: map[string]interface{}{
				"template_id": id,
			},
		}
		if err := auditWriter(reqCtx, ctx, entry); err != nil {
			ctx.Logger.Warn("failed to log audit entry", zap.Error(err))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
	}
}

// AssignPlanToTenantHandler assigns a plan to a tenant
func AssignPlanToTenantHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		tenantIDStr := chi.URLParam(r, "tenant_id")
		tenantID, err := uuid.Parse(tenantIDStr)
		if err != nil {
			http.Error(w, "invalid tenant id", http.StatusBadRequest)
			return
		}

		var req struct {
			PlanTemplateID uuid.UUID `json:"plan_template_id"`
			OverridePrice  *float64  `json:"override_price,omitempty"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if req.PlanTemplateID == uuid.Nil {
			http.Error(w, "plan_template_id is required", http.StatusBadRequest)
			return
		}

		billingService := billing.NewBillingService(ctx.DB, ctx.Logger)
		tenantPlan, err := billingService.AssignPlanToTenant(reqCtx, tenantID, req.PlanTemplateID, req.OverridePrice)
		if err != nil {
			if err == billing.ErrPlanTemplateNotFound {
				http.Error(w, "plan template not found", http.StatusNotFound)
				return
			}
			if err == billing.ErrPlanDeprecated {
				http.Error(w, "plan template is deprecated", http.StatusBadRequest)
				return
			}
			ctx.Logger.Error("failed to assign plan to tenant", zap.Error(err))
			http.Error(w, "failed to assign plan", http.StatusInternalServerError)
			return
		}

		// Audit log
		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.plan_assign",
			TenantID:   &tenantID,
			Metadata: map[string]interface{}{
				"plan_template_id": req.PlanTemplateID,
				"plan_name":        tenantPlan.PlanName,
				"override_price":   req.OverridePrice != nil,
			},
		}
		if err := auditWriter(reqCtx, ctx, entry); err != nil {
			ctx.Logger.Warn("failed to log audit entry", zap.Error(err))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(tenantPlan)
	}
}

// GetTenantBillingHandler returns billing and usage info for a tenant
func GetTenantBillingHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		tenantIDStr := chi.URLParam(r, "tenant_id")
		tenantID, err := uuid.Parse(tenantIDStr)
		if err != nil {
			http.Error(w, "invalid tenant id", http.StatusBadRequest)
			return
		}

		billingService := billing.NewBillingService(ctx.DB, ctx.Logger)
		usageService := billing.NewUsageService(ctx.DB, ctx.Logger)
		enforcementService := billing.NewEnforcementService(ctx.DB, ctx.Logger, billingService, usageService)

		// Get tenant plan
		plan, err := billingService.GetTenantPlan(reqCtx, tenantID)
		if err != nil {
			if err == billing.ErrTenantPlanNotFound {
				// Return empty response if no plan assigned
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"plan":  nil,
					"usage": nil,
				})
				return
			}
			ctx.Logger.Error("failed to get tenant plan", zap.Error(err))
			http.Error(w, "failed to get tenant plan", http.StatusInternalServerError)
			return
		}

		// Get usage status
		usageStatus, err := enforcementService.GetUsageStatus(reqCtx, tenantID)
		if err != nil {
			ctx.Logger.Warn("failed to get usage status", zap.Error(err))
		}

		// Get monthly usage
		monthlyUsage, _ := usageService.GetCurrentMonthUsage(reqCtx, tenantID)

		// Build plan response
		planResponse := map[string]interface{}{
			"id":                     plan.ID,
			"plan_name":              plan.PlanName,
			"plan_template_id":       plan.PlanTemplateID,
			"ai_reply_quota":         plan.AIReplyQuota,
			"whatsapp_message_quota": plan.WhatsAppMessageQuota,
			"post_quota":             plan.PostQuota,
			"storage_limit_mb":       plan.StorageLimitMB,
			"api_calls_limit":        plan.APICallsLimit,
			"platforms_limit":        plan.PlatformsLimit,
			"ai_negotiation_enabled": plan.AINegotiationEnabled,
			"escalation_behavior":    plan.EscalationBehavior,
			"overages_allowed":       plan.OveragesAllowed,
			"monthly_price":          plan.MonthlyPrice,
			"billing_status":         plan.BillingStatus,
			"payment_status":         plan.PaymentStatus,
			"billing_cycle_start":    plan.BillingCycleStart.Format(time.RFC3339),
			"quotas_overridden":      plan.QuotasOverridden,
			"price_overridden":       plan.PriceOverridden,
		}

		if plan.BillingCycleEnd != nil {
			planResponse["billing_cycle_end"] = plan.BillingCycleEnd.Format(time.RFC3339)
		}
		if plan.NextBillingDate != nil {
			planResponse["next_billing_date"] = plan.NextBillingDate.Format(time.RFC3339)
		}
		if plan.TrialEndsAt != nil {
			planResponse["trial_ends_at"] = plan.TrialEndsAt.Format(time.RFC3339)
		}

		// Build usage response
		usageResponse := map[string]interface{}{
			"monthly": map[string]interface{}{
				"ai_reply_count":         monthlyUsage.AIReplyCount,
				"ai_tokens_used":         monthlyUsage.AITokensUsed,
				"whatsapp_message_count": monthlyUsage.WhatsAppMessageCount,
				"post_count":             monthlyUsage.PostCount,
				"total_cost_usd":         monthlyUsage.TotalCostUSD,
			},
		}

		if usageStatus != nil {
			usageResponse["daily"] = map[string]interface{}{
				"ai_reply": map[string]interface{}{
					"used":    usageStatus.AIReply.Used,
					"limit":   usageStatus.AIReply.Limit,
					"percent": usageStatus.AIReply.Percent,
					"status":  usageStatus.AIReply.Status,
				},
				"whatsapp_message": map[string]interface{}{
					"used":    usageStatus.WhatsAppMessage.Used,
					"limit":   usageStatus.WhatsAppMessage.Limit,
					"percent": usageStatus.WhatsAppMessage.Percent,
					"status":  usageStatus.WhatsAppMessage.Status,
				},
				"post": map[string]interface{}{
					"used":    usageStatus.Post.Used,
					"limit":   usageStatus.Post.Limit,
					"percent": usageStatus.Post.Percent,
					"status":  usageStatus.Post.Status,
				},
			}
		}

		response := map[string]interface{}{
			"plan":  planResponse,
			"usage": usageResponse,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// UpdateTenantPlanQuotasHandler allows admin to override tenant quotas
func UpdateTenantPlanQuotasHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		tenantIDStr := chi.URLParam(r, "tenant_id")
		tenantID, err := uuid.Parse(tenantIDStr)
		if err != nil {
			http.Error(w, "invalid tenant id", http.StatusBadRequest)
			return
		}

		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Convert to quotas map
		quotas := make(map[string]interface{})
		if aiQuota, ok := req["ai_reply_quota"].(float64); ok {
			quotas["ai_reply_quota"] = int(aiQuota)
		}
		if waQuota, ok := req["whatsapp_message_quota"].(float64); ok {
			quotas["whatsapp_message_quota"] = int(waQuota)
		}
		if postQuota, ok := req["post_quota"].(float64); ok {
			quotas["post_quota"] = int(postQuota)
		}
		if storageLimit, ok := req["storage_limit_mb"].(float64); ok {
			quotas["storage_limit_mb"] = int(storageLimit)
		}
		if apiLimit, ok := req["api_calls_limit"].(float64); ok {
			quotas["api_calls_limit"] = int(apiLimit)
		}
		if platformsLimit, ok := req["platforms_limit"].(float64); ok {
			quotas["platforms_limit"] = int(platformsLimit)
		}

		billingService := billing.NewBillingService(ctx.DB, ctx.Logger)
		if err := billingService.UpdateTenantPlanQuotas(reqCtx, tenantID, quotas); err != nil {
			if err == billing.ErrTenantPlanNotFound {
				http.Error(w, "tenant plan not found", http.StatusNotFound)
				return
			}
			ctx.Logger.Error("failed to update tenant plan quotas", zap.Error(err))
			http.Error(w, "failed to update quotas", http.StatusInternalServerError)
			return
		}

		// Audit log
		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.quota_override",
			TenantID:   &tenantID,
			Metadata: map[string]interface{}{
				"quotas": quotas,
			},
		}
		if err := auditWriter(reqCtx, ctx, entry); err != nil {
			ctx.Logger.Warn("failed to log audit entry", zap.Error(err))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
	}
}

// UpdateTenantPlanPriceHandler allows admin to override tenant price
func UpdateTenantPlanPriceHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		tenantIDStr := chi.URLParam(r, "tenant_id")
		tenantID, err := uuid.Parse(tenantIDStr)
		if err != nil {
			http.Error(w, "invalid tenant id", http.StatusBadRequest)
			return
		}

		var req struct {
			Price float64 `json:"price"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		billingService := billing.NewBillingService(ctx.DB, ctx.Logger)
		if err := billingService.UpdateTenantPlanPrice(reqCtx, tenantID, req.Price); err != nil {
			if err == billing.ErrTenantPlanNotFound {
				http.Error(w, "tenant plan not found", http.StatusNotFound)
				return
			}
			ctx.Logger.Error("failed to update tenant plan price", zap.Error(err))
			http.Error(w, "failed to update price", http.StatusInternalServerError)
			return
		}

		// Audit log
		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.price_override",
			TenantID:   &tenantID,
			Metadata: map[string]interface{}{
				"price": req.Price,
			},
		}
		if err := auditWriter(reqCtx, ctx, entry); err != nil {
			ctx.Logger.Warn("failed to log audit entry", zap.Error(err))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
	}
}

// PauseTenantPlanHandler pauses a tenant's plan
func PauseTenantPlanHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		tenantIDStr := chi.URLParam(r, "tenant_id")
		tenantID, err := uuid.Parse(tenantIDStr)
		if err != nil {
			http.Error(w, "invalid tenant id", http.StatusBadRequest)
			return
		}

		billingService := billing.NewBillingService(ctx.DB, ctx.Logger)
		if err := billingService.PauseTenantPlan(reqCtx, tenantID); err != nil {
			if err == billing.ErrTenantPlanNotFound {
				http.Error(w, "tenant plan not found", http.StatusNotFound)
				return
			}
			ctx.Logger.Error("failed to pause tenant plan", zap.Error(err))
			http.Error(w, "failed to pause plan", http.StatusInternalServerError)
			return
		}

		// Audit log
		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.plan_pause",
			TenantID:   &tenantID,
		}
		if err := auditWriter(reqCtx, ctx, entry); err != nil {
			ctx.Logger.Warn("failed to log audit entry", zap.Error(err))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
	}
}

// ResumeTenantPlanHandler resumes a tenant's plan
func ResumeTenantPlanHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		tenantIDStr := chi.URLParam(r, "tenant_id")
		tenantID, err := uuid.Parse(tenantIDStr)
		if err != nil {
			http.Error(w, "invalid tenant id", http.StatusBadRequest)
			return
		}

		billingService := billing.NewBillingService(ctx.DB, ctx.Logger)
		if err := billingService.ResumeTenantPlan(reqCtx, tenantID); err != nil {
			if err == billing.ErrTenantPlanNotFound {
				http.Error(w, "tenant plan not found", http.StatusNotFound)
				return
			}
			ctx.Logger.Error("failed to resume tenant plan", zap.Error(err))
			http.Error(w, "failed to resume plan", http.StatusInternalServerError)
			return
		}

		// Audit log
		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.plan_resume",
			TenantID:   &tenantID,
		}
		if err := auditWriter(reqCtx, ctx, entry); err != nil {
			ctx.Logger.Warn("failed to log audit entry", zap.Error(err))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
	}
}
