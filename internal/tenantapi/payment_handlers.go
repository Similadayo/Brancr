package tenantapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/receipt"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// =======================
// Payment Management
// =======================

// listPayments lists all payments for the tenant
func (h *Handler) listPayments(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Get query parameters
	status := r.URL.Query().Get("status")
	verificationStatus := r.URL.Query().Get("verification_status")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 20
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	offset := 0
	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	query := h.DB.Where("tenant_id = ?", claims.TenantID)

	if status != "" {
		query = query.Where("status = ?", status)
	}
	if verificationStatus != "" {
		query = query.Where("verification_status = ?", verificationStatus)
	}

	var payments []storage.Payment
	var total int64

	query.Model(&storage.Payment{}).Count(&total)

	if err := query.Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&payments).Error; err != nil {
		h.Logger.Error("failed to fetch payments", zap.Error(err))
		http.Error(w, "failed to fetch payments", http.StatusInternalServerError)
		return
	}

	type paymentResp struct {
		ID                 uuid.UUID `json:"id"`
		OrderID            uuid.UUID `json:"order_id"`
		PaymentReference   string    `json:"payment_reference"`
		Amount             float64   `json:"amount"`
		Currency           string    `json:"currency"`
		PaymentMethod      string    `json:"payment_method"`
		Status             string    `json:"status"`
		VerificationStatus string    `json:"verification_status"`
		ClaimedByCustomer  bool      `json:"claimed_by_customer"`
		VerifiedAt         *string   `json:"verified_at,omitempty"`
		CreatedAt          string    `json:"created_at"`
	}

	resp := make([]paymentResp, len(payments))
	for i, payment := range payments {
		var verifiedAt *string
		if payment.VerifiedAt != nil {
			verified := payment.VerifiedAt.Format("2006-01-02T15:04:05Z")
			verifiedAt = &verified
		}

		resp[i] = paymentResp{
			ID:                 payment.ID,
			OrderID:            payment.OrderID,
			PaymentReference:   payment.PaymentReference,
			Amount:             payment.Amount,
			Currency:           payment.Currency,
			PaymentMethod:      payment.PaymentMethod,
			Status:             payment.Status,
			VerificationStatus: payment.VerificationStatus,
			ClaimedByCustomer:  payment.ClaimedByCustomer,
			VerifiedAt:         verifiedAt,
			CreatedAt:          payment.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"payments": resp,
		"count":    len(resp),
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	})
}

// getPayment gets a single payment
func (h *Handler) getPayment(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid payment id", http.StatusBadRequest)
		return
	}

	var payment storage.Payment
	if err := h.DB.Where("id = ? AND tenant_id = ?", id, claims.TenantID).
		First(&payment).Error; err != nil {
		http.Error(w, "payment not found", http.StatusNotFound)
		return
	}

	var verifiedAt, completedAt *string
	if payment.VerifiedAt != nil {
		verified := payment.VerifiedAt.Format("2006-01-02T15:04:05Z")
		verifiedAt = &verified
	}
	if payment.CompletedAt != nil {
		completed := payment.CompletedAt.Format("2006-01-02T15:04:05Z")
		completedAt = &completed
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"payment": map[string]any{
			"id":                  payment.ID,
			"order_id":            payment.OrderID,
			"payment_reference":   payment.PaymentReference,
			"amount":              payment.Amount,
			"currency":            payment.Currency,
			"payment_method":      payment.PaymentMethod,
			"status":              payment.Status,
			"verification_status": payment.VerificationStatus,
			"claimed_by_customer": payment.ClaimedByCustomer,
			"receipt_id":          payment.ReceiptID,
			"receipt_url":         payment.ReceiptURL,
			"portal_token":        payment.PortalToken,
			"portal_url":          payment.PortalURL,
			"verified_at":         verifiedAt,
			"completed_at":        completedAt,
			"created_at":          payment.CreatedAt.Format("2006-01-02T15:04:05Z"),
		},
	})
}

// verifyPayment verifies a payment
func (h *Handler) verifyPayment(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid payment id", http.StatusBadRequest)
		return
	}

	var payment storage.Payment
	if err := h.DB.Where("id = ? AND tenant_id = ?", id, claims.TenantID).
		First(&payment).Error; err != nil {
		http.Error(w, "payment not found", http.StatusNotFound)
		return
	}

	var req struct {
		Notes string `json:"notes"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	now := time.Now()
	payment.Status = "verified"
	payment.VerificationStatus = "tenant_verified"
	payment.VerifiedAt = &now
	// TODO: Get tenant's Telegram ID from claims
	// payment.VerifiedBy = &claims.TelegramID

	if err := h.DB.Save(&payment).Error; err != nil {
		h.Logger.Error("failed to verify payment", zap.Error(err))
		http.Error(w, "failed to verify payment", http.StatusInternalServerError)
		return
	}

	// Update order status if payment is verified
	var order storage.Order
	if err := h.DB.First(&order, payment.OrderID).Error; err == nil {
		if order.Status == "pending" {
			order.Status = "completed"
			order.ConfirmedAt = &now
			order.CompletedAt = &now
			h.DB.Save(&order)
		}
	}

	// Auto-generate receipt on the payment (best-effort)
	cloudinaryURL := h.getCloudinaryURL()
	publicURL := h.Config.PublicURL
	if publicURL == "" {
		publicURL = "https://api.brancr.com"
	}
	receiptService := receipt.NewReceiptService(h.DB, h.Logger, cloudinaryURL, publicURL, h.EmailService)
	if generated, err := receiptService.GenerateReceipt(r.Context(), payment.ID, claims.TenantID); err == nil {
		payment = *generated
	} else {
		h.Logger.Warn("failed to auto-generate receipt", zap.Error(err), zap.Stringer("payment_id", payment.ID))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "payment verified",
		"payment": map[string]any{
			"id":                  payment.ID,
			"status":              payment.Status,
			"verification_status": payment.VerificationStatus,
			"receipt_id":          payment.ReceiptID,
			"receipt_url":         payment.ReceiptURL,
		},
	})
}

// disputePayment marks a payment as disputed
func (h *Handler) disputePayment(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid payment id", http.StatusBadRequest)
		return
	}

	var payment storage.Payment
	if err := h.DB.Where("id = ? AND tenant_id = ?", id, claims.TenantID).
		First(&payment).Error; err != nil {
		http.Error(w, "payment not found", http.StatusNotFound)
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	payment.VerificationStatus = "disputed"
	if req.Reason != "" {
		var metadata map[string]interface{}
		if payment.Metadata != "" {
			json.Unmarshal([]byte(payment.Metadata), &metadata)
		} else {
			metadata = make(map[string]interface{})
		}
		metadata["dispute_reason"] = req.Reason
		metadataJSON, _ := json.Marshal(metadata)
		payment.Metadata = string(metadataJSON)
	}

	if err := h.DB.Save(&payment).Error; err != nil {
		h.Logger.Error("failed to dispute payment", zap.Error(err))
		http.Error(w, "failed to dispute payment", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "payment marked as disputed",
		"payment": map[string]any{
			"id":                  payment.ID,
			"verification_status": payment.VerificationStatus,
		},
	})
}

// getPaymentStats returns payment statistics for the dashboard
func (h *Handler) getPaymentStats(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var stats struct {
		TotalPayments    int64   `json:"total_payments"`
		PendingCount     int64   `json:"pending_count"`
		VerifiedCount    int64   `json:"verified_count"`
		ConfirmedCount   int64   `json:"confirmed_count"`
		FailedCount      int64   `json:"failed_count"`
		DisputedCount    int64   `json:"disputed_count"`
		TotalRevenue     float64 `json:"total_revenue"`
		PendingRevenue   float64 `json:"pending_revenue"`
		VerifiedRevenue  float64 `json:"verified_revenue"`
		ThisMonthRevenue float64 `json:"this_month_revenue"`
		LastMonthRevenue float64 `json:"last_month_revenue"`
		TodayPayments    int64   `json:"today_payments"`
		TodayRevenue     float64 `json:"today_revenue"`
	}

	// Total payments
	h.DB.Model(&storage.Payment{}).
		Where("tenant_id = ?", claims.TenantID).
		Count(&stats.TotalPayments)

	// Pending payments
	h.DB.Model(&storage.Payment{}).
		Where("tenant_id = ? AND status = ?", claims.TenantID, "pending").
		Count(&stats.PendingCount)

	// Verified payments
	h.DB.Model(&storage.Payment{}).
		Where("tenant_id = ? AND status = ?", claims.TenantID, "verified").
		Count(&stats.VerifiedCount)

	// Confirmed payments
	h.DB.Model(&storage.Payment{}).
		Where("tenant_id = ? AND status = ?", claims.TenantID, "confirmed").
		Count(&stats.ConfirmedCount)

	// Failed payments
	h.DB.Model(&storage.Payment{}).
		Where("tenant_id = ? AND status = ?", claims.TenantID, "failed").
		Count(&stats.FailedCount)

	// Disputed payments
	h.DB.Model(&storage.Payment{}).
		Where("tenant_id = ? AND verification_status = ?", claims.TenantID, "disputed").
		Count(&stats.DisputedCount)

	// Total revenue (verified + confirmed)
	h.DB.Model(&storage.Payment{}).
		Where("tenant_id = ? AND status IN ?", claims.TenantID, []string{"verified", "confirmed"}).
		Select("COALESCE(SUM(amount), 0)").
		Scan(&stats.TotalRevenue)

	// Pending revenue
	h.DB.Model(&storage.Payment{}).
		Where("tenant_id = ? AND status = ?", claims.TenantID, "pending").
		Select("COALESCE(SUM(amount), 0)").
		Scan(&stats.PendingRevenue)

	// Verified revenue (not yet completed)
	h.DB.Model(&storage.Payment{}).
		Where("tenant_id = ? AND status = ?", claims.TenantID, "verified").
		Select("COALESCE(SUM(amount), 0)").
		Scan(&stats.VerifiedRevenue)

	// This month revenue
	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	h.DB.Model(&storage.Payment{}).
		Where("tenant_id = ? AND status IN ? AND created_at >= ?",
			claims.TenantID, []string{"verified", "confirmed"}, monthStart).
		Select("COALESCE(SUM(amount), 0)").
		Scan(&stats.ThisMonthRevenue)

	// Last month revenue
	lastMonthStart := monthStart.AddDate(0, -1, 0)
	h.DB.Model(&storage.Payment{}).
		Where("tenant_id = ? AND status IN ? AND created_at >= ? AND created_at < ?",
			claims.TenantID, []string{"verified", "confirmed"}, lastMonthStart, monthStart).
		Select("COALESCE(SUM(amount), 0)").
		Scan(&stats.LastMonthRevenue)

	// Today's payments
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	h.DB.Model(&storage.Payment{}).
		Where("tenant_id = ? AND created_at >= ?", claims.TenantID, todayStart).
		Count(&stats.TodayPayments)

	// Today's revenue
	h.DB.Model(&storage.Payment{}).
		Where("tenant_id = ? AND status IN ? AND created_at >= ?",
			claims.TenantID, []string{"verified", "confirmed"}, todayStart).
		Select("COALESCE(SUM(amount), 0)").
		Scan(&stats.TodayRevenue)

	// Get recent payments (last 5)
	var recentPayments []storage.Payment
	h.DB.Where("tenant_id = ?", claims.TenantID).
		Order("created_at DESC").
		Limit(5).
		Find(&recentPayments)

	type recentPaymentResp struct {
		ID               uuid.UUID `json:"id"`
		PaymentReference string    `json:"payment_reference"`
		Amount           float64   `json:"amount"`
		Currency         string    `json:"currency"`
		Status           string    `json:"status"`
		CreatedAt        string    `json:"created_at"`
	}

	recent := make([]recentPaymentResp, len(recentPayments))
	for i, p := range recentPayments {
		recent[i] = recentPaymentResp{
			ID:               p.ID,
			PaymentReference: p.PaymentReference,
			Amount:           p.Amount,
			Currency:         p.Currency,
			Status:           p.Status,
			CreatedAt:        p.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"stats":           stats,
		"recent_payments": recent,
	})
}
