package observability

type SignalType string

const (
	SignalGuardFailure        SignalType = "GUARD_FAILURE"
	SignalForbiddenAction     SignalType = "FORBIDDEN_ACTION_ATTEMPT"
	SignalLowConfidence       SignalType = "CONSISTENT_LOW_CONFIDENCE"
	SignalPaymentFailure      SignalType = "PAYMENT_EXECUTION_FAILURE"
	SignalKillSwitchActivated SignalType = "KILL_SWITCH_ACTIVATED"
	SignalSafetyLimitExceeded SignalType = "SAFETY_LIMIT_EXCEEDED"
)

// In a real system, EmitSignal would push to PagerDuty/Slack
func EmitSignal(s SignalType, details string) {
	// Stub: Log or Push
}
