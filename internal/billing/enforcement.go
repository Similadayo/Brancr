package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var (
	ErrQuotaExceeded = errors.New("quota exceeded")
	ErrPlanPaused    = errors.New("tenant plan is paused")
	ErrTrialExpired  = errors.New("trial period has expired")
)

// EnforcementService handles quota limit enforcement
type EnforcementService struct {
	DB             *gorm.DB
	Logger         *zap.Logger
	BillingService *BillingService
	UsageService   *UsageService
}

// NewEnforcementService creates a new enforcement service
func NewEnforcementService(db *gorm.DB, logger *zap.Logger, billingService *BillingService, usageService *UsageService) *EnforcementService {
	return &EnforcementService{
		DB:             db,
		Logger:         logger,
		BillingService: billingService,
		UsageService:   usageService,
	}
}

// CheckAIReplyQuota checks if tenant can send an AI reply
func (s *EnforcementService) CheckAIReplyQuota(ctx context.Context, tenantID uuid.UUID) error {
	plan, err := s.BillingService.GetTenantPlan(ctx, tenantID)
	if err != nil {
		if errors.Is(err, ErrTenantPlanNotFound) {
			// No plan assigned - deny access
			return fmt.Errorf("no plan assigned to tenant")
		}
		return err
	}

	// Check if plan is paused
	if plan.BillingStatus == "paused" {
		return ErrPlanPaused
	}

	// Check if trial expired
	if plan.BillingStatus == "trial" && plan.TrialEndsAt != nil {
		now := time.Now()
		if now.After(*plan.TrialEndsAt) {
			return ErrTrialExpired
		}
	}

	// Get current usage
	usage, err := s.UsageService.GetCurrentDayUsage(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("failed to get usage: %w", err)
	}

	// Check quota (0 = unlimited)
	if plan.AIReplyQuota > 0 {
		if usage.AIReplyCount >= plan.AIReplyQuota {
			// Check if overages allowed
			if !plan.OveragesAllowed {
				return fmt.Errorf("%w: AI reply quota (%d) exceeded", ErrQuotaExceeded, plan.AIReplyQuota)
			}
			// Overages allowed but require manual approval - for now, deny
			// TODO: Implement overage approval workflow
			return fmt.Errorf("%w: AI reply quota (%d) exceeded (overages require approval)", ErrQuotaExceeded, plan.AIReplyQuota)
		}

		// Check thresholds for warnings
		usagePercent := float64(usage.AIReplyCount) / float64(plan.AIReplyQuota) * 100
		if usagePercent >= 90 {
			s.Logger.Warn("tenant approaching AI reply quota limit",
				zap.Stringer("tenant_id", tenantID),
				zap.Float64("usage_percent", usagePercent),
				zap.Int("used", usage.AIReplyCount),
				zap.Int("limit", plan.AIReplyQuota))
		} else if usagePercent >= 70 {
			s.Logger.Info("tenant at 70% of AI reply quota",
				zap.Stringer("tenant_id", tenantID),
				zap.Float64("usage_percent", usagePercent))
		}
	}

	return nil
}

// CheckWhatsAppMessageQuota checks if tenant can send a WhatsApp message
func (s *EnforcementService) CheckWhatsAppMessageQuota(ctx context.Context, tenantID uuid.UUID) error {
	plan, err := s.BillingService.GetTenantPlan(ctx, tenantID)
	if err != nil {
		if errors.Is(err, ErrTenantPlanNotFound) {
			return fmt.Errorf("no plan assigned to tenant")
		}
		return err
	}

	if plan.BillingStatus == "paused" {
		return ErrPlanPaused
	}

	usage, err := s.UsageService.GetCurrentDayUsage(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("failed to get usage: %w", err)
	}

	if plan.WhatsAppMessageQuota > 0 {
		if usage.WhatsAppMessageCount >= plan.WhatsAppMessageQuota {
			if !plan.OveragesAllowed {
				return fmt.Errorf("%w: WhatsApp message quota (%d) exceeded", ErrQuotaExceeded, plan.WhatsAppMessageQuota)
			}
			return fmt.Errorf("%w: WhatsApp message quota (%d) exceeded (overages require approval)", ErrQuotaExceeded, plan.WhatsAppMessageQuota)
		}
	}

	return nil
}

// CheckPostQuota checks if tenant can publish a post
func (s *EnforcementService) CheckPostQuota(ctx context.Context, tenantID uuid.UUID) error {
	plan, err := s.BillingService.GetTenantPlan(ctx, tenantID)
	if err != nil {
		if errors.Is(err, ErrTenantPlanNotFound) {
			return fmt.Errorf("no plan assigned to tenant")
		}
		return err
	}

	if plan.BillingStatus == "paused" {
		return ErrPlanPaused
	}

	usage, err := s.UsageService.GetCurrentDayUsage(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("failed to get usage: %w", err)
	}

	if plan.PostQuota > 0 {
		if usage.PostCount >= plan.PostQuota {
			if !plan.OveragesAllowed {
				return fmt.Errorf("%w: post quota (%d) exceeded", ErrQuotaExceeded, plan.PostQuota)
			}
			return fmt.Errorf("%w: post quota (%d) exceeded (overages require approval)", ErrQuotaExceeded, plan.PostQuota)
		}
	}

	return nil
}

// GetUsageStatus returns usage status with thresholds for a tenant
func (s *EnforcementService) GetUsageStatus(ctx context.Context, tenantID uuid.UUID) (*UsageStatus, error) {
	plan, err := s.BillingService.GetTenantPlan(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	usage, err := s.UsageService.GetCurrentDayUsage(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	status := &UsageStatus{
		TenantID: tenantID,
		PlanName: plan.PlanName,
	}

	// Calculate AI reply status
	if plan.AIReplyQuota > 0 {
		status.AIReply = QuotaStatus{
			Used:    usage.AIReplyCount,
			Limit:   plan.AIReplyQuota,
			Percent: float64(usage.AIReplyCount) / float64(plan.AIReplyQuota) * 100,
		}
		if status.AIReply.Percent >= 100 {
			status.AIReply.Status = "exceeded"
		} else if status.AIReply.Percent >= 90 {
			status.AIReply.Status = "critical"
		} else if status.AIReply.Percent >= 70 {
			status.AIReply.Status = "warning"
		} else {
			status.AIReply.Status = "ok"
		}
	} else {
		status.AIReply = QuotaStatus{
			Used:    usage.AIReplyCount,
			Limit:   0, // unlimited
			Percent: 0,
			Status:  "ok",
		}
	}

	// Calculate WhatsApp message status
	if plan.WhatsAppMessageQuota > 0 {
		status.WhatsAppMessage = QuotaStatus{
			Used:    usage.WhatsAppMessageCount,
			Limit:   plan.WhatsAppMessageQuota,
			Percent: float64(usage.WhatsAppMessageCount) / float64(plan.WhatsAppMessageQuota) * 100,
		}
		if status.WhatsAppMessage.Percent >= 100 {
			status.WhatsAppMessage.Status = "exceeded"
		} else if status.WhatsAppMessage.Percent >= 90 {
			status.WhatsAppMessage.Status = "critical"
		} else if status.WhatsAppMessage.Percent >= 70 {
			status.WhatsAppMessage.Status = "warning"
		} else {
			status.WhatsAppMessage.Status = "ok"
		}
	} else {
		status.WhatsAppMessage = QuotaStatus{
			Used:    usage.WhatsAppMessageCount,
			Limit:   0,
			Percent: 0,
			Status:  "ok",
		}
	}

	// Calculate post status
	if plan.PostQuota > 0 {
		status.Post = QuotaStatus{
			Used:    usage.PostCount,
			Limit:   plan.PostQuota,
			Percent: float64(usage.PostCount) / float64(plan.PostQuota) * 100,
		}
		if status.Post.Percent >= 100 {
			status.Post.Status = "exceeded"
		} else if status.Post.Percent >= 90 {
			status.Post.Status = "critical"
		} else if status.Post.Percent >= 70 {
			status.Post.Status = "warning"
		} else {
			status.Post.Status = "ok"
		}
	} else {
		status.Post = QuotaStatus{
			Used:    usage.PostCount,
			Limit:   0,
			Percent: 0,
			Status:  "ok",
		}
	}

	status.TotalCostUSD = usage.TotalCostUSD
	status.BillingStatus = plan.BillingStatus

	return status, nil
}

// UsageStatus represents the usage status for a tenant
type UsageStatus struct {
	TenantID        uuid.UUID
	PlanName        string
	BillingStatus   string
	AIReply         QuotaStatus
	WhatsAppMessage QuotaStatus
	Post            QuotaStatus
	TotalCostUSD    float64
}

// QuotaStatus represents the status of a specific quota
type QuotaStatus struct {
	Used    int
	Limit   int
	Percent float64
	Status  string // ok, warning, critical, exceeded
}
