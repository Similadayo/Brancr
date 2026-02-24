package tenantapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// getUsage returns the tenant's current usage metrics
// GET /api/tenant/usage
func (h *Handler) getUsage(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Fetch tenant's plan for limits
	var tenantPlan storage.TenantPlan
	hasPlan := h.DB.Where("tenant_id = ?", claims.TenantID).First(&tenantPlan).Error == nil

	// Count conversations this month
	now := time.Now()
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	var conversationsUsed int64
	h.DB.Model(&storage.Conversation{}).
		Where("tenant_id = ? AND created_at >= ?", claims.TenantID, startOfMonth).
		Count(&conversationsUsed)

	// Count active seats (team members) - for now just count 1 (owner)
	activeSeats := 1

	response := map[string]any{
		"conversations": map[string]any{
			"used":  conversationsUsed,
			"limit": getPlanLimit(tenantPlan, hasPlan, "conversations"),
		},
		"active_seats": map[string]any{
			"used":  activeSeats,
			"limit": getPlanLimit(tenantPlan, hasPlan, "seats"),
		},
	}

	writeJSON(w, http.StatusOK, response)
}

// getBilling returns the tenant's plan and subscription info
// GET /api/tenant/billing
func (h *Handler) getBilling(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Fetch tenant
	var tenant storage.Tenant
	if err := h.DB.First(&tenant, claims.TenantID).Error; err != nil {
		h.Logger.Error("failed to fetch tenant", zap.Error(err), zap.Stringer("tenant_id", claims.TenantID))
		http.Error(w, "tenant not found", http.StatusNotFound)
		return
	}

	// Fetch tenant's plan
	var tenantPlan storage.TenantPlan
	hasPlan := h.DB.Where("tenant_id = ?", claims.TenantID).First(&tenantPlan).Error == nil

	response := map[string]any{
		"plan":         buildPlanInfo(tenant, tenantPlan, hasPlan),
		"trial":        buildTrialInfo(tenant),
		"subscription": buildSubscriptionInfo(tenant, tenantPlan, hasPlan),
	}

	writeJSON(w, http.StatusOK, response)
}

// getBillingSummary returns BOTH plan and usage (combined endpoint)
// GET /api/billing/summary
func (h *Handler) getBillingSummary(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Fetch tenant
	var tenant storage.Tenant
	if err := h.DB.First(&tenant, claims.TenantID).Error; err != nil {
		h.Logger.Error("failed to fetch tenant", zap.Error(err), zap.Stringer("tenant_id", claims.TenantID))
		http.Error(w, "tenant not found", http.StatusNotFound)
		return
	}

	// Fetch tenant's plan (TenantPlan is the source of truth)
	var tenantPlan storage.TenantPlan
	hasPlan := h.DB.Where("tenant_id = ?", claims.TenantID).First(&tenantPlan).Error == nil

	// Fetch current usage (count conversations this month)
	now := time.Now()
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	var conversationsUsed int64
	h.DB.Model(&storage.Conversation{}).
		Where("tenant_id = ? AND created_at >= ?", claims.TenantID, startOfMonth).
		Count(&conversationsUsed)

	// Count active seats (team members) - for now just count 1 (owner)
	activeSeats := 1

	// Build response
	response := map[string]any{
		"plan":  buildPlanInfo(tenant, tenantPlan, hasPlan),
		"trial": buildTrialInfo(tenant),
		"usage": map[string]any{
			"conversations": map[string]any{
				"used":  conversationsUsed,
				"limit": getPlanLimit(tenantPlan, hasPlan, "conversations"),
			},
			"active_seats": map[string]any{
				"used":  activeSeats,
				"limit": getPlanLimit(tenantPlan, hasPlan, "seats"),
			},
		},
		"subscription": buildSubscriptionInfo(tenant, tenantPlan, hasPlan),
	}

	writeJSON(w, http.StatusOK, response)
}

// buildPlanInfo returns plan details for the response
func buildPlanInfo(tenant storage.Tenant, plan storage.TenantPlan, hasPlan bool) map[string]any {
	planName := tenant.PlanType
	if planName == "" {
		planName = "trial"
	}

	// Default values for trial
	price := float64(0)
	currency := "USD"
	billingPeriod := "monthly"
	features := []string{
		"AI-powered customer replies",
		"WhatsApp, Instagram, Facebook integrations",
		"Basic analytics",
	}

	if hasPlan {
		price = plan.MonthlyPrice
		planName = plan.PlanName

		// Build features list based on plan
		features = []string{}
		if plan.AIReplyQuota > 0 {
			features = append(features, "AI assisted replies")
		}
		features = append(features, "WhatsApp, Instagram, Facebook integrations")
		if plan.AIReplyQuota > 1000 {
			features = append(features, "Priority support")
		}
	}

	return map[string]any{
		"type":           planName,
		"name":           formatPlanName(planName),
		"price":          price,
		"currency":       currency,
		"billing_period": billingPeriod,
		"features":       features,
	}
}

// buildTrialInfo returns trial status information
func buildTrialInfo(tenant storage.Tenant) map[string]any {
	isTrial := tenant.PlanType == "trial" || tenant.PlanType == ""

	var daysRemaining int
	var endsAt *time.Time

	if isTrial && tenant.TrialEndsAt != nil {
		endsAt = tenant.TrialEndsAt
		remaining := time.Until(*tenant.TrialEndsAt)
		if remaining > 0 {
			daysRemaining = int(remaining.Hours() / 24)
		}
	} else if isTrial {
		// No trial end date set, default to 30 days from creation
		defaultEnd := tenant.CreatedAt.Add(30 * 24 * time.Hour)
		endsAt = &defaultEnd
		remaining := time.Until(defaultEnd)
		if remaining > 0 {
			daysRemaining = int(remaining.Hours() / 24)
		}
	}

	return map[string]any{
		"is_trial":       isTrial,
		"days_remaining": daysRemaining,
		"ends_at":        endsAt,
	}
}

// buildSubscriptionInfo returns subscription status
func buildSubscriptionInfo(tenant storage.Tenant, plan storage.TenantPlan, hasPlan bool) map[string]any {
	status := "trial"
	if hasPlan {
		status = plan.BillingStatus
	}
	if tenant.Status == "suspended" {
		status = "suspended"
	}

	result := map[string]any{
		"status":     status,
		"expires_at": tenant.SubscriptionExpiresAt,
	}

	// Add last payment info if available
	if tenant.SubscriptionLastPaymentAt != nil && tenant.SubscriptionLastPaymentAmount != nil {
		result["last_payment"] = map[string]any{
			"amount":   *tenant.SubscriptionLastPaymentAmount,
			"currency": "USD",
			"date":     tenant.SubscriptionLastPaymentAt,
		}
	}

	return result
}

// getPlanLimit returns the limit for a specific resource
func getPlanLimit(plan storage.TenantPlan, hasPlan bool, resource string) int {
	if !hasPlan {
		// Default trial limits
		switch resource {
		case "conversations":
			return 500
		case "seats":
			return 2
		case "ai_requests":
			return 1000
		default:
			return 100
		}
	}

	switch resource {
	case "conversations":
		if plan.WhatsAppMessageQuota > 0 {
			return plan.WhatsAppMessageQuota
		}
		return 5000 // Default for paid plans
	case "seats":
		if plan.PlatformsLimit > 0 {
			return plan.PlatformsLimit
		}
		return 10
	case "ai_requests":
		if plan.AIReplyQuota > 0 {
			return plan.AIReplyQuota
		}
		return plan.APICallsLimit
	default:
		return 100
	}
}

// formatPlanName converts plan code to display name
func formatPlanName(code string) string {
	names := map[string]string{
		"trial":      "Trial",
		"starter":    "Starter Plan",
		"growth":     "Growth Plan",
		"pro":        "Pro Plan",
		"enterprise": "Enterprise",
	}
	if name, ok := names[code]; ok {
		return name
	}
	return code
}

// updateBillingPlan updates tenant's plan (for future payment integration)
// POST /api/billing/plan
func (h *Handler) updateBillingPlan(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		PlanCode string `json:"plan_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// For now, just log the upgrade request (payment integration to come)
	h.Logger.Info("plan upgrade requested",
		zap.Stringer("tenant_id", claims.TenantID),
		zap.String("requested_plan", req.PlanCode))

	writeJSON(w, http.StatusOK, map[string]any{
		"success":        true,
		"message":        "Plan upgrade request received. Payment integration coming soon!",
		"requested_plan": req.PlanCode,
	})
}
