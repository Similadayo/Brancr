package observability

// KillSwitch is the global emergency control.
// When AIEnabled is false, the system reverts to "Safe Mode" (dumb/static).
type KillSwitch struct {
	AIEnabled bool
}

var GlobalKillSwitch = &KillSwitch{AIEnabled: true}

func IsAIEnabled() bool {
	return GlobalKillSwitch.AIEnabled
}

func PanicDisable() {
	GlobalKillSwitch.AIEnabled = false
}
