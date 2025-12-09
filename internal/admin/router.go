package admin

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/similadayo/brancr/pkg/metrics"
)

// AdminRouter returns the admin subrouter (mount at /admin)
func AdminRouter(ctx *AdminContext, m *metrics.Metrics) http.Handler {
	r := chi.NewRouter()
	r.Use(AdminPanicMiddleware(ctx))
	r.Use(AdminLoggingMiddleware(ctx, m)) // Add comprehensive logging
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
		r.Get("/usage", UsageStatsPageHandler(ctx))    // HTML page
		r.Get("/usage/api", UsageStatsAPIHandler(ctx)) // JSON API endpoint

		// WhatsApp phone number management
		r.Get("/whatsapp", WhatsAppNumbersPageHandler(ctx))     // HTML page
		r.Get("/whatsapp/numbers", WhatsAppNumbersHandler(ctx)) // JSON API endpoint
		r.Post("/whatsapp/sync", WhatsAppSyncHandler(ctx))
		r.Post("/whatsapp/register", WhatsAppRegisterNumberHandler(ctx)) // Register phone number (required before verify)
		r.Post("/whatsapp/request-code", WhatsAppAddNumberHandler(ctx))  // Request verification code for existing number
		r.Post("/whatsapp/verify", WhatsAppVerifyNumberHandler(ctx))

		// WhatsApp phone number requests from tenants
		r.Get("/whatsapp/requests", WhatsAppRequestsHandler(ctx))              // List all requests
		r.Post("/whatsapp/requests/mark-added", WhatsAppMarkAddedHandler(ctx)) // Mark as added to Meta
		r.Post("/whatsapp/requests/verify", WhatsAppVerifyRequestHandler(ctx)) // Verify with code from tenant
		r.Post("/whatsapp/requests/reject", WhatsAppRejectRequestHandler(ctx)) // Reject request

		r.Post("/logout", LogoutHandler(ctx))

	})

	return r
}
