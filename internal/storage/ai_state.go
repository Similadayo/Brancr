package storage

import (
	"time"

	"github.com/google/uuid" // Assuming uuid package is used for uuid.UUID
	"github.com/similadayo/brancr/internal/domain"
)

// Base struct is assumed to be defined elsewhere, providing ID, CreatedAt, UpdatedAt, DeletedAt.
// For the purpose of this edit, we assume its existence and that it provides uuid.UUID for ID.

// ConversationAIState is a small, persisted snapshot of the AI's last behavioral state
// for a given conversation. It supports hysteresis (e.g., staying in Support until calm streak).
//
// This is NOT raw message storage; it only stores derived signals and state for safe continuity.
type ConversationAIState struct {
	Base
	TenantID       uuid.UUID `gorm:"index"`
	CustomerID     uuid.UUID `gorm:"index"`
	ConversationID uuid.UUID `gorm:"uniqueIndex"`

	State              string  `gorm:"size:30;index"` // greeting, discovery, sales, support, confirm, payment, post_payment, escalation
	Mode               string  `gorm:"size:40"`       // supportive_support, persuasive_sales, neutral_assistant
	EmotionLabel       string  `gorm:"size:30"`
	EmotionIntensity   float64 `gorm:"default:0"`
	CalmStreak         int     `gorm:"default:0"`
	HostilityStreak    int     `gorm:"default:0"` // consecutive hostile/frustrated turns (for behavioral escalation thresholds)
	RiskLevel          string  `gorm:"size:20;default:'low'"`
	RiskReasons        string  `gorm:"type:jsonb;default:'[]'"`
	DecisionConfidence float64 `gorm:"default:0"`

	LastEscalationReason   string `gorm:"size:255"`
	LastEscalationCategory string `gorm:"size:30"`

	// Phase 1: Transaction State (New Architecture)
	TransactionState domain.TransactionState `gorm:"size:30;default:'browsing'"`

	// Phase 5: Commitment Tracking
	CommittedAt *time.Time // Set when entering Committed, cleared on reversal

	// Phase 7: Payment Finalization
	PaidAt *time.Time // Set when entering Paid, never cleared
}

// AIFeedbackEvent is a tenant-scoped feedback signal used for evaluation/learning.
// Examples: tenant edited suggested reply; escalation marked premature; thumbs up/down.
type AIFeedbackEvent struct {
	Base
	TenantID       uuid.UUID `gorm:"index"`
	CustomerID     uuid.UUID `gorm:"index"`
	ConversationID uuid.UUID `gorm:"index"`
	InteractionID  uuid.UUID `gorm:"index"`

	Type string `gorm:"size:50;index"` // tenant_edit, escalation_premature, escalation_late, thumbs_up, thumbs_down

	SuggestedReply string `gorm:"type:text"`
	FinalReply     string `gorm:"type:text"`

	DecisionState string `gorm:"size:30"`
	DecisionMeta  string `gorm:"type:jsonb;default:'{}'"`
}
