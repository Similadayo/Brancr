package repositories

import (
	"github.com/google/uuid"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

type DraftRepo struct {
	db *gorm.DB
}

func NewDraftRepo(db *gorm.DB) *DraftRepo { return &DraftRepo{db: db} }

func (r *DraftRepo) Create(d *storage.Draft) error {
	d.CreatedAt = time.Now()
	d.UpdatedAt = time.Now()
	return r.db.Create(d).Error
}

func (r *DraftRepo) Update(d *storage.Draft) error {
	d.UpdatedAt = time.Now()
	return r.db.Save(d).Error
}

func (r *DraftRepo) GetByID(id uuid.UUID) (*storage.Draft, error) {
	var d storage.Draft
	if err := r.db.First(&d, id).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *DraftRepo) GetByTenantAndKey(tenantID uuid.UUID, key string) (*storage.Draft, error) {
	var d storage.Draft
	if err := r.db.Where("tenant_id = ? AND key = ?", tenantID, key).First(&d).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *DraftRepo) ListByTenant(tenantID uuid.UUID) ([]storage.Draft, error) {
	var ds []storage.Draft
	if err := r.db.Where("tenant_id = ?", tenantID).Find(&ds).Error; err != nil {
		return nil, err
	}
	return ds, nil
}

func (r *DraftRepo) Delete(id uuid.UUID) error {
	return r.db.Delete(&storage.Draft{}, id).Error
}
