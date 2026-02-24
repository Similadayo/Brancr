package subscription

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/alerts"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Service handles subscription monitoring and alerts
type Service struct {
	DB           *gorm.DB
	Logger       *zap.Logger
	AlertService *alerts.AlertService
}

// NewService creates a new subscription service
func NewService(db *gorm.DB, logger *zap.Logger, alertService *alerts.AlertService) *Service {
	return &Service{
		DB:           db,
		Logger:       logger,
		AlertService: alertService,
	}
}

// CheckExpiringSubscriptions checks for subscriptions expiring soon and sends alerts
func (s *Service) CheckExpiringSubscriptions(ctx context.Context) error {
	now := time.Now()

	// Check for subscriptions expiring in 7 days
	sevenDaysFromNow := now.Add(7 * 24 * time.Hour)
	tenants7Days, err := s.getTenantsExpiringBetween(ctx, now, sevenDaysFromNow)
	if err != nil {
		return fmt.Errorf("failed to get tenants expiring in 7 days: %w", err)
	}

	for _, tenant := range tenants7Days {
		if err := s.sendExpirationAlert(ctx, &tenant, 7); err != nil {
			s.Logger.Warn("failed to send 7-day expiration alert",
				zap.Stringer("tenant_id", tenant.ID),
				zap.Error(err))
		}
	}

	// Check for subscriptions expiring in 3 days
	threeDaysFromNow := now.Add(3 * 24 * time.Hour)
	tenants3Days, err := s.getTenantsExpiringBetween(ctx, now, threeDaysFromNow)
	if err != nil {
		return fmt.Errorf("failed to get tenants expiring in 3 days: %w", err)
	}

	for _, tenant := range tenants3Days {
		if err := s.sendExpirationAlert(ctx, &tenant, 3); err != nil {
			s.Logger.Warn("failed to send 3-day expiration alert",
				zap.Stringer("tenant_id", tenant.ID),
				zap.Error(err))
		}
	}

	// Check for subscriptions expiring in 1 day
	oneDayFromNow := now.Add(24 * time.Hour)
	tenants1Day, err := s.getTenantsExpiringBetween(ctx, now, oneDayFromNow)
	if err != nil {
		return fmt.Errorf("failed to get tenants expiring in 1 day: %w", err)
	}

	for _, tenant := range tenants1Day {
		if err := s.sendExpirationAlert(ctx, &tenant, 1); err != nil {
			s.Logger.Warn("failed to send 1-day expiration alert",
				zap.Stringer("tenant_id", tenant.ID),
				zap.Error(err))
		}
	}

	// Check for expired subscriptions
	tenantsExpired, err := s.getExpiredTenants(ctx, now)
	if err != nil {
		return fmt.Errorf("failed to get expired tenants: %w", err)
	}

	for _, tenant := range tenantsExpired {
		if err := s.sendExpiredAlert(ctx, &tenant); err != nil {
			s.Logger.Warn("failed to send expired alert",
				zap.Stringer("tenant_id", tenant.ID),
				zap.Error(err))
		}
	}

	return nil
}

// getTenantsExpiringBetween gets tenants with subscriptions expiring between two dates
func (s *Service) getTenantsExpiringBetween(ctx context.Context, start, end time.Time) ([]storage.Tenant, error) {
	var tenants []storage.Tenant

	// Only check active tenants with subscription plans (not free/trial)
	query := s.DB.WithContext(ctx).
		Where("status = ?", "active").
		Where("plan_type NOT IN (?)", []string{"free", "trial", ""}).
		Where("subscription_expires_at IS NOT NULL").
		Where("subscription_expires_at >= ?", start).
		Where("subscription_expires_at <= ?", end)

	// Exclude tenants that already received an alert for this expiration window
	// We'll check if they have a recent alert of this type
	query = query.Where(`
		NOT EXISTS (
			SELECT 1 FROM tenant_alerts 
			WHERE tenant_alerts.tenant_id = tenants.id 
			AND tenant_alerts.type = 'billing'
			AND tenant_alerts.metadata::text LIKE '%"expiration_days":%'
			AND tenant_alerts.created_at > ?
		)
	`, time.Now().Add(-24*time.Hour))

	if err := query.Find(&tenants).Error; err != nil {
		return nil, err
	}

	return tenants, nil
}

// getExpiredTenants gets tenants with expired subscriptions
func (s *Service) getExpiredTenants(ctx context.Context, now time.Time) ([]storage.Tenant, error) {
	var tenants []storage.Tenant

	query := s.DB.WithContext(ctx).
		Where("status = ?", "active").
		Where("plan_type NOT IN (?)", []string{"free", "trial", ""}).
		Where("subscription_expires_at IS NOT NULL").
		Where("subscription_expires_at < ?", now)

	// Exclude tenants that already received an expired alert in the last 24 hours
	query = query.Where(`
		NOT EXISTS (
			SELECT 1 FROM tenant_alerts 
			WHERE tenant_alerts.tenant_id = tenants.id 
			AND tenant_alerts.type = 'billing'
			AND tenant_alerts.metadata::text LIKE '%"expired":true%'
			AND tenant_alerts.created_at > ?
		)
	`, time.Now().Add(-24*time.Hour))

	if err := query.Find(&tenants).Error; err != nil {
		return nil, err
	}

	return tenants, nil
}

// sendExpirationAlert sends an alert for a subscription expiring soon
func (s *Service) sendExpirationAlert(ctx context.Context, tenant *storage.Tenant, daysUntilExpiration int) error {
	if s.AlertService == nil {
		return fmt.Errorf("alert service not available")
	}

	var expiresAt time.Time
	if tenant.SubscriptionExpiresAt != nil {
		expiresAt = *tenant.SubscriptionExpiresAt
	} else {
		return fmt.Errorf("tenant has no subscription expiration date")
	}

	severity := "warning"
	if daysUntilExpiration == 1 {
		severity = "critical"
	}

	title := fmt.Sprintf("Subscription Expiring in %d Day(s)", daysUntilExpiration)
	message := fmt.Sprintf(
		"Your %s subscription for %s will expire on %s. Please renew to continue using all features.",
		tenant.PlanType,
		tenant.Name,
		expiresAt.Format("January 2, 2006"),
	)

	// Create metadata for the alert
	metadata := map[string]interface{}{
		"expiration_days": daysUntilExpiration,
		"expires_at":      expiresAt.Format(time.RFC3339),
		"plan_type":       tenant.PlanType,
	}
	metadataJSON, _ := json.Marshal(metadata)

	alert, err := s.AlertService.SendAlert(
		ctx,
		tenant.ID,
		"billing",
		severity,
		title,
		message,
		"", // action URL - could link to billing page
		"Renew Subscription",
	)

	if err != nil {
		return err
	}

	// Update alert metadata
	if alert != nil {
		alert.Metadata = string(metadataJSON)
		s.DB.Save(alert)
	}

	s.Logger.Info("subscription expiration alert sent",
		zap.Stringer("tenant_id", tenant.ID),
		zap.Int("days_until_expiration", daysUntilExpiration))

	return nil
}

// sendExpiredAlert sends an alert for an expired subscription
func (s *Service) sendExpiredAlert(ctx context.Context, tenant *storage.Tenant) error {
	if s.AlertService == nil {
		return fmt.Errorf("alert service not available")
	}

	var expiresAt time.Time
	if tenant.SubscriptionExpiresAt != nil {
		expiresAt = *tenant.SubscriptionExpiresAt
	} else {
		return fmt.Errorf("tenant has no subscription expiration date")
	}

	title := "Subscription Expired"
	message := fmt.Sprintf(
		"Your %s subscription for %s expired on %s. Please renew immediately to restore access to all features.",
		tenant.PlanType,
		tenant.Name,
		expiresAt.Format("January 2, 2006"),
	)

	// Create metadata for the alert
	metadata := map[string]interface{}{
		"expired":    true,
		"expires_at": expiresAt.Format(time.RFC3339),
		"plan_type":  tenant.PlanType,
	}
	metadataJSON, _ := json.Marshal(metadata)

	alert, err := s.AlertService.SendAlert(
		ctx,
		tenant.ID,
		"billing",
		"critical",
		title,
		message,
		"", // action URL - could link to billing page
		"Renew Subscription",
	)

	if err != nil {
		return err
	}

	// Update alert metadata
	if alert != nil {
		alert.Metadata = string(metadataJSON)
		s.DB.Save(alert)
	}

	s.Logger.Info("subscription expired alert sent",
		zap.Stringer("tenant_id", tenant.ID))

	return nil
}

// OnSubscriptionPayment handles subscription payment events
func (s *Service) OnSubscriptionPayment(ctx context.Context, tenantID uuid.UUID, amount float64, currency string, paymentMethod string, subscriptionDurationDays int) error {
	var tenant storage.Tenant
	if err := s.DB.WithContext(ctx).First(&tenant, tenantID).Error; err != nil {
		return fmt.Errorf("tenant not found: %w", err)
	}

	// Calculate new expiration date
	now := time.Now()
	var newExpiresAt time.Time

	if tenant.SubscriptionExpiresAt != nil && tenant.SubscriptionExpiresAt.After(now) {
		// Extend existing subscription
		newExpiresAt = tenant.SubscriptionExpiresAt.Add(time.Duration(subscriptionDurationDays) * 24 * time.Hour)
	} else {
		// New subscription or expired subscription
		newExpiresAt = now.Add(time.Duration(subscriptionDurationDays) * 24 * time.Hour)
	}

	// Update tenant subscription
	tenant.SubscriptionExpiresAt = &newExpiresAt
	tenant.SubscriptionLastPaymentAt = &now
	tenant.SubscriptionLastPaymentAmount = &amount

	if err := s.DB.WithContext(ctx).Save(&tenant).Error; err != nil {
		return fmt.Errorf("failed to update tenant subscription: %w", err)
	}

	// Send payment confirmation alert
	if s.AlertService != nil {
		title := "Subscription Payment Received"
		message := fmt.Sprintf(
			"Your subscription payment of %s %.2f has been received. Your %s subscription is now active until %s.",
			currency,
			amount,
			tenant.PlanType,
			newExpiresAt.Format("January 2, 2006"),
		)

		metadata := map[string]interface{}{
			"payment_received": true,
			"amount":           amount,
			"currency":         currency,
			"payment_method":   paymentMethod,
			"expires_at":       newExpiresAt.Format(time.RFC3339),
		}
		metadataJSON, _ := json.Marshal(metadata)

		alert, err := s.AlertService.SendAlert(
			ctx,
			tenantID,
			"billing",
			"info",
			title,
			message,
			"",
			"",
		)

		if err != nil {
			s.Logger.Warn("failed to send payment confirmation alert",
				zap.Stringer("tenant_id", tenantID),
				zap.Error(err))
		} else if alert != nil {
			alert.Metadata = string(metadataJSON)
			s.DB.Save(alert)
		}
	}

	s.Logger.Info("subscription payment processed",
		zap.Stringer("tenant_id", tenantID),
		zap.Float64("amount", amount),
		zap.String("currency", currency),
		zap.Time("expires_at", newExpiresAt))

	return nil
}

// GetTenantsByPlanType gets tenants filtered by plan type
func (s *Service) GetTenantsByPlanType(ctx context.Context, planType string) ([]storage.Tenant, error) {
	var tenants []storage.Tenant

	query := s.DB.WithContext(ctx)
	if planType != "" {
		query = query.Where("plan_type = ?", planType)
	}

	if err := query.Find(&tenants).Error; err != nil {
		return nil, err
	}

	return tenants, nil
}
