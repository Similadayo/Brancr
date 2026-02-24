package repositories

import (
	"github.com/google/uuid"
	"context"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// SocialAccountRepository handles social account operations
type SocialAccountRepository struct {
	db *gorm.DB
}

// NewSocialAccountRepository creates a new social account repository
func NewSocialAccountRepository(db *gorm.DB) *SocialAccountRepository {
	return &SocialAccountRepository{db: db}
}

// GetByTenantAndPlatform retrieves a social account by tenant and platform
func (r *SocialAccountRepository) GetByTenantAndPlatform(ctx context.Context, tenantID uuid.UUID, platform string) (*storage.SocialAccount, error) {
	var account storage.SocialAccount
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND platform = ?", tenantID, platform).
		First(&account).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

// GetConnected retrieves all connected social accounts for a tenant
func (r *SocialAccountRepository) GetConnected(ctx context.Context, tenantID uuid.UUID) ([]storage.SocialAccount, error) {
	var accounts []storage.SocialAccount
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND connected = ?", tenantID, true).
		Find(&accounts).Error; err != nil {
		return nil, err
	}
	return accounts, nil
}

// CreateOrUpdate creates or updates a social account
func (r *SocialAccountRepository) CreateOrUpdate(ctx context.Context, account *storage.SocialAccount) error {
	return r.db.WithContext(ctx).Save(account).Error
}

// Delete soft deletes a social account
func (r *SocialAccountRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&storage.SocialAccount{}, id).Error
}
