package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

func AuditLogsPageHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := ctx.DB.Model(&storage.AdminAuditLog{})

		if adminEmail := strings.TrimSpace(r.URL.Query().Get("admin_email")); adminEmail != "" {
			q = q.Where("admin_email = ?", adminEmail)
		}
		if action := strings.TrimSpace(r.URL.Query().Get("action")); action != "" {
			q = q.Where("action = ?", action)
		}
		if tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantID != "" {
			if v, err := strconv.ParseUint(tenantID, 10, 64); err == nil {
				q = q.Where("tenant_id = ?", uint(v))
			}
		}

		limit := 200
		if l := strings.TrimSpace(r.URL.Query().Get("limit")); l != "" {
			if v, err := strconv.Atoi(l); err == nil {
				if v > 0 && v <= 500 {
					limit = v
				}
			}
		}

		var logs []storage.AdminAuditLog
		if err := q.Order("created_at DESC").Limit(limit).Find(&logs).Error; err != nil {
			ctx.Logger.Error("failed to fetch admin audit logs", zap.Error(err))
			http.Error(w, "failed to fetch audit logs", http.StatusInternalServerError)
			return
		}

		data := map[string]any{
			"Logs":  logs,
			"Limit": limit,
			"Query": map[string]string{
				"admin_email": r.URL.Query().Get("admin_email"),
				"action":      r.URL.Query().Get("action"),
				"tenant_id":   r.URL.Query().Get("tenant_id"),
			},
			"Page": "audit_logs",
		}

		renderTemplate(w, r, "audit_logs.html", data, ctx.Logger)
	}
}
