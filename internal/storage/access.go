package storage

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Global DB accessor for pure system functions (Executors)
// that need direct DB access without passing DB through the entire call stack.
var globalDB *gorm.DB

// SetDB initializes the global DB connection for storage helpers.
// Should be called during application startup or pipeline initialization.
func SetDB(db *gorm.DB) {
	globalDB = db
}

// GetPrimaryOfferingsByTenant fetches active products for a tenant.
// It prioritizes Products. If no products, it could look for Services (omitted for now to match Prompt simplicity).
func GetPrimaryOfferingsByTenant(tenantID uuid.UUID) ([]Product, error) {
	if globalDB == nil {
		// Fallback or error if DB not initialized
		return nil, gorm.ErrInvalidDB
	}

	var products []Product
	// Logic matches ProductRepository.GetActiveByTenant
	if err := globalDB.Where("tenant_id = ? AND is_active = ?", tenantID, true).
		Order("created_at DESC").
		Find(&products).Error; err != nil {
		return nil, err
	}

	return products, nil
}
