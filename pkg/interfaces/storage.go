package interfaces

import (
	"context"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// Repository defines common database operations
type Repository interface {
	DB() *gorm.DB
}

// TenantRepository handles tenant data operations
type TenantRepository interface {
	GetByID(ctx context.Context, id uint) (*storage.Tenant, error)
	GetByOwnerTelegramID(ctx context.Context, ownerID int64) (*storage.Tenant, error)
	Create(ctx context.Context, tenant *storage.Tenant) error
	Update(ctx context.Context, tenant *storage.Tenant) error
}

// CustomerRepository handles customer data operations
type CustomerRepository interface {
	GetByPlatform(ctx context.Context, tenantID uint, channel, platformID string) (*storage.Customer, error)
	CreateOrUpdate(ctx context.Context, customer *storage.Customer) error
}

// SocialAccountRepository handles social account operations
type SocialAccountRepository interface {
	GetByTenantAndPlatform(ctx context.Context, tenantID uint, platform string) (*storage.SocialAccount, error)
	GetConnected(ctx context.Context, tenantID uint) ([]storage.SocialAccount, error)
	CreateOrUpdate(ctx context.Context, account *storage.SocialAccount) error
}
