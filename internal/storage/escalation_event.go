package storage

import (
	"time"

	"github.com/google/uuid"
)

// EscalationEvent contains context for a negotiation escalation (human approval step)
// MinPrice is stored for tenant-human context but must never be exposed to customers or logs without tenant approval.
type EscalationEvent struct {
	ID                  uuid.UUID `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	TenantID            uuid.UUID `gorm:"type:uuid;index"`
	SalesConversationID uuid.UUID `gorm:"type:uuid;index"`
	ProductID           uuid.UUID `gorm:"type:uuid;index"`
	ListPrice           float64
	MinPrice            float64   // sensitive internal floor, for tenant use only
	CustomerOffers      string    `gorm:"type:jsonb"` // JSON array of numeric offers
	AIOffers            string    `gorm:"type:jsonb"` // JSON array of AI concession offers
	Reason              string    `gorm:"size:255"`
	Status              string    `gorm:"size:50"` // pending/approved/rejected
	CreatedAt           time.Time `gorm:"autoCreateTime"`
	UpdatedAt           time.Time `gorm:"autoUpdateTime"`
}
