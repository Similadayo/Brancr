package admin

import (
	"net/http"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

type orderRow struct {
	ID          uint
	TenantID    uint
	TenantName  string
	OrderNumber string
	Platform    string
	Status      string
	Amount      float64
	Currency    string
	CreatedAt   string
}

type paymentRow struct {
	ID           uint
	TenantID     uint
	TenantName   string
	PaymentRef   string
	OrderID      uint
	Status       string
	Verification string
	Amount       float64
	Currency     string
	CreatedAt    string
}

func OrdersHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var orders []storage.Order
		if err := ctx.DB.Order("created_at DESC").Limit(200).Find(&orders).Error; err != nil {
			ctx.Logger.Error("failed to fetch orders", zap.Error(err))
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}

		tenantIDs := make([]uint, 0, len(orders))
		seen := map[uint]struct{}{}
		for _, o := range orders {
			if _, ok := seen[o.TenantID]; !ok {
				seen[o.TenantID] = struct{}{}
				tenantIDs = append(tenantIDs, o.TenantID)
			}
		}

		tenantNames := map[uint]string{}
		if len(tenantIDs) > 0 {
			var tenants []storage.Tenant
			_ = ctx.DB.Where("id IN ?", tenantIDs).Find(&tenants).Error
			for _, t := range tenants {
				tenantNames[t.ID] = t.Name
			}
		}

		rows := make([]orderRow, 0, len(orders))
		for _, o := range orders {
			rows = append(rows, orderRow{
				ID:          o.ID,
				TenantID:    o.TenantID,
				TenantName:  tenantNames[o.TenantID],
				OrderNumber: o.OrderNumber,
				Platform:    o.Platform,
				Status:      o.Status,
				Amount:      o.TotalAmount,
				Currency:    o.Currency,
				CreatedAt:   o.CreatedAt.Format("Jan 2, 2006 15:04"),
			})
		}

		data := map[string]any{
			"Page":   "orders",
			"Orders": rows,
			"Total":  len(rows),
		}
		renderTemplate(w, r, "orders.html", data, ctx.Logger)
	}
}

func PaymentsHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payments []storage.Payment
		if err := ctx.DB.Order("created_at DESC").Limit(200).Find(&payments).Error; err != nil {
			ctx.Logger.Error("failed to fetch payments", zap.Error(err))
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}

		tenantIDs := make([]uint, 0, len(payments))
		seen := map[uint]struct{}{}
		for _, p := range payments {
			if _, ok := seen[p.TenantID]; !ok {
				seen[p.TenantID] = struct{}{}
				tenantIDs = append(tenantIDs, p.TenantID)
			}
		}

		tenantNames := map[uint]string{}
		if len(tenantIDs) > 0 {
			var tenants []storage.Tenant
			_ = ctx.DB.Where("id IN ?", tenantIDs).Find(&tenants).Error
			for _, t := range tenants {
				tenantNames[t.ID] = t.Name
			}
		}

		rows := make([]paymentRow, 0, len(payments))
		for _, p := range payments {
			rows = append(rows, paymentRow{
				ID:           p.ID,
				TenantID:     p.TenantID,
				TenantName:   tenantNames[p.TenantID],
				PaymentRef:   p.PaymentReference,
				OrderID:      p.OrderID,
				Status:       p.Status,
				Verification: p.VerificationStatus,
				Amount:       p.Amount,
				Currency:     p.Currency,
				CreatedAt:    p.CreatedAt.Format("Jan 2, 2006 15:04"),
			})
		}

		data := map[string]any{
			"Page":     "payments",
			"Payments": rows,
			"Total":    len(rows),
		}
		renderTemplate(w, r, "payments.html", data, ctx.Logger)
	}
}
