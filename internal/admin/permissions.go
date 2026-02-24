package admin

var RolePermissions = map[string]map[string]bool{
	RoleSuperAdmin: {
		"*": true, // Full access - Founder/CTO
	},
	RoleSupportAdmin: { // Growth & Customer Success
		"tenant.read":     true,
		"tenant.tag":      true,          // Tag tenants
		"case.create":     true,
		"insights.read":   true,          // View qualitative insights
		"insights.write":  true,          // Add/edit insights
		"ai.behavior.view": true,         // View AI behavior metrics (read-only)
		"messaging.view":  true,          // View messaging activity
	},
	RolePlatformAdmin: { // Project Manager
		"tenant.read":     true,
		"case.create":     true,
		"trace.read":      true,
		"tenant.impersonate": true,
		"health.view":     true,          // View system health
		"posting.view":    true,          // View posting status
		"insights.read":   true,          // Read insights for task creation
		"blocker.flag":    true,          // Flag blockers
	},
	RoleBillingAdmin: {
		"billing.read":  true,
		"billing.write": true,
	},
	RoleComplianceAdmin: {
		"case.create":  true,
		"content.read": true,
		"audit.read":   true,
	},
	RoleOperationsSupport: { // Operations/Admin Support
		"tenant.read":        true,
		"insights.read":      true,
		"insights.organize":  true,       // Organize notes
		"followup.schedule":  true,       // Schedule follow-ups
	},
}
