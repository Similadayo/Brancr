package alerts

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/bot"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// AlertService handles sending alerts to tenants
type AlertService struct {
	DB           *gorm.DB
	Logger       *zap.Logger
	EmailService interface {
		SendEmail(to, subject, body string, attachments ...string) error
	}
	BotManager *bot.TenantBotManager
}

// NewAlertService creates a new alert service
func NewAlertService(
	db *gorm.DB,
	logger *zap.Logger,
	emailService interface {
		SendEmail(to, subject, body string, attachments ...string) error
	},
	botManager *bot.TenantBotManager,
) *AlertService {
	return &AlertService{
		DB:           db,
		Logger:       logger,
		EmailService: emailService,
		BotManager:   botManager,
	}
}

// SendAlert sends an alert to a tenant via configured channels
func (s *AlertService) SendAlert(
	ctx context.Context,
	tenantID uuid.UUID,
	alertType, severity, title, message string,
	actionURL, actionLabel string,
) (*storage.TenantAlert, error) {
	// Create alert record
	alert := &storage.TenantAlert{
		TenantID:       tenantID,
		Type:           alertType,
		Severity:       severity,
		Title:          title,
		Message:        message,
		ActionURL:      actionURL,
		ActionLabel:    actionLabel,
		SentInApp:      true, // Always create in-app notification
		InAppCreatedAt: timePtr(time.Now()),
	}

	if err := s.DB.WithContext(ctx).Create(alert).Error; err != nil {
		return nil, fmt.Errorf("failed to create alert: %w", err)
	}

	// Get tenant
	var tenant storage.Tenant
	if err := s.DB.WithContext(ctx).First(&tenant, tenantID).Error; err != nil {
		return alert, fmt.Errorf("tenant not found: %w", err)
	}

	// Send via email if tenant has email
	if tenant.Email != "" && s.EmailService != nil {
		if err := s.sendEmailAlert(ctx, alert, &tenant); err != nil {
			s.Logger.Warn("failed to send alert email", zap.Error(err), zap.Stringer("tenant_id", tenantID))
		} else {
			alert.SentEmail = true
			alert.EmailSentAt = timePtr(time.Now())
			s.DB.Save(alert)
		}
	}

	// NOTE: Telegram alerts are disabled for automated alerts
	// Alerts should only go to dashboard (in-app) and email
	// Telegram is reserved for user-initiated escalations

	return alert, nil
}

// sendEmailAlert sends alert via email
func (s *AlertService) sendEmailAlert(ctx context.Context, alert *storage.TenantAlert, tenant *storage.Tenant) error {
	severityEmoji := map[string]string{
		"info":     "ℹ️",
		"warning":  "⚠️",
		"critical": "🚨",
	}
	emoji := severityEmoji[alert.Severity]
	if emoji == "" {
		emoji = "📢"
	}

	subject := fmt.Sprintf("%s %s: %s", emoji, alert.Type, alert.Title)

	body := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
	<meta charset="UTF-8">
	<style>
		body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
		.container { max-width: 600px; margin: 0 auto; padding: 20px; }
		.header { background: #2563eb; color: white; padding: 20px; text-align: center; border-radius: 8px 8px 0 0; }
		.content { background: #f9fafb; padding: 20px; border-radius: 0 0 8px 8px; }
		.button { display: inline-block; padding: 12px 24px; background: #2563eb; color: white; text-decoration: none; border-radius: 6px; margin: 10px 0; }
		.footer { text-align: center; margin-top: 20px; color: #666; font-size: 12px; }
	</style>
</head>
<body>
	<div class="container">
		<div class="header">
			<h1>%s Alert</h1>
		</div>
		<div class="content">
			<p>Hi %s,</p>
			<p>%s</p>
			%s
			<p>You can view this alert in your Brancr dashboard.</p>
			<p>Best regards,<br>Brancr Team</p>
		</div>
		<div class="footer">
			<p>This is an automated alert from Brancr.</p>
		</div>
	</div>
</body>
</html>
`, alert.Title, tenant.Name, alert.Message, s.actionButtonHTML(alert))

	return s.EmailService.SendEmail(tenant.Email, subject, body)
}

// sendTelegramAlert sends alert via Telegram
func (s *AlertService) sendTelegramAlert(ctx context.Context, alert *storage.TenantAlert, tenant *storage.Tenant) error {
	severityEmoji := map[string]string{
		"info":     "ℹ️",
		"warning":  "⚠️",
		"critical": "🚨",
	}
	emoji := severityEmoji[alert.Severity]
	if emoji == "" {
		emoji = "📢"
	}

	message := fmt.Sprintf(
		"%s *%s Alert*\n\n*%s*\n\n%s",
		emoji,
		alert.Type,
		alert.Title,
		alert.Message,
	)

	if alert.ActionURL != "" && alert.ActionLabel != "" {
		message += fmt.Sprintf("\n\n[%s](%s)", alert.ActionLabel, alert.ActionURL)
	}

	s.BotManager.NotifyOwnerWithMarkdown(tenant.ID, message)
	return nil
}

func (s *AlertService) actionButtonHTML(alert *storage.TenantAlert) string {
	if alert.ActionURL == "" || alert.ActionLabel == "" {
		return ""
	}
	return fmt.Sprintf(`<p><a href="%s" class="button">%s</a></p>`, alert.ActionURL, alert.ActionLabel)
}

func timePtr(t time.Time) *time.Time {
	return &t
}
