package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Default alert templates to seed on startup
var DefaultAlertTemplates = []storage.AlertTemplate{
	// Account/Onboarding
	{
		Key:         "signup_welcome",
		Name:        "Welcome Signup",
		Type:        "system",
		Severity:    "info",
		Title:       "Welcome to Brancr! 🎉",
		Message:     "Hi {{.TenantName}}! Your account has been created successfully. Complete your onboarding to start using Brancr and grow your business.",
		ActionURL:   "/app/onboarding",
		ActionLabel: "Complete Onboarding",
		IsActive:    true,
		IsEditable:  true,
		Description: "Sent immediately after user creates an account",
	},
	{
		Key:         "onboarding_complete",
		Name:        "Onboarding Complete",
		Type:        "system",
		Severity:    "info",
		Title:       "Onboarding Complete! 🚀",
		Message:     "Congratulations {{.TenantName}}! You've completed your onboarding. Your Brancr account is now fully set up and ready to help you manage your social media presence.",
		ActionURL:   "/app/dashboard",
		ActionLabel: "Go to Dashboard",
		IsActive:    true,
		IsEditable:  true,
		Description: "Sent when user completes onboarding flow",
	},
	{
		Key:         "telegram_connected",
		Name:        "Telegram Bot Connected",
		Type:        "feature",
		Severity:    "info",
		Title:       "Telegram Bot Connected! 🤖",
		Message:     "Your Telegram bot @{{.BotUsername}} is now connected to Brancr. You can now receive customer messages, post updates, and manage your business directly from Telegram.",
		ActionURL:   "/app/settings/integrations",
		ActionLabel: "View Integrations",
		IsActive:    true,
		IsEditable:  true,
		Description: "Sent when user connects their Telegram bot",
	},
	// Billing
	{
		Key:         "trial_expiring",
		Name:        "Trial Expiring Soon",
		Type:        "billing",
		Severity:    "warning",
		Title:       "Trial Ending in {{.DaysLeft}} Days ⏰",
		Message:     "Your free trial expires in {{.DaysLeft}} days. Upgrade now to keep all your features and continue growing your business with Brancr.",
		ActionURL:   "/app/billing/upgrade",
		ActionLabel: "Upgrade Now",
		IsActive:    true,
		IsEditable:  true,
		Description: "Sent 3 days before trial expiration",
	},
	{
		Key:         "trial_expired",
		Name:        "Trial Expired",
		Type:        "billing",
		Severity:    "critical",
		Title:       "Trial Expired ⚠️",
		Message:     "Your free trial has ended. Some features may be limited. Upgrade to a paid plan to restore full access and continue using Brancr.",
		ActionURL:   "/app/billing/upgrade",
		ActionLabel: "Choose a Plan",
		IsActive:    true,
		IsEditable:  true,
		Description: "Sent when trial period ends",
	},
	{
		Key:         "plan_upgraded",
		Name:        "Plan Upgraded",
		Type:        "billing",
		Severity:    "info",
		Title:       "Upgraded to {{.PlanName}}! 🎉",
		Message:     "Your account has been upgraded to the {{.PlanName}} plan. You now have access to all premium features. Thank you for choosing Brancr!",
		ActionURL:   "/app/billing",
		ActionLabel: "View Plan Details",
		IsActive:    true,
		IsEditable:  true,
		Description: "Sent when user upgrades their subscription",
	},
	{
		Key:         "payment_failed",
		Name:        "Payment Failed",
		Type:        "billing",
		Severity:    "critical",
		Title:       "Payment Failed ⚠️",
		Message:     "We couldn't process your payment: {{.Reason}}. Please update your payment method to avoid service interruption.",
		ActionURL:   "/app/billing/payment",
		ActionLabel: "Update Payment",
		IsActive:    true,
		IsEditable:  true,
		Description: "Sent when payment processing fails",
	},
	// Quota
	{
		Key:         "quota_exceeded",
		Name:        "Quota Exceeded",
		Type:        "quota",
		Severity:    "warning",
		Title:       "{{.QuotaType}} Quota Exceeded ⚠️",
		Message:     "You've used {{.Used}} out of {{.Limit}} {{.QuotaType}} this month. Upgrade your plan to increase your limits.",
		ActionURL:   "/app/billing/upgrade",
		ActionLabel: "Upgrade Plan",
		IsActive:    true,
		IsEditable:  true,
		Description: "Sent when user exceeds their monthly quota",
	},
	{
		Key:         "quota_warning",
		Name:        "Quota Warning",
		Type:        "quota",
		Severity:    "info",
		Title:       "{{.QuotaType}} Quota at {{.PercentUsed}}% 📊",
		Message:     "You've used {{.PercentUsed}}% of your monthly {{.QuotaType}} quota. Consider upgrading if you need more.",
		ActionURL:   "/app/usage",
		ActionLabel: "View Usage",
		IsActive:    true,
		IsEditable:  true,
		Description: "Sent when user reaches 80% of their quota",
	},
	// Features
	{
		Key:         "first_post_scheduled",
		Name:        "First Post Scheduled",
		Type:        "feature",
		Severity:    "info",
		Title:       "First Post Scheduled! 📅",
		Message:     "Congratulations! You've scheduled your first post. Brancr will automatically post it at the scheduled time. You can view and manage all your scheduled posts from the dashboard.",
		ActionURL:   "/app/posts/scheduled",
		ActionLabel: "View Scheduled Posts",
		IsActive:    true,
		IsEditable:  true,
		Description: "Sent when user schedules their first post",
	},
	// Security
	{
		Key:         "password_changed",
		Name:        "Password Changed",
		Type:        "security",
		Severity:    "info",
		Title:       "Password Changed 🔐",
		Message:     "Your password has been successfully changed. If you didn't make this change, please contact support immediately.",
		ActionURL:   "/app/settings/security",
		ActionLabel: "Security Settings",
		IsActive:    true,
		IsEditable:  true,
		Description: "Sent after password is changed or reset",
	},
	{
		Key:         "suspicious_login",
		Name:        "Suspicious Login",
		Type:        "security",
		Severity:    "critical",
		Title:       "Suspicious Login Detected ⚠️",
		Message:     "We detected a login from a new location: {{.Location}} (IP: {{.IPAddress}}). If this wasn't you, please change your password immediately.",
		ActionURL:   "/app/settings/security",
		ActionLabel: "Secure Account",
		IsActive:    true,
		IsEditable:  true,
		Description: "Sent when login from new location is detected",
	},
}

// SeedAlertTemplates seeds the default alert templates into the database
func SeedAlertTemplates(db *gorm.DB, logger *zap.Logger) error {
	for _, template := range DefaultAlertTemplates {
		// Check if template already exists
		var existing storage.AlertTemplate
		if err := db.Where("key = ?", template.Key).First(&existing).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				// Create new template
				if err := db.Create(&template).Error; err != nil {
					if logger != nil {
						logger.Error("failed to seed alert template", zap.String("key", template.Key), zap.Error(err))
					}
					continue
				}
				if logger != nil {
					logger.Info("seeded alert template", zap.String("key", template.Key))
				}
			}
		}
		// If exists, don't overwrite (admin may have edited it)
	}
	return nil
}

// AlertTemplateResponse represents an alert template in API responses
type AlertTemplateResponse struct {
	ID          uuid.UUID `json:"id"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Severity    string    `json:"severity"`
	Title       string    `json:"title"`
	Message     string    `json:"message"`
	ActionURL   string    `json:"action_url"`
	ActionLabel string    `json:"action_label"`
	IsActive    bool      `json:"is_active"`
	IsEditable  bool      `json:"is_editable"`
	Description string    `json:"description"`
	UpdatedAt   string    `json:"updated_at"`
}

// UpdateTemplateRequest represents a request to update an alert template
type UpdateTemplateRequest struct {
	Title       string `json:"title"`
	Message     string `json:"message"`
	ActionURL   string `json:"action_url,omitempty"`
	ActionLabel string `json:"action_label,omitempty"`
	IsActive    bool   `json:"is_active"`
}

// ListAlertTemplatesHandler returns all alert templates for admin dashboard
func ListAlertTemplatesHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		var templates []storage.AlertTemplate
		query := ctx.DB.WithContext(reqCtx).Order("type ASC, key ASC")

		// Filter by type if specified
		typeFilter := strings.TrimSpace(r.URL.Query().Get("type"))
		if typeFilter != "" {
			query = query.Where("type = ?", typeFilter)
		}

		if err := query.Find(&templates).Error; err != nil {
			ctx.Logger.Error("failed to fetch alert templates", zap.Error(err))
			http.Error(w, "failed to fetch templates", http.StatusInternalServerError)
			return
		}

		// Convert to response format
		responses := make([]AlertTemplateResponse, 0, len(templates))
		for _, t := range templates {
			responses = append(responses, AlertTemplateResponse{
				ID:          t.ID,
				Key:         t.Key,
				Name:        t.Name,
				Type:        t.Type,
				Severity:    t.Severity,
				Title:       t.Title,
				Message:     t.Message,
				ActionURL:   t.ActionURL,
				ActionLabel: t.ActionLabel,
				IsActive:    t.IsActive,
				IsEditable:  t.IsEditable,
				Description: t.Description,
				UpdatedAt:   t.UpdatedAt.Format(time.RFC3339),
			})
		}

		respondJSON(w, responses)
	}
}

// GetAlertTemplateHandler returns a single alert template by ID or key
func GetAlertTemplateHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		// Get template ID or key from query params
		idStr := strings.TrimSpace(r.URL.Query().Get("id"))
		key := strings.TrimSpace(r.URL.Query().Get("key"))

		if idStr == "" && key == "" {
			http.Error(w, "id or key required", http.StatusBadRequest)
			return
		}

		var template storage.AlertTemplate
		var err error

		if idStr != "" {
			id, parseErr := uuid.Parse(idStr)
			if parseErr != nil {
				http.Error(w, "invalid id format", http.StatusBadRequest)
				return
			}
			err = ctx.DB.WithContext(reqCtx).First(&template, id).Error
		} else {
			err = ctx.DB.WithContext(reqCtx).Where("key = ?", key).First(&template).Error
		}

		if err != nil {
			if err == gorm.ErrRecordNotFound {
				http.Error(w, "template not found", http.StatusNotFound)
				return
			}
			ctx.Logger.Error("failed to fetch alert template", zap.Error(err))
			http.Error(w, "failed to fetch template", http.StatusInternalServerError)
			return
		}

		response := AlertTemplateResponse{
			ID:          template.ID,
			Key:         template.Key,
			Name:        template.Name,
			Type:        template.Type,
			Severity:    template.Severity,
			Title:       template.Title,
			Message:     template.Message,
			ActionURL:   template.ActionURL,
			ActionLabel: template.ActionLabel,
			IsActive:    template.IsActive,
			IsEditable:  template.IsEditable,
			Description: template.Description,
			UpdatedAt:   template.UpdatedAt.Format(time.RFC3339),
		}

		respondJSON(w, response)
	}
}

// UpdateAlertTemplateHandler updates an alert template
func UpdateAlertTemplateHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		// Get template ID from query params
		idStr := strings.TrimSpace(r.URL.Query().Get("id"))
		if idStr == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}

		id, err := uuid.Parse(idStr)
		if err != nil {
			http.Error(w, "invalid id format", http.StatusBadRequest)
			return
		}

		// Parse request body
		var req UpdateTemplateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Fetch existing template
		var template storage.AlertTemplate
		if err := ctx.DB.WithContext(reqCtx).First(&template, id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				http.Error(w, "template not found", http.StatusNotFound)
				return
			}
			ctx.Logger.Error("failed to fetch alert template", zap.Error(err))
			http.Error(w, "failed to fetch template", http.StatusInternalServerError)
			return
		}

		// Check if editable
		if !template.IsEditable {
			http.Error(w, "this template cannot be edited", http.StatusForbidden)
			return
		}

		// Update fields
		updates := map[string]interface{}{
			"title":        req.Title,
			"message":      req.Message,
			"action_url":   req.ActionURL,
			"action_label": req.ActionLabel,
			"is_active":    req.IsActive,
		}

		if err := ctx.DB.WithContext(reqCtx).Model(&template).Updates(updates).Error; err != nil {
			ctx.Logger.Error("failed to update alert template", zap.Error(err))
			http.Error(w, "failed to update template", http.StatusInternalServerError)
			return
		}

		// Audit log
		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "alert_template.update",
			Metadata: map[string]interface{}{
				"template_id":  id.String(),
				"template_key": template.Key,
			},
		}
		if err := auditWriter(reqCtx, ctx, entry); err != nil {
			ctx.Logger.Warn("failed to log audit entry", zap.Error(err))
		}

		respondJSON(w, map[string]interface{}{
			"status":  "ok",
			"message": "Template updated successfully",
		})
	}
}

// ResetAlertTemplateHandler resets a template to its default value
func ResetAlertTemplateHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		// Get template key from query params
		key := strings.TrimSpace(r.URL.Query().Get("key"))
		if key == "" {
			http.Error(w, "key required", http.StatusBadRequest)
			return
		}

		// Find default template
		var defaultTemplate *storage.AlertTemplate
		for _, t := range DefaultAlertTemplates {
			if t.Key == key {
				defaultTemplate = &t
				break
			}
		}

		if defaultTemplate == nil {
			http.Error(w, "no default template found for this key", http.StatusNotFound)
			return
		}

		// Update to default values
		updates := map[string]interface{}{
			"title":        defaultTemplate.Title,
			"message":      defaultTemplate.Message,
			"action_url":   defaultTemplate.ActionURL,
			"action_label": defaultTemplate.ActionLabel,
			"is_active":    defaultTemplate.IsActive,
		}

		if err := ctx.DB.WithContext(reqCtx).Model(&storage.AlertTemplate{}).Where("key = ?", key).Updates(updates).Error; err != nil {
			ctx.Logger.Error("failed to reset alert template", zap.Error(err))
			http.Error(w, "failed to reset template", http.StatusInternalServerError)
			return
		}

		// Audit log
		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "alert_template.reset",
			Metadata: map[string]interface{}{
				"template_key": key,
			},
		}
		if err := auditWriter(reqCtx, ctx, entry); err != nil {
			ctx.Logger.Warn("failed to log audit entry", zap.Error(err))
		}

		respondJSON(w, map[string]interface{}{
			"status":  "ok",
			"message": "Template reset to default",
		})
	}
}
