package admin

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
)

func persistAdminAuditEvent(ctx *AdminContext, r *http.Request, entry storage.AdminAuditLog) {
	if ctx == nil || ctx.DB == nil || r == nil {
		return
	}

	adminEmail := getAdminUserFromRequest(r)
	adminRole := AdminRoleFromRequest(r)

	entry.AdminEmail = adminEmail
	entry.AdminRole = adminRole
	entry.Method = r.Method
	entry.Path = r.URL.Path
	entry.Success = entry.Success || entry.StatusCode < 400
	if entry.StatusCode == 0 {
		entry.StatusCode = http.StatusOK
	}
	entry.IP = r.RemoteAddr
	entry.UserAgent = r.UserAgent()

	if entry.Metadata == "" {
		if rid := middleware.GetRequestID(r.Context()); rid != "" {
			if b, err := json.Marshal(map[string]any{"request_id": rid}); err == nil {
				entry.Metadata = string(b)
			}
		}
	}

	if entry.TenantID == nil {
		if tid := r.URL.Query().Get("tenant_id"); tid != "" {
			if v, err := uuid.Parse(tid); err == nil {
				entry.TenantID = &v
			}
		}
	}

	_ = ctx.DB.WithContext(r.Context()).Create(&entry).Error
}
