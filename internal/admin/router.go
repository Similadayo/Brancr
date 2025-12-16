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

	// Serve built SPA assets without auth (static-only, no tenant content).
	// This avoids browsers receiving HTML (login/redirect) when fetching CSS/JS.
	r.Get("/app/assets/*", AdminSPAAssetsServe("adminapp/dist"))

	r.Group(func(r chi.Router) {
		r.Use(AdminAuthMiddleware(ctx))

		// New admin app (SPA). Kept alongside existing server-rendered templates during migration.
		spa := AdminSPAServe("adminapp/dist")

		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin, RoleBillingAdmin, RoleComplianceAdmin)).Get("/app", spa)
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin, RoleBillingAdmin, RoleComplianceAdmin)).Get("/app/*", spa)

		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin, RoleBillingAdmin, RoleComplianceAdmin)).Get("/api/stats", AdminAppStatsHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin, RoleBillingAdmin, RoleComplianceAdmin)).Get("/api/events", AdminAppEventsSSEHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin, RoleBillingAdmin, RoleComplianceAdmin)).Get("/api/dashboard", AdminDashboardDataHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin, RoleBillingAdmin)).Post("/api/tenants/{id}/status", AdminTenantStatusAPIHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin)).Post("/api/tenants/{id}/mode", AdminTenantModeAPIHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin)).Post("/api/tenants/{id}/onboarding/reset", AdminTenantOnboardingResetHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RolePlatformAdmin)).Post("/api/tenants/{id}/tokens/refresh", AdminTenantTokenRefreshHandler(ctx))

		// /admin/api/me endpoint
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin, RoleBillingAdmin, RoleComplianceAdmin)).Get("/api/me", AdminMeHandler(ctx))

		// Compliance role: read-only audit logs/system state
		r.With(requireAnyRole(RoleSuperAdmin, RoleComplianceAdmin)).Get("/api/audit", AuditLogsAPIHandler(ctx))

		// Main
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAgent, RoleTechnicalOperator, RoleBillingAdmin)).Get("/", DashboardHandler(ctx))

		// Tenants
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAgent, RoleTechnicalOperator, RoleBillingAdmin)).Get("/tenants", TenantsHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAgent, RoleTechnicalOperator, RoleBillingAdmin)).Get("/tenants/{id}", TenantDetailHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAgent)).Post("/tenants/{id}/status", TenantUpdateStatusHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAgent)).Post("/tenants/{id}/mode", TenantUpdateAIModeHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleBillingAdmin)).Post("/tenants/{id}/quota", TenantUpdateQuotaHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAgent, RoleTechnicalOperator)).Get("/tenants/{id}/analytics", NewAnalyticsHandler(ctx.DB, ctx.Logger, nil).GetTenantAnalytics(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleTechnicalOperator)).Get("/analytics", NewAnalyticsHandler(ctx.DB, ctx.Logger, nil).GetAnalyticsDashboard(ctx))

		// Operations
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAgent)).Get("/escalations", EscalationsHandler(ctx))
		// Escalation full content (case-gated, permissioned)
		r.With(
			RequirePermission("content.read"),
			RequireActiveAdminCase(ctx),
		).Get("/escalations/{id}/content", EscalationContentHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RolePlatformAdmin)).Post("/escalations/{id}/resolve", EscalationResolveHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAgent, RoleBillingAdmin)).Get("/orders", OrdersHandler(ctx))
		// Admin portal token generation for orders (admin-only)
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAgent, RoleBillingAdmin)).Get("/tenants/{id}/orders/{orderId}/portal-token", AdminGeneratePortalTokenHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleBillingAdmin)).Get("/payments", PaymentsHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleTechnicalOperator)).Get("/scheduled", ScheduledHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleTechnicalOperator)).Get("/scheduled/force_cancel_form", ForceCancelFormHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleTechnicalOperator)).Post("/scheduled/force_cancel", ForceCancelHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleTechnicalOperator)).Get("/social", (SocialAccountsHandler(ctx)))
		r.With(requireAnyRole(RoleSuperAdmin, RoleTechnicalOperator)).Get("/usage", UsageStatsPageHandler(ctx))    // HTML page
		r.With(requireAnyRole(RoleSuperAdmin, RoleTechnicalOperator)).Get("/usage/api", UsageStatsAPIHandler(ctx)) // JSON API endpoint

		// WhatsApp phone number management
		r.With(requireAnyRole(RoleSuperAdmin, RoleTechnicalOperator)).Get("/whatsapp", WhatsAppNumbersPageHandler(ctx))     // HTML page
		r.With(requireAnyRole(RoleSuperAdmin, RoleTechnicalOperator)).Get("/whatsapp/numbers", WhatsAppNumbersHandler(ctx)) // JSON API endpoint
		r.With(requireAnyRole(RoleSuperAdmin, RoleTechnicalOperator)).Post("/whatsapp/sync", WhatsAppSyncHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleTechnicalOperator)).Post("/whatsapp/register", WhatsAppRegisterNumberHandler(ctx)) // Register phone number (required before verify)
		r.With(requireAnyRole(RoleSuperAdmin, RoleTechnicalOperator)).Post("/whatsapp/request-code", WhatsAppAddNumberHandler(ctx))  // Request verification code for existing number
		r.With(requireAnyRole(RoleSuperAdmin, RoleTechnicalOperator)).Post("/whatsapp/verify", WhatsAppVerifyNumberHandler(ctx))

		// WhatsApp phone number requests from tenants
		r.With(requireAnyRole(RoleSuperAdmin, RoleTechnicalOperator)).Get("/whatsapp/requests", WhatsAppRequestsHandler(ctx))              // List all requests
		r.With(requireAnyRole(RoleSuperAdmin, RoleTechnicalOperator)).Post("/whatsapp/requests/mark-added", WhatsAppMarkAddedHandler(ctx)) // Mark as added to Meta
		r.With(requireAnyRole(RoleSuperAdmin, RoleTechnicalOperator)).Post("/whatsapp/requests/verify", WhatsAppVerifyRequestHandler(ctx)) // Verify with code from tenant
		r.With(requireAnyRole(RoleSuperAdmin, RoleTechnicalOperator)).Post("/whatsapp/requests/reject", WhatsAppRejectRequestHandler(ctx)) // Reject request

		// System
		r.With(requireAnyRole(RoleSuperAdmin, RoleTechnicalOperator)).Get("/health", HealthHandler(ctx))
		r.Get("/_health", HealthJSONHandler(ctx))
		r.Get("/_health_partial", HealthPartialHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleTechnicalOperator)).Get("/logs", LogsHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleTechnicalOperator)).Get("/logs/_partial", LogsPartialHandler(ctx))

		// Security
		r.With(requireAnyRole(RoleSuperAdmin)).Get("/security/admin-users", AdminUsersPageHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin)).Post("/security/admin-users/create", AdminUsersCreateHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin)).Post("/security/admin-users/update", AdminUsersUpdateHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleComplianceAdmin)).Get("/security/audit-logs", AuditLogsPageHandler(ctx))

		// Support
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAgent)).Get("/support/impersonate", ImpersonationPageHandler(ctx))
		r.With(
			RequirePermission("tenant.impersonate"),
			RequireActiveAdminCase(ctx),
		).Post("/support/impersonate", ImpersonationStartHandler(ctx))
		r.With(
			RequirePermission("tenant.impersonate"),
			RequireActiveAdminCase(ctx),
		).Post("/support/stop-impersonation", ImpersonationStopHandler(ctx))

		r.Post("/logout", LogoutHandler(ctx))

	})

	return r
}
