package tenantapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/similadayo/brancr/internal/middleware"
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
		ID                 uint    `json:"id"`
		OrderID            uint    `json:"order_id"`
		PaymentReference   string  `json:"payment_reference"`
		Amount             float64 `json:"amount"`
		Currency           string  `json:"currency"`
		PaymentMethod      string  `json:"payment_method"`
		Status             string  `json:"status"`
		VerificationStatus string  `json:"verification_status"`
		ClaimedByCustomer  bool   `json:"claimed_by_customer"`
		VerifiedAt         *string `json:"verified_at,omitempty"`
		CreatedAt          string  `json:"created_at"`
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
	id, err := strconv.ParseUint(idStr, 10, 32)
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
			"id":                 payment.ID,
			"order_id":           payment.OrderID,
			"payment_reference":  payment.PaymentReference,
			"amount":             payment.Amount,
			"currency":           payment.Currency,
			"payment_method":     payment.PaymentMethod,
			"status":             payment.Status,
			"verification_status": payment.VerificationStatus,
			"claimed_by_customer": payment.ClaimedByCustomer,
			"receipt_id":         payment.ReceiptID,
			"receipt_url":        payment.ReceiptURL,
			"portal_token":       payment.PortalToken,
			"portal_url":         payment.PortalURL,
			"verified_at":        verifiedAt,
			"completed_at":       completedAt,
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
	id, err := strconv.ParseUint(idStr, 10, 32)
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
			order.Status = "confirmed"
			order.ConfirmedAt = &now
			h.DB.Save(&order)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "payment verified",
		"payment": map[string]any{
			"id":                 payment.ID,
			"status":             payment.Status,
			"verification_status": payment.VerificationStatus,
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
	id, err := strconv.ParseUint(idStr, 10, 32)
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
			"id":                 payment.ID,
			"verification_status": payment.VerificationStatus,
		},
	})
}

