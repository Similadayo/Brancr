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
		r.Get("/scheduled", ScheduledHandler(ctx))
		r.Get("/scheduled/force_cancel_form", ForceCancelFormHandler(ctx))
		r.Post("/scheduled/force_cancel", ForceCancelHandler(ctx))
		r.Get("/social", (SocialAccountsHandler(ctx)))
		r.Post("/logout", LogoutHandler(ctx))

	})

	return r
}
