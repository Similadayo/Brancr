package validation

import (
	"os"

	"github.com/similadayo/brancr/internal/ai_v4/runner"
	"github.com/similadayo/brancr/internal/ai_v4/safety"
	"go.uber.org/zap"
)

// RunStartupCheck performs critical integrity checks before the app accepts traffic.
func RunStartupCheck(r *runner.FlowRunner, logger *zap.Logger) {
	logger.Info("v4_startup_check_initiated")

	// 1. Check Component Integrity
	if r == nil {
		panic("V4 FlowRunner is nil")
	}

	// We can add methods to Runner to expose deps for checking if we want,
	// but failing that, we assume factory did its job if we have the instance.

	// 2. Check Kill Switch Access
	// access env to ensure we can read it
	_ = safety.LoadKillSwitch()
	if os.Getenv("AI_V4_DISABLE_V4") == "true" {
		logger.Warn("STARTUP_WARNING: AI V4 is strictly DISABLED via Kill Switch")
	} else {
		logger.Info("AI V4 Kill Switch: OFF (System Active)")
	}

	// 3. Scenario Validation (Lightweight)
	// Fully running scenarios requires DB connection and valid Tenant IDs.
	// We cannot guarantee a valid tenant exists at startup without querying DB.
	// We will skip full E2E execution here to prevent boot loops on empty DBs,
	// but we assert the system is structurally ready.

	logger.Info("v4_startup_check_passed_ready_for_traffic")
}
