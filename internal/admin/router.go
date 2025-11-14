package admin

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// AdminRouter returns the admin subrouter (mount at /admin)
func AdminRouter(ctx *AdminContext) http.Handler {
	r := chi.NewRouter()
	r.Use(AdminPanicMiddleware(ctx))
	r.Get("/login", GetLoginHandler(ctx))
	r.Post("/login", PostLoginHandler(ctx))

	r.Group(func(r chi.Router) {
		r.Use(AdminAuthMiddleware(ctx))
		r.Get("/", DashboardHandler(ctx))
		r.Get("/health", HealthHandler(ctx))
		r.Get("/_health", HealthJSONHandler(ctx))
		r.Get("/_health_partial", HealthPartialHandler(ctx))
		r.Get("/logs", LogsHandler(ctx))
		r.Get("/logs/_partial", LogsPartialHandler(ctx))
		r.Get("/tenants", TenantsHandler(ctx))
		r.Get("/tenants/{id}", TenantDetailHandler(ctx))
		r.Get("/tenants/{id}/analytics", NewAnalyticsHandler(ctx.DB, ctx.Logger, nil).GetTenantAnalytics(ctx))
		r.Get("/analytics", NewAnalyticsHandler(ctx.DB, ctx.Logger, nil).GetAnalyticsDashboard(ctx))
		r.Get("/scheduled", ScheduledHandler(ctx))
		r.Get("/scheduled/force_cancel_form", ForceCancelFormHandler(ctx))
		r.Post("/scheduled/force_cancel", ForceCancelHandler(ctx))
		r.Get("/social", (SocialAccountsHandler(ctx)))
		
		// WhatsApp phone number management
		r.Get("/whatsapp/numbers", WhatsAppNumbersHandler(ctx))
		r.Post("/whatsapp/sync", WhatsAppSyncHandler(ctx))
		r.Post("/whatsapp/request-code", WhatsAppAddNumberHandler(ctx)) // Request verification code for existing number
		r.Post("/whatsapp/verify", WhatsAppVerifyNumberHandler(ctx))
		r.Post("/logout", LogoutHandler(ctx))

	})

	return r
}
