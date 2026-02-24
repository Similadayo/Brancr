package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// AuditLogResponse represents an audit log in the API response
type AuditLogResponse struct {
	ID         uuid.UUID  `json:"id"`
	AdminEmail string     `json:"admin_email"`
	AdminRole  string     `json:"admin_role"`
	Action     string     `json:"action"`
	Method     string     `json:"method,omitempty"`
	Path       string     `json:"path,omitempty"`
	TenantID   *uuid.UUID `json:"tenant_id,omitempty"`
	StatusCode *int       `json:"status_code,omitempty"`
	CreatedAt  string     `json:"created_at"`
}

// AuditLogsAPIHandler returns audit logs as JSON for API consumers (RBAC enforced in router).
func AuditLogsAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := ctx.DB.Model(&storage.AdminAuditLog{})

		if adminEmail := strings.TrimSpace(r.URL.Query().Get("admin_email")); adminEmail != "" {
			q = q.Where("admin_email = ?", adminEmail)
		}
		if action := strings.TrimSpace(r.URL.Query().Get("action")); action != "" {
			q = q.Where("action = ?", action)
		}
		if tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantID != "" {
			if v, err := uuid.Parse(tenantID); err == nil {
				q = q.Where("tenant_id = ?", v)
			}
		}

		// Date filtering
		var since, until *time.Time
		startDateStr := r.URL.Query().Get("start_date")
		endDateStr := r.URL.Query().Get("end_date")

		if startDateStr != "" && endDateStr != "" {
			s, err1 := time.Parse("2006-01-02", startDateStr)
			e, err2 := time.Parse("2006-01-02", endDateStr)
			if err1 == nil && err2 == nil {
				since = &s
				e = e.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
				until = &e
			}
		}

		if since != nil && until != nil {
			q = q.Where("created_at >= ? AND created_at <= ?", *since, *until)
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

		// Transform to response format
		responses := make([]AuditLogResponse, 0, len(logs))
		for _, log := range logs {
			createdAtStr := ""
			if !log.CreatedAt.IsZero() {
				createdAtStr = log.CreatedAt.Format(time.RFC3339)
			}

			var statusCode *int
			if log.StatusCode != 0 {
				statusCode = &log.StatusCode
			}

			responses = append(responses, AuditLogResponse{
				ID:         log.ID,
				AdminEmail: log.AdminEmail,
				AdminRole:  log.AdminRole,
				Action:     log.Action,
				Method:     log.Method,
				Path:       log.Path,
				TenantID:   log.TenantID,
				StatusCode: statusCode,
				CreatedAt:  createdAtStr,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(responses)
	}
}
