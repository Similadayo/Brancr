package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

// PaymentsResponse represents tenant-to-Brancr payments (subscriptions)
type PaymentsResponse struct {
	Total int64              `json:"total"`
	Items []PaymentDetail    `json:"items"`
	Summary PaymentSummary   `json:"summary"`
}

// PaymentDetail represents a tenant payment to Brancr
type PaymentDetail struct {
	ID             uint       `json:"id"`
	TenantID       uint       `json:"tenant_id"`
	TenantName     string     `json:"tenant_name"`
	Amount         float64    `json:"amount"`
	Currency       string     `json:"currency"`
	Status         string     `json:"status"` // pending, completed, failed, refunded
	PaymentMethod  string     `json:"payment_method"`
	InvoiceID      string     `json:"invoice_id,omitempty"`
	SubscriptionID string     `json:"subscription_id,omitempty"`
	PlanType       string     `json:"plan_type"`
	PeriodStart    *time.Time `json:"period_start,omitempty"`
	PeriodEnd      *time.Time `json:"period_end,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	FailedAt       *time.Time `json:"failed_at,omitempty"`
	Error          string     `json:"error,omitempty"`
}

// PaymentSummary represents payment statistics
type PaymentSummary struct {
	TotalRevenue      float64 `json:"total_revenue"`
	ThisMonthRevenue  float64 `json:"this_month_revenue"`
	FailedPayments    int64   `json:"failed_payments"`
	PendingPayments   int64   `json:"pending_payments"`
	ActiveSubscriptions int64 `json:"active_subscriptions"`
}

// PaymentsAPIHandler returns tenant-to-Brancr payments (subscriptions, not customer payments)
func PaymentsAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		// Get filters
		status := strings.TrimSpace(r.URL.Query().Get("status"))
		tenantIDStr := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
		period := strings.TrimSpace(r.URL.Query().Get("period"))
		if period == "" {
			period = "30d"
		}

		data, err := BuildPaymentsData(ctx, status, tenantIDStr, period, reqCtx)
		if err != nil {
			ctx.Logger.Error("failed to build payments data", zap.Error(err))
			http.Error(w, "failed to build payments data", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(data); err != nil {
			ctx.Logger.Error("failed to encode payments data", zap.Error(err))
			http.Error(w, "encoding error", http.StatusInternalServerError)
			return
		}
	}
}

// BuildPaymentsData builds tenant-to-Brancr payment data
// NOTE: This is for tenant subscriptions/payments to Brancr, NOT customer payments
func BuildPaymentsData(ctx *AdminContext, status, tenantIDStr, period string, reqCtx context.Context) (*PaymentsResponse, error) {
	// Calculate time period (for future use when subscription tracking is implemented)
	_ = period // TODO: Use when implementing subscription tracking

	// TODO: When tenant subscription/payment tracking is implemented, query it here
	// For now, return empty data structure - this will need to be implemented when
	// we have tenant subscription/payment tracking model
	
	// Placeholder: This would query TenantSubscription or TenantPayment model
	// to track payments from tenants to Brancr for subscriptions/usage

	response := &PaymentsResponse{
		Total: 0,
		Items: []PaymentDetail{},
		Summary: PaymentSummary{},
	}

	return response, nil
}

