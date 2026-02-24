package storage

import (
	"time"

	"github.com/google/uuid"
)

// NegotiationState tracks per-conversation per-product negotiation state
// Stored separately for audibility and deterministic state machine enforcement
// Status values: "idle", "negotiating", "final_ai_offer", "escalation_eligible", "escalated"
type NegotiationState struct {
	ID                   uuid.UUID `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	SalesConversationID  uuid.UUID `gorm:"type:uuid;index"`
	ProductID            uuid.UUID `gorm:"type:uuid;index"` // optional - uuid.Nil if not tied to a specific product
	Status               string    `gorm:"size:50;default:'idle'"`
	LastCustomerOffer    float64   // last observed customer numeric offer
	SameOfferCount       int       `gorm:"default:0"`
	AIConcessionCount    int       `gorm:"default:0"`
	FlexibilityAttempted bool      `gorm:"default:false"` // true when non-price lever attempted
	LockAIPriceChanges   bool      `gorm:"default:false"` // once escalated, AI cannot change price
	CreatedAt            time.Time `gorm:"autoCreateTime"`
	UpdatedAt            time.Time `gorm:"autoUpdateTime"`
}
