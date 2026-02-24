package repositories

import (
	"github.com/google/uuid"
	"context"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// BookingRepo provides DB access for Bookings
type BookingRepo struct{ DB *gorm.DB }

func NewBookingRepo(db *gorm.DB) *BookingRepo { return &BookingRepo{DB: db} }

func (r *BookingRepo) Create(ctx context.Context, b *storage.Booking) error {
	return r.DB.WithContext(ctx).Create(b).Error
}

func (r *BookingRepo) GetByID(ctx context.Context, id uuid.UUID) (*storage.Booking, error) {
	var b storage.Booking
	if err := r.DB.WithContext(ctx).First(&b, id).Error; err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *BookingRepo) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]storage.Booking, error) {
	var out []storage.Booking
	if err := r.DB.WithContext(ctx).Where("tenant_id = ?", tenantID).Order("start_at desc").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *BookingRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	return r.DB.WithContext(ctx).Model(&storage.Booking{}).Where("id = ?", id).Update("status", status).Error
}
