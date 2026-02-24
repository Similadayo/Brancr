package repositories

import (
	"time"

	"github.com/google/uuid"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

type TenantKYBRepo struct {
	db *gorm.DB
}

func NewTenantKYBRepo(db *gorm.DB) *TenantKYBRepo { return &TenantKYBRepo{db: db} }

func (r *TenantKYBRepo) SubmitKYB(tenantID uuid.UUID, data string) error {
	return r.db.Model(&storage.Tenant{}).Where("id = ?", tenantID).Updates(map[string]interface{}{
		"kyb_status":       "pending",
		"kyb_data":         data,
		"kyb_submitted_at": time.Now(),
	}).Error
}

func (r *TenantKYBRepo) GetKYB(tenantID uuid.UUID) (*storage.Tenant, error) {
	var t storage.Tenant
	if err := r.db.First(&t, tenantID).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *TenantKYBRepo) VerifyKYB(tenantID uuid.UUID, adminID uuid.UUID) error {
	return r.db.Model(&storage.Tenant{}).Where("id = ?", tenantID).Updates(map[string]interface{}{
		"kyb_status":               "verified",
		"kyb_verified_at":          time.Now(),
		"kyb_verified_by_admin_id": adminID,
	}).Error
}

func (r *TenantKYBRepo) RejectKYB(tenantID uuid.UUID, reason string) error {
	return r.db.Model(&storage.Tenant{}).Where("id = ?", tenantID).Updates(map[string]interface{}{
		"kyb_status": "rejected",
		"kyb_data":   gorm.Expr("jsonb_set(kyb_data, '{rejection_reason}', to_jsonb(?::text))", reason),
	}).Error
}
