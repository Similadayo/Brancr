package bot

import (
	"errors"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// Prevent duplicate events of the same type & sender within 5 minutes
func isDuplicateEvent(db *gorm.DB, tenantID uint, eventType, sender, message string) bool {
	var recent storage.EventLog
	err := db.Where("tenant_id = ? AND event_type = ? AND sender_name = ? AND created_at > ?", tenantID, eventType, sender, time.Now().Add(-5*time.Minute)).
		Order("id desc").Limit(1).Take(&recent).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// no previous record — not a duplicate
			return false
		}
		// On any DB error, err on the side of safety (do not treat as duplicate)
		return false
	}
	return recent.Message == message
}
