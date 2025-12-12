package repositories

import (
	"context"
	"errors"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

type NotificationSettingsRepository struct {
	db *gorm.DB
}

func NewNotificationSettingsRepository(db *gorm.DB) *NotificationSettingsRepository {
	return &NotificationSettingsRepository{db: db}
}

func (r *NotificationSettingsRepository) GetOrCreateDefault(ctx context.Context, tenantID uint) (*storage.TenantNotificationSettings, error) {
	var settings storage.TenantNotificationSettings
	if err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).First(&settings).Error; err == nil {
		return &settings, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	settings = storage.TenantNotificationSettings{
		TenantID:                   tenantID,
		EmailNotificationsEnabled:  true,
		EmailOrderStatusChanges:    true,
		EmailPaymentConfirmations:  true,
		EmailReceiptGenerated:      true,
		WebhookEnabled:             false,
		WebhookURL:                 "",
		WebhookEvents:              "[]",
		WebhookSecret:              "",
		LastTestAt:                 nil,
		LastTestStatus:             "",
	}

	if err := r.db.WithContext(ctx).Create(&settings).Error; err != nil {
		return nil, err
	}

	return &settings, nil
}

func (r *NotificationSettingsRepository) Update(ctx context.Context, settings *storage.TenantNotificationSettings) error {
	return r.db.WithContext(ctx).Save(settings).Error
}
