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

		// Enforce case-gate: must have active case, assigned, status investigating, tenant match
		caseObj := AdminCaseFromRequest(r)
		if caseObj == nil || caseObj.Status != "investigating" || caseObj.TenantID != tenantID {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		adminEmail := AdminEmailFromRequest(r)
		if caseObj.AssignedTo == nil || *caseObj.AssignedTo != adminEmail {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		email := strings.TrimSpace(r.FormValue("email"))
		if email == "" {
			email = tenant.Email
		}

		// TTL configurable via config (minutes) — default to 60
		ttlMinutes := 60
		if ctx.Config != nil && ctx.Config.TenantImpersonationTTLMinutes > 0 {
			ttlMinutes = ctx.Config.TenantImpersonationTTLMinutes
		}
		ttl := time.Duration(ttlMinutes) * time.Minute
		token, err := middleware.CreateTenantToken([]byte(ctx.Config.TenantJWTSecret), tenantID, email, ttl)
		if err != nil {
			ctx.Logger.Error("failed to create tenant impersonation token", zap.Error(err))
			http.Error(w, "failed to impersonate", http.StatusInternalServerError)
			return
		}

		// Audit: must succeed or abort (fail-closed)
		ae := AdminAuditEntry{
			AdminEmail: adminEmail,
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.impersonate.start",
			TenantID:   &tenantID,
			CaseID:     &caseObj.ID,
			Metadata: map[string]interface{}{
				"case_id":     caseObj.ID,
				"email":       email,
				"ttl_seconds": int(ttl.Seconds()),
			},
		}
		if err := auditWriter(r.Context(), ctx, ae); err != nil {
			http.Error(w, "audit log failed", http.StatusInternalServerError)
			return
		}

		// Commit action: set cookie and redirect
		http.SetCookie(w, &http.Cookie{
			Name:     tenantSessionCookieName,
			Value:    token,
			Path:     "/admin",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   int(ttl.Seconds()),
			Expires:  time.Now().Add(ttl),
		})

		http.Redirect(w, r, "/admin/support/impersonate", http.StatusSeeOther)
	}
}

func ImpersonationStopHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(tenantSessionCookieName)
		if err != nil {
			http.Redirect(w, r, "/admin/support/impersonate", http.StatusSeeOther)
			return
		}

		// Enforce case-gate: must have active case, assigned, status investigating, tenant match
		caseObj := AdminCaseFromRequest(r)
		if caseObj == nil || caseObj.Status != "investigating" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		adminEmail := AdminEmailFromRequest(r)
		if caseObj.AssignedTo == nil || *caseObj.AssignedTo != adminEmail {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		// Audit: must succeed or abort (fail-closed)
		ae := AdminAuditEntry{
			AdminEmail: adminEmail,
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.impersonate.stop",
			CaseID:     &caseObj.ID,
			Metadata: map[string]interface{}{
				"case_id": caseObj.ID,
				"token":   cookie.Value,
			},
		}
		if err := auditWriter(r.Context(), ctx, ae); err != nil {
			http.Error(w, "audit log failed", http.StatusInternalServerError)
			return
		}

		// Clear cookie
		http.SetCookie(w, &http.Cookie{
			Name:     tenantSessionCookieName,
			Value:    "",
			Path:     "/admin",
			HttpOnly: true,
			MaxAge:   -1,
			Expires:  time.Unix(0, 0),
		})

		http.Redirect(w, r, "/admin/support/impersonate", http.StatusSeeOther)
	}
}
