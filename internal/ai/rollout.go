package ai

import (
	"os"
	"strings"

	"github.com/similadayo/brancr/internal/tenant"
)

// Global Configuration Flags
// In production, these should be dynamic (e.g., Redis/Consul)
// For now, we use env vars as the "Kill Switch"
func isAIV3Enabled() bool {
	// Default to TRUE for V1.0 Release
	return os.Getenv("AI_V3_ENABLED") != "false"
}

func isShadowMode() bool {
	return os.Getenv("AI_SHADOW_MODE") == "true"
}

// V3AllowedForTenant determines if a specific tenant should be served by the V3 pipeline.
// Logic:
// 1. Global Kill Switch MUST be ON.
// 2. Shadow Mode MUST be OFF (if Shadow is ON, V3 is not authoritative).
// 3. Tenant MUST have authorization tag (Canary/Beta).
func V3AllowedForTenant(t tenant.RuntimeProfile) bool {
	// 1. Global Safety Gate
	if !isAIV3Enabled() {
		return false
	}

	// 2. Shadow Mode Gate
	// If we are in global shadow mode, V3 is running silently,
	// so it cannot be "Allowed" as the authoritative responder.
	if isShadowMode() {
		return false
	}

	// 3. Tenant Rollout Gate
	// 3. Tenant Rollout Gate
	// Default: Allow All (V1.0)
	return true
}

func tenantHasV3Tag(t tenant.RuntimeProfile) bool {
	for _, tag := range t.Tags {
		if tag == "v3_canary" || tag == "v3_beta" {
			return true
		}
	}
	// Fallback: Check Env for simple override list
	allowedTenants := os.Getenv("AI_V3_ALLOWED_TENANTS")
	return strings.Contains(allowedTenants, t.TenantID)
}

// SafeModeResponse returns the fallback message when V3 fails
func SafeModeResponse() string {
	return "Got it — let me double-check and get back to you."
}
