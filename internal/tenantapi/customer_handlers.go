package tenantapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

type customerListRow struct {
	ID            uuid.UUID `json:"id"`
	Channel       string    `json:"channel"`
	PlatformID    string    `json:"platform_id"`
	DisplayName   string    `json:"display_name"`
	Username      string    `json:"username"`
	ProfilePicURL string    `json:"profile_pic_url"`
	LastMessageAt string    `json:"last_message_at"`
	CreatedAt     string    `json:"created_at"`
	TotalPaid     int64     `json:"total_paid"`
	LastPaidAt    *string   `json:"last_paid_at"`
}

type customerListRowDB struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	Channel       string
	PlatformID    string
	DisplayName   string
	Username      string
	ProfilePicURL string
	LastMessageAt time.Time
	CreatedAt     time.Time
	TotalPaid     int64
	LastPaidAt    *time.Time
}

func (h *Handler) listCustomers(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	tenantIDText := claims.TenantID.String()

	paidStr := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("paid")))
	paidOnly := paidStr == "true" || paidStr == "1" || paidStr == "yes"

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

	base := h.DB.Table("customers").
		Where("customers.tenant_id::text = ?", tenantIDText).
		Joins("LEFT JOIN payments ON payments.customer_id::text = customers.id::text AND payments.tenant_id::text = customers.tenant_id::text AND payments.status IN ?", []string{"verified", "confirmed"}).
		Select("customers.id, customers.tenant_id, customers.channel, customers.platform_id, customers.display_name, customers.username, customers.profile_pic_url, customers.last_message_at, customers.created_at, COUNT(payments.id) AS total_paid, MAX(payments.verified_at) AS last_paid_at").
		Group("customers.id")

	if paidOnly {
		base = base.Having("COUNT(payments.id) > 0")
	}

	var total int64
	if paidOnly {
		if err := h.DB.Raw(
			"SELECT COUNT(DISTINCT customer_id::text) FROM payments WHERE tenant_id::text = ? AND status IN ?",
			tenantIDText,
			[]string{"verified", "confirmed"},
		).Scan(&total).Error; err != nil {
			h.Logger.Error("failed to count paid customers", zap.Error(err))
			http.Error(w, "failed to fetch customers", http.StatusInternalServerError)
			return
		}
	} else {
		if err := h.DB.Raw(
			"SELECT COUNT(*) FROM customers WHERE tenant_id::text = ?",
			tenantIDText,
		).Scan(&total).Error; err != nil {
			h.Logger.Error("failed to count customers", zap.Error(err))
			http.Error(w, "failed to fetch customers", http.StatusInternalServerError)
			return
		}
	}

	var rows []customerListRowDB
	if err := base.Order("customers.last_message_at DESC").Limit(limit).Offset(offset).Scan(&rows).Error; err != nil {
		h.Logger.Error("failed to fetch customers", zap.Error(err))
		http.Error(w, "failed to fetch customers", http.StatusInternalServerError)
		return
	}

	out := make([]customerListRow, 0, len(rows))
	for _, row := range rows {
		var lastPaidAt *string
		if row.LastPaidAt != nil {
			s := row.LastPaidAt.UTC().Format("2006-01-02T15:04:05Z")
			lastPaidAt = &s
		}
		out = append(out, customerListRow{
			ID:            row.ID,
			Channel:       row.Channel,
			PlatformID:    row.PlatformID,
			DisplayName:   row.DisplayName,
			Username:      row.Username,
			ProfilePicURL: row.ProfilePicURL,
			TotalPaid:     row.TotalPaid,
			LastPaidAt:    lastPaidAt,
			LastMessageAt: row.LastMessageAt.UTC().Format("2006-01-02T15:04:05Z"),
			CreatedAt:     row.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"customers": out,
		"count":     len(out),
		"total":     total,
		"limit":     limit,
		"offset":    offset,
	})
}

type customerOrderRow struct {
	ID               uuid.UUID `json:"id"`
	OrderNumber      string    `json:"order_number"`
	PaymentReference string    `json:"payment_reference"`
	Status           string    `json:"status"`
	TotalAmount      float64   `json:"total_amount"`
	Currency         string    `json:"currency"`
	CreatedAt        string    `json:"created_at"`
	ConfirmedAt      *string   `json:"confirmed_at,omitempty"`
	CompletedAt      *string   `json:"completed_at,omitempty"`
}

type requirementSubmissionRowDB struct {
	ID                  uuid.UUID
	OrderID             uuid.UUID
	OrderItemID         string
	RequirementID       uuid.UUID
	RequirementLabel    string
	RequirementDataType string
	Value               string
	Verified            bool
	CreatedAt           time.Time
}

func (h *Handler) getCustomerProfile(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid customer id", http.StatusBadRequest)
		return
	}

	var customer storage.Customer
	if err := h.DB.Where("id = ? AND tenant_id = ?", id, claims.TenantID).First(&customer).Error; err != nil {
		http.Error(w, "customer not found", http.StatusNotFound)
		return
	}

	var orders []storage.Order
	if err := h.DB.Where("tenant_id = ? AND customer_id = ?", claims.TenantID, customer.ID).
		Order("created_at DESC").
		Find(&orders).Error; err != nil {
		h.Logger.Error("failed to fetch customer orders", zap.Error(err))
		http.Error(w, "failed to fetch customer", http.StatusInternalServerError)
		return
	}

	orderResp := make([]customerOrderRow, 0, len(orders))
	for _, o := range orders {
		var confirmedAt, completedAt *string
		if o.ConfirmedAt != nil {
			s := o.ConfirmedAt.UTC().Format("2006-01-02T15:04:05Z")
			confirmedAt = &s
		}
		if o.CompletedAt != nil {
			s := o.CompletedAt.UTC().Format("2006-01-02T15:04:05Z")
			completedAt = &s
		}
		orderResp = append(orderResp, customerOrderRow{
			ID:               o.ID,
			OrderNumber:      o.OrderNumber,
			PaymentReference: o.PaymentReference,
			Status:           o.Status,
			TotalAmount:      o.TotalAmount,
			Currency:         o.Currency,
			CreatedAt:        o.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			ConfirmedAt:      confirmedAt,
			CompletedAt:      completedAt,
		})
	}

	var latestPaid storage.Payment
	latestPaidFound := false
	if err := h.DB.Where("tenant_id = ? AND customer_id = ? AND status IN ?", claims.TenantID, customer.ID, []string{"verified", "confirmed"}).
		Order("verified_at DESC").
		Order("created_at DESC").
		First(&latestPaid).Error; err == nil {
		latestPaidFound = true
	}

	var submissions []requirementSubmissionRowDB
	if err := h.DB.Table("requirement_submissions rs").
		Select("rs.id, fs.order_id, fs.order_item_id, rs.requirement_id, r.label AS requirement_label, r.data_type AS requirement_data_type, rs.value, rs.verified, rs.created_at").
		Joins("JOIN fulfillment_states fs ON fs.id = rs.fulfillment_state_id").
		Joins("JOIN requirements r ON r.id = rs.requirement_id").
		Where("fs.tenant_id = ? AND fs.customer_id = ?", claims.TenantID, customer.ID).
		Order("rs.created_at DESC").
		Limit(500).
		Scan(&submissions).Error; err != nil {
		h.Logger.Error("failed to fetch customer requirement submissions", zap.Error(err))
		http.Error(w, "failed to fetch customer", http.StatusInternalServerError)
		return
	}

	reqResp := make([]map[string]any, 0, len(submissions))
	for _, s := range submissions {
		reqResp = append(reqResp, map[string]any{
			"id":                    s.ID,
			"order_id":              s.OrderID,
			"order_item_id":         s.OrderItemID,
			"requirement_id":        s.RequirementID,
			"requirement_label":     s.RequirementLabel,
			"requirement_data_type": s.RequirementDataType,
			"value":                 s.Value,
			"verified":              s.Verified,
			"created_at":            s.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}

	paymentSummary := map[string]any{
		"has_paid": false,
	}
	if latestPaidFound {
		paymentSummary["has_paid"] = true
		paymentSummary["payment_id"] = latestPaid.ID
		paymentSummary["status"] = latestPaid.Status
		paymentSummary["amount"] = latestPaid.Amount
		paymentSummary["currency"] = latestPaid.Currency
		if latestPaid.VerifiedAt != nil {
			paymentSummary["verified_at"] = latestPaid.VerifiedAt.UTC().Format("2006-01-02T15:04:05Z")
		} else {
			paymentSummary["verified_at"] = nil
		}
		paymentSummary["receipt_url"] = latestPaid.ReceiptURL
		paymentSummary["portal_url"] = latestPaid.PortalURL
		paymentSummary["portal_token"] = latestPaid.PortalToken
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"customer": map[string]any{
			"id":              customer.ID,
			"channel":         customer.Channel,
			"platform_id":     customer.PlatformID,
			"display_name":    customer.DisplayName,
			"username":        customer.Username,
			"profile_pic_url": customer.ProfilePicURL,
			"last_message_at": customer.LastMessageAt.UTC().Format("2006-01-02T15:04:05Z"),
			"created_at":      customer.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		},
		"payment_summary":         paymentSummary,
		"orders":                  orderResp,
		"requirement_submissions": reqResp,
	})
}
