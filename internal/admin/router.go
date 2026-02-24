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

	// Root redirect: /admin → /admin/login (if not authenticated) or /admin/app (if authenticated)
	r.Get("/", AdminRootRedirectHandler(ctx))

	r.Get("/login", GetLoginHandler(ctx))
	r.Post("/login", PostLoginHandler(ctx))

	// Serve built SPA assets without auth (static-only, no tenant content).
	// This avoids browsers receiving HTML (login/redirect) when fetching CSS/JS.
	// Serve from both paths: /app/assets/* (with base) and /assets/* (fallback for old builds)
	assetsHandler := AdminSPAAssetsServe("adminapp/dist")
	r.Get("/app/assets/*", assetsHandler)
	r.Get("/assets/*", assetsHandler)

	r.Group(func(r chi.Router) {
		r.Use(AdminAuthMiddleware(ctx))

		// New admin app (SPA). Kept alongside existing server-rendered templates during migration.
		spa := AdminSPAServe("adminapp/dist")

		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin, RoleBillingAdmin, RoleComplianceAdmin)).Get("/app", spa)
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin, RoleBillingAdmin, RoleComplianceAdmin)).Get("/app/*", spa)

		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin, RoleBillingAdmin, RoleComplianceAdmin)).Get("/api/stats", AdminAppStatsHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin, RoleBillingAdmin, RoleComplianceAdmin)).Get("/api/events", AdminAppEventsSSEHandler(ctx))

		// New unified dashboard v2 endpoint
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin, RoleOperationsSupport)).Get("/api/dashboard/v2", UnifiedDashboardAPIHandler(ctx))

		// Section-specific dashboard endpoints
		r.With(RequireSectionAccess("system_health")).Get("/api/dashboard/system-health", SystemHealthAPIHandler(ctx))
		r.With(RequireSectionAccess("ai_behavior")).Get("/api/dashboard/ai-behavior", AIBehaviorAPIHandler(ctx))
		r.With(RequireSectionAccess("tenant_overview")).Get("/api/dashboard/tenant-overview", TenantOverviewAPIHandler(ctx))
		r.With(RequireSectionAccess("messaging_activity")).Get("/api/dashboard/messaging-activity", MessagingActivityAPIHandler(ctx))
		r.With(RequireSectionAccess("posting_status")).Get("/api/dashboard/posting-status", PostingStatusAPIHandler(ctx))
		r.With(RequireSectionAccess("cost_control")).Get("/api/dashboard/cost-control", CostControlAPIHandler(ctx))
		r.With(RequireSectionAccess("cost_control")).Get("/api/cost-control/settings", GetCostControlSettingsHandler(ctx))
		r.With(RequireSectionAccess("cost_control")).Post("/api/cost-control/settings", UpdateCostControlSettingsHandler(ctx))
		r.With(RequireSectionAccess("qualitative_insights")).Get("/api/dashboard/qualitative-insights", QualitativeInsightsAPIHandler(ctx))

		// New analytics endpoints
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin)).Get("/api/dashboard/escalation-reasons", EscalationReasonsHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin)).Get("/api/dashboard/conversation-attribution", ConversationAttributionHandler(ctx))

		// Action endpoints
		r.With(requireAnyRole(RoleSuperAdmin)).Post("/api/tenants/{id}/pause", PauseTenantHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin)).Post("/api/tenants/{id}/tags", TagTenantHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin)).Post("/api/tenants/{id}/cap-usage", CapTenantUsageHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin)).Post("/api/insights", CreateInsightHandler(ctx))
		r.With(requireAnyRole(RolePlatformAdmin)).Post("/api/blockers/flag", FlagBlockerHandler(ctx))

		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin, RoleBillingAdmin)).Post("/api/tenants/{id}/status", AdminTenantStatusAPIHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin)).Post("/api/tenants/{id}/mode", AdminTenantModeAPIHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin)).Post("/api/tenants/{id}/onboarding/reset", AdminTenantOnboardingResetHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RolePlatformAdmin)).Post("/api/tenants/{id}/tokens/refresh", AdminTenantTokenRefreshHandler(ctx))

		// Tenant management (SuperAdmin only)
		r.With(requireAnyRole(RoleSuperAdmin)).Delete("/api/tenants/{id}", AdminDeleteTenantHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin)).Post("/api/tenants/{id}/block", AdminBlockTenantHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin)).Post("/api/tenants/{id}/unblock", AdminUnblockTenantHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin)).Post("/api/tenants/{id}/suspend", AdminSuspendTenantHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin)).Post("/api/tenants/{id}/unsuspend", AdminUnsuspendTenantHandler(ctx))

		// /admin/api/me endpoint
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin, RoleBillingAdmin, RoleComplianceAdmin)).Get("/api/me", AdminMeHandler(ctx))

		// Admin sessions (device/session management). Requires Redis.
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin, RoleBillingAdmin, RoleComplianceAdmin, RoleOperationsSupport)).Get("/api/sessions", AdminSessionsListHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin, RoleBillingAdmin, RoleComplianceAdmin, RoleOperationsSupport)).Delete("/api/sessions/{jti}", AdminSessionsRevokeHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin, RoleBillingAdmin, RoleComplianceAdmin, RoleOperationsSupport)).Post("/api/sessions/revoke-all", AdminSessionsRevokeAllHandler(ctx))

		// Admin user management (SuperAdmin only)
		r.With(requireAnyRole(RoleSuperAdmin)).Get("/api/admin-users", AdminUsersAPIHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin)).Post("/api/admin-users", AdminUsersCreateAPIHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin)).Put("/api/admin-users/{id}", AdminUsersUpdateAPIHandler(ctx))

		// Compliance role: read-only audit logs/system state
		r.With(requireAnyRole(RoleSuperAdmin, RoleComplianceAdmin)).Get("/api/audit", AuditLogsAPIHandler(ctx))

		// Failures tracking (all admin roles can see failures)
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin, RoleOperationsSupport)).Get("/api/failures", FailuresAPIHandler(ctx))

		// Enhanced tenant APIs
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin, RoleBillingAdmin)).Get("/api/tenants", AdminTenantsListAPIHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin, RoleBillingAdmin)).Get("/api/tenants/{id}", AdminTenantDetailAPIHandler(ctx))

		// Social accounts management (including WhatsApp via Gupshup)
		r.With(requireAnyRole(RoleSuperAdmin, RolePlatformAdmin, RoleOperationsSupport)).Get("/api/social-accounts", SocialAccountsAPIHandler(ctx))

		// Plan Templates Management
		r.With(requireAnyRole(RoleSuperAdmin, RoleBillingAdmin)).Get("/api/plans/templates", ListPlanTemplatesHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleBillingAdmin)).Post("/api/plans/templates", CreatePlanTemplateHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleBillingAdmin)).Put("/api/plans/templates/{id}", UpdatePlanTemplateHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleBillingAdmin)).Post("/api/plans/templates/{id}/deprecate", DeprecatePlanTemplateHandler(ctx))

		// Tenant Plan Management
		r.With(requireAnyRole(RoleSuperAdmin, RoleBillingAdmin)).Post("/api/tenants/{tenant_id}/plans/assign", AssignPlanToTenantHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleBillingAdmin, RoleSupportAdmin, RolePlatformAdmin)).Get("/api/tenants/{tenant_id}/billing", GetTenantBillingHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleBillingAdmin)).Put("/api/tenants/{tenant_id}/plans/quotas", UpdateTenantPlanQuotasHandler(ctx))

		// Gupshup WhatsApp Usage & Cost Control
		r.With(requireAnyRole(RoleSuperAdmin, RoleBillingAdmin)).Get("/api/gupshup-usage", GetGupshupUsageHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleBillingAdmin)).Get("/api/gupshup-discount", GetGupshupDiscountHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleBillingAdmin)).Post("/api/gupshup-usage/sync", SyncGupshupUsageHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleBillingAdmin)).Put("/api/tenants/{tenant_id}/plans/price", UpdateTenantPlanPriceHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleBillingAdmin)).Post("/api/tenants/{tenant_id}/plans/pause", PauseTenantPlanHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleBillingAdmin)).Post("/api/tenants/{tenant_id}/plans/resume", ResumeTenantPlanHandler(ctx))

		// Paystack webhook (no auth required - verified by signature)
		r.Post("/api/paystack/webhook", PaystackWebhookHandler(ctx))

		// Tenant-to-Brancr payments (subscriptions)
		r.With(requireAnyRole(RoleSuperAdmin, RoleBillingAdmin)).Get("/api/payments", PaymentsAPIHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleBillingAdmin)).Post("/api/subscriptions/payment", ProcessSubscriptionPaymentHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleBillingAdmin, RoleSupportAdmin)).Get("/api/subscriptions/tenants", GetTenantsByPlanTypeHandler(ctx))

		// Scheduled posts management
		r.With(requireAnyRole(RoleSuperAdmin, RolePlatformAdmin, RoleOperationsSupport)).Get("/api/scheduled-posts", ScheduledPostsAPIHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RolePlatformAdmin)).Post("/api/scheduled-posts/{id}/cancel", ForceCancelScheduledPostHandler(ctx))

		// Escalations management
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin)).Get("/api/escalations", EscalationsAPIHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin)).Get("/api/escalations/metrics", EscalationMetricsAPIHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RolePlatformAdmin)).Post("/api/escalations/{id}/resolve", ResolveEscalationHandler(ctx))

		// Analytics
		r.With(requireAnyRole(RoleSuperAdmin, RolePlatformAdmin, RoleOperationsSupport)).Get("/api/analytics", AnalyticsAPIHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin)).Get("/api/analytics/tenant", TenantAnalyticsAPIHandler(ctx))

		// Experience Checkpoints
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin)).Get("/api/checkpoints/stats", CheckpointStatsHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin)).Get("/api/checkpoints/responses", CheckpointResponsesHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin)).Post("/api/checkpoints/trigger", TriggerCheckpointHandler(ctx))

		// Health & Logs
		r.With(requireAnyRole(RoleSuperAdmin, RoleOperationsSupport)).Get("/api/health", HealthAPIHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleOperationsSupport)).Get("/api/logs", LogsAPIHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleOperationsSupport)).Get("/ws/logs", LogStreamHandler(ctx.LogBroadcaster, ctx.Logger))
		r.With(requireAnyRole(RoleSuperAdmin, RoleOperationsSupport)).Get("/api/logs/stream", LogStreamSSEHandler(ctx.LogBroadcaster, ctx.Logger))

		// Alerts & Newsletters
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin)).Post("/api/alerts", SendAlertHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAdmin, RolePlatformAdmin)).Get("/api/alerts", GetAlertsHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RolePlatformAdmin)).Post("/api/newsletters", CreateNewsletterHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RolePlatformAdmin)).Post("/api/newsletters/send", SendNewsletterHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RolePlatformAdmin)).Get("/api/newsletters", GetNewslettersHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RolePlatformAdmin)).Get("/api/newsletters/subscriptions", GetSubscriptionsHandler(ctx))

		// Alert Templates (editable automated messages)
		r.With(requireAnyRole(RoleSuperAdmin, RolePlatformAdmin)).Get("/api/alert-templates", ListAlertTemplatesHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RolePlatformAdmin)).Get("/api/alert-templates/get", GetAlertTemplateHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RolePlatformAdmin)).Put("/api/alert-templates", UpdateAlertTemplateHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RolePlatformAdmin)).Post("/api/alert-templates/reset", ResetAlertTemplateHandler(ctx))

		// Main
		// DashboardHandler & Pages removed

		// Tenants
		r.With(requireAnyRole(RoleSuperAdmin, RoleSupportAgent)).Post("/tenants/{id}/status", TenantUpdateStatusHandler(ctx))

		// KYB endpoints (Compliance team)
		r.With(requireAnyRole(RoleSuperAdmin, RoleComplianceAdmin)).Get("/api/tenants/{id}/kyb", AdminGetTenantKYBHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleComplianceAdmin)).Post("/api/tenants/{id}/kyb/verify", AdminVerifyTenantKYBHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RoleComplianceAdmin)).Post("/api/tenants/{id}/kyb/reject", AdminRejectTenantKYBHandler(ctx))

		// Operations
		// Legacy UI pages (Escalations, Orders, etc) removed
		// Escalation full content (case-gated, permissioned)
		r.With(
			RequirePermission("content.read"),
			RequireActiveAdminCase(ctx),
			RequireRecentAuth(ctx),
		).Get("/escalations/{id}/content", EscalationContentHandler(ctx))
		// Reauth endpoint for sensitive actions
		r.Post("/reauth", PostReauthHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin, RolePlatformAdmin)).Post("/escalations/{id}/resolve", EscalationResolveHandler(ctx))
		// Orders, Payments, Scheduled, Social UIs removed
		r.With(requireAnyRole(RoleSuperAdmin, RoleTechnicalOperator)).Post("/scheduled/force_cancel", ForceCancelHandler(ctx))
		// Usage stats pages removed (legacy)
		r.With(requireAnyRole(RoleSuperAdmin, RoleTechnicalOperator)).Get("/usage/api", UsageStatsAPIHandler(ctx)) // JSON API endpoint

		// WhatsApp phone number management
		// Page handler removed
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
		// Health/Logs UI removed
		r.Get("/_health", HealthJSONHandler(ctx))

		// Security
		// Legacy UI pages removed
		r.With(requireAnyRole(RoleSuperAdmin)).Post("/security/admin-users/create", AdminUsersCreateHandler(ctx))
		r.With(requireAnyRole(RoleSuperAdmin)).Post("/security/admin-users/update", AdminUsersUpdateHandler(ctx))

		// Support
		// Legacy frontend impersonation page removed
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
