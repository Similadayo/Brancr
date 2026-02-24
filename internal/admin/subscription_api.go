package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/alerts"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/subscription"
	"go.uber.org/zap"
)

// ProcessSubscriptionPaymentRequest represents a request to process a subscription payment
type ProcessSubscriptionPaymentRequest struct {
	TenantID                 uuid.UUID `json:"tenant_id"`
	Amount                   float64   `json:"amount"`
	Currency                 string    `json:"currency"`
	PaymentMethod            string    `json:"payment_method"`
	SubscriptionDurationDays int       `json:"subscription_duration_days"` // e.g., 30 for monthly, 365 for yearly
}

// ProcessSubscriptionPaymentHandler processes a subscription payment and sends alerts
func ProcessSubscriptionPaymentHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		var req ProcessSubscriptionPaymentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Validate
		if req.TenantID == uuid.Nil {
			http.Error(w, "tenant_id is required", http.StatusBadRequest)
			return
		}
		if req.Amount <= 0 {
			http.Error(w, "amount must be greater than 0", http.StatusBadRequest)
			return
		}
		if req.SubscriptionDurationDays <= 0 {
			req.SubscriptionDurationDays = 30 // Default to 30 days
		}
		if req.Currency == "" {
			req.Currency = "USD" // Default currency
		}
		if req.PaymentMethod == "" {
			req.PaymentMethod = "unknown"
		}

		// Create subscription service with alert service if available
		var alertSvc *alerts.AlertService
		if ctx.AlertService != nil {
			if typed, ok := ctx.AlertService.(*alerts.AlertService); ok {
				alertSvc = typed
			}
		}
		subscriptionSvc := subscription.NewService(ctx.DB, ctx.Logger, alertSvc)

		// Process the subscription payment
		if err := subscriptionSvc.OnSubscriptionPayment(
			reqCtx,
			req.TenantID,
			req.Amount,
			req.Currency,
			req.PaymentMethod,
			req.SubscriptionDurationDays,
		); err != nil {
			ctx.Logger.Error("failed to process subscription payment",
				zap.Stringer("tenant_id", req.TenantID),
				zap.Error(err))
			http.Error(w, "failed to process subscription payment", http.StatusInternalServerError)
			return
		}

		// Audit log
		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "subscription.payment_processed",
			TenantID:   &req.TenantID,
			Metadata: map[string]interface{}{
				"amount":                     req.Amount,
				"currency":                   req.Currency,
				"payment_method":             req.PaymentMethod,
				"subscription_duration_days": req.SubscriptionDurationDays,
			},
		}
		if err := auditWriter(reqCtx, ctx, entry); err != nil {
			ctx.Logger.Warn("failed to log audit entry", zap.Error(err))
		}

		respondJSON(w, map[string]interface{}{
			"status":  "ok",
			"message": "subscription payment processed successfully",
		})
	}
}

// GetTenantsByPlanTypeHandler returns tenants filtered by plan type
func GetTenantsByPlanTypeHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		planType := r.URL.Query().Get("plan_type")
		status := r.URL.Query().Get("status")
		if status == "" {
			status = "active"
		}

		// Create subscription service with alert service if available
		var alertSvc *alerts.AlertService
		if ctx.AlertService != nil {
			if typed, ok := ctx.AlertService.(*alerts.AlertService); ok {
				alertSvc = typed
			}
		}
		subscriptionSvc := subscription.NewService(ctx.DB, ctx.Logger, alertSvc)

		tenants, err := subscriptionSvc.GetTenantsByPlanType(reqCtx, planType)
		if err != nil {
			ctx.Logger.Error("failed to get tenants by plan type", zap.Error(err))
			http.Error(w, "failed to get tenants", http.StatusInternalServerError)
			return
		}

		// Filter by status if specified
		if status != "" {
			filtered := make([]storage.Tenant, 0)
			for _, tenant := range tenants {
				if tenant.Status == status {
					filtered = append(filtered, tenant)
				}
			}
			tenants = filtered
		}

		// Return simplified tenant info
		type TenantInfo struct {
			ID                    uuid.UUID  `json:"id"`
			Name                  string     `json:"name"`
			Email                 string     `json:"email"`
			PlanType              string     `json:"plan_type"`
			Status                string     `json:"status"`
			SubscriptionExpiresAt *time.Time `json:"subscription_expires_at,omitempty"`
			TrialEndsAt           *time.Time `json:"trial_ends_at,omitempty"`
		}

		tenantInfos := make([]TenantInfo, len(tenants))
		for i, tenant := range tenants {
			tenantInfos[i] = TenantInfo{
				ID:                    tenant.ID,
				Name:                  tenant.Name,
				Email:                 tenant.Email,
				PlanType:              tenant.PlanType,
				Status:                tenant.Status,
				SubscriptionExpiresAt: tenant.SubscriptionExpiresAt,
				TrialEndsAt:           tenant.TrialEndsAt,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"total":   len(tenantInfos),
			"tenants": tenantInfos,
		})
	}
}
