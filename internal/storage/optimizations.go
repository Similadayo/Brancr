package storage

import (
	"gorm.io/gorm"
)

// OptimizeQueries provides query optimization utilities
type OptimizeQueries struct {
	DB *gorm.DB
}

// NewOptimizeQueries creates a new query optimizer
func NewOptimizeQueries(db *gorm.DB) *OptimizeQueries {
	return &OptimizeQueries{DB: db}
}

// FindInBatches finds records in batches to avoid loading too much into memory
func (o *OptimizeQueries) FindInBatches(dest interface{}, batchSize int, fc func(tx *gorm.DB, batch int) error) error {
	result := o.DB.FindInBatches(dest, batchSize, fc)
	return result.Error
}

// SelectFields selects only specified fields to reduce data transfer
func (o *OptimizeQueries) SelectFields(fields []string) *gorm.DB {
	return o.DB.Select(fields)
}

// BatchCreate creates records in batches
func (o *OptimizeQueries) BatchCreate(dest interface{}, batchSize int) error {
	return o.DB.CreateInBatches(dest, batchSize).Error
}

// PreloadRelations preloads relations efficiently
func (o *OptimizeQueries) PreloadRelations(relations []string) *gorm.DB {
	query := o.DB
	for _, rel := range relations {
		query = query.Preload(rel)
	}
	return query
}
