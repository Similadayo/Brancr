package tenantapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/notifications"
	"github.com/similadayo/brancr/internal/receipt"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/webhooks"
	"go.uber.org/zap"
)

// =======================
// Order Management
// =======================

// listOrders lists all orders for the tenant
func (h *Handler) listOrders(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Get query parameters
	status := r.URL.Query().Get("status")
	platform := r.URL.Query().Get("platform")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 20 // Default limit
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
	if platform != "" {
		query = query.Where("platform = ?", platform)
	}

	var orders []storage.Order
	var total int64

	// Get total count
	query.Model(&storage.Order{}).Count(&total)

	// Get orders
	if err := query.Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&orders).Error; err != nil {
		h.Logger.Error("failed to fetch orders", zap.Error(err))
		http.Error(w, "failed to fetch orders", http.StatusInternalServerError)
		return
	}

	type orderResp struct {
		ID               uint                `json:"id"`
		OrderNumber      string              `json:"order_number"`
		PaymentReference string              `json:"payment_reference"`
		CustomerName     string              `json:"customer_name"`
		CustomerPhone    string              `json:"customer_phone"`
		CustomerEmail    string              `json:"customer_email"`
		TotalAmount      float64             `json:"total_amount"`
		Currency         string              `json:"currency"`
		Status           string              `json:"status"`
		Platform         string              `json:"platform"`
		Items            []storage.OrderItem `json:"items"`
		CreatedAt        string              `json:"created_at"`
		ConfirmedAt      *string             `json:"confirmed_at,omitempty"`
		CompletedAt      *string             `json:"completed_at,omitempty"`
	}

	resp := make([]orderResp, len(orders))
	for i, order := range orders {
		var items []storage.OrderItem
		if order.Items != "" {
			json.Unmarshal([]byte(order.Items), &items)
		}

		var confirmedAt, completedAt *string
		if order.ConfirmedAt != nil {
			confirmed := order.ConfirmedAt.Format("2006-01-02T15:04:05Z")
			confirmedAt = &confirmed
		}
		if order.CompletedAt != nil {
			completed := order.CompletedAt.Format("2006-01-02T15:04:05Z")
			completedAt = &completed
		}

		resp[i] = orderResp{
			ID:               order.ID,
			OrderNumber:      order.OrderNumber,
			PaymentReference: order.PaymentReference,
			CustomerName:     order.CustomerName,
			CustomerPhone:    order.CustomerPhone,
			CustomerEmail:    order.CustomerEmail,
			TotalAmount:      order.TotalAmount,
			Currency:         order.Currency,
			Status:           order.Status,
			Platform:         order.Platform,
			Items:            items,
			CreatedAt:        order.CreatedAt.Format("2006-01-02T15:04:05Z"),
			ConfirmedAt:      confirmedAt,
			CompletedAt:      completedAt,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"orders": resp,
		"count":  len(resp),
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// getOrder gets a single order
func (h *Handler) getOrder(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid order id", http.StatusBadRequest)
		return
	}

	var order storage.Order
	if err := h.DB.Where("id = ? AND tenant_id = ?", id, claims.TenantID).
		First(&order).Error; err != nil {
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}

	var items []storage.OrderItem
	if order.Items != "" {
		json.Unmarshal([]byte(order.Items), &items)
	}

	var confirmedAt, completedAt *string
	if order.ConfirmedAt != nil {
		confirmed := order.ConfirmedAt.Format("2006-01-02T15:04:05Z")
		confirmedAt = &confirmed
	}
	if order.CompletedAt != nil {
		completed := order.CompletedAt.Format("2006-01-02T15:04:05Z")
		completedAt = &completed
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"order": map[string]any{
			"id":                   order.ID,
			"order_number":         order.OrderNumber,
			"payment_reference":    order.PaymentReference,
			"customer_name":        order.CustomerName,
			"customer_phone":       order.CustomerPhone,
			"customer_email":       order.CustomerEmail,
			"total_amount":         order.TotalAmount,
			"currency":             order.Currency,
			"status":               order.Status,
			"platform":             order.Platform,
			"items":                items,
			"payment_instructions": order.PaymentInstructions,
			"notes":                order.Notes,
			"created_at":           order.CreatedAt.Format("2006-01-02T15:04:05Z"),
			"confirmed_at":         confirmedAt,
			"completed_at":         completedAt,
		},
	})
}

// updateOrder updates an order
func (h *Handler) updateOrder(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid order id", http.StatusBadRequest)
		return
	}

	var order storage.Order
	if err := h.DB.Where("id = ? AND tenant_id = ?", id, claims.TenantID).
		First(&order).Error; err != nil {
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}

	var req struct {
		Status *string `json:"status"` // pending, confirmed, processing, completed, cancelled
		Notes  *string `json:"notes"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Status != nil {
		validStatuses := map[string]bool{
			"pending":    true,
			"confirmed":  true,
			"processing": true,
			"completed":  true,
			"cancelled":  true,
		}
		if !validStatuses[*req.Status] {
			http.Error(w, "invalid status", http.StatusBadRequest)
			return
		}
		oldStatus := order.Status
		order.Status = *req.Status

		// Update timestamps based on status
		now := time.Now()
		if *req.Status == "confirmed" && order.ConfirmedAt == nil {
			order.ConfirmedAt = &now
		}
		if *req.Status == "completed" && order.CompletedAt == nil {
			order.CompletedAt = &now
		}

		// Save order first
		if err := h.DB.Save(&order).Error; err != nil {
			h.Logger.Error("failed to update order", zap.Error(err))
			http.Error(w, "failed to update order", http.StatusInternalServerError)
			return
		}

		ns := notifications.NewOrderNotificationService(h.DB, h.Logger)
		if h.EmailService != nil {
			ns.SetEmailService(h.EmailService)
		}
		ns.SetWebhookService(webhooks.NewWebhookService(h.DB, h.Logger))
		_ = ns.NotifyOrderStatusChange(r.Context(), &order, oldStatus, order.Status)
	} else {
		// No status change, just update notes
		if req.Notes != nil {
			order.Notes = *req.Notes
		}

		if err := h.DB.Save(&order).Error; err != nil {
			h.Logger.Error("failed to update order", zap.Error(err))
			http.Error(w, "failed to update order", http.StatusInternalServerError)
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"order": map[string]any{
			"id":     order.ID,
			"status": order.Status,
			"notes":  order.Notes,
		},
	})
}

// confirmOrderPayment confirms that payment was received for an order
func (h *Handler) confirmOrderPayment(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid order id", http.StatusBadRequest)
		return
	}

	var order storage.Order
	if err := h.DB.Where("id = ? AND tenant_id = ?", id, claims.TenantID).
		First(&order).Error; err != nil {
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}

	var req struct {
		PaymentReference string `json:"payment_reference"`
		Notes            string `json:"notes"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Verify payment reference matches
	if req.PaymentReference != "" && req.PaymentReference != order.PaymentReference {
		http.Error(w, "payment reference does not match", http.StatusBadRequest)
		return
	}

	// Update order status
	now := time.Now()
	oldStatus := order.Status
	order.Status = "completed"
	order.ConfirmedAt = &now
	order.CompletedAt = &now
	if req.Notes != "" {
		if order.Notes != "" {
			order.Notes += "\n\n" + req.Notes
		} else {
			order.Notes = req.Notes
		}
	}

	if err := h.DB.Save(&order).Error; err != nil {
		h.Logger.Error("failed to confirm payment", zap.Error(err))
		http.Error(w, "failed to confirm payment", http.StatusInternalServerError)
		return
	}

	ns := notifications.NewOrderNotificationService(h.DB, h.Logger)
	if h.EmailService != nil {
		ns.SetEmailService(h.EmailService)
	}
	ns.SetWebhookService(webhooks.NewWebhookService(h.DB, h.Logger))
	_ = ns.NotifyOrderStatusChange(r.Context(), &order, oldStatus, order.Status)

	// Update or create payment record
	var payment storage.Payment
	if err := h.DB.Where("order_id = ?", order.ID).First(&payment).Error; err != nil {
		// Create new payment record
		payment = storage.Payment{
			TenantID:           claims.TenantID,
			OrderID:            order.ID,
			CustomerID:         order.CustomerID,
			PaymentReference:   order.PaymentReference,
			Amount:             order.TotalAmount,
			Currency:           order.Currency,
			PaymentMethod:      "manual", // Tenant manually confirmed
			Status:             "verified",
			VerificationStatus: "tenant_verified",
			VerifiedAt:         &now,
		}
		h.DB.Create(&payment)
	} else {
		// Update existing payment
		payment.Status = "verified"
		payment.VerificationStatus = "tenant_verified"
		payment.VerifiedAt = &now
		h.DB.Save(&payment)
	}

	// Generate receipt automatically when payment is confirmed
	cloudinaryURL := h.getCloudinaryURL()
	publicURL := h.Config.PublicURL
	if publicURL == "" {
		publicURL = "https://api.brancr.com" // Default fallback
	}
	receiptService := receipt.NewReceiptService(h.DB, h.Logger, cloudinaryURL, publicURL, h.EmailService)
	_, err = receiptService.GenerateReceipt(r.Context(), payment.ID, claims.TenantID)
	if err != nil {
		h.Logger.Warn("failed to auto-generate receipt", zap.Error(err), zap.Uint("payment_id", payment.ID))
		// Don't fail the request if receipt generation fails - tenant can generate manually
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "payment confirmed",
		"order": map[string]any{
			"id":     order.ID,
			"status": order.Status,
		},
		"payment": map[string]any{
			"id":         payment.ID,
			"receipt_id": payment.ReceiptID,
		},
	})
}

// getOrderStats returns order statistics
func (h *Handler) getOrderStats(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var stats struct {
		TotalOrders       int64   `json:"total_orders"`
		PendingOrders     int64   `json:"pending_orders"`
		ConfirmedOrders   int64   `json:"confirmed_orders"`
		CompletedOrders   int64   `json:"completed_orders"`
		TotalRevenue      float64 `json:"total_revenue"`
		PendingRevenue    float64 `json:"pending_revenue"`
		AverageOrderValue float64 `json:"average_order_value"`
	}

	// Total orders
	h.DB.Model(&storage.Order{}).
		Where("tenant_id = ?", claims.TenantID).
		Count(&stats.TotalOrders)

	// Pending orders
	h.DB.Model(&storage.Order{}).
		Where("tenant_id = ? AND status = ?", claims.TenantID, "pending").
		Count(&stats.PendingOrders)

	// Confirmed orders
	h.DB.Model(&storage.Order{}).
		Where("tenant_id = ? AND status = ?", claims.TenantID, "confirmed").
		Count(&stats.ConfirmedOrders)

	// Completed orders
	h.DB.Model(&storage.Order{}).
		Where("tenant_id = ? AND status = ?", claims.TenantID, "completed").
		Count(&stats.CompletedOrders)

	// Total revenue (from completed orders)
	h.DB.Model(&storage.Order{}).
		Where("tenant_id = ? AND status = ?", claims.TenantID, "completed").
		Select("COALESCE(SUM(total_amount), 0)").
		Scan(&stats.TotalRevenue)

	// Pending revenue
	h.DB.Model(&storage.Order{}).
		Where("tenant_id = ? AND status = ?", claims.TenantID, "pending").
		Select("COALESCE(SUM(total_amount), 0)").
		Scan(&stats.PendingRevenue)

	// Average order value
	if stats.TotalOrders > 0 {
		var totalAmount float64
		h.DB.Model(&storage.Order{}).
			Where("tenant_id = ?", claims.TenantID).
			Select("COALESCE(SUM(total_amount), 0)").
			Scan(&totalAmount)
		stats.AverageOrderValue = totalAmount / float64(stats.TotalOrders)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"stats": stats,
	})
}
