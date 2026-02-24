package safety

import (
	"os"
	"strings"
)

// KillSwitch controls emergency shutoffs for V4 subsystems.
type KillSwitch struct {
	DisableV4        bool
	DisableExecution bool
	DisableLLM       bool
}

// LoadKillSwitch loads settings from environment variables.
func LoadKillSwitch() KillSwitch {
	return KillSwitch{
		DisableV4:        isTrue(os.Getenv("AI_V4_DISABLE_V4")),
		DisableExecution: isTrue(os.Getenv("AI_V4_DISABLE_EXECUTION")),
		DisableLLM:       isTrue(os.Getenv("AI_V4_DISABLE_LLM")),
	}
}

func isTrue(val string) bool {
	return strings.ToLower(strings.TrimSpace(val)) == "true"
}
