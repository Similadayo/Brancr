package repositories

import (
	"github.com/google/uuid"
	"context"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// IndustryRepository handles industry data operations
type IndustryRepository struct {
	db *gorm.DB
}

// NewIndustryRepository creates a new industry repository
func NewIndustryRepository(db *gorm.DB) *IndustryRepository {
	return &IndustryRepository{db: db}
}

// GetByID retrieves an industry by ID
func (r *IndustryRepository) GetByID(ctx context.Context, id uuid.UUID) (*storage.Industry, error) {
	var industry storage.Industry
	if err := r.db.WithContext(ctx).First(&industry, id).Error; err != nil {
		return nil, err
	}
	return &industry, nil
}

// GetByName retrieves an industry by name
func (r *IndustryRepository) GetByName(ctx context.Context, name string) (*storage.Industry, error) {
	var industry storage.Industry
	if err := r.db.WithContext(ctx).Where("name = ?", name).First(&industry).Error; err != nil {
		return nil, err
	}
	return &industry, nil
}

// GetAll retrieves all industries
func (r *IndustryRepository) GetAll(ctx context.Context) ([]storage.Industry, error) {
	var industries []storage.Industry
	if err := r.db.WithContext(ctx).Order("name ASC").Find(&industries).Error; err != nil {
		return nil, err
	}
	return industries, nil
}

// GetByCategory retrieves industries by category
func (r *IndustryRepository) GetByCategory(ctx context.Context, category string) ([]storage.Industry, error) {
	var industries []storage.Industry
	if err := r.db.WithContext(ctx).Where("category = ?", category).Find(&industries).Error; err != nil {
		return nil, err
	}
	return industries, nil
}

// Create creates a new industry
func (r *IndustryRepository) Create(ctx context.Context, industry *storage.Industry) error {
	return r.db.WithContext(ctx).Create(industry).Error
}

// Update updates an existing industry
func (r *IndustryRepository) Update(ctx context.Context, industry *storage.Industry) error {
	return r.db.WithContext(ctx).Save(industry).Error
}
