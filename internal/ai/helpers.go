package ai

import (
	"github.com/google/uuid"
	"context"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// IsAIEnabled returns true if tenant is in AI mode (not human mode)
func IsAIEnabled(ctx context.Context, db *gorm.DB, tenantID uuid.UUID) (bool, error) {
	var t storage.Tenant
	if err := db.WithContext(ctx).Select("ai_human_mode").First(&t, tenantID).Error; err != nil {
		return false, err
	}
	return t.AIHumanMode != "human", nil
}
