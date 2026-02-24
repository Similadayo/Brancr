package notifications

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// OrderNotificationService handles order status notifications
type OrderNotificationService struct {
	DB           *gorm.DB
	Logger       *zap.Logger
	EmailService interface {
		SendOrderStatusEmail(to, customerName, orderNumber, status, portalURL string) error
	}
	WebhookService interface {
		SendWebhook(ctx context.Context, tenantID uuid.UUID, event string, payload map[string]interface{}) error
	}
	AlertService interface {
		SendAlert(ctx context.Context, tenantID uuid.UUID, alertType, severity, title, message, actionURL, actionLabel string) (*storage.TenantAlert, error)
	}
}

// NewOrderNotificationService creates a new notification service
func NewOrderNotificationService(db *gorm.DB, logger *zap.Logger) *OrderNotificationService {
	return &OrderNotificationService{
		DB:     db,
		Logger: logger,
	}
}

// SetEmailService sets the email service
func (s *OrderNotificationService) SetEmailService(emailService interface {
	SendOrderStatusEmail(to, customerName, orderNumber, status, portalURL string) error
}) {
	s.EmailService = emailService
}

// SetWebhookService sets the webhook service
func (s *OrderNotificationService) SetWebhookService(webhookService interface {
	SendWebhook(ctx context.Context, tenantID uuid.UUID, event string, payload map[string]interface{}) error
}) {
	s.WebhookService = webhookService
}

// SetAlertService sets the alert service
func (s *OrderNotificationService) SetAlertService(alertService interface {
	SendAlert(ctx context.Context, tenantID uuid.UUID, alertType, severity, title, message, actionURL, actionLabel string) (*storage.TenantAlert, error)
}) {
	s.AlertService = alertService
}

// NotifyOrderStatusChange sends notifications when order status changes
func (s *OrderNotificationService) NotifyOrderStatusChange(ctx context.Context, order *storage.Order, oldStatus, newStatus string) error {
	if oldStatus == newStatus {
		return nil // No change
	}

	// Tenant notification settings (missing settings -> defaults).
	emailAllowed := true
	webhookAllowed := false
	var settings storage.TenantNotificationSettings
	if err := s.DB.WithContext(ctx).Where("tenant_id = ?", order.TenantID).First(&settings).Error; err == nil {
		emailAllowed = settings.EmailNotificationsEnabled && settings.EmailOrderStatusChanges
		webhookAllowed = settings.WebhookEnabled && eventAllowed(settings.WebhookEvents, "order.status_changed")
	}

	// Always allow dashboard alerts for now (could be configurable later)
	dashboardAllowed := true

	// Get payment to find portal URL
	var payment storage.Payment
	portalURL := ""
	if err := s.DB.Where("order_id = ?", order.ID).First(&payment).Error; err == nil && payment.PortalURL != "" {
		portalURL = payment.PortalURL
	}

	// Send email notification if enabled.
	if emailAllowed && s.EmailService != nil && order.CustomerEmail != "" {
		if err := s.EmailService.SendOrderStatusEmail(
			order.CustomerEmail,
			order.CustomerName,
			order.OrderNumber,
			newStatus,
			portalURL,
		); err != nil {
			s.Logger.Warn("failed to send order status email", zap.Error(err), zap.String("email", order.CustomerEmail))
			// Don't fail notification if email fails
		} else {
			s.Logger.Info("order status email sent", zap.String("email", order.CustomerEmail), zap.String("status", newStatus))
		}
	}

	// Send webhook notification if enabled.
	if webhookAllowed && s.WebhookService != nil {
		payload := map[string]interface{}{
			"event":        "order.status_changed",
			"order_id":     order.ID,
			"order_number": order.OrderNumber,
			"old_status":   oldStatus,
			"new_status":   newStatus,
			"customer_id":  order.CustomerID,
			"total_amount": order.TotalAmount,
			"currency":     order.Currency,
			"timestamp":    order.CreatedAt,
		}

		if err := s.WebhookService.SendWebhook(ctx, order.TenantID, "order.status_changed", payload); err != nil {
			s.Logger.Warn("failed to send order status webhook", zap.Error(err), zap.Stringer("tenant_id", order.TenantID))
			// Don't fail notification if webhook fails
		} else {
			s.Logger.Info("order status webhook sent", zap.Stringer("tenant_id", order.TenantID), zap.String("status", newStatus))
		}
	}

	// Send dashboard alert if enabled.
	if dashboardAllowed && s.AlertService != nil {
		title := fmt.Sprintf("Order %s updated: %s", order.OrderNumber, newStatus)
		message := fmt.Sprintf("Order for %s has been updated to %s. Total: %s%.2f", order.CustomerName, newStatus, order.Currency, order.TotalAmount)
		actionURL := fmt.Sprintf("/app/orders/%s", order.ID)

		if _, err := s.AlertService.SendAlert(ctx, order.TenantID, "system", "info", title, message, actionURL, "View Order"); err != nil {
			s.Logger.Warn("failed to send order status alert", zap.Error(err), zap.Stringer("tenant_id", order.TenantID))
		}
	}

	return nil
}

func eventAllowed(eventsJSON string, event string) bool {
	if eventsJSON == "" {
		return true
	}
	var events []string
	if err := json.Unmarshal([]byte(eventsJSON), &events); err != nil {
		return true
	}
	if len(events) == 0 {
		return true
	}
	for _, e := range events {
		if e == event {
			return true
		}
	}
	return false
}
