package repositories

import (
	"github.com/google/uuid"
	"context"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// ProductRepository handles product data operations
type ProductRepository struct {
	db *gorm.DB
}

// NewProductRepository creates a new product repository
func NewProductRepository(db *gorm.DB) *ProductRepository {
	return &ProductRepository{db: db}
}

// GetByID retrieves a product by ID
func (r *ProductRepository) GetByID(ctx context.Context, id uuid.UUID) (*storage.Product, error) {
	var product storage.Product
	if err := r.db.WithContext(ctx).First(&product, id).Error; err != nil {
		return nil, err
	}
	return &product, nil
}

// GetByTenant retrieves all products for a tenant
func (r *ProductRepository) GetByTenant(ctx context.Context, tenantID uuid.UUID) ([]storage.Product, error) {
	var products []storage.Product
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("created_at DESC").
		Find(&products).Error; err != nil {
		return nil, err
	}
	return products, nil
}

// GetActiveByTenant retrieves active products for a tenant
func (r *ProductRepository) GetActiveByTenant(ctx context.Context, tenantID uuid.UUID) ([]storage.Product, error) {
	var products []storage.Product
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND is_active = ?", tenantID, true).
		Order("created_at DESC").
		Find(&products).Error; err != nil {
		return nil, err
	}
	return products, nil
}

// SearchByTenant searches products by name, description, or category
func (r *ProductRepository) SearchByTenant(ctx context.Context, tenantID uuid.UUID, query string) ([]storage.Product, error) {
	var products []storage.Product
	searchPattern := "%" + query + "%"
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND is_active = ?", tenantID, true).
		Where("name ILIKE ? OR description ILIKE ? OR category ILIKE ?", searchPattern, searchPattern, searchPattern).
		Order("created_at DESC").
		Find(&products).Error; err != nil {
		return nil, err
	}
	return products, nil
}

// GetByCategory retrieves products by category for a tenant
func (r *ProductRepository) GetByCategory(ctx context.Context, tenantID uuid.UUID, category string) ([]storage.Product, error) {
	var products []storage.Product
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND category = ? AND is_active = ?", tenantID, category, true).
		Order("created_at DESC").
		Find(&products).Error; err != nil {
		return nil, err
	}
	return products, nil
}

// Create creates a new product
func (r *ProductRepository) Create(ctx context.Context, product *storage.Product) error {
	return r.db.WithContext(ctx).Create(product).Error
}

// Update updates an existing product
func (r *ProductRepository) Update(ctx context.Context, product *storage.Product) error {
	return r.db.WithContext(ctx).Save(product).Error
}

// Delete soft deletes a product
func (r *ProductRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&storage.Product{}, id).Error
}
