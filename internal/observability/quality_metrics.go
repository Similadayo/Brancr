package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	catalogMatchSuccessTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ai_catalog_match_success_total",
			Help: "Total successful catalog matches by source.",
		},
		[]string{"source"},
	)

	catalogFallbackTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ai_catalog_fallback_total",
			Help: "Total catalog fallback events by reason.",
		},
		[]string{"reason"},
	)

	stageRegressionBlockedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "ai_stage_regression_blocked_total",
			Help: "Total order stage regression attempts blocked by guardrails.",
		},
	)

	repeatPromptTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "ai_repeat_prompt_total",
			Help: "Total repeated prompts (same AI reply sent consecutively).",
		},
	)

	timeoutFallbackTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "ai_timeout_fallback_total",
			Help: "Total timeout fallback replies sent to customers.",
		},
	)

	escalationTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ai_escalation_total",
			Help: "Total escalations by reason.",
		},
		[]string{"reason"},
	)

	skuLockBlockedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "ai_v4_sku_lock_blocked_total",
			Help: "Total turns blocked by SKU lock mismatch guards.",
		},
	)

	skuSwitchConfirmedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "ai_v4_sku_switch_confirmed_total",
			Help: "Total explicit SKU switch confirmations.",
		},
	)

	unavailableModelGuardHitTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "ai_v4_unavailable_model_guard_hit_total",
			Help: "Total unavailable-model deterministic guard hits.",
		},
	)

	specPriorityInterruptionTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "ai_v4_spec_priority_interruption_total",
			Help: "Total stage prompts interrupted to answer direct spec questions first.",
		},
	)

	closeoutSuppressedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "ai_v4_closeout_suppressed_total",
			Help: "Total repeated close-out prompts suppressed.",
		},
	)

	confirmationPayloadMismatchBlockedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "ai_v4_confirmation_payload_mismatch_blocked_total",
			Help: "Total confirmation payload mismatches blocked by deterministic renderer.",
		},
	)
)

func IncCatalogMatchSuccess(source string) {
	catalogMatchSuccessTotal.WithLabelValues(source).Inc()
}

func IncCatalogFallback(reason string) {
	catalogFallbackTotal.WithLabelValues(reason).Inc()
}

func IncStageRegressionBlocked() {
	stageRegressionBlockedTotal.Inc()
}

func IncRepeatPrompt() {
	repeatPromptTotal.Inc()
}

func IncTimeoutFallback() {
	timeoutFallbackTotal.Inc()
}

func IncEscalation(reason string) {
	if reason == "" {
		reason = "unspecified"
	}
	escalationTotal.WithLabelValues(reason).Inc()
}

func IncSKULockBlocked() {
	skuLockBlockedTotal.Inc()
}

func IncSKUSwitchConfirmed() {
	skuSwitchConfirmedTotal.Inc()
}

func IncUnavailableModelGuardHit() {
	unavailableModelGuardHitTotal.Inc()
}

func IncSpecPriorityInterruption() {
	specPriorityInterruptionTotal.Inc()
}

func IncCloseoutSuppressed() {
	closeoutSuppressedTotal.Inc()
}

func IncConfirmationPayloadMismatchBlocked() {
	confirmationPayloadMismatchBlockedTotal.Inc()
}
