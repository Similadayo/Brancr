package storage

import (
	"time"

	"github.com/google/uuid"
)

// AdminCase represents a formal admin authorization case for sensitive actions.
type AdminCase struct {
	ID         uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TenantID   uuid.UUID `gorm:"index"`
	Source     string
	Status     string
	Reason     string
	CreatedBy  string
	AssignedTo *string
	CreatedAt  time.Time
	ClosedAt   *time.Time
}
