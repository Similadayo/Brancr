package admin

import (
	"net/http"
	"strconv"

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

	if entry.TenantID == nil {
		if tid := r.URL.Query().Get("tenant_id"); tid != "" {
			if v, err := strconv.ParseUint(tid, 10, 64); err == nil {
				t := uint(v)
				entry.TenantID = &t
			}
		}
	}

	_ = ctx.DB.WithContext(r.Context()).Create(&entry).Error
}
