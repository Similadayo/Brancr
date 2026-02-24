package repositories

import (
	"github.com/google/uuid"
	"context"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// ServiceRepository handles service data operations
type ServiceRepository struct {
	db *gorm.DB
}

// NewServiceRepository creates a new service repository
func NewServiceRepository(db *gorm.DB) *ServiceRepository {
	return &ServiceRepository{db: db}
}

// GetByID retrieves a service by ID
func (r *ServiceRepository) GetByID(ctx context.Context, id uuid.UUID) (*storage.Service, error) {
	var service storage.Service
	if err := r.db.WithContext(ctx).First(&service, id).Error; err != nil {
		return nil, err
	}
	return &service, nil
}

// GetByTenant retrieves all services for a tenant
func (r *ServiceRepository) GetByTenant(ctx context.Context, tenantID uuid.UUID) ([]storage.Service, error) {
	var services []storage.Service
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("created_at DESC").
		Find(&services).Error; err != nil {
		return nil, err
	}
	return services, nil
}

// GetActiveByTenant retrieves active services for a tenant
func (r *ServiceRepository) GetActiveByTenant(ctx context.Context, tenantID uuid.UUID) ([]storage.Service, error) {
	var services []storage.Service
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND is_active = ?", tenantID, true).
		Order("created_at DESC").
		Find(&services).Error; err != nil {
		return nil, err
	}
	return services, nil
}

// SearchByTenant searches services by name or description
func (r *ServiceRepository) SearchByTenant(ctx context.Context, tenantID uuid.UUID, query string) ([]storage.Service, error) {
	var services []storage.Service
	searchPattern := "%" + query + "%"
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND is_active = ?", tenantID, true).
		Where("name ILIKE ? OR description ILIKE ?", searchPattern, searchPattern).
		Order("created_at DESC").
		Find(&services).Error; err != nil {
		return nil, err
	}
	return services, nil
}

// GetByCategory retrieves services by category for a tenant
func (r *ServiceRepository) GetByCategory(ctx context.Context, tenantID uuid.UUID, category string) ([]storage.Service, error) {
	var services []storage.Service
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND category = ? AND is_active = ?", tenantID, category, true).
		Order("created_at DESC").
		Find(&services).Error; err != nil {
		return nil, err
	}
	return services, nil
}

// Create creates a new service
func (r *ServiceRepository) Create(ctx context.Context, service *storage.Service) error {
	return r.db.WithContext(ctx).Create(service).Error
}

// Update updates an existing service
func (r *ServiceRepository) Update(ctx context.Context, service *storage.Service) error {
	return r.db.WithContext(ctx).Save(service).Error
}

// Delete soft deletes a service
func (r *ServiceRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&storage.Service{}, id).Error
}
