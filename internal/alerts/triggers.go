package alerts

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// TenantAlertTriggers provides methods to trigger automated alerts for various user actions
type TenantAlertTriggers struct {
	service *AlertService
	db      *gorm.DB
	logger  *zap.Logger
}

// NewTenantAlertTriggers creates a new alert triggers instance
func NewTenantAlertTriggers(service *AlertService, db *gorm.DB, logger *zap.Logger) *TenantAlertTriggers {
	return &TenantAlertTriggers{
		service: service,
		db:      db,
		logger:  logger,
	}
}

// getTemplate fetches an alert template by key from the database
// Returns nil if not found or not active
func (t *TenantAlertTriggers) getTemplate(key string) *storage.AlertTemplate {
	if t.db == nil {
		return nil
	}
	var template storage.AlertTemplate
	if err := t.db.Where("key = ? AND is_active = ?", key, true).First(&template).Error; err != nil {
		return nil
	}
	return &template
}

// replacePlaceholders replaces template placeholders with actual values
func replacePlaceholders(text string, replacements map[string]string) string {
	result := text
	for key, value := range replacements {
		result = strings.ReplaceAll(result, "{{."+key+"}}", value)
	}
	return result
}

// alertDefaults holds fallback values for an alert
type alertDefaults struct {
	Type        string
	Severity    string
	Title       string
	Message     string
	ActionURL   string
	ActionLabel string
}

// sendTemplatedAlert sends an alert using a template from DB with fallback to defaults
func (t *TenantAlertTriggers) sendTemplatedAlert(
	ctx context.Context,
	tenantID uuid.UUID,
	templateKey string,
	defaults alertDefaults,
	replacements map[string]string,
) {
	template := t.getTemplate(templateKey)

	alertType := defaults.Type
	severity := defaults.Severity
	title := defaults.Title
	message := defaults.Message
	actionURL := defaults.ActionURL
	actionLabel := defaults.ActionLabel

	if template != nil {
		alertType = template.Type
		severity = template.Severity
		title = replacePlaceholders(template.Title, replacements)
		message = replacePlaceholders(template.Message, replacements)
		if template.ActionURL != "" {
			actionURL = template.ActionURL
		}
		if template.ActionLabel != "" {
			actionLabel = template.ActionLabel
		}
	} else {
		// Apply replacements to defaults too
		title = replacePlaceholders(title, replacements)
		message = replacePlaceholders(message, replacements)
	}

	_, err := t.service.SendAlert(ctx, tenantID, alertType, severity, title, message, actionURL, actionLabel)
	if err != nil && t.logger != nil {
		t.logger.Warn("failed to send alert",
			zap.String("template_key", templateKey),
			zap.Error(err),
			zap.Stringer("tenant_id", tenantID))
	}
}

// ========================================
// ACCOUNT & ONBOARDING ALERTS
// ========================================

// OnNewSignup triggers an alert when a new user signs up
func (t *TenantAlertTriggers) OnNewSignup(ctx context.Context, tenantID uuid.UUID, tenantName, email string) {
	if t.service == nil {
		return
	}
	go func() {
		t.sendTemplatedAlert(ctx, tenantID, "signup_welcome", alertDefaults{
			Type:        "system",
			Severity:    "info",
			Title:       "Welcome to Brancr! 🎉",
			Message:     "Hi {{.TenantName}}! Your account has been created successfully. Complete your onboarding to start using Brancr and grow your business.",
			ActionURL:   "/app/onboarding",
			ActionLabel: "Complete Onboarding",
		}, map[string]string{"TenantName": tenantName, "Email": email})
	}()
}

// OnOnboardingComplete triggers an alert when user completes onboarding
func (t *TenantAlertTriggers) OnOnboardingComplete(ctx context.Context, tenantID uuid.UUID, tenantName string) {
	if t.service == nil {
		return
	}
	go func() {
		t.sendTemplatedAlert(ctx, tenantID, "onboarding_complete", alertDefaults{
			Type:        "system",
			Severity:    "info",
			Title:       "Onboarding Complete! 🚀",
			Message:     "Congratulations {{.TenantName}}! You've completed your onboarding. Your Brancr account is now fully set up and ready to help you manage your social media presence.",
			ActionURL:   "/app/dashboard",
			ActionLabel: "Go to Dashboard",
		}, map[string]string{"TenantName": tenantName})
	}()
}

// OnTelegramConnected triggers an alert when user connects their Telegram bot
func (t *TenantAlertTriggers) OnTelegramConnected(ctx context.Context, tenantID uuid.UUID, tenantName, botUsername string) {
	if t.service == nil {
		return
	}
	go func() {
		t.sendTemplatedAlert(ctx, tenantID, "telegram_connected", alertDefaults{
			Type:        "feature",
			Severity:    "info",
			Title:       "Telegram Bot Connected! 🤖",
			Message:     "Your Telegram bot @{{.BotUsername}} is now connected to Brancr. You can now receive customer messages, post updates, and manage your business directly from Telegram.",
			ActionURL:   "/app/settings/integrations",
			ActionLabel: "View Integrations",
		}, map[string]string{"TenantName": tenantName, "BotUsername": botUsername})
	}()
}

// ========================================
// BILLING & TRIAL ALERTS
// ========================================

// OnTrialExpiringSoon triggers an alert when trial is expiring soon (e.g., 3 days left)
func (t *TenantAlertTriggers) OnTrialExpiringSoon(ctx context.Context, tenantID uuid.UUID, daysLeft int) {
	if t.service == nil {
		return
	}
	go func() {
		t.sendTemplatedAlert(ctx, tenantID, "trial_expiring", alertDefaults{
			Type:        "billing",
			Severity:    "warning",
			Title:       "Trial Ending in {{.DaysLeft}} Days ⏰",
			Message:     "Your free trial expires in {{.DaysLeft}} days. Upgrade now to keep all your features and continue growing your business with Brancr.",
			ActionURL:   "/app/billing/upgrade",
			ActionLabel: "Upgrade Now",
		}, map[string]string{"DaysLeft": fmt.Sprintf("%d", daysLeft)})
	}()
}

// OnTrialExpired triggers an alert when trial has expired
func (t *TenantAlertTriggers) OnTrialExpired(ctx context.Context, tenantID uuid.UUID) {
	if t.service == nil {
		return
	}
	go func() {
		t.sendTemplatedAlert(ctx, tenantID, "trial_expired", alertDefaults{
			Type:        "billing",
			Severity:    "critical",
			Title:       "Trial Expired ⚠️",
			Message:     "Your free trial has ended. Some features may be limited. Upgrade to a paid plan to restore full access and continue using Brancr.",
			ActionURL:   "/app/billing/upgrade",
			ActionLabel: "Choose a Plan",
		}, nil)
	}()
}

// OnPlanUpgraded triggers an alert when user upgrades their plan
func (t *TenantAlertTriggers) OnPlanUpgraded(ctx context.Context, tenantID uuid.UUID, newPlan string) {
	if t.service == nil {
		return
	}
	go func() {
		t.sendTemplatedAlert(ctx, tenantID, "plan_upgraded", alertDefaults{
			Type:        "billing",
			Severity:    "info",
			Title:       "Upgraded to {{.PlanName}}! 🎉",
			Message:     "Your account has been upgraded to the {{.PlanName}} plan. You now have access to all premium features. Thank you for choosing Brancr!",
			ActionURL:   "/app/billing",
			ActionLabel: "View Plan Details",
		}, map[string]string{"PlanName": newPlan})
	}()
}

// OnPaymentFailed triggers an alert when a payment fails
func (t *TenantAlertTriggers) OnPaymentFailed(ctx context.Context, tenantID uuid.UUID, reason string) {
	if t.service == nil {
		return
	}
	go func() {
		t.sendTemplatedAlert(ctx, tenantID, "payment_failed", alertDefaults{
			Type:        "billing",
			Severity:    "critical",
			Title:       "Payment Failed ⚠️",
			Message:     "We couldn't process your payment: {{.Reason}}. Please update your payment method to avoid service interruption.",
			ActionURL:   "/app/billing/payment",
			ActionLabel: "Update Payment",
		}, map[string]string{"Reason": reason})
	}()
}

// ========================================
// USAGE ALERTS
// ========================================

// OnQuotaExceeded triggers an alert when user exceeds their quota
func (t *TenantAlertTriggers) OnQuotaExceeded(ctx context.Context, tenantID uuid.UUID, quotaType string, used, limit int) {
	if t.service == nil {
		return
	}
	go func() {
		t.sendTemplatedAlert(ctx, tenantID, "quota_exceeded", alertDefaults{
			Type:        "quota",
			Severity:    "warning",
			Title:       "{{.QuotaType}} Quota Exceeded ⚠️",
			Message:     "You've used {{.Used}} out of {{.Limit}} {{.QuotaType}} this month. Upgrade your plan to increase your limits.",
			ActionURL:   "/app/billing/upgrade",
			ActionLabel: "Upgrade Plan",
		}, map[string]string{
			"QuotaType": quotaType,
			"Used":      fmt.Sprintf("%d", used),
			"Limit":     fmt.Sprintf("%d", limit),
		})
	}()
}

// OnQuotaWarning triggers an alert when user is approaching quota (e.g., 80%)
func (t *TenantAlertTriggers) OnQuotaWarning(ctx context.Context, tenantID uuid.UUID, quotaType string, percentUsed int) {
	if t.service == nil {
		return
	}
	go func() {
		t.sendTemplatedAlert(ctx, tenantID, "quota_warning", alertDefaults{
			Type:        "quota",
			Severity:    "info",
			Title:       "{{.QuotaType}} Quota at {{.Percent}}% 📊",
			Message:     "You've used {{.Percent}}% of your monthly {{.QuotaType}} quota. Consider upgrading if you need more.",
			ActionURL:   "/app/usage",
			ActionLabel: "View Usage",
		}, map[string]string{
			"QuotaType": quotaType,
			"Percent":   fmt.Sprintf("%d", percentUsed),
		})
	}()
}

// OnFirstPostScheduled triggers an alert when user schedules their first post
func (t *TenantAlertTriggers) OnFirstPostScheduled(ctx context.Context, tenantID uuid.UUID) {
	if t.service == nil {
		return
	}
	go func() {
		t.sendTemplatedAlert(ctx, tenantID, "first_post_scheduled", alertDefaults{
			Type:        "feature",
			Severity:    "info",
			Title:       "First Post Scheduled! 📅",
			Message:     "Congratulations! You've scheduled your first post. Brancr will automatically post it at the scheduled time. You can view and manage all your scheduled posts from the dashboard.",
			ActionURL:   "/app/posts/scheduled",
			ActionLabel: "View Scheduled Posts",
		}, nil)
	}()
}

// ========================================
// SECURITY ALERTS
// ========================================

// OnPasswordChanged triggers an alert when user changes their password
func (t *TenantAlertTriggers) OnPasswordChanged(ctx context.Context, tenantID uuid.UUID) {
	if t.service == nil {
		return
	}
	go func() {
		t.sendTemplatedAlert(ctx, tenantID, "password_changed", alertDefaults{
			Type:        "security",
			Severity:    "info",
			Title:       "Password Changed 🔐",
			Message:     "Your password has been successfully changed. If you didn't make this change, please contact support immediately.",
			ActionURL:   "/app/settings/security",
			ActionLabel: "Security Settings",
		}, nil)
	}()
}

// OnSuspiciousLogin triggers an alert when a suspicious login is detected
func (t *TenantAlertTriggers) OnSuspiciousLogin(ctx context.Context, tenantID uuid.UUID, ipAddress, location string) {
	if t.service == nil {
		return
	}
	go func() {
		t.sendTemplatedAlert(ctx, tenantID, "suspicious_login", alertDefaults{
			Type:        "security",
			Severity:    "critical",
			Title:       "Suspicious Login Detected ⚠️",
			Message:     "We detected a login from a new location: {{.Location}} (IP: {{.IPAddress}}). If this wasn't you, please change your password immediately.",
			ActionURL:   "/app/settings/security",
			ActionLabel: "Secure Account",
		}, map[string]string{
			"Location":  location,
			"IPAddress": ipAddress,
		})
	}()
}

// ========================================
// SCHEDULED CHECKER (for trial expiry, etc.)
// ========================================

// CheckTrialExpirations checks for expiring trials and sends alerts
// Should be called periodically (e.g., daily cron job)
func (t *TenantAlertTriggers) CheckTrialExpirations(ctx context.Context) {
	if t.db == nil || t.service == nil {
		return
	}

	now := time.Now()

	// Check for trials expiring in 3 days
	threeDaysFromNow := now.AddDate(0, 0, 3)
	var expiringIn3Days []storage.Tenant
	t.db.WithContext(ctx).
		Where("plan_type = ?", "trial").
		Where("trial_ends_at BETWEEN ? AND ?", now, threeDaysFromNow).
		Find(&expiringIn3Days)

	for _, tenant := range expiringIn3Days {
		daysLeft := int(time.Until(*tenant.TrialEndsAt).Hours() / 24)
		if daysLeft <= 0 {
			daysLeft = 1
		}
		t.OnTrialExpiringSoon(ctx, tenant.ID, daysLeft)
	}

	// Check for expired trials (just expired today)
	yesterday := now.AddDate(0, 0, -1)
	var expiredToday []storage.Tenant
	t.db.WithContext(ctx).
		Where("plan_type = ?", "trial").
		Where("trial_ends_at BETWEEN ? AND ?", yesterday, now).
		Find(&expiredToday)

	for _, tenant := range expiredToday {
		t.OnTrialExpired(ctx, tenant.ID)
	}

	if t.logger != nil {
		t.logger.Info("trial expiration check complete",
			zap.Int("expiring_soon", len(expiringIn3Days)),
			zap.Int("expired_today", len(expiredToday)))
	}
}
