package storage

import "time"

// AdminCase represents a formal admin authorization case for sensitive actions.
type AdminCase struct {
	ID         uint
	TenantID   uint
	Source     string
	Status     string
	Reason     string
	CreatedBy  string
	AssignedTo *string
	CreatedAt  time.Time
	ClosedAt   *time.Time
}
