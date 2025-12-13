package storage

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// EnsureTenantOnboarding ensures the minimal onboarding artifacts exist for a tenant.
// It is safe to call multiple times.
func EnsureTenantOnboarding(ctx context.Context, db *gorm.DB, tenantID uint, companyName string) error {
	if tenantID == 0 {
		return errors.New("tenantID is required")
	}
	if db == nil {
		return errors.New("db is required")
	}

	// Business profile (used by onboarding + bot behavior)
	var profile BusinessProfile
	if err := db.WithContext(ctx).
		Where(&BusinessProfile{TenantID: tenantID}).
		Attrs(&BusinessProfile{TenantID: tenantID, Name: companyName, Tone: "friendly"}).
		FirstOrCreate(&profile).Error; err != nil {
		return err
	}

	// Persona (required by AI/bot defaults; PK=tenant_id)
	var persona Persona
	if err := db.WithContext(ctx).
		Where(&Persona{TenantID: tenantID}).
		Attrs(&Persona{TenantID: tenantID, BotName: "Assistant", Tone: "friendly, helpful", Language: "en", Humor: true, StyleNotes: ""}).
		FirstOrCreate(&persona).Error; err != nil {
		return err
	}

	// Notification settings (used by webhook/email notifications)
	var notif TenantNotificationSettings
	if err := db.WithContext(ctx).
		Where(&TenantNotificationSettings{TenantID: tenantID}).
		Attrs(&TenantNotificationSettings{
			TenantID:                  tenantID,
			EmailNotificationsEnabled: true,
			EmailOrderStatusChanges:   true,
			EmailPaymentConfirmations: true,
			EmailReceiptGenerated:     true,
			WebhookEnabled:            false,
			WebhookURL:                "",
			WebhookEvents:             "[]",
			WebhookSecret:             "",
			LastTestAt:                nil,
			LastTestStatus:            "",
		}).
		FirstOrCreate(&notif).Error; err != nil {
		return err
	}

	return nil
}
