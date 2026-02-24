package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// GetGupshupUsageHandler returns WhatsApp usage data for a tenant
// GET /admin/api/gupshup-usage?tenant_id={id}&from_date={YYYY-MM-DD}&to_date={YYYY-MM-DD}
func GetGupshupUsageHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get query parameters
		tenantIDStr := r.URL.Query().Get("tenant_id")
		fromDate := r.URL.Query().Get("from_date")
		toDate := r.URL.Query().Get("to_date")

		if tenantIDStr == "" {
			http.Error(w, "tenant_id is required", http.StatusBadRequest)
			return
		}

		tenantID, err := uuid.Parse(tenantIDStr)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}

		// Default to last 30 days if dates not provided
		if fromDate == "" {
			fromDate = time.Now().AddDate(0, 0, -30).Format("2006-01-02")
		}
		if toDate == "" {
			toDate = time.Now().Format("2006-01-02")
		}

		// Query usage data
		var usageList []storage.GupshupUsage
		query := ctx.DB.Where("tenant_id = ? AND date >= ? AND date <= ?", tenantID, fromDate, toDate).
			Order("date DESC")

		if err := query.Find(&usageList).Error; err != nil {
			ctx.Logger.Error("failed to query Gupshup usage", zap.Error(err))
			http.Error(w, "failed to query usage data", http.StatusInternalServerError)
			return
		}

		// Convert to response format
		response := make([]map[string]interface{}, len(usageList))
		for i, usage := range usageList {
			response[i] = map[string]interface{}{
				"id":                           usage.ID,
				"date":                         usage.Date,
				"gupshup_app_id":               usage.GupshupAppID,
				"authentication":               usage.Authentication,
				"cumulative_bill":              usage.CumulativeBill,
				"currency":                     usage.Currency,
				"discount":                     usage.Discount,
				"fep":                          usage.FEP,
				"ftc":                          usage.FTC,
				"gs_cap":                       usage.GSCap,
				"gs_fees":                      usage.GSFees,
				"incoming_msg":                 usage.IncomingMsg,
				"outgoing_msg":                 usage.OutgoingMsg,
				"outgoing_media_msg":           usage.OutgoingMediaMsg,
				"marketing":                    usage.Marketing,
				"service":                      usage.Service,
				"template_msg":                 usage.TemplateMsg,
				"template_media_msg":           usage.TemplateMediaMsg,
				"total_fees":                   usage.TotalFees,
				"total_msg":                    usage.TotalMsg,
				"utility":                      usage.Utility,
				"wa_fees":                      usage.WAFees,
				"international_authentication": usage.InternationalAuthentication,
				"created_at":                   usage.CreatedAt.Format(time.RFC3339),
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"usage":     response,
			"from_date": fromDate,
			"to_date":   toDate,
			"count":     len(response),
		}); err != nil {
			ctx.Logger.Error("failed to encode usage response", zap.Error(err))
			http.Error(w, "failed to encode response", http.StatusInternalServerError)
			return
		}
	}
}

// GetGupshupDiscountHandler returns discount data for a tenant
// GET /admin/api/gupshup-discount?tenant_id={id}&year={YYYY}&month={MM}
func GetGupshupDiscountHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get query parameters
		tenantIDStr := r.URL.Query().Get("tenant_id")
		yearStr := r.URL.Query().Get("year")
		monthStr := r.URL.Query().Get("month")

		if tenantIDStr == "" {
			http.Error(w, "tenant_id is required", http.StatusBadRequest)
			return
		}

		tenantID, err := uuid.Parse(tenantIDStr)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}

		// Default to current month if not provided
		now := time.Now()
		year := now.Year()
		month := int(now.Month())

		if yearStr != "" {
			if y, err := strconv.Atoi(yearStr); err == nil {
				year = y
			}
		}
		if monthStr != "" {
			if m, err := strconv.Atoi(monthStr); err == nil && m >= 1 && m <= 12 {
				month = m
			}
		}

		// Query discount data
		var discountList []storage.GupshupDiscount
		query := ctx.DB.Where("tenant_id = ? AND year = ? AND month = ?", tenantID, year, month).
			Order("day ASC")

		if err := query.Find(&discountList).Error; err != nil {
			ctx.Logger.Error("failed to query Gupshup discount", zap.Error(err))
			http.Error(w, "failed to query discount data", http.StatusInternalServerError)
			return
		}

		// Convert to response format
		response := make([]map[string]interface{}, len(discountList))
		for i, discount := range discountList {
			response[i] = map[string]interface{}{
				"id":              discount.ID,
				"gupshup_app_id":  discount.GupshupAppID,
				"year":            discount.Year,
				"month":           discount.Month,
				"day":             discount.Day,
				"cumulative_bill": discount.CumulativeBill,
				"daily_bill":      discount.DailyBill,
				"discount":        discount.Discount,
				"gs_cap":          discount.GSCap,
				"gs_fees":         discount.GSFees,
				"partner_id":      discount.PartnerID,
				"created_at":      discount.CreatedAt.Format(time.RFC3339),
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"discounts": response,
			"year":      year,
			"month":     month,
			"count":     len(response),
		}); err != nil {
			ctx.Logger.Error("failed to encode discount response", zap.Error(err))
			http.Error(w, "failed to encode response", http.StatusInternalServerError)
			return
		}
	}
}

// SyncGupshupUsageHandler triggers a manual sync of usage data
// POST /admin/api/gupshup-usage/sync
// Body: {"tenant_id": 1, "app_id": "...", "from_date": "YYYY-MM-DD", "to_date": "YYYY-MM-DD"}
func SyncGupshupUsageHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload struct {
			TenantID uuid.UUID `json:"tenant_id"`
			AppID    string    `json:"app_id"`
			FromDate string    `json:"from_date"`
			ToDate   string    `json:"to_date"`
		}

		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if payload.TenantID == uuid.Nil || payload.AppID == "" {
			http.Error(w, "tenant_id and app_id are required", http.StatusBadRequest)
			return
		}

		// Default dates if not provided
		if payload.FromDate == "" {
			payload.FromDate = time.Now().AddDate(0, 0, -7).Format("2006-01-02")
		}
		if payload.ToDate == "" {
			payload.ToDate = time.Now().Format("2006-01-02")
		}

		// Check if GupshupUsageSyncService is available
		if ctx.UsageSyncService == nil {
			http.Error(w, "usage sync service not available", http.StatusServiceUnavailable)
			return
		}

		// Trigger sync
		if err := ctx.UsageSyncService.SyncAppUsage(r.Context(), payload.TenantID, payload.AppID, payload.FromDate, payload.ToDate); err != nil {
			ctx.Logger.Error("failed to sync usage", zap.Error(err))
			http.Error(w, "failed to sync usage: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Usage data synced successfully",
		})
	}
}
