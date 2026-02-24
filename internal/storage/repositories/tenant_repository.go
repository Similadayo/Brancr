package repositories

import (
	"github.com/google/uuid"
	"context"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// TenantRepository handles tenant data operations
type TenantRepository struct {
	db *gorm.DB
}

// NewTenantRepository creates a new tenant repository
func NewTenantRepository(db *gorm.DB) *TenantRepository {
	return &TenantRepository{db: db}
}

// GetByID retrieves a tenant by ID
func (r *TenantRepository) GetByID(ctx context.Context, id uuid.UUID) (*storage.Tenant, error) {
	var tenant storage.Tenant
	if err := r.db.WithContext(ctx).Preload("BusinessProfile").First(&tenant, id).Error; err != nil {
		return nil, err
	}
	return &tenant, nil
}

// GetByOwnerTelegramID retrieves a tenant by owner Telegram ID
func (r *TenantRepository) GetByOwnerTelegramID(ctx context.Context, ownerID int64) (*storage.Tenant, error) {
	var tenant storage.Tenant
	if err := r.db.WithContext(ctx).Where("owner_telegram_id = ?", ownerID).Preload("BusinessProfile").First(&tenant).Error; err != nil {
		return nil, err
	}
	return &tenant, nil
}

// Create creates a new tenant
func (r *TenantRepository) Create(ctx context.Context, tenant *storage.Tenant) error {
	return r.db.WithContext(ctx).Create(tenant).Error
}

// Update updates an existing tenant
func (r *TenantRepository) Update(ctx context.Context, tenant *storage.Tenant) error {
	return r.db.WithContext(ctx).Save(tenant).Error
}

// GetActiveTenants retrieves all active tenants
func (r *TenantRepository) GetActiveTenants(ctx context.Context) ([]storage.Tenant, error) {
	var tenants []storage.Tenant
	if err := r.db.WithContext(ctx).Where("status = ?", "active").Find(&tenants).Error; err != nil {
		return nil, err
	}
	return tenants, nil
}

// Delete soft deletes a tenant
func (r *TenantRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&storage.Tenant{}, id).Error
}
