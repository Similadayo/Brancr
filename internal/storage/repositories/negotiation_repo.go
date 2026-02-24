package repositories

import (
	"context"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// NegotiationRepo provides access to negotiation history records
type NegotiationRepo struct {
	DB *gorm.DB
}

func NewNegotiationRepo(db *gorm.DB) *NegotiationRepo { return &NegotiationRepo{DB: db} }

func (r *NegotiationRepo) ListBySalesConversation(ctx context.Context, salesConvID uuid.UUID) ([]storage.NegotiationHistory, error) {
	var out []storage.NegotiationHistory
	if err := r.DB.WithContext(ctx).Where("sales_conversation_id = ?", salesConvID).Order("created_at desc").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
