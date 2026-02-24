package admin

import (
	"math"
	"time"

	"github.com/google/uuid"
)

// --- Shared Types ---

type TenantSummary struct {
	ID                 uuid.UUID `json:"id"`
	Name               string    `json:"name"`
	Email              string    `json:"email"`
	PlanType           string    `json:"plan_type"`
	Status             string    `json:"status"`
	OnboardingStep     string    `json:"onboarding_step"`
	OnboardingComplete bool      `json:"onboarding_complete"`
	Platforms          []string  `json:"platforms"`
	LastActivity       string    `json:"last_activity"`
	UsagePercent       float64   `json:"usage_percent"`
	AIHumanMode        string    `json:"ai_human_mode"`
	KYBStatus          string    `json:"kyb_status"`
	AdminTags          []string  `json:"admin_tags"`
	PendingEscalations int64     `json:"pending_escalations"`
	CreatedAt          time.Time `json:"created_at"`
}

type OnboardingSummary struct {
	CompletionRate      float64          `json:"completion_rate"`
	DropOffs            []DropOff        `json:"drop_offs"`
	ManualInterventions int              `json:"manual_interventions"`
	StepTotals          map[string]int64 `json:"step_totals"`
}

type DropOff struct {
	Step    string  `json:"step"`
	Count   int64   `json:"count"`
	Percent float64 `json:"percent"`
}

type IntegrationStatus struct {
	Name        string  `json:"name"`
	Status      string  `json:"status"`
	ErrorRate   float64 `json:"error_rate"`
	Connected   int64   `json:"connected"`
	Total       int64   `json:"total"`
	ExpiresSoon int64   `json:"expires_soon"`
}

type MessagingSummary struct {
	PlatformVolumes  []PlatformVolume `json:"platform_volumes"`
	AiPercent        float64          `json:"ai_percent"`
	HumanPercent     float64          `json:"human_percent"`
	FailedDeliveries int64            `json:"failed_deliveries"`
	LatencySeconds   float64          `json:"latency_seconds"`
}

type PlatformVolume struct {
	Platform string `json:"platform"`
	Count    int64  `json:"count"`
}

type BillingSummary struct {
	Usage          []BillingUsage `json:"usage"`
	OverageWatch   string         `json:"overage_watch"`
	FailedPayments int64          `json:"failed_payments"`
	CreditsApplied int64          `json:"credits_applied"`
}

type BillingUsage struct {
	Name    string  `json:"name"`
	Percent float64 `json:"percent"`
	Tenant  string  `json:"tenant"`
}

type EscalationsSummary struct {
	Provider   []string `json:"provider"`
	Compliance []string `json:"compliance"`
	Billing    []string `json:"billing"`
}

// --- Shared Helper Functions ---

func round(value float64, decimals int) float64 {
	factor := math.Pow(10, float64(decimals))
	return math.Round(value*factor) / factor
}

func percentage(part, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

func percentOf(value, limit int) float64 {
	if limit <= 0 {
		return 0
	}
	return float64(value) / float64(limit) * 100
}

func maxFloat(values ...float64) float64 {
	max := 0.0
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	return max
}

func truncate(value string, length int) string {
	if len(value) <= length {
		return value
	}
	runes := []rune(value)
	if len(runes) <= length {
		return value
	}
	return string(runes[:length]) + "…"
}
