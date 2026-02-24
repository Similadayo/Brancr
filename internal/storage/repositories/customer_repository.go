package repositories

import (
	"github.com/google/uuid"
	"context"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CustomerRepository handles customer data operations
type CustomerRepository struct {
	db *gorm.DB
}

// NewCustomerRepository creates a new customer repository
func NewCustomerRepository(db *gorm.DB) *CustomerRepository {
	return &CustomerRepository{db: db}
}

// GetByPlatform retrieves a customer by tenant, channel, and platform ID
func (r *CustomerRepository) GetByPlatform(ctx context.Context, tenantID uuid.UUID, channel, platformID string) (*storage.Customer, error) {
	var customer storage.Customer
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND channel = ? AND platform_id = ?", tenantID, channel, platformID).
		First(&customer).Error; err != nil {
		return nil, err
	}
	return &customer, nil
}

// CreateOrUpdate creates or updates a customer
func (r *CustomerRepository) CreateOrUpdate(ctx context.Context, customer *storage.Customer) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "channel"}, {Name: "platform_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_message_at", "display_name", "profile_pic_url", "username"}),
	}).Create(customer).Error
}

// GetByTenant retrieves all customers for a tenant
func (r *CustomerRepository) GetByTenant(ctx context.Context, tenantID uuid.UUID) ([]storage.Customer, error) {
	var customers []storage.Customer
	if err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&customers).Error; err != nil {
		return nil, err
	}
	return customers, nil
}

// UpdateLastMessage updates the last message timestamp for a customer
func (r *CustomerRepository) UpdateLastMessage(ctx context.Context, customerID uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&storage.Customer{}).
		Where("id = ?", customerID).
		Update("last_message_at", "NOW()").Error
}
