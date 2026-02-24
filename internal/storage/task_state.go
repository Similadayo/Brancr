package storage

import (
	"time"

	"github.com/google/uuid"
)

// TaskState stores conversational task state in the database
// This enables multi-turn task completion where the bot remembers
// what information has been gathered and what's still needed
type TaskState struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TenantID  uuid.UUID `gorm:"index;not null"`
	ChatID    int64     `gorm:"index;not null"`
	TaskType  string    `gorm:"size:50;not null"`               // "schedule_post", "connect_account", "update_business"
	TaskData  string    `gorm:"type:text"`                      // JSON serialized ActiveTask with slots
	Status    string    `gorm:"size:20;default:'active';index"` // active, completed, abandoned
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName specifies the table name for GORM
func (TaskState) TableName() string {
	return "task_states"
}
