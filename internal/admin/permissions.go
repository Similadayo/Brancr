package admin

var RolePermissions = map[string]map[string]bool{
	RoleSupportAdmin: {
		"tenant.read":  true,
		"case.create":  true,
	},
	RolePlatformAdmin: {
		"tenant.read":        true,
		"case.create":        true,
		"trace.read":         true,
		"tenant.impersonate": true,
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
	RoleSuperAdmin: {
		"*": true,
	},
}
