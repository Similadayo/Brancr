package repositories

import (
	"github.com/google/uuid"
	"context"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// MenuItemRepository handles menu item data operations
type MenuItemRepository struct {
	db *gorm.DB
}

// NewMenuItemRepository creates a new menu item repository
func NewMenuItemRepository(db *gorm.DB) *MenuItemRepository {
	return &MenuItemRepository{db: db}
}

// GetByID retrieves a menu item by ID
func (r *MenuItemRepository) GetByID(ctx context.Context, id uuid.UUID) (*storage.MenuItem, error) {
	var item storage.MenuItem
	if err := r.db.WithContext(ctx).First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// GetByTenant retrieves all menu items for a tenant
func (r *MenuItemRepository) GetByTenant(ctx context.Context, tenantID uuid.UUID) ([]storage.MenuItem, error) {
	var items []storage.MenuItem
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("category ASC, created_at DESC").
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// GetActiveByTenant retrieves active menu items for a tenant
func (r *MenuItemRepository) GetActiveByTenant(ctx context.Context, tenantID uuid.UUID) ([]storage.MenuItem, error) {
	var items []storage.MenuItem
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND is_active = ?", tenantID, true).
		Order("category ASC, created_at DESC").
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// SearchByTenant searches menu items by name or description
func (r *MenuItemRepository) SearchByTenant(ctx context.Context, tenantID uuid.UUID, query string) ([]storage.MenuItem, error) {
	var items []storage.MenuItem
	searchPattern := "%" + query + "%"
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND is_active = ?", tenantID, true).
		Where("name ILIKE ? OR description ILIKE ?", searchPattern, searchPattern).
		Order("category ASC, created_at DESC").
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// GetByCategory retrieves menu items by category for a tenant
func (r *MenuItemRepository) GetByCategory(ctx context.Context, tenantID uuid.UUID, category string) ([]storage.MenuItem, error) {
	var items []storage.MenuItem
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND category = ? AND is_active = ?", tenantID, category, true).
		Order("created_at DESC").
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// Create creates a new menu item
func (r *MenuItemRepository) Create(ctx context.Context, item *storage.MenuItem) error {
	return r.db.WithContext(ctx).Create(item).Error
}

// Update updates an existing menu item
func (r *MenuItemRepository) Update(ctx context.Context, item *storage.MenuItem) error {
	return r.db.WithContext(ctx).Save(item).Error
}

// Delete soft deletes a menu item
func (r *MenuItemRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&storage.MenuItem{}, id).Error
}
