package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var (
	ErrPlanTemplateNotFound = errors.New("plan template not found")
	ErrTenantPlanNotFound   = errors.New("tenant plan not found")
	ErrPlanDeprecated       = errors.New("plan template is deprecated")
	ErrInvalidPlanStatus    = errors.New("invalid plan status")
)

// BillingService manages plan templates and tenant plan assignments
type BillingService struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

// NewBillingService creates a new billing service
func NewBillingService(db *gorm.DB, logger *zap.Logger) *BillingService {
	return &BillingService{
		DB:     db,
		Logger: logger,
	}
}

// CreatePlanTemplate creates a new plan template
func (s *BillingService) CreatePlanTemplate(ctx context.Context, template *storage.PlanTemplate) (*storage.PlanTemplate, error) {
	if err := s.DB.WithContext(ctx).Create(template).Error; err != nil {
		return nil, fmt.Errorf("failed to create plan template: %w", err)
	}
	s.Logger.Info("plan template created",
		zap.String("name", template.Name),
		zap.Stringer("id", template.ID))
	return template, nil
}

// GetPlanTemplate gets a plan template by ID or name
func (s *BillingService) GetPlanTemplate(ctx context.Context, idOrName interface{}) (*storage.PlanTemplate, error) {
	var template storage.PlanTemplate
	query := s.DB.WithContext(ctx)

	switch v := idOrName.(type) {
	case uuid.UUID:
		query = query.Where("id = ?", v)
	case string:
		query = query.Where("name = ?", v)
	default:
		return nil, fmt.Errorf("invalid idOrName type")
	}

	if err := query.First(&template).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPlanTemplateNotFound
		}
		return nil, fmt.Errorf("failed to get plan template: %w", err)
	}
	return &template, nil
}

// ListPlanTemplates lists all plan templates
func (s *BillingService) ListPlanTemplates(ctx context.Context, includeDeprecated bool) ([]storage.PlanTemplate, error) {
	var templates []storage.PlanTemplate
	query := s.DB.WithContext(ctx)

	if !includeDeprecated {
		query = query.Where("status = ?", "active")
	}

	if err := query.Order("created_at ASC").Find(&templates).Error; err != nil {
		return nil, fmt.Errorf("failed to list plan templates: %w", err)
	}
	return templates, nil
}

// UpdatePlanTemplate updates a plan template (only affects new assignments)
func (s *BillingService) UpdatePlanTemplate(ctx context.Context, id uuid.UUID, updates map[string]interface{}) error {
	// Verify template exists
	var template storage.PlanTemplate
	if err := s.DB.WithContext(ctx).First(&template, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrPlanTemplateNotFound
		}
		return fmt.Errorf("failed to get plan template: %w", err)
	}

	// Prevent updating name (would break references)
	if _, ok := updates["name"]; ok {
		return fmt.Errorf("cannot update plan template name")
	}

	if err := s.DB.WithContext(ctx).Model(&template).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update plan template: %w", err)
	}

	s.Logger.Info("plan template updated",
		zap.Stringer("id", id),
		zap.String("name", template.Name))
	return nil
}

// DeprecatePlanTemplate marks a plan template as deprecated
func (s *BillingService) DeprecatePlanTemplate(ctx context.Context, id uuid.UUID) error {
	return s.UpdatePlanTemplate(ctx, id, map[string]interface{}{
		"status": "deprecated",
	})
}

// AssignPlanToTenant assigns a plan template to a tenant (creates snapshot)
func (s *BillingService) AssignPlanToTenant(ctx context.Context, tenantID uuid.UUID, templateID uuid.UUID, overridePrice *float64) (*storage.TenantPlan, error) {
	// Get plan template
	template, err := s.GetPlanTemplate(ctx, templateID)
	if err != nil {
		return nil, err
	}

	if template.Status == "deprecated" {
		return nil, ErrPlanDeprecated
	}

	// Determine billing status
	billingStatus := "active"
	if template.Name == "Trial" {
		billingStatus = "trial"
	}

	// Calculate billing cycle
	now := time.Now()
	billingCycleStart := now
	var billingCycleEnd *time.Time
	var trialEndsAt *time.Time

	if billingStatus == "trial" {
		// Trial is typically 14-30 days
		trialDuration := 14 * 24 * time.Hour // Default 14 days, can be configured
		trialEnd := now.Add(trialDuration)
		trialEndsAt = &trialEnd
		billingCycleEnd = &trialEnd
	} else {
		// Monthly billing cycle
		cycleEnd := now.AddDate(0, 1, 0) // 1 month from now
		billingCycleEnd = &cycleEnd
	}

	// Determine price
	price := template.DefaultMonthlyPrice
	priceOverridden := false
	if overridePrice != nil {
		price = *overridePrice
		priceOverridden = true
	}

	// Check if tenant already has a plan
	var tenantPlan storage.TenantPlan
	err = s.DB.WithContext(ctx).Where("tenant_id = ?", tenantID).First(&tenantPlan).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Create new plan
		tenantPlan = storage.TenantPlan{
			TenantID:             tenantID,
			PlanTemplateID:       template.ID,
			PlanName:             template.Name,
			AIReplyQuota:         template.DefaultAIReplyQuota,
			WhatsAppMessageQuota: template.DefaultWhatsAppMessageQuota,
			PostQuota:            template.DefaultPostQuota,
			StorageLimitMB:       template.DefaultStorageLimitMB,
			APICallsLimit:        template.DefaultAPICallsLimit,
			PlatformsLimit:       template.DefaultPlatformsLimit,
			AINegotiationEnabled: template.AINegotiationEnabled,
			EscalationBehavior:   template.EscalationBehavior,
			OveragesAllowed:      template.OveragesAllowed,
			MonthlyPrice:         price,
			BillingCycleStart:    billingCycleStart,
			BillingCycleEnd:      billingCycleEnd,
			BillingStatus:        billingStatus,
			PaymentStatus:        "pending",
			NextBillingDate:      billingCycleEnd,
			TrialEndsAt:          trialEndsAt,
			QuotasOverridden:     false,
			PriceOverridden:      priceOverridden,
		}

		if err := s.DB.WithContext(ctx).Create(&tenantPlan).Error; err != nil {
			return nil, fmt.Errorf("failed to create tenant plan: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("failed to check existing plan: %w", err)
	} else {
		// Update existing plan
		// Update all fields to match the new plan template
		tenantPlan.PlanTemplateID = template.ID
		tenantPlan.PlanName = template.Name
		tenantPlan.AIReplyQuota = template.DefaultAIReplyQuota
		tenantPlan.WhatsAppMessageQuota = template.DefaultWhatsAppMessageQuota
		tenantPlan.PostQuota = template.DefaultPostQuota
		tenantPlan.StorageLimitMB = template.DefaultStorageLimitMB
		tenantPlan.APICallsLimit = template.DefaultAPICallsLimit
		tenantPlan.PlatformsLimit = template.DefaultPlatformsLimit
		tenantPlan.AINegotiationEnabled = template.AINegotiationEnabled
		tenantPlan.EscalationBehavior = template.EscalationBehavior
		tenantPlan.OveragesAllowed = template.OveragesAllowed
		tenantPlan.MonthlyPrice = price
		tenantPlan.BillingCycleStart = billingCycleStart
		tenantPlan.BillingCycleEnd = billingCycleEnd
		tenantPlan.BillingStatus = billingStatus
		tenantPlan.PaymentStatus = "pending" // Reset payment status for new plan
		tenantPlan.NextBillingDate = billingCycleEnd
		tenantPlan.TrialEndsAt = trialEndsAt
		tenantPlan.QuotasOverridden = false
		tenantPlan.PriceOverridden = priceOverridden

		if err := s.DB.WithContext(ctx).Save(&tenantPlan).Error; err != nil {
			return nil, fmt.Errorf("failed to update tenant plan: %w", err)
		}
	}

	// Sync to TenantQuota for backward compatibility
	if err := s.syncToTenantQuota(ctx, tenantID, &tenantPlan); err != nil {
		s.Logger.Warn("failed to sync tenant plan to quota",
			zap.Stringer("tenant_id", tenantID),
			zap.Error(err))
	}

	s.Logger.Info("plan assigned to tenant",
		zap.Stringer("tenant_id", tenantID),
		zap.String("plan_name", template.Name),
		zap.Stringer("plan_id", tenantPlan.ID))

	return &tenantPlan, nil
}

// GetTenantPlan gets the active plan for a tenant
func (s *BillingService) GetTenantPlan(ctx context.Context, tenantID uuid.UUID) (*storage.TenantPlan, error) {
	var plan storage.TenantPlan
	if err := s.DB.WithContext(ctx).
		Where("tenant_id = ? AND billing_status IN (?)", tenantID, []string{"trial", "active"}).
		Order("created_at DESC").
		First(&plan).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTenantPlanNotFound
		}
		return nil, fmt.Errorf("failed to get tenant plan: %w", err)
	}
	return &plan, nil
}

// UpdateTenantPlanQuotas allows admin to override quotas for a specific tenant
func (s *BillingService) UpdateTenantPlanQuotas(ctx context.Context, tenantID uuid.UUID, quotas map[string]interface{}) error {
	plan, err := s.GetTenantPlan(ctx, tenantID)
	if err != nil {
		return err
	}

	// Update quotas
	updates := make(map[string]interface{})
	if aiQuota, ok := quotas["ai_reply_quota"].(int); ok {
		updates["ai_reply_quota"] = aiQuota
	}
	if waQuota, ok := quotas["whatsapp_message_quota"].(int); ok {
		updates["whatsapp_message_quota"] = waQuota
	}
	if postQuota, ok := quotas["post_quota"].(int); ok {
		updates["post_quota"] = postQuota
	}
	if storageLimit, ok := quotas["storage_limit_mb"].(int); ok {
		updates["storage_limit_mb"] = storageLimit
	}
	if apiLimit, ok := quotas["api_calls_limit"].(int); ok {
		updates["api_calls_limit"] = apiLimit
	}
	if platformsLimit, ok := quotas["platforms_limit"].(int); ok {
		updates["platforms_limit"] = platformsLimit
	}

	if len(updates) > 0 {
		updates["quotas_overridden"] = true
		if err := s.DB.WithContext(ctx).Model(plan).Updates(updates).Error; err != nil {
			return fmt.Errorf("failed to update tenant plan quotas: %w", err)
		}

		// Sync to TenantQuota
		if err := s.syncToTenantQuota(ctx, tenantID, plan); err != nil {
			s.Logger.Warn("failed to sync updated quotas",
				zap.Stringer("tenant_id", tenantID),
				zap.Error(err))
		}
	}

	return nil
}

// UpdateTenantPlanPrice allows admin to override price for a specific tenant
func (s *BillingService) UpdateTenantPlanPrice(ctx context.Context, tenantID uuid.UUID, price float64) error {
	plan, err := s.GetTenantPlan(ctx, tenantID)
	if err != nil {
		return err
	}

	if err := s.DB.WithContext(ctx).Model(plan).Updates(map[string]interface{}{
		"monthly_price":    price,
		"price_overridden": true,
	}).Error; err != nil {
		return fmt.Errorf("failed to update tenant plan price: %w", err)
	}

	return nil
}

// PauseTenantPlan pauses a tenant's plan
func (s *BillingService) PauseTenantPlan(ctx context.Context, tenantID uuid.UUID) error {
	plan, err := s.GetTenantPlan(ctx, tenantID)
	if err != nil {
		return err
	}

	if err := s.DB.WithContext(ctx).Model(plan).Update("billing_status", "paused").Error; err != nil {
		return fmt.Errorf("failed to pause tenant plan: %w", err)
	}

	return nil
}

// ResumeTenantPlan resumes a tenant's plan
func (s *BillingService) ResumeTenantPlan(ctx context.Context, tenantID uuid.UUID) error {
	plan, err := s.GetTenantPlan(ctx, tenantID)
	if err != nil {
		return err
	}

	// Determine correct status based on plan name
	status := "active"
	if plan.PlanName == "Trial" {
		status = "trial"
	}

	if err := s.DB.WithContext(ctx).Model(plan).Update("billing_status", status).Error; err != nil {
		return fmt.Errorf("failed to resume tenant plan: %w", err)
	}

	return nil
}

// syncToTenantQuota syncs TenantPlan values to TenantQuota for backward compatibility
func (s *BillingService) syncToTenantQuota(ctx context.Context, tenantID uuid.UUID, plan *storage.TenantPlan) error {
	var quota storage.TenantQuota
	err := s.DB.WithContext(ctx).Where("tenant_id = ?", tenantID).First(&quota).Error

	updates := map[string]interface{}{
		"ai_calls_limit":   plan.AIReplyQuota,
		"posts_limit":      plan.PostQuota,
		"storage_limit_mb": plan.StorageLimitMB,
		"api_calls_limit":  plan.APICallsLimit,
		"platforms_limit":  plan.PlatformsLimit,
		"plan_type":        plan.PlanName,
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Create new quota
		quota = storage.TenantQuota{
			TenantID:       tenantID,
			AICallsLimit:   plan.AIReplyQuota,
			PostsLimit:     plan.PostQuota,
			StorageLimitMB: plan.StorageLimitMB,
			APICallsLimit:  plan.APICallsLimit,
			PlatformsLimit: plan.PlatformsLimit,
			PlanType:       plan.PlanName,
			ResetFrequency: "daily",
			LastResetAt:    time.Now(),
		}
		return s.DB.WithContext(ctx).Create(&quota).Error
	} else if err != nil {
		return err
	}

	// Update existing quota
	return s.DB.WithContext(ctx).Model(&quota).Updates(updates).Error
}
