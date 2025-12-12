package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

const tenantSessionCookieName = "brancr_tenant_session"

func ImpersonationPageHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var current map[string]any
		if ctx.Config != nil && ctx.Config.TenantJWTSecret != "" {
			if c, err := r.Cookie(tenantSessionCookieName); err == nil && strings.TrimSpace(c.Value) != "" {
				if claims, err := middleware.ParseTenantToken([]byte(ctx.Config.TenantJWTSecret), strings.TrimSpace(c.Value)); err == nil {
					current = map[string]any{
						"TenantID": claims.TenantID,
						"Email":    claims.Email,
						"ExpiresAt": func() string {
							if claims.ExpiresAt == nil {
								return ""
							}
							return claims.ExpiresAt.Time.Format(time.RFC3339)
						}(),
					}
				}
			}
		}

		data := map[string]any{
			"Current": current,
			"Page":    "impersonate",
		}
		renderTemplate(w, r, "impersonate.html", data, ctx.Logger)
	}
}

func ImpersonationStartHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if ctx.Config == nil || ctx.Config.TenantJWTSecret == "" {
			http.Error(w, "tenant jwt not configured", http.StatusBadRequest)
			return
		}

		tenantIDStr := strings.TrimSpace(r.FormValue("tenant_id"))
		tenantID64, err := strconv.ParseUint(tenantIDStr, 10, 64)
		if err != nil || tenantID64 == 0 {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		tenantID := uint(tenantID64)

		var tenant storage.Tenant
		if err := ctx.DB.First(&tenant, tenantID).Error; err != nil {
			http.Error(w, "tenant not found", http.StatusNotFound)
			return
		}

		email := strings.TrimSpace(r.FormValue("email"))
		if email == "" {
			email = tenant.Email
		}

		ttl := 1 * time.Hour
		token, err := middleware.CreateTenantToken([]byte(ctx.Config.TenantJWTSecret), tenantID, email, ttl)
		if err != nil {
			ctx.Logger.Error("failed to create tenant impersonation token", zap.Error(err))
			http.Error(w, "failed to impersonate", http.StatusInternalServerError)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     tenantSessionCookieName,
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   int(ttl.Seconds()),
			Expires:  time.Now().Add(ttl),
		})

		persistAdminAuditEvent(ctx, r, storage.AdminAuditLog{
			Action:     "admin.impersonate.start",
			TenantID:   &tenantID,
			Resource:   "tenant",
			ResourceID: strconv.FormatUint(uint64(tenantID), 10),
			Metadata:   mustJSON(map[string]any{"email": email, "ttl_seconds": int(ttl.Seconds())}),
		})

		http.Redirect(w, r, "/admin/support/impersonate", http.StatusSeeOther)
	}
}

func ImpersonationStopHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:     tenantSessionCookieName,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   -1,
		})

		persistAdminAuditEvent(ctx, r, storage.AdminAuditLog{
			Action:   "admin.impersonate.stop",
			Resource: "tenant",
			Metadata: mustJSON(map[string]any{"cleared_cookie": tenantSessionCookieName}),
		})

		http.Redirect(w, r, "/admin/support/impersonate", http.StatusSeeOther)
	}
}
