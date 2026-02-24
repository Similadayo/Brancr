package storage

import (
	"time"

	"github.com/google/uuid"
)

// ExperienceCheckpoint represents a feedback checkpoint triggered by an event
type ExperienceCheckpoint struct {
	Base
	TenantID    uuid.UUID `gorm:"index"`
	EventType   string    `gorm:"size:50;index"` // first_reply, escalation, ai_toggle, onboarding_48h
	EventID     uuid.UUID `gorm:"index"`         // Reference to the triggering event
	Status      string    `gorm:"size:20;index"` // pending, responded, dismissed, expired
	DismissedAt *time.Time
	ExpiresAt   time.Time // Auto-expire after 72 hours
}

// CheckpointResponse represents a tenant's response to a checkpoint
type CheckpointResponse struct {
	Base
	CheckpointID uuid.UUID `gorm:"uniqueIndex"`
	TenantID     uuid.UUID `gorm:"index"`
	EventType    string    `gorm:"size:50;index"` // Denormalized for faster queries

	// Structured Response - based on event type
	SelectedReason string `gorm:"size:100"`     // e.g., "didnt_trust_reply", "customer_sensitive"
	TrustScore     *int   `gorm:"default:null"` // Optional 1-5 trust score
	FreeText       string `gorm:"type:text"`    // Optional free text

	// Context (for admin analysis)
	IndustryID  uuid.UUID `gorm:"index"`
	PlanID      uuid.UUID `gorm:"index"`
	DaysActive  int       // Days since tenant signup
	AIReplies   int       // Total AI replies at time of feedback
	Escalations int       // Total escalations at time of feedback
}
